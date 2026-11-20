package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// toleratedSpec is a spec a strict parse rejects for a cosmetic reason that the
// tolerance path repairs: a boolean `required` (Swagger 2.0 style) inside an
// OpenAPI 3 document. The document is otherwise perfectly usable, which is the
// whole point of the opt-in.
const toleratedSpec = `{
  "swagger": "2.0",
  "info": {"title": "Tolerated API", "version": "1.0.0"},
  "paths": {
    "/widgets": {
      "get": {
        "operationId": "listWidgets",
        "responses": {"200": {"description": "ok", "schema": {"$ref": "#/definitions/Widget"}}}
      }
    }
  },
  "definitions": {
    "Widget": {
      "type": "object",
      "required": true,
      "properties": {"id": {"type": "string"}}
    }
  }
}`

// writeSpecFile writes spec bytes to a file under dir and returns its path.
func writeSpecFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return path
}

// TestRefreshKeepsToleranceOptIn proves the opt-in a connection was created
// with survives into every later parse: a connection stored with Tolerate=true
// must refresh without the strict re-parse failing, and it must report the same
// tolerated-defect summary a connect reports.
func TestRefreshKeepsToleranceOptIn(t *testing.T) {
	specDir := t.TempDir()
	specPath := writeSpecFile(t, specDir, "tolerated.json", toleratedSpec)

	r := NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = r.Close() }()

	// Strict connect of this document must fail: the opt-in is what makes it
	// available at all.
	if _, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath}); err == nil {
		t.Fatal("strict connect of the tolerated fixture succeeded; the fixture does not exercise tolerance")
	}

	connected, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath, Tolerate: true})
	if err != nil {
		t.Fatalf("tolerant connect: %v", err)
	}
	if len(connected.Tolerated) == 0 {
		t.Fatal("tolerant connect reported no tolerated defects")
	}
	if !connected.Connection.Tolerate {
		t.Fatal("connection did not record the tolerance opt-in")
	}

	// The opt-in must be persisted, not just held in memory: a later process
	// reads the connection back from the store before refreshing.
	stored, err := r.Get(connected.Connection.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !stored.Tolerate {
		t.Fatal("tolerance opt-in did not survive the store round-trip")
	}

	refreshed, err := Refresh(context.Background(), r, stored.ID)
	if err != nil {
		t.Fatalf("refresh of a tolerated connection failed: %v", err)
	}
	if len(refreshed.Tolerated) == 0 {
		t.Fatal("refresh reported no tolerated defects")
	}
	if got, want := len(refreshed.Tolerated), len(connected.Tolerated); got != want {
		t.Fatalf("refresh tolerated %d defect(s), connect tolerated %d — the same document must report the same summary", got, want)
	}
	if !refreshed.Connection.Tolerate {
		t.Fatal("refresh dropped the connection's tolerance opt-in")
	}
}

// TestRefreshStrictOverride proves the per-run override: an explicitly strict
// refresh of a tolerated connection fails the parse rather than silently
// reusing the repair pass, and --tolerate turns it back on.
func TestRefreshStrictOverride(t *testing.T) {
	specDir := t.TempDir()
	specPath := writeSpecFile(t, specDir, "tolerated.json", toleratedSpec)

	r := NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = r.Close() }()

	connected, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath, Tolerate: true})
	if err != nil {
		t.Fatalf("tolerant connect: %v", err)
	}

	off := false
	if _, err := RefreshWithOptions(context.Background(), r, connected.Connection.ID, RefreshOptions{Tolerate: &off}); err == nil {
		t.Fatal("strict override refreshed a document only tolerance can parse")
	}

	on := true
	refreshed, err := RefreshWithOptions(context.Background(), r, connected.Connection.ID, RefreshOptions{Tolerate: &on})
	if err != nil {
		t.Fatalf("tolerant override refresh: %v", err)
	}
	if len(refreshed.Tolerated) == 0 {
		t.Fatal("tolerant override reported no tolerated defects")
	}
}

// TestApplyToleranceStrictIsUntouched proves the strict path returns the
// caller's bytes unmodified and reports nothing — no caller's default behaviour
// changed when the tolerance plumbing landed.
func TestApplyToleranceStrictIsUntouched(t *testing.T) {
	raw := []byte(toleratedSpec)
	out, warnings, repaired, err := ApplyTolerance(raw, false)
	if err != nil {
		t.Fatalf("ApplyTolerance: %v", err)
	}
	if string(out) != string(raw) {
		t.Fatal("strict ApplyTolerance modified the document")
	}
	if len(warnings) != 0 || repaired {
		t.Fatalf("strict ApplyTolerance reported repairs: %v repaired=%v", warnings, repaired)
	}
}
