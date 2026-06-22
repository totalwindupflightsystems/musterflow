package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/wojons/muster/pkg/mcp/handlers"
)

// petstoreSpec is a minimal OpenAPI 3.0 spec for testing tool generation.
const petstoreSpec = `{
  "openapi": "3.0.0",
  "info": {
    "title": "Test Petstore",
    "version": "1.0.0"
  },
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List all pets",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": { "type": "integer" }
          }
        ]
      }
    }
  }
}`

// writeTempSpec writes the petstore spec to a temp file and returns the path.
func writeTempSpec(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/spec.json"
	if err := writeFile(path, petstoreSpec); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return path
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// AC-001.1: tools/list returns valid MCP tools with Name, Description, InputSchema.
func TestToolRegistry_ListTools(t *testing.T) {
	specPath := writeTempSpec(t)

	reg := app.NewRegistry(t.TempDir())
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

	tools := tr.ListTools()
	if len(tools) == 0 {
		t.Fatal("expected at least 1 tool, got 0")
	}

	// Find the listPets tool
	var listPetsTool *handlers.Tool
	for i := range tools {
		if tools[i].Name == "listPets" {
			listPetsTool = &tools[i]
			break
		}
	}
	if listPetsTool == nil {
		t.Fatalf("expected tool 'listPets', got: %v", toolNames(tools))
	}

	if listPetsTool.Description == "" {
		t.Error("expected Description to be populated, got empty")
	}

	if len(listPetsTool.InputSchema) == 0 {
		t.Error("expected InputSchema to be populated, got empty")
	}

	// Verify InputSchema is valid JSON
	var schema map[string]interface{}
	if err := json.Unmarshal(listPetsTool.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema is not valid JSON: %v", err)
	}

	// Verify schema has properties
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected schema to have 'properties'")
	}
	if _, ok := props["limit"]; !ok {
		t.Error("expected schema properties to include 'limit'")
	}
}

// AC-001.2: tools/call dispatches correctly. A network error is acceptable
// (no real API), but the response must be valid JSON-RPC.
func TestHTTPServer_ToolsCall(t *testing.T) {
	specPath := writeTempSpec(t)

	reg := app.NewRegistry(t.TempDir())
	conn := &app.APIConnection{
		ID:      "test-api",
		Name:    "test-petstore",
		SpecURL: specPath,
		BaseURL: "http://localhost:1", // invalid port → fast failure
	}
	if err := reg.Add(conn); err != nil {
		t.Fatalf("add connection: %v", err)
	}

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	handlerReg := handlers.NewRegistry()
	handlerReg.Register(handlers.NewListToolsHandler(tr))
	handlerReg.Register(handlers.NewCallToolHandler(tr))

	server := NewHTTPServer(handlerReg)

	// Send tools/call request
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"listPets","arguments":{"limit":3}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp rpcResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, rr.Body.String())
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc=2.0, got %q", resp.JSONRPC)
	}

	// The call should either return a result (with error content) or an error.
	// Since we're pointing at an invalid port, it should be a CallToolResult
	// with isError=true (the handler wraps errors in CallToolResult).
	if resp.Error != nil {
		// Direct JSON-RPC error is also acceptable
		return
	}

	if resp.Result == nil {
		t.Fatal("expected result or error, both nil")
	}

	// Verify result is a CallToolResult with content
	resultBytes, _ := json.Marshal(resp.Result)
	var callResult handlers.CallToolResult
	if err := json.Unmarshal(resultBytes, &callResult); err != nil {
		t.Fatalf("result is not a CallToolResult: %v\nresult: %s", err, string(resultBytes))
	}

	if len(callResult.Content) == 0 {
		t.Error("expected content in CallToolResult")
	}
}

