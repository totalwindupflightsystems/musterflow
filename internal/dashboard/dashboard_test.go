package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

func TestServer_HealthEndpoint(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	s := NewServer(r, ":0")

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
	r.Add(&app.APIConnection{ID: "a", Name: "first"})
	r.Add(&app.APIConnection{ID: "b", Name: "second"})

	s := NewServer(r, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var body map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&body)

	connected := body["connected_apis"]
	// connected is float64 from JSON decoding
	if connected != float64(2) {
		t.Errorf("expected connected_apis=2, got %v", connected)
	}
}

func TestServer_APIsEndpoint_Empty(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	s := NewServer(r, ":0")

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
	r.Add(&app.APIConnection{
		ID:        "abc123",
		Name:      "test-api",
		SpecURL:   "https://example.com/openapi.json",
		BaseURL:   "https://api.example.com",
		AuthType:  "bearer",
		EndpointCount: 10,
	})

	s := NewServer(r, ":0")

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
	s := NewServer(r, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/apis/nonexistent", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent API, got %d", rec.Code)
	}
}

func TestServer_APIByID_Success(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	r.Add(&app.APIConnection{
		ID:        "found-me",
		Name:      "found-api",
		SpecURL:   "https://example.com/spec.json",
		BaseURL:   "https://api.example.com",
		AuthType:  "none",
		EndpointCount: 3,
	})

	s := NewServer(r, ":0")

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
	s := NewServer(r, ":0")

	req := httptest.NewRequest(http.MethodGet, "/api/apis/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing ID, got %d", rec.Code)
	}
}

func TestServer_APIByID_Delete(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	r.Add(&app.APIConnection{ID: "delete-me", Name: "temp"})

	s := NewServer(r, ":0")

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
	s := NewServer(r, ":0")

	req := httptest.NewRequest(http.MethodDelete, "/api/apis/nonexistent", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for delete nonexistent, got %d", rec.Code)
	}
}

func TestServer_APIByID_MethodNotAllowed(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	s := NewServer(r, ":0")

	req := httptest.NewRequest(http.MethodPut, "/api/apis/something", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestServer_APIs_MethodNotAllowed(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	s := NewServer(r, ":0")

	req := httptest.NewRequest(http.MethodPost, "/api/apis", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestServer_IndexEndpoint(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	s := NewServer(r, ":0")

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
	s := NewServer(r, ":0")

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
	s := NewServer(r, ":0")

	// Set a simple MCP handler
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
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
	s := NewServer(r, ":0")

	h := s.Handler()
	if h == nil {
		t.Fatal("Handler() returned nil")
	}
}
