package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// df020TestSpec is a minimal OpenAPI 3.0 spec with a GET endpoint that returns
// a JSON array, so CSV/JSONL/Parquet writers have real data to transform.
const df020TestSpec = `{
  "openapi": "3.0.0",
  "info": {"title": "DF020 Test API", "version": "1.0.0"},
  "servers": [{"url": "%s"}],
  "paths": {
    "/widgets": {
      "get": {
        "operationId": "listWidgets",
        "summary": "List widgets",
        "responses": {
          "200": {
            "description": "ok",
            "content": {"application/json": {"schema": {"type": "array", "items": {"type": "object"}}}}
          }
        }
      },
      "post": {
        "operationId": "createWidget",
        "summary": "Create a widget",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"type": "object"}}}
        },
        "responses": {
          "200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object"}}}}
        }
      }
    }
  }
}`

// df020Setup creates a test HTTP server, writes a spec file pointing at it,
// connects an API to a registry, and returns a root command ready to execute
// generated leaf commands.
func df020Setup(t *testing.T) (*cobra.Command, *httptest.Server) {
	t.Helper()

	// Test server that returns a JSON array of widgets.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		widgets := []map[string]interface{}{
			{"id": 1.0, "name": "alpha"},
			{"id": 2.0, "name": "beta"},
		}
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(widgets)
		_, _ = w.Write(buf.Bytes())
	}))

	// Write the spec file with the test server URL.
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "df020-spec.json")
	specContent := strings.Replace(df020TestSpec, "%s", ts.URL, 1)
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	// Create registry and add a connection.
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	conn := &app.APIConnection{
		ID:            "df020-test-id",
		Name:          "df020-test-api",
		SpecURL:       specPath,
		BaseURL:       ts.URL,
		AuthType:      "none",
		EndpointCount: 2,
	}
	if err := r.Add(conn); err != nil {
		t.Fatalf("Add: %v", err)
	}

	root := NewRootCommand(r)
	// Register the same persistent flags that main.go adds.
	root.PersistentFlags().String("data-dir", "", "Data directory")
	root.PersistentFlags().String("dashboard-addr", "", "Dashboard address")

	// Reset the package-level apiCommands map so each test gets a fresh
	// lazy load.  Without this, the sync.Once from a prior test's same
	// connection ID prevents the new root's API group from loading its
	// subcommands (they were added to the prior root's tree, not this one).
	apiCommandsMu.Lock()
	apiCommands = make(map[string]*apiCommandState)
	apiCommandsMu.Unlock()

	// Reset the package-level outputFlag so it doesn't leak between tests.
	oldOutputFlag := outputFlag
	outputFlag = ""
	t.Cleanup(func() { outputFlag = oldOutputFlag })

	return root, ts
}


// runLeaf executes the root command with the given args and returns the
// captured stdout output and any error.
func runLeaf(root *cobra.Command, args []string) (string, error) {
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestDF020_CSVOutput(t *testing.T) {
	root, ts := df020Setup(t)
	defer ts.Close()


	output, err := runLeaf(root, []string{"df020-test-api", "widgets", "list-widgets", "-o", "csv"})
	if err != nil {
		t.Fatalf("list-widgets -o csv: %v", err)
	}

	// CSV should have a header row with column names and data rows.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least header + 1 data row, got %d lines: %q", len(lines), output)
	}
	// Header should contain "id" and "name".
	if !strings.Contains(lines[0], "id") || !strings.Contains(lines[0], "name") {
		t.Errorf("CSV header missing 'id' or 'name': %q", lines[0])
	}
}

func TestDF020_JSONLOutput(t *testing.T) {
	root, ts := df020Setup(t)
	defer ts.Close()


	output, err := runLeaf(root, []string{"df020-test-api", "widgets", "list-widgets", "-o", "jsonl"})
	if err != nil {
		t.Fatalf("list-widgets -o jsonl: %v", err)
	}

	// JSONL should have one JSON object per line.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines (2 widgets), got %d: %q", len(lines), output)
	}
	for i, line := range lines {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %q (err: %v)", i, line, err)
		}
	}
}

