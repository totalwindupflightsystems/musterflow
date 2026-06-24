package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry(t.TempDir())
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.dbPath == "" {
		t.Error("dbPath is empty")
	}
}

func TestRegistry_Add_Get(t *testing.T) {
	r := NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer r.Close()

	conn := &APIConnection{
		ID:      "test-id",
		Name:    "Test API",
		SpecURL: "https://example.com/openapi.json",
		BaseURL: "https://api.example.com",
	}
	if err := r.Add(conn); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := r.Get("test-id")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Test API" {
		t.Errorf("Name = %q, want %q", got.Name, "Test API")
	}
	if got.SpecURL != "https://example.com/openapi.json" {
		t.Errorf("SpecURL = %q", got.SpecURL)
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer r.Close()

	conns := r.List()
	if len(conns) != 0 {
		t.Errorf("expected empty, got %d", len(conns))
	}

	r.Add(&APIConnection{ID: "a", Name: "A", SpecURL: "url", BaseURL: "url"})
	r.Add(&APIConnection{ID: "b", Name: "B", SpecURL: "url", BaseURL: "url"})

	conns = r.List()
	if len(conns) != 2 {
		t.Errorf("expected 2, got %d", len(conns))
	}
}

func TestRegistry_Remove(t *testing.T) {
	r := NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer r.Close()

	r.Add(&APIConnection{ID: "x", Name: "X", SpecURL: "url", BaseURL: "url"})

	if err := r.Remove("x"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := r.Get("x"); err == nil {
		t.Error("expected error after remove")
	}
}

func TestRegistry_Remove_NotFound(t *testing.T) {
	r := NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer r.Close()

	if err := r.Remove("nonexistent"); err == nil {
		t.Error("expected error")
	}
}

func TestRegistry_Persistence(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir)
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	r.Add(&APIConnection{ID: "p1", Name: "P1", SpecURL: "url1", BaseURL: "url1"})
	r.Close()

	// Reload
	r2 := NewRegistry(dir)
	if err := r2.Load(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	defer r2.Close()

	conns := r2.List()
	if len(conns) != 1 {
		t.Fatalf("expected 1 connection after reload, got %d", len(conns))
	}
	if conns[0].ID != "p1" {
		t.Errorf("ID = %q", conns[0].ID)
	}
}

func TestConnect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"openapi": "3.0.0",
			"info": {"title": "Test API", "version": "1.0.0", "description": "A test API"},
			"servers": [{"url": "https://test.example.com"}],
			"paths": {
				"/pets": {
					"get": {"operationId": "listPets"},
					"post": {"operationId": "createPet"}
				}
			}
		}`))
	}))
	defer ts.Close()

	r := NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer r.Close()

	result, err := Connect(context.Background(), r, ConnectOptions{
		SpecURL: ts.URL + "/openapi.json",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if result.EndpointCount != 2 {
		t.Errorf("EndpointCount = %d, want 2", result.EndpointCount)
	}
	if result.SpecTitle != "test-api" {
		t.Errorf("SpecTitle = %q, want test-api", result.SpecTitle)
	}
}

func TestConnect_InvalidURL(t *testing.T) {
	r := NewRegistry(t.TempDir())
	_ = r.Load()
	defer r.Close()

	_, err := Connect(context.Background(), r, ConnectOptions{SpecURL: "http://invalid.example.com/nonexistent.json"})
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestConnect_BadSpec(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer ts.Close()

	r := NewRegistry(t.TempDir())
	_ = r.Load()
	defer r.Close()

	_, err := Connect(context.Background(), r, ConnectOptions{SpecURL: ts.URL})
	if err == nil {
		t.Error("expected error for bad spec")
	}
}

func TestConnect_FileSpec(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "test-spec.json")
	os.WriteFile(specPath, []byte(`{
		"openapi": "3.0.0",
		"info": {"title": "File API", "version": "1.0"},
		"paths": {
			"/items": {"get": {"operationId": "listItems"}}
		}
	}`), 0644)

	r := NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer r.Close()

	result, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath})
	if err != nil {
		t.Fatalf("Connect file: %v", err)
	}
	if result.EndpointCount != 1 {
		t.Errorf("EndpointCount = %d, want 1", result.EndpointCount)
	}
}

func TestConnect_CustomName(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"openapi": "3.0.0",
			"info": {"title": "Original Name", "version": "1.0"},
			"paths": {"/x": {"get": {"operationId": "getX"}}}
		}`))
	}))
	defer ts.Close()

	r := NewRegistry(t.TempDir())
	_ = r.Load()
	defer r.Close()

	result, err := Connect(context.Background(), r, ConnectOptions{
		SpecURL: ts.URL,
		Name:    "my-custom-name",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if result.SpecTitle != "my-custom-name" {
		t.Errorf("SpecTitle = %q, want my-custom-name", result.SpecTitle)
	}
}

func TestConnect_CustomBaseURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"openapi": "3.0.0",
			"info": {"title": "Base Test", "version": "1.0"},
			"servers": [{"url": "https://original.example.com"}],
			"paths": {"/x": {"get": {"operationId": "getX"}}}
		}`))
	}))
	defer ts.Close()

	r := NewRegistry(t.TempDir())
	_ = r.Load()
	defer r.Close()

	result, err := Connect(context.Background(), r, ConnectOptions{
		SpecURL: ts.URL,
		BaseURL: "https://custom.example.com",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if result.Connection.BaseURL != "https://custom.example.com" {
		t.Errorf("BaseURL = %q", result.Connection.BaseURL)
	}
}

func TestDisconnect(t *testing.T) {
	r := NewRegistry(t.TempDir())
	_ = r.Load()
	defer r.Close()

	r.Add(&APIConnection{ID: "disc", Name: "D", SpecURL: "url", BaseURL: "url"})
	if err := Disconnect(r, "disc"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if conns := r.List(); len(conns) != 0 {
		t.Errorf("expected empty after disconnect, got %d", len(conns))
	}
}

func TestDisconnect_NotFound(t *testing.T) {
	r := NewRegistry(t.TempDir())
	_ = r.Load()
	defer r.Close()

	if err := Disconnect(r, "nonexistent"); err == nil {
		t.Error("expected error")
	}
}

func TestGenerateCommandConfig(t *testing.T) {
	conn := &APIConnection{Name: "test-api"}
	cfg := GenerateCommandConfig(conn)
	if cfg.AppName != "musterflow" {
		t.Errorf("AppName = %q", cfg.AppName)
	}
	if cfg.DefaultFormat != "table" {
		t.Errorf("DefaultFormat = %q", cfg.DefaultFormat)
	}
}

func TestDeriveName(t *testing.T) {
	tests := []struct {
		specURL string
		want    string
	}{
		{"https://api.example.com/openapi.json", "api-example-com"},
		{"/local/path/spec.yaml", "spec"},
		{"spec.json", "spec"},
	}
	for _, tt := range tests {
		got := deriveName(tt.specURL, nil)
		if got != tt.want {
			t.Errorf("deriveName(%q) = %q, want %q", tt.specURL, got, tt.want)
		}
	}
}

func TestCollapseHyphens(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello--world", "hello-world"},
		{"a---b", "a-b"},
		{"normal", "normal"},
		{"", ""},
	}
	for _, tt := range tests {
		got := collapseHyphens(tt.input)
		if got != tt.want {
			t.Errorf("collapseHyphens(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStore_AddList(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	conn := &APIConnection{ID: "s1", Name: "S1", SpecURL: "url", BaseURL: "url"}
	if err := store.Add(conn); err != nil {
		t.Fatalf("Add: %v", err)
	}

	conns, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(conns) != 1 {
		t.Errorf("expected 1, got %d", len(conns))
	}
}

func TestStore_Remove(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()

	store.Add(&APIConnection{ID: "r1", Name: "R1", SpecURL: "url", BaseURL: "url"})
	if err := store.Remove("r1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if store.Has("r1") {
		t.Error("should not exist after remove")
	}
}

func TestJSONL_ExportImport(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "test.db"))
	defer store.Close()

	store.Add(&APIConnection{ID: "e1", Name: "E1", SpecURL: "url", BaseURL: "url"})
	store.Add(&APIConnection{ID: "e2", Name: "E2", SpecURL: "url", BaseURL: "url"})

	exportPath := filepath.Join(dir, "export.jsonl")
	if err := ExportJSONL(store, exportPath); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Import into new store
	store2, _ := NewStore(filepath.Join(dir, "test2.db"))
	defer store2.Close()

	n, err := ImportJSONL(store2, exportPath)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if n != 2 {
		t.Errorf("imported %d, want 2", n)
	}

	conns, _ := store2.List()
	if len(conns) != 2 {
		t.Errorf("expected 2 after import, got %d", len(conns))
	}
}

func TestMigrateJSONToStore(t *testing.T) {
	dir := t.TempDir()

	// Write legacy JSON registry
	jsonPath := filepath.Join(dir, "registry.json")
	os.WriteFile(jsonPath, []byte(`{"legacy-1":{"id":"legacy-1","name":"Legacy API","spec_url":"url","base_url":"url"}}`), 0644)

	store, _ := NewStore(filepath.Join(dir, "musterflow.db"))
	defer store.Close()

	n, err := MigrateJSONToStore(store, dir)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if n != 1 {
		t.Errorf("migrated %d, want 1", n)
	}

	// Old file should be renamed
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Error("old JSON file should be renamed")
	}
	if _, err := os.Stat(jsonPath + ".bak"); os.IsNotExist(err) {
		t.Error("backup file should exist")
	}

	conns, _ := store.List()
	if len(conns) != 1 {
		t.Errorf("expected 1 after migration, got %d", len(conns))
	}
}

func TestLoad_WithLegacyJSON(t *testing.T) {
	dir := t.TempDir()

	// Write legacy JSON
	os.WriteFile(filepath.Join(dir, "registry.json"), []byte(`{"old":{"id":"old","name":"Old","spec_url":"url","base_url":"url"}}`), 0644)

	r := NewRegistry(dir)
	if err := r.Load(); err != nil {
		t.Fatalf("Load with legacy: %v", err)
	}
	defer r.Close()

	conns := r.List()
	if len(conns) != 1 {
		t.Errorf("expected 1 after auto-migration, got %d", len(conns))
	}
	if conns[0].ID != "old" {
		t.Errorf("ID = %q", conns[0].ID)
	}
}

func TestStore_Has(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()

	if store.Has("nonexistent") {
		t.Error("Has should return false")
	}
	store.Add(&APIConnection{ID: "h1", Name: "H1", SpecURL: "url", BaseURL: "url"})
	if !store.Has("h1") {
		t.Error("Has should return true")
	}
}

func TestExportJSONL_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "test.db"))
	defer store.Close()

	exportPath := filepath.Join(dir, "empty.jsonl")
	if err := ExportJSONL(store, exportPath); err != nil {
		t.Fatalf("Export empty: %v", err)
	}

	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if strings.TrimSpace(string(data)) != "" {
		t.Errorf("expected empty file, got: %s", data)
	}
}