// AC-001.2 (supporting): tools/list via HTTP returns valid JSON-RPC.
func TestHTTPServer_ToolsList(t *testing.T) {
	specPath := writeTempSpec(t)

	reg := app.NewRegistry(t.TempDir())
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

	handlerReg := handlers.NewRegistry()
	handlerReg.Register(handlers.NewListToolsHandler(tr))

	server := NewHTTPServer(handlerReg)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp rpcResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, rr.Body.String())
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc=2.0, got %q", resp.JSONRPC)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	// Verify result has tools
	resultBytes, _ := json.Marshal(resp.Result)
	var listResult handlers.ListToolsResult
	if err := json.Unmarshal(resultBytes, &listResult); err != nil {
		t.Fatalf("result is not ListToolsResult: %v\nresult: %s", err, string(resultBytes))
	}

	if len(listResult.Tools) == 0 {
		t.Fatal("expected tools in list result, got 0")
	}
}

// AC-001.3: After connecting a second API, Refresh() adds its tools.
// Tools from BOTH APIs appear in tools/list.
func TestToolRegistry_DynamicTools(t *testing.T) {
	specPath1 := writeTempSpec(t)

	// Second spec with a different operation
	const spec2 = `{
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
	dir2 := t.TempDir()
	specPath2 := dir2 + "/spec.json"
	if err := writeFile(specPath2, spec2); err != nil {
		t.Fatalf("write spec2: %v", err)
	}

	reg := app.NewRegistry(t.TempDir())

	// Connect first API
	conn1 := &app.APIConnection{
		ID:      "api-1",
		Name:    "petstore",
		SpecURL: specPath1,
		BaseURL: "http://example.com",
	}
	if err := reg.Add(conn1); err != nil {
		t.Fatalf("add conn1: %v", err)
	}

	tr := NewToolRegistry(reg)
	if err := tr.Refresh(); err != nil {
		t.Fatalf("refresh 1: %v", err)
	}

	toolsAfter1 := tr.ListTools()
	if !hasTool(toolsAfter1, "listPets") {
		t.Fatalf("expected listPets after first refresh, got: %v", toolNames(toolsAfter1))
	}
	if hasTool(toolsAfter1, "listUsers") {
		t.Fatal("did not expect listUsers before connecting second API")
	}

	// Connect second API
	conn2 := &app.APIConnection{
		ID:      "api-2",
		Name:    "users-api",
		SpecURL: specPath2,
		BaseURL: "http://example.com",
	}
	if err := reg.Add(conn2); err != nil {
		t.Fatalf("add conn2: %v", err)
	}

	// Refresh again — should now include tools from BOTH APIs
	if err := tr.Refresh(); err != nil {
		t.Fatalf("refresh 2: %v", err)
	}

	toolsAfter2 := tr.ListTools()
	if !hasTool(toolsAfter2, "listPets") {
		t.Error("expected listPets to still be present after second refresh")
	}
	if !hasTool(toolsAfter2, "listUsers") {
		t.Errorf("expected listUsers after second refresh, got: %v", toolNames(toolsAfter2))
	}
}

// Test that GET requests are rejected with 405.
func TestHTTPServer_MethodNotAllowed(t *testing.T) {
	handlerReg := handlers.NewRegistry()
	server := NewHTTPServer(handlerReg)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// Test that initialize works via HTTP.
func TestHTTPServer_Initialize(t *testing.T) {
	handlerReg := handlers.NewRegistry()
	handlerReg.Register(handlers.NewInitializeHandler("test-mcp", "0.1.0"))
	server := NewHTTPServer(handlerReg)

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
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
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var initResult handlers.InitializeResult
	if err := json.Unmarshal(resultBytes, &initResult); err != nil {
		t.Fatalf("result is not InitializeResult: %v", err)
	}
	if initResult.ServerInfo.Name != "test-mcp" {
		t.Errorf("expected server name 'test-mcp', got %q", initResult.ServerInfo.Name)
	}
}

// Test method not found returns JSON-RPC error.
func TestHTTPServer_MethodNotFound(t *testing.T) {
	handlerReg := handlers.NewRegistry()
	server := NewHTTPServer(handlerReg)

	body := `{"jsonrpc":"2.0","id":1,"method":"nonexistent/method"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp rpcResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
}

// --- helpers ---

func hasTool(tools []handlers.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

func toolNames(tools []handlers.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}