func TestDF020_ParquetOutput(t *testing.T) {
	root, ts := df020Setup(t)
	defer ts.Close()


	output, err := runLeaf(root, []string{"df020-test-api", "widgets", "list-widgets", "-o", "parquet"})
	if err != nil {
		t.Fatalf("list-widgets -o parquet: %v", err)
	}

	// Parquet files start with the magic bytes "PAR1".
	if len(output) < 4 {
		t.Fatalf("parquet output too short (%d bytes): %v", len(output), []byte(output))
	}
	magic := output[:4]
	if magic != "PAR1" {
		t.Errorf("expected PAR1 magic bytes, got %q", magic)
	}
}

func TestDF020_OutputFileParquet(t *testing.T) {
	root, ts := df020Setup(t)
	defer ts.Close()


	outPath := filepath.Join(t.TempDir(), "widgets.parquet")
	output, err := runLeaf(root, []string{
		"df020-test-api", "widgets", "list-widgets",
		"--output-file", outPath,
	})
	if err != nil {
		t.Fatalf("list-widgets --output-file x.parquet: %v (output: %q)", err, output)
	}

	// File should exist and be non-empty.
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}

	// File should start with PAR1 magic.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if len(data) < 4 || string(data[:4]) != "PAR1" {
		t.Errorf("expected PAR1 magic at start of parquet file, got %q", data[:min(4, len(data))])
	}
}

func TestDF020_TableOutputNoRegression(t *testing.T) {
	root, ts := df020Setup(t)
	defer ts.Close()


	output, err := runLeaf(root, []string{"df020-test-api", "widgets", "list-widgets", "-o", "table"})
	if err != nil {
		t.Fatalf("list-widgets -o table: %v", err)
	}

	// Table should contain the column headers and data.
	if !strings.Contains(output, "id") {
		t.Errorf("table output missing 'id': %q", output)
	}
	if !strings.Contains(output, "name") {
		t.Errorf("table output missing 'name': %q", output)
	}
	if !strings.Contains(output, "alpha") {
		t.Errorf("table output missing 'alpha': %q", output)
	}
}

func TestDF020_JSONOutputNoRegression(t *testing.T) {
	root, ts := df020Setup(t)
	defer ts.Close()


	output, err := runLeaf(root, []string{"df020-test-api", "widgets", "list-widgets", "-o", "json"})
	if err != nil {
		t.Fatalf("list-widgets -o json: %v", err)
	}

	// JSON should be valid pretty-printed JSON.
	var data interface{}
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		t.Fatalf("output is not valid JSON: %v (output: %q)", err, output)
	}
}

func TestDF020_OutputFileCSV(t *testing.T) {
	root, ts := df020Setup(t)
	defer ts.Close()


	outPath := filepath.Join(t.TempDir(), "widgets.csv")
	output, err := runLeaf(root, []string{
		"df020-test-api", "widgets", "list-widgets",
		"--output-file", outPath,
	})
	if err != nil {
		t.Fatalf("list-widgets --output-file x.csv: %v (output: %q)", err, output)
	}

	// File should exist and contain CSV.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	content := string(data)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) < 2 {
		t.Fatalf("CSV file should have at least header + 1 data row, got %d lines: %q", len(lines), content)
	}
	if !strings.Contains(lines[0], "id") || !strings.Contains(lines[0], "name") {
		t.Errorf("CSV header missing 'id' or 'name': %q", lines[0])
	}
}

func TestDF020_OutputFileJSON(t *testing.T) {
	root, ts := df020Setup(t)
	defer ts.Close()


	outPath := filepath.Join(t.TempDir(), "widgets.json")
	output, err := runLeaf(root, []string{
		"df020-test-api", "widgets", "list-widgets",
		"-o", "json", "--output-file", outPath,
	})
	if err != nil {
		t.Fatalf("list-widgets -o json --output-file: %v (output: %q)", err, output)
	}

	// File should exist and contain valid JSON.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Errorf("output file is not valid JSON: %v (content: %q)", err, string(data))
	}
}

