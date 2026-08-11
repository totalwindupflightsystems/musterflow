package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
	"github.com/totalwindupflightsystems/musterflow/internal/mcp"
)

func TestServer_HealthEndpoint(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", body["status"])
	}
}

func TestServer_HealthEndpoint_ConnectedCount(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = r.Add(&app.APIConnection{ID: "a", Name: "first"})
	_ = r.Add(&app.APIConnection{ID: "b", Name: "second"})

	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var body map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&body)

	connected := body["connected_apis"]
	// connected is float64 from JSON decoding
	if connected != float64(2) {
		t.Errorf("expected connected_apis=2, got %v", connected)
	}
}

func TestServer_APIsEndpoint_Empty(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/apis", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	apis, ok := body["apis"]
	if !ok {
		t.Fatal("expected 'apis' key in response")
	}
	if apis == nil {
		t.Error("expected non-null apis array")
	}
}

func TestServer_APIsEndpoint_WithData(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = r.Add(&app.APIConnection{
		ID:            "abc123",
		Name:          "test-api",
		SpecURL:       "https://example.com/openapi.json",
		BaseURL:       "https://api.example.com",
		AuthType:      "bearer",
		EndpointCount: 10,
	})

	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/apis", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	apis, ok := body["apis"].([]interface{})
	if !ok {
		t.Fatal("expected apis to be an array")
	}
	if len(apis) != 1 {
		t.Fatalf("expected 1 api, got %d", len(apis))
	}
}

func TestServer_APIByID_NotFound(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/apis/nonexistent", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent API, got %d", rec.Code)
	}
}

func TestServer_APIByID_Success(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = r.Add(&app.APIConnection{
		ID:            "found-me",
		Name:          "found-api",
		SpecURL:       "https://example.com/spec.json",
		BaseURL:       "https://api.example.com",
		AuthType:      "none",
		EndpointCount: 3,
	})

	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/apis/found-me", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var conn map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&conn); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if conn["name"] != "found-api" {
		t.Errorf("expected name 'found-api', got %v", conn["name"])
	}
}

func TestServer_APIByID_MissingID(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/apis/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing ID, got %d", rec.Code)
	}
}

func TestServer_APIByID_Delete(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = r.Add(&app.APIConnection{ID: "delete-me", Name: "temp"})

	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodDelete, "/api/apis/delete-me", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	// Verify it's actually deleted
	if _, err := r.Get("delete-me"); err == nil {
		t.Error("expected API to be deleted")
	}
}

func TestServer_APIByID_Delete_NotFound(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodDelete, "/api/apis/nonexistent", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for delete nonexistent, got %d", rec.Code)
	}
}

func TestServer_APIByID_MethodNotAllowed(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodPut, "/api/apis/something", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestServer_APIs_MethodNotAllowed(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodPost, "/api/apis", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestServer_IndexEndpoint(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected text/html Content-Type, got %q", contentType)
	}

	if rec.Body.Len() == 0 {
		t.Error("expected non-empty HTML body")
	}
}

func TestServer_MCP_NoHandler(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Should return a JSON-RPC error about not configured
	errObj, ok := body["error"]
	if !ok {
		t.Fatal("expected error in MCP response")
	}
	errMap := errObj.(map[string]interface{})
	if errMap["code"].(float64) != -32000 {
		t.Errorf("expected error code -32000, got %v", errMap["code"])
	}
}

func TestServer_MCP_WithHandler(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	// Set a simple MCP handler
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	})
	s.SetMCPHandler(mockHandler)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["result"] == nil {
		t.Error("expected result from MCP handler")
	}
}

func TestServer_Handler(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	h := s.Handler()
	if h == nil {
		t.Fatal("Handler() returned nil")
	}
}

// --- Catalog search tests ---

func TestServer_CatalogSearch_NoQuery(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/search", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&body)

	if total, ok := body["total"].(float64); !ok || total != 0 {
		t.Errorf("expected total=0, got %v", body["total"])
	}
}

