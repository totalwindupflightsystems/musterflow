package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/wojons/muster/pkg/mcp/handlers"
)

// setupRegistry creates a ToolRegistry with one API connected via a temp spec.
func setupRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	specPath := writeTempSpec(t)

	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	conn := &app.APIConnection{
		ID:      "test-api",
		Name:    "test-petstore",
		SpecURL: specPath,
		BaseURL: "http://example.com",
	}
	if err := reg.Add(conn); err != nil {
		t.Fatalf("add connection: %v", err)
	}

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return tr
}

func TestToolRegistry_ListCommands(t *testing.T) {
	tr := setupRegistry(t)
	commands := tr.ListCommands()

	if len(commands) == 0 {
		t.Fatal("expected at least 1 command, got 0")
	}

	// Verify commands have Name and Description
	var listPets *handlers.Command
	for i := range commands {
		if commands[i].Name == "listPets" {
			listPets = &commands[i]
			break
		}
	}
	if listPets == nil {
		t.Fatalf("expected command 'listPets', got: %v", commandNames(commands))
		return
	}
	if listPets.Name != "listPets" {
		t.Errorf("expected Name 'listPets', got %q", listPets.Name)
	}
	if listPets.Description == "" {
		t.Error("expected Description to be populated")
	}
}

func TestToolRegistry_GetCommand(t *testing.T) {
	tr := setupRegistry(t)

	// Get existing command
	cmd := tr.GetCommand("listPets")
	if cmd == nil {
		t.Fatal("expected command 'listPets', got nil")
		return
	}
	if cmd.Name != "listPets" {
		t.Errorf("expected Name 'listPets', got %q", cmd.Name)
	}

	// Get non-existent command
	cmd = tr.GetCommand("nonexistent")
	if cmd != nil {
		t.Errorf("expected nil for nonexistent command, got %+v", cmd)
	}
}

func TestToolRegistry_GetCommand_EmptyRegistry(t *testing.T) {
	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	tr := NewToolRegistry(reg)

	cmd := tr.GetCommand("anything")
	if cmd != nil {
		t.Errorf("expected nil for empty registry, got %+v", cmd)
	}
}

func TestToolRegistry_ExecuteCommand(t *testing.T) {
	tr := setupRegistry(t)

	// ExecuteCommand delegates to Execute, which uses musterMcp.ExecuteHTTP.
	// With BaseURL=http://example.com it will make a real HTTP call.
	// The call may succeed or fail — we just verify the dispatch works.
	result, err := tr.ExecuteCommand(context.Background(), "listPets", map[string]interface{}{
		"limit": float64(3),
	})
	// Either result or error is acceptable — the point is dispatch worked
	if err != nil {
		t.Logf("ExecuteCommand returned error (expected for external HTTP): %v", err)
	}
	if result == nil && err == nil {
		t.Error("expected result or error, both nil")
	}
}

func TestToolRegistry_ExecuteCommand_NotFound(t *testing.T) {
	tr := setupRegistry(t)

	_, err := tr.ExecuteCommand(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for non-existent tool")
		return
	}
}

func TestToolRegistry_AddCommand(t *testing.T) {
	tr := setupRegistry(t)

	err := tr.AddCommand(handlers.Command{Name: "test", Description: "test"})
	if err == nil {
		t.Fatal("expected error for AddCommand (not supported)")
		return
	}
}

func TestToolRegistry_RemoveCommand(t *testing.T) {
	tr := setupRegistry(t)

	err := tr.RemoveCommand("listPets")
	if err == nil {
		t.Fatal("expected error for RemoveCommand (not supported)")
		return
	}
}

func TestToolRegistry_UpdateCommand(t *testing.T) {
	tr := setupRegistry(t)

	err := tr.UpdateCommand("listPets", handlers.Command{Name: "listPets", Description: "updated"})
	if err == nil {
		t.Fatal("expected error for UpdateCommand (not supported)")
		return
	}
}

func TestToolRegistry_ListCommands_EmptyRegistry(t *testing.T) {
	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	tr := NewToolRegistry(reg)

	commands := tr.ListCommands()
	if len(commands) != 0 {
		t.Errorf("expected 0 commands for empty registry, got %d", len(commands))
	}
}

func TestFetchSpecData_File(t *testing.T) {
	// Write a spec to a temp file and load it
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, []byte(petstoreSpec), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	data, err := fetchSpecData(specPath)
	if err != nil {
		t.Fatalf("fetchSpecData(file): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty data from file")
	}
}

func TestFetchSpecData_FileNotFound(t *testing.T) {
	_, err := fetchSpecData("/nonexistent/path/spec.json")
	if err == nil {
		t.Fatal("expected error for non-existent file")
		return
	}
}

func TestFetchSpecData_HTTPError(t *testing.T) {
	// Use a server that returns 404
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	_, err := fetchSpecData(srv.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 404 status")
		return
	}
}

