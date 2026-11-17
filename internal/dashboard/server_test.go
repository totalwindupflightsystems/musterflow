package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/mcp"
	"github.com/wojons/muster/pkg/mcp/handlers"
)

// --- DF-029: MCP tool registry must follow DISCONNECT (and IMPORT) ---
//
// README contract: "connect a new API while the server is running and tools
// update without restart". DF-015 made that true for connect/refresh, but the
// DELETE branch of handleAPIByID removed the connection from the app registry
// and never re-derived the MCP tools, so tools/list kept serving the removed
// API's tools (and its execution configs) until the process restarted. The
// CLI's `musterflow disconnect` hits that same endpoint when a dashboard is
// running, so the whole user-facing path was affected.
//
// These tests drive the real HTTP surfaces: POST /api/apis to connect,
// POST /mcp (JSON-RPC tools/list) to observe the tool list, DELETE /api/apis/<id>
// to disconnect, and GET /api/apis to prove the registry agrees.

// df029RPCResponse is the JSON-RPC envelope returned by the dashboard /mcp
// endpoint.
type df029RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// df029MCPHandler builds the tools/list + tools/call JSON-RPC handler the
// running dashboard mounts at /mcp (see cmd/musterflow/main.go), backed by tr.
func df029MCPHandler(tr *mcp.ToolRegistry) http.Handler {
	handlerReg := handlers.NewRegistry()
	handlerReg.Register(handlers.NewListToolsHandler(tr))
	handlerReg.Register(handlers.NewCallToolHandler(tr))
	return mcp.NewHTTPServer(handlerReg)
}

// df029NewServer builds a dashboard Server wired to a real app.Registry and a
// real ToolRegistry, with the MCP handler mounted exactly like production.
func df029NewServer(t *testing.T) (*Server, *app.Registry, *mcp.ToolRegistry) {
	t.Helper()
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load registry: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	tr := mcp.NewToolRegistry(r)
	s := NewServer(r, nil, tr, ":0")
	s.SetMCPHandler(df029MCPHandler(tr))
	return s, r, tr
}

// df029Connect connects an API through POST /api/apis and returns its ID.
func df029Connect(t *testing.T, s *Server, name, specPath string) string {
	t.Helper()
	body := fmt.Sprintf(`{"spec_url":%q,"base_url":"http://example.com","name":%q,"auth_type":"none"}`, specPath, name)
	req := httptest.NewRequest(http.MethodPost, "/api/apis", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("connect %s: expected 201, got %d: %s", name, rec.Code, rec.Body.String())
	}

	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("connect %s: decode: %v", name, err)
	}
	if created.ID == "" {
		t.Fatalf("connect %s: empty id in response", name)
	}
	return created.ID
}