func TestServer_CatalogSearch_WithResults(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Start httptest server that returns a catalog index
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"stripe","name":"Stripe","description":"Payments API","score":10,"quality_tier":"official"}]`))
	}))
	defer ts.Close()

	cc := catalog.NewClientWithRepoURL(ts.URL)
	s := NewServer(r, cc, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/search?q=stripe", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&body)

	if total, ok := body["total"].(float64); !ok || total < 1 {
		t.Errorf("expected total>=1, got %v", body["total"])
	}
}

func TestServer_CatalogSearch_CatalogError(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Server that returns 500
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cc := catalog.NewClientWithRepoURL(ts.URL)
	s := NewServer(r, cc, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/catalog/search?q=test", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for catalog failure, got %d", rec.Code)
	}
}

// --- MCP info tests ---

func TestServer_MCPInfo_NoRegistry(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":9876")

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/info", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&body)

	if body["tool_count"].(float64) != 0 {
		t.Errorf("expected tool_count=0 when no registry, got %v", body["tool_count"])
	}
	// DF-009: api_count must be present even when toolRegistry is nil.
	if body["api_count"] == nil {
		t.Errorf("expected api_count field present, got nil")
	} else if body["api_count"].(float64) != 0 {
		t.Errorf("expected api_count=0 when no registry, got %v", body["api_count"])
	}
	if body["endpoint"] != "http://localhost:9876/mcp" {
		t.Errorf("expected endpoint http://localhost:9876/mcp, got %v", body["endpoint"])
	}
}

func TestServer_MCPInfo_WithTools(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Add an API connection — the ToolRegistry reads from it via Refresh()
	_ = r.Add(&app.APIConnection{ID: "test", Name: "test-api", SpecURL: "https://petstore3.swagger.io/api/v3/openapi.json"})

	tr := mcp.NewToolRegistry(r)
	// Refresh fetches the spec and populates tools; errors on network access in test, but the struct is ready
	_ = tr.Refresh() // may fail in test without network, but handler code path is the same

	s := NewServer(r, nil, tr, ":9876")

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/info", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(rec.Body).Decode(&body)

	// With a ToolRegistry (even empty), endpoint and transport are set
	if body["endpoint"] != "http://localhost:9876/mcp" {
		t.Errorf("expected endpoint http://localhost:9876/mcp, got %v", body["endpoint"])
	}
	if body["transport"] != "HTTP JSON-RPC 2.0" {
		t.Errorf("expected transport 'HTTP JSON-RPC 2.0', got %v", body["transport"])
	}
	// DF-009: api_count must reflect the number of connected APIs.
	if body["api_count"] == nil {
		t.Errorf("expected api_count field present, got nil")
	} else if body["api_count"].(float64) != 1 {
		t.Errorf("expected api_count=1 (one API connection added), got %v", body["api_count"])
	}
}

// --- buildExampleArgs / exampleValueForType tests ---

func TestBuildExampleArgs_Empty(t *testing.T) {
	args := buildExampleArgs(json.RawMessage{})
	if len(args) != 0 {
		t.Errorf("expected empty args, got %d keys", len(args))
	}
}

func TestBuildExampleArgs_InvalidJSON(t *testing.T) {
	args := buildExampleArgs(json.RawMessage(`not-json`))
	if len(args) != 0 {
		t.Errorf("expected empty args for invalid JSON, got %d keys", len(args))
	}
}

func TestBuildExampleArgs_StringProperty(t *testing.T) {
	args := buildExampleArgs(json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`))
	if args["name"] != "value" {
		t.Errorf("expected string placeholder 'value', got %v", args["name"])
	}
}

func TestBuildExampleArgs_IntegerProperty(t *testing.T) {
	args := buildExampleArgs(json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer"}}}`))
	if args["count"] != 1 {
		t.Errorf("expected integer placeholder 1, got %v", args["count"])
	}
}

func TestBuildExampleArgs_BooleanProperty(t *testing.T) {
	args := buildExampleArgs(json.RawMessage(`{"type":"object","properties":{"active":{"type":"boolean"}}}`))
	if args["active"] != false {
		t.Errorf("expected boolean placeholder false, got %v", args["active"])
	}
}

func TestExampleValueForType_All(t *testing.T) {
	tests := []struct {
		typ      string
		expected interface{}
	}{
		{"string", "value"},
		{"integer", 1},
		{"number", 1},
		{"boolean", false},
		{"array", []interface{}{}},
		{"object", map[string]interface{}{}},
		{"unknown", "value"},
	}

	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			got := exampleValueForType(tt.typ)
			if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", tt.expected) {
				t.Errorf("exampleValueForType(%q) = %v, want %v", tt.typ, got, tt.expected)
			}
		})
	}
}

