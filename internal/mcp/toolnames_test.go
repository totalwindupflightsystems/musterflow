package mcp

// DF-039: MCP tool names must be namespaced by the owning API.
//
// Before this fix the registry flattened every connected API's operations into
// one map keyed by operationId alone, so two APIs that both define
// `listWidgets` published that name twice in tools/list and left
// toolConfigs["listWidgets"] pointing at whichever API was converted last —
// "last connect wins", silently, with the call routed to the wrong base URL.
// These tests pin the three properties that make the collision impossible:
// no duplicate published names, per-API routing for the namespaced name, and
// the bare name surviving only while it is globally unique.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	musterMcp "github.com/wojons/muster/pkg/mcp"
	"github.com/wojons/muster/pkg/mcp/handlers"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// df039Echo is an httptest API that answers every request with
// {"api":"<id>"} and records the paths it served, so a test can prove which
// API a tools/call actually reached and how many requests it took.
type df039Echo struct {
	*httptest.Server
	apiID string

	mu        sync.Mutex
	pathsSeen []string
}

func newDF039Echo(t *testing.T, apiID string) *df039Echo {
	t.Helper()
	echo := &df039Echo{apiID: apiID}
	echo.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		echo.mu.Lock()
		echo.pathsSeen = append(echo.pathsSeen, r.URL.Path)
		echo.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"api":%q,"path":%q}`, echo.apiID, r.URL.Path)
	}))
	t.Cleanup(echo.Close)
	return echo
}

// hits returns how many requests this API has served so far.
func (e *df039Echo) hits() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.pathsSeen)
}

// df039WriteSpec writes a one-operation OAS3 spec and returns its path.
func df039WriteSpec(t *testing.T, dir, filename, opID, path string) string {
	t.Helper()
	spec := fmt.Sprintf(`{
  "openapi": "3.0.0",
  "info": { "title": "DF039 Test API", "version": "1.0.0" },
  "paths": {
    %q: {
      "get": {
        "operationId": %q,
        "summary": "Operation %s"
      }
    }
  }
}`, path, opID, opID)

	full := dir + "/" + filename
	if err := os.WriteFile(full, []byte(spec), 0644); err != nil {
		t.Fatalf("write spec %s: %v", full, err)
	}
	return full
}

// df039Connect registers one connection against the given spec and base URL.
func df039Connect(t *testing.T, reg *app.Registry, id, name, specPath, baseURL string) {
	t.Helper()
	conn := &app.APIConnection{
		ID:      id,
		Name:    name,
		SpecURL: specPath,
		BaseURL: baseURL,
	}
	if err := reg.Add(conn); err != nil {
		t.Fatalf("add connection %s: %v", id, err)
	}
}

// df039Registry builds a loaded app.Registry in a temp dir.
func df039Registry(t *testing.T) *app.Registry {
	t.Helper()
	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	return reg
}

// df039NoDuplicateNames fails the test if any tool name is published twice.
func df039NoDuplicateNames(t *testing.T, tools []handlers.Tool) {
	t.Helper()
	seen := make(map[string]int, len(tools))
	for _, tool := range tools {
		seen[tool.Name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("tool name %q published %d times (tools: %v)", name, count, toolNames(tools))
		}
	}
}

// TestRefresh_CollidingOperationIDs_NoDuplicateNames is acceptance item 1: with
// two connected APIs that share an operationId, tools/list serves each name at
// most once, both APIs keep their own namespaced tool, and the colliding bare
// name is not published at all.
func TestRefresh_CollidingOperationIDs_NoDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	specA := df039WriteSpec(t, dir, "a.json", "listWidgets", "/widgets")
	specB := df039WriteSpec(t, dir, "b.json", "listWidgets", "/widgets")
	echoA := newDF039Echo(t, "api-a")
	echoB := newDF039Echo(t, "api-b")

	reg := df039Registry(t)
	df039Connect(t, reg, "id-a", "api-a", specA, echoA.URL)
	df039Connect(t, reg, "id-b", "api-b", specB, echoB.URL)

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	tools := tr.ListTools()
	df039NoDuplicateNames(t, tools)

	// Exactly the two namespaced tools — the bare name is ambiguous, so it is
	// not published.
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools (one namespaced per API), got %v", toolNames(tools))
	}
	for _, want := range []string{"api-a__listWidgets", "api-b__listWidgets"} {
		if !hasTool(tools, want) {
			t.Errorf("expected namespaced tool %q, got %v", want, toolNames(tools))
		}
	}
	if hasTool(tools, "listWidgets") {
		t.Errorf("bare colliding name %q must not be published, got %v", "listWidgets", toolNames(tools))
	}

	// Every published name must satisfy muster's conventional MCP tool-name
	// pattern; the slug separator must not produce an illegal name.
	for _, tool := range tools {
		if !musterMcp.MCPToolNamePattern.MatchString(tool.Name) {
			t.Errorf("tool name %q violates MCPToolNamePattern %s", tool.Name, musterMcp.MCPToolNamePattern)
		}
	}
}

// TestExecute_CollidingOperationIDs_RoutesPerAPI is acceptance item 2: one
// tools/call of the colliding operation reaches API A's base URL for A's tool
// and API B's base URL for B's tool — in exactly one request each.
func TestExecute_CollidingOperationIDs_RoutesPerAPI(t *testing.T) {
	dir := t.TempDir()
	specA := df039WriteSpec(t, dir, "a.json", "listWidgets", "/widgets")
	specB := df039WriteSpec(t, dir, "b.json", "listWidgets", "/widgets")
	echoA := newDF039Echo(t, "api-a")
	echoB := newDF039Echo(t, "api-b")

	reg := df039Registry(t)
	df039Connect(t, reg, "id-a", "api-a", specA, echoA.URL)
	df039Connect(t, reg, "id-b", "api-b", specB, echoB.URL)

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// One call to A's namespaced tool must reach A, and only A.
	result, err := tr.Execute(context.Background(), "api-a__listWidgets", nil)
	if err != nil {
		t.Fatalf("Execute(api-a__listWidgets): %v", err)
	}
	if got := df039ResponseAPI(t, result); got != "api-a" {
		t.Errorf("api-a__listWidgets reached %q, want api-a", got)
	}
	if echoA.hits() != 1 || echoB.hits() != 0 {
		t.Fatalf("after one call to A's tool: api-a hits=%d api-b hits=%d, want 1/0", echoA.hits(), echoB.hits())
	}

	// One call to B's namespaced tool must reach B, and only B.
	result, err = tr.Execute(context.Background(), "api-b__listWidgets", nil)
	if err != nil {
		t.Fatalf("Execute(api-b__listWidgets): %v", err)
	}
	if got := df039ResponseAPI(t, result); got != "api-b" {
		t.Errorf("api-b__listWidgets reached %q, want api-b", got)
	}
	if echoA.hits() != 1 || echoB.hits() != 1 {
		t.Fatalf("after one call to B's tool: api-a hits=%d api-b hits=%d, want 1/1", echoA.hits(), echoB.hits())
	}
}

// TestExecute_BareNameRoutingWhenUnique is acceptance item 4's uniqueness
// fallback: operations that no other connected API defines keep their bare
// name, and that bare name routes to its own API.
func TestExecute_BareNameRoutingWhenUnique(t *testing.T) {
	dir := t.TempDir()
	specA := df039WriteSpec(t, dir, "a.json", "listWidgets", "/widgets")
	specB := df039WriteSpec(t, dir, "b.json", "listGadgets", "/gadgets")
	echoA := newDF039Echo(t, "api-a")
	echoB := newDF039Echo(t, "api-b")

	reg := df039Registry(t)
	df039Connect(t, reg, "id-a", "api-a", specA, echoA.URL)
	df039Connect(t, reg, "id-b", "api-b", specB, echoB.URL)

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	tools := tr.ListTools()
	df039NoDuplicateNames(t, tools)
	for _, want := range []string{
		"api-a__listWidgets", "api-b__listGadgets", // always namespaced
		"listWidgets", "listGadgets", // bare, globally unique
	} {
		if !hasTool(tools, want) {
			t.Errorf("expected tool %q, got %v", want, toolNames(tools))
		}
	}
	if len(tools) != 4 {
		t.Errorf("expected 4 tools, got %v", toolNames(tools))
	}

	for _, tc := range []struct {
		tool   string
		wantID string
	}{
		{"listWidgets", "api-a"},
		{"listGadgets", "api-b"},
		{"api-a__listWidgets", "api-a"},
		{"api-b__listGadgets", "api-b"},
	} {
		result, err := tr.Execute(context.Background(), tc.tool, nil)
		if err != nil {
			t.Fatalf("Execute(%s): %v", tc.tool, err)
		}
		if got := df039ResponseAPI(t, result); got != tc.wantID {
			t.Errorf("Execute(%s) reached %q, want %s", tc.tool, got, tc.wantID)
		}
	}
	if echoA.hits() != 2 || echoB.hits() != 2 {
		t.Errorf("expected 2 calls each (bare + namespaced), got api-a=%d api-b=%d", echoA.hits(), echoB.hits())
	}
}

// TestRefresh_DisconnectKeepsRouting is acceptance item 3: after the colliding
// API is disconnected the survivor's namespaced tool still exists, routes to
// its own base URL in one request, and its operation name becomes unique again
// so the bare alias is republished.
func TestRefresh_DisconnectKeepsRouting(t *testing.T) {
	dir := t.TempDir()
	specA := df039WriteSpec(t, dir, "a.json", "listWidgets", "/widgets")
	specB := df039WriteSpec(t, dir, "b.json", "listWidgets", "/widgets")
	echoA := newDF039Echo(t, "api-a")
	echoB := newDF039Echo(t, "api-b")

	reg := df039Registry(t)
	df039Connect(t, reg, "id-a", "api-a", specA, echoA.URL)
	df039Connect(t, reg, "id-b", "api-b", specB, echoB.URL)

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("initial Refresh: %v", err)
	}
	if hasTool(tr.ListTools(), "listWidgets") {
		t.Fatalf("premise failed: bare name published while two APIs collide: %v", toolNames(tr.ListTools()))
	}

	// Disconnect B and re-derive, exactly like the dashboard DELETE handler
	// (Server.refreshMCPTools) does.
	if err := reg.Remove("id-b"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := tr.Refresh(); err != nil {
		t.Fatalf("Refresh after remove: %v", err)
	}

	tools := tr.ListTools()
	df039NoDuplicateNames(t, tools)
	if !hasTool(tools, "api-a__listWidgets") {
		t.Fatalf("survivor's namespaced tool missing after disconnect: %v", toolNames(tools))
	}
	if !hasTool(tools, "listWidgets") {
		t.Errorf("bare name should be republished once unique again, got %v", toolNames(tools))
	}
	if hasTool(tools, "api-b__listWidgets") {
		t.Errorf("disconnected API's namespaced tool still listed: %v", toolNames(tools))
	}

	// One request, routed to A; B is gone and must not be touched.
	result, err := tr.Execute(context.Background(), "api-a__listWidgets", nil)
	if err != nil {
		t.Fatalf("Execute after disconnect: %v", err)
	}
	if got := df039ResponseAPI(t, result); got != "api-a" {
		t.Errorf("post-disconnect call reached %q, want api-a", got)
	}
	if echoA.hits() != 1 || echoB.hits() != 0 {
		t.Errorf("after one post-disconnect call: api-a hits=%d api-b hits=%d, want 1/0", echoA.hits(), echoB.hits())
	}

	// The removed API's configs are gone too: both of its names fail at
	// lookup, before any HTTP request.
	for _, gone := range []string{"api-b__listWidgets", "api-b__listAnything"} {
		if _, err := tr.Execute(context.Background(), gone, nil); err == nil {
			t.Errorf("Execute(%s) after disconnect: expected error, got nil", gone)
		}
	}
}

// TestRefresh_SameConnectionNames_GetDistinctSlugs covers the second way to
// collide: two connections whose display names are identical must still get
// separate namespaces, or the fix would reintroduce the bug it removes.
func TestRefresh_SameConnectionNames_GetDistinctSlugs(t *testing.T) {
	dir := t.TempDir()
	specA := df039WriteSpec(t, dir, "a.json", "listWidgets", "/widgets")
	specB := df039WriteSpec(t, dir, "b.json", "listWidgets", "/widgets")
	echoA := newDF039Echo(t, "api-a")
	echoB := newDF039Echo(t, "api-b")

	reg := df039Registry(t)
	// Same display name on both connections.
	df039Connect(t, reg, "id-a", "echo", specA, echoA.URL)
	df039Connect(t, reg, "id-b", "echo", specB, echoB.URL)

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	tools := tr.ListTools()
	df039NoDuplicateNames(t, tools)
	for _, want := range []string{"echo__listWidgets", "echo-2__listWidgets"} {
		if !hasTool(tools, want) {
			t.Errorf("expected namespaced tool %q for duplicate display names, got %v", want, toolNames(tools))
		}
	}

	// Slugs are assigned in registry order (store lists by name, then id), and
	// each must route to its own API.
	result, err := tr.Execute(context.Background(), "echo__listWidgets", nil)
	if err != nil {
		t.Fatalf("Execute(echo__listWidgets): %v", err)
	}
	first := df039ResponseAPI(t, result)
	result, err = tr.Execute(context.Background(), "echo-2__listWidgets", nil)
	if err != nil {
		t.Fatalf("Execute(echo-2__listWidgets): %v", err)
	}
	second := df039ResponseAPI(t, result)

	if first == second {
		t.Errorf("both duplicate-name tools reached %q; namespaces collided", first)
	}
	if echoA.hits() != 1 || echoB.hits() != 1 {
		t.Errorf("expected one call per API, got api-a=%d api-b=%d", echoA.hits(), echoB.hits())
	}
}

// TestRefresh_SlugSanitization covers connection names that are not legal MCP
// tool-name fragments: the slug must stay inside [A-Za-z0-9_-] and still
// identify the API in the description.
func TestRefresh_SlugSanitization(t *testing.T) {
	dir := t.TempDir()
	specA := df039WriteSpec(t, dir, "a.json", "listWidgets", "/widgets")
	echoA := newDF039Echo(t, "api-a")

	reg := df039Registry(t)
	df039Connect(t, reg, "id-a", "Echo API (v2).local", specA, echoA.URL)

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	tools := tr.ListTools()
	names := toolNames(tools)
	if !hasTool(tools, "Echo_API_v2_local__listWidgets") {
		t.Fatalf("expected sanitized slug in tool name, got %v", names)
	}
	for _, tool := range tools {
		if !musterMcp.MCPToolNamePattern.MatchString(tool.Name) {
			t.Errorf("tool name %q violates MCPToolNamePattern %s", tool.Name, musterMcp.MCPToolNamePattern)
		}
	}

	// The namespaced tool names its API so the slug is not opaque; the bare
	// alias keeps the original description and schema untouched.
	for _, tool := range tools {
		if tool.Name != "Echo_API_v2_local__listWidgets" {
			continue
		}
		if want := "Operation listWidgets (API: Echo API (v2).local)"; tool.Description != want {
			t.Errorf("namespaced description = %q, want %q", tool.Description, want)
		}
		if len(tool.InputSchema) == 0 {
			t.Error("namespaced tool lost its InputSchema")
		}
	}
}

// TestHTTPServer_ToolsList_NoDuplicateNames is acceptance item 1 at the wire
// level: the JSON-RPC tools/list a client actually reads contains no repeated
// tool name when two connected APIs share operationIds.
func TestHTTPServer_ToolsList_NoDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	specA := df039WriteSpec(t, dir, "a.json", "listWidgets", "/widgets")
	specB := df039WriteSpec(t, dir, "b.json", "listWidgets", "/widgets")
	echoA := newDF039Echo(t, "api-a")
	echoB := newDF039Echo(t, "api-b")

	reg := df039Registry(t)
	df039Connect(t, reg, "id-a", "api-a", specA, echoA.URL)
	df039Connect(t, reg, "id-b", "api-b", specB, echoB.URL)

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	handlerReg := handlers.NewRegistry()
	handlerReg.Register(handlers.NewListToolsHandler(tr))
	server := NewHTTPServer(handlerReg)

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp rpcResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("tools/list error: %s", resp.Error.Message)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var listResult handlers.ListToolsResult
	if err := json.Unmarshal(resultBytes, &listResult); err != nil {
		t.Fatalf("result is not ListToolsResult: %v", err)
	}

	df039NoDuplicateNames(t, listResult.Tools)
	if len(listResult.Tools) != 2 {
		t.Errorf("expected 2 tools over the wire, got %v", toolNames(listResult.Tools))
	}

	// The schema must survive the rename (ListTools preserves InputSchema).
	for _, tool := range listResult.Tools {
		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q has empty InputSchema", tool.Name)
		}
	}
}

// df039ResponseAPI extracts the "api" marker from an echo API response.
func df039ResponseAPI(t *testing.T, result interface{}) string {
	t.Helper()
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object result, got %T: %#v", result, result)
	}
	id, _ := m["api"].(string)
	return id
}