// --- Unit tests for helper functions ---

func TestParseFlagFromArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		longName  string
		shortName string
		want      string
		wantFound bool
	}{
		{"long form", []string{"--output", "csv"}, "output", "o", "csv", true},
		{"short form", []string{"-o", "json"}, "output", "o", "json", true},
		{"long equals", []string{"--output=jsonl"}, "output", "o", "jsonl", true},
		{"short equals", []string{"-o=parquet"}, "output", "o", "parquet", true},
		{"not found", []string{"--other", "csv"}, "output", "o", "", false},
		{"empty short no match", []string{"-o", "csv"}, "output", "", "", false},
		{"flag at end no value", []string{"--output"}, "output", "o", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := parseFlagFromArgs(tt.args, tt.longName, tt.shortName)
			if got != tt.want || found != tt.wantFound {
				t.Errorf("parseFlagFromArgs(%v, %q, %q) = (%q, %v), want (%q, %v)",
					tt.args, tt.longName, tt.shortName, got, found, tt.want, tt.wantFound)
			}
		})
	}
}

func TestParseBoolFlagFromArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		longName  string
		shortName string
		want      bool
	}{
		{"long present", []string{"--raw"}, "raw", "r", true},
		{"short present", []string{"-r"}, "raw", "r", true},
		{"long equals true", []string{"--raw=true"}, "raw", "r", true},
		{"long equals false", []string{"--raw=false"}, "raw", "r", false},
		{"not present", []string{"--output", "csv"}, "raw", "r", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBoolFlagFromArgs(tt.args, tt.longName, tt.shortName)
			if got != tt.want {
				t.Errorf("parseBoolFlagFromArgs(%v, %q, %q) = %v, want %v",
					tt.args, tt.longName, tt.shortName, got, tt.want)
			}
		})
	}
}

func TestStripFlagFromArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		longName  string
		shortName string
		isBool    bool
		want      []string
	}{
		{"long form with value", []string{"widgets", "list", "--output", "csv"}, "output", "o", false, []string{"widgets", "list"}},
		{"short form with value", []string{"-o", "csv", "widgets"}, "output", "o", false, []string{"widgets"}},
		{"long equals", []string{"widgets", "--output=jsonl"}, "output", "o", false, []string{"widgets"}},
		{"short equals", []string{"widgets", "-o=parquet"}, "output", "o", false, []string{"widgets"}},
		{"not present", []string{"widgets", "list"}, "output", "o", false, []string{"widgets", "list"}},
		{"bool flag no value", []string{"--raw", "widgets"}, "raw", "r", true, []string{"widgets"}},
		{"output-file long form", []string{"--output-file", "/tmp/x.csv", "widgets"}, "output-file", "", false, []string{"widgets"}},
		{"output-file equals", []string{"--output-file=/tmp/x.csv", "widgets"}, "output-file", "", false, []string{"widgets"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripFlagFromArgs(tt.args, tt.longName, tt.shortName, tt.isBool)
			if len(got) != len(tt.want) {
				t.Errorf("stripFlagFromArgs(%v) = %v, want %v", tt.args, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("stripFlagFromArgs(%v) = %v, want %v (index %d)", tt.args, got, tt.want, i)
				}
			}
		})
	}
}

func TestIsExtendedFormat(t *testing.T) {
	tests := []struct {
		format string
		want   bool
	}{
		{"csv", true},
		{"jsonl", true},
		{"parquet", true},
		{"table", false},
		{"json", false},
		{"yaml", false},
		{"", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			if got := isExtendedFormat(tt.format); got != tt.want {
				t.Errorf("isExtendedFormat(%q) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestCreateOutputFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "output.csv")
	f, err := createOutputFile(path)
	if err != nil {
		t.Fatalf("createOutputFile: %v", err)
	}
	_, _ = f.WriteString("test")
	_ = f.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("file is empty")
	}
}