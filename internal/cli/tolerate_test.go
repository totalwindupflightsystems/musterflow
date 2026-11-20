package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// toleratedSpecFile is rejected by a strict parse (boolean `required`) and
// parses once the tolerance pass repairs it.
const toleratedSpecFile = `{
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

// TestLoadAPICommandsToleranceOptIn proves the command-generation parse site
// honours the connection's persisted opt-in. Without it a connection made with
// --tolerate would connect and refresh but then fail to build its own
// subcommands — the same document, parsed three different ways.
func TestLoadAPICommandsToleranceOptIn(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "tolerated.json")
	if err := os.WriteFile(specPath, []byte(toleratedSpecFile), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	newConn := func(tolerate bool) *app.APIConnection {
		return &app.APIConnection{
			ID:       "tolerated-api",
			Name:     "tolerated",
			SpecURL:  specPath,
			BaseURL:  "http://example.com",
			Tolerate: tolerate,
		}
	}

	strictParent := &cobra.Command{Use: "musterflow"}
	if err := loadAPICommands(strictParent, newConn(false)); err == nil {
		t.Fatal("strict command generation accepted a document only tolerance can parse")
	}
	if len(strictParent.Commands()) != 0 {
		t.Fatalf("strict command generation added %d subcommand(s) for an unparseable spec", len(strictParent.Commands()))
	}

	tolerantParent := &cobra.Command{Use: "musterflow"}
	if err := loadAPICommands(tolerantParent, newConn(true)); err != nil {
		t.Fatalf("tolerant command generation: %v", err)
	}
	if len(tolerantParent.Commands()) == 0 {
		t.Fatal("tolerant command generation added no subcommands")
	}
}
