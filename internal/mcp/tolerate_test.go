package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// toleratedSpecDoc is rejected by a strict parse for a cosmetic reason (a
// boolean `required`) and parses once the tolerance pass repairs it.
const toleratedSpecDoc = `{
  "swagger": "2.0",
  "info": {"title": "Tolerated API", "version": "1.0.0"},
  "paths": {
    "/widgets": {
      "get": {
        "operationId": "listWidgets",
        "responses": {"200": {"description": "ok"}}
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

// TestToolRegistryToleranceOptIn proves the MCP tool surface honours the
// connection's persisted opt-in: without it, a tolerated connection's spec
// fails to parse and the API disappears from tools/list while its CLI commands
// still work — a split-brain the opt-in exists to prevent.
func TestToolRegistryToleranceOptIn(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "tolerated.json")
	if err := os.WriteFile(specPath, []byte(toleratedSpecDoc), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	newRegistry := func(tolerate bool) *app.Registry {
		reg := app.NewRegistry(t.TempDir())
		if err := reg.Load(); err != nil {
			t.Fatalf("Load: %v", err)
		}
		t.Cleanup(func() { _ = reg.Close() })
		if err := reg.Add(&app.APIConnection{
			ID:       "tolerated-api",
			Name:     "tolerated",
			SpecURL:  specPath,
			BaseURL:  "http://example.com",
			Tolerate: tolerate,
		}); err != nil {
			t.Fatalf("add connection: %v", err)
		}
		return reg
	}

	strict := NewToolRegistry(newRegistry(false))
	if err := strict.Refresh(); err == nil {
		t.Fatal("strict tool registry loaded a document only tolerance can parse")
	}
	if n := len(strict.ListCommands()); n != 0 {
		t.Fatalf("strict registry published %d tool(s) for an unparseable spec", n)
	}

	tolerant := NewToolRegistry(newRegistry(true))
	if err := tolerant.Refresh(); err != nil {
		t.Fatalf("tolerant refresh: %v", err)
	}
	commands := tolerant.ListCommands()
	if len(commands) == 0 {
		t.Fatal("tolerant registry published no tools for a tolerated connection")
	}
	found := false
	for _, c := range commands {
		if c.Name == "listWidgets" || c.Name == "tolerated__listWidgets" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected listWidgets among %v", commandNames(commands))
	}
}