// df029ToolsList posts a JSON-RPC tools/list to /mcp and returns the tool names.
func df029ToolsList(t *testing.T, s *Server) []string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp df029RPCResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("tools/list: decode: %v (body: %s)", err, rec.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("tools/list: JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var list handlers.ListToolsResult
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatalf("tools/list: decode result: %v (result: %s)", err, string(resp.Result))
	}

	names := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// df029APIs GETs /api/apis and returns the connected connection IDs.
func df029APIs(t *testing.T, s *Server) []string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/apis", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/apis: expected 200, got %d", rec.Code)
	}
	var body struct {
		APIs []app.APIConnection `json:"apis"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("GET /api/apis: decode: %v", err)
	}
	ids := make([]string, 0, len(body.APIs))
	for _, conn := range body.APIs {
		ids = append(ids, conn.ID)
	}
	return ids
}

func df029Contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestServer_APIDelete_RefreshesMCPTools is the DF-029 acceptance: with the
// dashboard running, DELETE /api/apis/<id> must drop that API's tools from a
// subsequent tools/list within the same process, keep the remaining APIs'
// tools, and stay consistent with GET /api/apis. Both resolution paths the
// handler supports (id and name) are covered.
func TestServer_APIDelete_RefreshesMCPTools(t *testing.T) {
	tests := []struct {
		name      string
		deleteVia string // "id" or "name"
	}{
		{name: "delete by id", deleteVia: "id"},
		{name: "delete by name", deleteVia: "name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specDir := t.TempDir()
			removedSpec := df015WriteSpec(t, specDir, "removed.json", "listWidgets", "List widgets")
			keptSpec := df015WriteSpec(t, specDir, "kept.json", "listGadgets", "List gadgets")

			s, _, tr := df029NewServer(t)

			removedID := df029Connect(t, s, "df029-removed", removedSpec)
			keptID := df029Connect(t, s, "df029-kept", keptSpec)

			// Premise: both APIs' tools are served before the delete.
			before := df029ToolsList(t, s)
			if len(before) != 2 {
				t.Fatalf("expected 2 tools before delete, got %d: %v", len(before), before)
			}
			for _, want := range []string{"listWidgets", "listGadgets"} {
				if !df029Contains(before, want) {
					t.Fatalf("premise failed: tool %q missing before delete: %v", want, before)
				}
			}

			// DELETE — the same endpoint `musterflow disconnect` uses when a
			// dashboard is running.
			target := removedID
			if tt.deleteVia == "name" {
				target = "df029-removed"
			}
			req := httptest.NewRequest(http.MethodDelete, "/api/apis/"+target, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
			}

			// The delete response contract must be unchanged by the fix.
			var deleted map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&deleted); err != nil {
				t.Fatalf("delete: decode response: %v", err)
			}
			if deleted["status"] != "deleted" {
				t.Errorf("delete response: expected status=deleted, got %q", deleted["status"])
			}
			if deleted["id"] != removedID {
				t.Errorf("delete response: expected id=%q, got %q", removedID, deleted["id"])
			}

			after := df029ToolsList(t, s)

			// Table of tool-presence expectations after the delete.
			presence := []struct {
				tool string
				want bool
			}{
				{"listWidgets", false}, // removed API's tool must be gone
				{"listGadgets", true},  // remaining API's tool must stay
			}
			for _, p := range presence {
				if got := df029Contains(after, p.tool); got != p.want {
					t.Errorf("after delete, tool %q present=%v, want %v (tools: %v)", p.tool, got, p.want, after)
				}
			}
			if len(after) != 1 {
				t.Errorf("expected 1 tool after delete, got %d: %v", len(after), after)
			}

			// GET /api/apis must agree with the tool list.
			ids := df029APIs(t, s)
			if len(ids) != 1 || ids[0] != keptID {
				t.Errorf("GET /api/apis after delete = %v, want [%s]", ids, keptID)
			}

			// The stale execution config must be gone too: a removed API's
			// tool can no longer be "executed" internally. This fails at the
			// config lookup, before any network call.
			if _, err := tr.Execute(context.Background(), "listWidgets", map[string]interface{}{}); err == nil {
				t.Error("expected Execute of the removed API's tool to fail after delete, got nil error")
			} else if !strings.Contains(err.Error(), "not found") {
				t.Errorf("expected 'not found' error for removed tool, got: %v", err)
			}
		})
	}
}

// TestServer_APIDelete_LastAPI_EmptiesToolList covers the degenerate case: the
// removed API was the only one connected, so tools/list must return nothing at
// all (not a stale remainder).
func TestServer_APIDelete_LastAPI_EmptiesToolList(t *testing.T) {
	specPath := df015WriteSpec(t, t.TempDir(), "only.json", "listOnly", "List only")
	s, _, _ := df029NewServer(t)

	id := df029Connect(t, s, "df029-only", specPath)
	if before := df029ToolsList(t, s); len(before) != 1 || before[0] != "listOnly" {
		t.Fatalf("expected [listOnly] before delete, got %v", before)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/apis/"+id, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if after := df029ToolsList(t, s); len(after) != 0 {
		t.Errorf("expected no tools after deleting the last API, got %v", after)
	}
	if ids := df029APIs(t, s); len(ids) != 0 {
		t.Errorf("expected no APIs after deleting the last one, got %v", ids)
	}
}

// TestServer_Import_RefreshesMCPTools covers the second door onto registry
// mutation: POST /api/import writes connections straight into the store, so it
// must re-derive the MCP tools too — otherwise imported APIs' tools never
// appear until a restart.
func TestServer_Import_RefreshesMCPTools(t *testing.T) {
	specPath := df015WriteSpec(t, t.TempDir(), "imported.json", "listImported", "List imported")
	s, _, _ := df029NewServer(t)

	// Premise: nothing connected, no tools served.
	if before := df029ToolsList(t, s); len(before) != 0 {
		t.Fatalf("expected 0 tools before import, got %v", before)
	}

	jsonl := fmt.Sprintf(
		`{"id":"imp-df029","name":"imported-api","spec_url":%q,"base_url":"http://example.com","auth_type":"none"}`+"\n",
		specPath,
	)
	req := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(jsonl))
	req.Header.Set("Content-Type", "application/x-ndjson")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result struct {
		Imported int `json:"imported"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("import: decode: %v", err)
	}
	if result.Imported != 1 {
		t.Fatalf("import: expected imported=1, got %d", result.Imported)
	}

	tools := df029ToolsList(t, s)
	if !df029Contains(tools, "listImported") {
		t.Errorf("expected imported API's tool after import (no restart), got %v", tools)
	}
	if ids := df029APIs(t, s); len(ids) != 1 || ids[0] != "imp-df029" {
		t.Errorf("GET /api/apis after import = %v, want [imp-df029]", ids)
	}
}

// TestServer_RefreshMCPTools_NilRegistry proves the helper is a no-op when the
// server was built without a tool registry (e.g. tests and embedded uses).
func TestServer_RefreshMCPTools_NilRegistry(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	s := NewServer(r, nil, nil, ":0")
	s.refreshMCPTools() // must not panic
}
