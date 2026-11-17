package catalog

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchEntryValid(t *testing.T) {
	entry := CatalogEntry{
		ID:          "test-entry",
		Name:        "Test Entry",
		Type:        "api",
		Description: "A test entry",
		SpecURL:     "https://example.com/spec.json",
		Author:      "community",
		Version:     "1.0.0",
		QualityTier: "community-inferred",
		AddedAt:     time.Now(),
		Downloads:   42,
	}
	body, _ := json.Marshal(entry)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/entries/test-entry.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewClientWithRepoURL(srv.URL)
	got, _, err := client.FetchEntry("test-entry")
	if err != nil {
		t.Fatalf("FetchEntry error: %v", err)
	}
	if got.ID != entry.ID {
		t.Errorf("ID: got %q, want %q", got.ID, entry.ID)
	}
	if got.Name != entry.Name {
		t.Errorf("Name: got %q, want %q", got.Name, entry.Name)
	}
	if got.Downloads != entry.Downloads {
		t.Errorf("Downloads: got %d, want %d", got.Downloads, entry.Downloads)
	}
}

func TestFetchEntryNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewClientWithRepoURL(srv.URL)
	got, _, err := client.FetchEntry("nonexistent")
	if err == nil {
		t.Fatal("expected error for not found, got nil")
		return
	}
	if got != nil {
		t.Errorf("expected nil entry on not found, got %+v", got)
	}
}

func TestFetchEntryInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not valid json"))
	}))
	defer srv.Close()

	client := NewClientWithRepoURL(srv.URL)
	_, _, err := client.FetchEntry("badjson")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
		return
	}
}

func TestSearchValidCatalog(t *testing.T) {
	entries := []CatalogEntry{
		{ID: "petstore", Name: "Petstore", Description: "Pet store API"},
		{ID: "github", Name: "GitHub", Description: "GitHub API"},
	}
	body, _ := json.Marshal(entries)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewClientWithRepoURL(srv.URL)
	results, err := client.Search("pet")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "petstore" {
		t.Errorf("expected petstore, got %q", results[0].ID)
	}
}

func TestClientSearchEmptyCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewClientWithRepoURL(srv.URL)
	results, err := client.Search("anything")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty catalog, got %d", len(results))
	}
}

func TestSearchBadStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClientWithRepoURL(srv.URL)
	_, err := client.Search("anything")
	if err == nil {
		t.Fatal("expected error for bad status code, got nil")
		return
	}
}

func TestSearchInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{broken"))
	}))
	defer srv.Close()

	client := NewClientWithRepoURL(srv.URL)
	_, err := client.Search("anything")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
		return
	}
}

func TestSearchNotFoundReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewClientWithRepoURL(srv.URL)
	results, err := client.Search("anything")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
		return
	}
	if results != nil {
		t.Errorf("expected nil results for 404, got %v", results)
	}
}

// TestSearchNotFoundIsClassified covers DF-035 at its source: the 404 site must
// stamp the error with ErrBackendUnavailable so callers (the dashboard handler)
// can distinguish "backend absent" from a generic failure, while the
// human-readable text operators see stays byte-identical to the pre-fix
// message. A non-404 status must NOT carry the sentinel — that failure is an
// upstream error, not an absent backend.
func TestSearchNotFoundIsClassified(t *testing.T) {
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notFound.Close()

	client := NewClientWithRepoURL(notFound.URL)
	_, err := client.Search("anything")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("404 error does not match ErrBackendUnavailable: %v", err)
	}
	want := "catalog backend not available (HTTP 404): repo " + notFound.URL + " absent or private"
	if got := err.Error(); got != want {
		t.Errorf("404 message text drifted from the local-mode wording:\n got: %q\nwant: %q", got, want)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	_, err = NewClientWithRepoURL(upstream.URL).Search("anything")
	if err == nil {
		t.Fatal("expected error for 503, got nil")
	}
	if errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("non-404 status must not be classified as backend-unavailable: %v", err)
	}
	if want := "catalog returned status 503"; err.Error() != want {
		t.Errorf("non-404 message: got %q, want %q", err.Error(), want)
	}
}