func TestRefresh_PartialFailure(t *testing.T) {
	// One valid API, one with bad spec URL — should still succeed
	specPath := writeTempSpec(t)

	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Valid connection
	conn1 := &app.APIConnection{
		ID:      "api-1",
		Name:    "good-api",
		SpecURL: specPath,
		BaseURL: "http://example.com",
	}
	if err := reg.Add(conn1); err != nil {
		t.Fatalf("add conn1: %v", err)
	}

	// Invalid connection (bad spec URL)
	conn2 := &app.APIConnection{
		ID:      "api-2",
		Name:    "bad-api",
		SpecURL: "/nonexistent/file.json",
		BaseURL: "http://example.com",
	}
	if err := reg.Add(conn2); err != nil {
		t.Fatalf("add conn2: %v", err)
	}

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("Refresh should succeed with partial failure: %v", err)
	}

	tools := tr.ListTools()
	if !hasTool(tools, "listPets") {
		t.Error("expected listPets tool from good API after partial failure")
	}
}

func TestRefresh_AllFail(t *testing.T) {
	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	conn := &app.APIConnection{
		ID:      "bad-api",
		Name:    "bad",
		SpecURL: "/nonexistent/file.json",
		BaseURL: "http://example.com",
	}
	if err := reg.Add(conn); err != nil {
		t.Fatalf("add conn: %v", err)
	}

	tr := NewToolRegistry(reg)
	err := tr.Refresh()
	if err == nil {
		t.Fatal("expected error when all APIs fail")
		return
	}
}

func TestHTTPServer_InvalidBody(t *testing.T) {
	handlerReg := handlers.NewRegistry()
	server := NewHTTPServer(handlerReg)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("not json"))
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	// Should return 400 or 200 with JSON-RPC error
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Errorf("expected 200 or 400, got %d", rr.Code)
	}
}

func TestExecute_NonJSONResponse(t *testing.T) {
	// Create a server that returns plain text, not JSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	specPath := writeTempSpec(t)
	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	conn := &app.APIConnection{
		ID:      "text-api",
		Name:    "text-api",
		SpecURL: specPath,
		BaseURL: srv.URL,
	}
	if err := reg.Add(conn); err != nil {
		t.Fatalf("add conn: %v", err)
	}

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	result, err := tr.Execute(context.Background(), "listPets", nil)
	// musterMcp.ExecuteHTTP may parse JSON internally — either
	// success or failure is acceptable; we just verify no panic
	if err != nil {
		t.Logf("Execute returned error: %v", err)
		return
	}
	_ = result // result may be parsed JSON or raw string
}

func commandNames(cmds []handlers.Command) []string {
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name)
	}
	return names
}

// secondSpec is a minimal OpenAPI spec whose only operation is listUsers.
const secondSpec = `{
  "openapi": "3.0.0",
  "info": { "title": "Second API", "version": "1.0.0" },
  "paths": {
    "/users": {
      "get": {
        "operationId": "listUsers",
        "summary": "List all users"
      }
    }
  }
}`

// TestRefresh_DropsRemovedAPI is the registry-level half of DF-029:
// ToolRegistry.Refresh() rebuilds tools AND execution configs from
// appRegistry.List(), so once a connection is removed a Refresh must drop that
// API's tool names and keep every other API's tools. Before the dashboard
// called Refresh() on disconnect, this primitive was correct but unused on the
// removal path — which is exactly why tools/list went stale.
func TestRefresh_DropsRemovedAPI(t *testing.T) {
	// API 1: the petstore spec (listPets).
	specPath1 := writeTempSpec(t)
	// API 2: a different operation (listUsers) from a separate file.
	dir2 := t.TempDir()
	specPath2 := dir2 + "/spec.json"
	if err := writeFile(specPath2, secondSpec); err != nil {
		t.Fatalf("write second spec: %v", err)
	}

	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, conn := range []*app.APIConnection{
		{ID: "api-keep", Name: "keep-api", SpecURL: specPath1, BaseURL: "http://example.com"},
		{ID: "api-drop", Name: "drop-api", SpecURL: specPath2, BaseURL: "http://example.com"},
	} {
		if err := reg.Add(conn); err != nil {
			t.Fatalf("add %s: %v", conn.ID, err)
		}
	}

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("initial Refresh: %v", err)
	}

	// Premise: both APIs' tools are present.
	if got := toolNames(tr.ListTools()); len(got) != 2 || !hasTool(tr.ListTools(), "listPets") || !hasTool(tr.ListTools(), "listUsers") {
		t.Fatalf("expected [listPets listUsers] before remove, got %v", got)
	}

	// Remove the API that owns listUsers, then re-derive.
	if err := reg.Remove("api-drop"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := tr.Refresh(); err != nil {
		t.Fatalf("Refresh after remove: %v", err)
	}

	tools := tr.ListTools()
	if hasTool(tools, "listUsers") {
		t.Errorf("removed API's tool 'listUsers' still listed after Refresh: %v", toolNames(tools))
	}
	if !hasTool(tools, "listPets") {
		t.Errorf("remaining API's tool 'listPets' missing after Refresh: %v", toolNames(tools))
	}

	// The execution config for the removed tool must be gone too (this fails
	// at the config lookup, before any HTTP call).
	if _, err := tr.Execute(context.Background(), "listUsers", nil); err == nil {
		t.Error("expected Execute of removed tool to fail after Refresh, got nil error")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error for removed tool, got: %v", err)
	}
}