func TestBuildToolExample_Basic(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}}}`)
	result := buildToolExample("listPets", schema)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("buildToolExample did not produce valid JSON: %v\noutput: %s", err, result)
	}

	if parsed["method"] != "tools/call" {
		t.Errorf("expected method tools/call, got %v", parsed["method"])
	}

	params := parsed["params"].(map[string]interface{})
	if params["name"] != "listPets" {
		t.Errorf("expected name listPets, got %v", params["name"])
	}

	args := params["arguments"].(map[string]interface{})
	if args["limit"] != float64(1) {
		t.Errorf("expected limit=1, got %v", args["limit"])
	}
}

func TestBuildToolExample_EmptySchema(t *testing.T) {
	result := buildToolExample("noParams", json.RawMessage{})
	if !strings.Contains(result, "noParams") {
		t.Errorf("expected tool name in example, got: %s", result)
	}
	if !strings.Contains(result, "tools/call") {
		t.Errorf("expected method tools/call, got: %s", result)
	}
}

// --- PERF-046: Benchmarks for hot paths ---

func BenchmarkServer_APIsHandler(b *testing.B) {
	r := app.NewRegistry(b.TempDir())
	if err := r.Load(); err != nil {
		b.Fatalf("Load: %v", err)
	}
	_ = r.Add(&app.APIConnection{ID: "a", Name: "api"})
	s := NewServer(r, nil, nil, ":0")
	handler := s.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/apis", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// --- Flow API tests ---

// newServerWithFlowsDir creates a Server with the flowsDir overridden to a
// temp directory for hermetic testing of flow routes.
func newServerWithFlowsDir(t *testing.T, flowsDir string) *Server {
	t.Helper()
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")
	s.flowsDir = flowsDir
	return s
}

func TestServer_FlowsList_Empty(t *testing.T) {
	s := newServerWithFlowsDir(t, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/flows", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	flows, ok := body["flows"].([]interface{})
	if !ok {
		t.Fatalf("expected 'flows' array, got %v", body["flows"])
	}
	if len(flows) != 0 {
		t.Errorf("expected empty flows array, got %d items", len(flows))
	}
}

func TestServer_FlowsCreate_AndList(t *testing.T) {
	dir := t.TempDir()
	s := newServerWithFlowsDir(t, dir)

	// Create a flow via POST
	createBody := `{"name":"test-flow","source":"print('hello')","webhook":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/flows", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created["name"] != "test-flow" {
		t.Errorf("expected name 'test-flow', got %v", created["name"])
	}

	// Now list and verify the flow appears
	req = httptest.NewRequest(http.MethodGet, "/api/flows", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var listBody struct {
		Flows []map[string]interface{} `json:"flows"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listBody.Flows) != 1 {
		t.Fatalf("expected 1 flow, got %d", len(listBody.Flows))
	}
	if listBody.Flows[0]["name"] != "test-flow" {
		t.Errorf("expected name 'test-flow', got %v", listBody.Flows[0]["name"])
	}
}

func TestServer_FlowsCreate_MissingName(t *testing.T) {
	s := newServerWithFlowsDir(t, t.TempDir())

	createBody := `{"source":"print('x')"}`
	req := httptest.NewRequest(http.MethodPost, "/api/flows", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServer_FlowsCreate_WebhookURL(t *testing.T) {
	dir := t.TempDir()
	s := newServerWithFlowsDir(t, dir)

	createBody := `{"name":"hook-flow","source":"print('triggered')","webhook":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/flows", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var created map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	webhookURL, ok := created["webhook_url"].(string)
	if !ok || webhookURL == "" {
		t.Errorf("expected non-empty webhook_url, got %v", created["webhook_url"])
	}
	if !strings.Contains(webhookURL, "/hooks/hook-flow") {
		t.Errorf("expected webhook_url to contain /hooks/hook-flow, got %q", webhookURL)
	}
}

func TestServer_FlowRun_Success(t *testing.T) {
	dir := t.TempDir()
	s := newServerWithFlowsDir(t, dir)

	// Create a flow that prints two lines (matching the output contract)
	createBody := `{"name":"e2e-flow","source":"print(\"42\")\nprint(\"trigger=none\")"}`
	req := httptest.NewRequest(http.MethodPost, "/api/flows", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Run the flow with nil body (content-length 0)
	req = httptest.NewRequest(http.MethodPost, "/api/flows/e2e-flow/run", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}

	output, ok := result["result"]
	if !ok {
		t.Fatal("expected 'result' key in response")
	}
	if !strings.Contains(output, "42") {
		t.Errorf("expected result to contain '42', got %q", output)
	}
	if !strings.Contains(output, "trigger=none") {
		t.Errorf("expected result to contain 'trigger=none', got %q", output)
	}
}

func TestServer_FlowRun_Nonexistent(t *testing.T) {
	s := newServerWithFlowsDir(t, t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/flows/nonexistent/run", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServer_FlowRun_WithTrigger(t *testing.T) {
	dir := t.TempDir()
	s := newServerWithFlowsDir(t, dir)

	// Create a flow that reads the trigger payload
	createBody := `{"name":"trigger-flow","source":"print(trigger[\"user\"])"}`
	req := httptest.NewRequest(http.MethodPost, "/api/flows", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Run with a trigger payload
	runBody := `{"trigger":{"user":"alice"}}`
	req = httptest.NewRequest(http.MethodPost, "/api/flows/trigger-flow/run", strings.NewReader(runBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result["result"] != "alice" {
		t.Errorf("expected result 'alice', got %q", result["result"])
	}
}

func TestServer_FlowsList_MethodNotAllowed(t *testing.T) {
	s := newServerWithFlowsDir(t, t.TempDir())

	req := httptest.NewRequest(http.MethodDelete, "/api/flows", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Export/Import API tests ---

func TestServer_Export_Empty(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/export", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/x-ndjson") {
		t.Errorf("expected application/x-ndjson, got %q", ct)
	}

	body := strings.TrimSpace(rec.Body.String())
	if body != "" {
		t.Errorf("expected empty body for empty registry, got %q", body)
	}
}

func TestServer_Export_WithData(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = r.Add(&app.APIConnection{
		ID:            "exp-1",
		Name:          "exported-api",
		SpecURL:       "https://example.com/spec.json",
		BaseURL:       "https://api.example.com",
		AuthType:      "none",
		EndpointCount: 5,
	})

	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/export", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %s", len(lines), rec.Body.String())
	}

	var conn app.APIConnection
	if err := json.Unmarshal([]byte(lines[0]), &conn); err != nil {
		t.Fatalf("unmarshal JSONL line: %v", err)
	}
	if conn.ID != "exp-1" {
		t.Errorf("expected ID 'exp-1', got %q", conn.ID)
	}
	if conn.Name != "exported-api" {
		t.Errorf("expected Name 'exported-api', got %q", conn.Name)
	}
}

func TestServer_Export_MethodNotAllowed(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodPost, "/api/export", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestServer_Import_Success(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	// JSONL body with one connection
	jsonlBody := `{"id":"imp-1","name":"imported-api","spec_url":"https://example.com/spec.json","base_url":"https://api.example.com","version":"","description":"","auth_type":"none","endpoint_count":3,"added_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}`

	req := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(jsonlBody))
	req.Header.Set("Content-Type", "application/x-ndjson")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["imported"].(float64) != 1 {
		t.Errorf("expected imported=1, got %v", result["imported"])
	}

	// Verify the connection was actually added
	conn, err := r.Get("imp-1")
	if err != nil {
		t.Fatalf("Get imp-1: %v", err)
	}
	if conn.Name != "imported-api" {
		t.Errorf("expected name 'imported-api', got %q", conn.Name)
	}
}

func TestServer_Import_EmptyBody(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodPost, "/api/import", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", rec.Code)
	}
}

func TestServer_Import_MethodNotAllowed(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/import", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestServer_ExportThenImport_RoundTrip(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = r.Add(&app.APIConnection{
		ID:            "rt-1",
		Name:          "roundtrip-api",
		SpecURL:       "https://example.com/spec.json",
		BaseURL:       "https://api.example.com",
		AuthType:      "bearer",
		EndpointCount: 7,
	})

	s := NewServer(r, nil, nil, ":0")

	// Export
	req := httptest.NewRequest(http.MethodGet, "/api/export", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("export: expected 200, got %d", rec.Code)
	}
	exportedJSONL := rec.Body.String()

	// Import into a fresh registry
	r2 := app.NewRegistry(t.TempDir())
	if err := r2.Load(); err != nil {
		t.Fatalf("Load r2: %v", err)
	}
	defer func() { _ = r2.Close() }()
	s2 := NewServer(r2, nil, nil, ":0")

	req2 := httptest.NewRequest(http.MethodPost, "/api/import", strings.NewReader(exportedJSONL))
	req2.Header.Set("Content-Type", "application/x-ndjson")
	rec2 := httptest.NewRecorder()
	s2.Handler().ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("import: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(rec2.Body).Decode(&result); err != nil {
		t.Fatalf("decode import response: %v", err)
	}
	if result["imported"].(float64) != 1 {
		t.Errorf("expected imported=1, got %v", result["imported"])
	}

	conn, err := r2.Get("rt-1")
	if err != nil {
		t.Fatalf("Get rt-1 from r2: %v", err)
	}
	if conn.Name != "roundtrip-api" {
		t.Errorf("expected name 'roundtrip-api', got %q", conn.Name)
	}
}
