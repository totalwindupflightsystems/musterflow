package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// df038WideWidgets returns one row whose "blob" value is far wider than the
// engine's per-cell table cap (80 columns), which is the shape the Off-by-One
// discovery `discover-solution` output had: a long JSON value cut short in the
// default table with no hint about how to see all of it (DF-038).
func df038WideWidgets() []map[string]interface{} {
	return []map[string]interface{}{
		{"id": 1.0, "name": "alpha", "blob": strings.Repeat("x", 400)},
	}
}

// TestDF038_TruncationHintNamesEscapeHatches is the DF-038 acceptance test: a
// default table whose cells were truncated must say so and name the flags that
// show full values.
func TestDF038_TruncationHintNamesEscapeHatches(t *testing.T) {
	root, ts := df020SetupWith(t, df038WideWidgets())
	defer ts.Close()

	output, err := runLeaf(root, []string{"df020-test-api", "widgets", "list-widgets"})
	if err != nil {
		t.Fatalf("list-widgets: %v", err)
	}

	// Premise: the engine really did truncate this row.
	if !strings.Contains(output, "...") {
		t.Fatalf("expected a truncated cell in the table, got: %q", output)
	}
	if strings.Contains(output, strings.Repeat("x", 400)) {
		t.Fatalf("expected the wide value to be truncated, but the full value is present")
	}

	if !strings.Contains(output, "output truncated") {
		t.Errorf("truncated table should say 'output truncated', got: %q", output)
	}
	if !strings.Contains(output, "--output json") {
		t.Errorf("truncation hint should name --output json, got: %q", output)
	}
	if !strings.Contains(output, "--raw") {
		t.Errorf("truncation hint should name --raw, got: %q", output)
	}

	// The hint belongs after the table, exactly once, as its own line.
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "output truncated") {
		t.Errorf("hint should be the last line, got last line: %q (full output: %q)", last, output)
	}
	if n := strings.Count(output, "output truncated"); n != 1 {
		t.Errorf("expected exactly one hint line, got %d: %q", n, output)
	}
}

// TestDF038_NoHintWithoutTruncation guards against a hint that fires on every
// table: narrow rows must stay exactly as they were.
func TestDF038_NoHintWithoutTruncation(t *testing.T) {
	root, ts := df020Setup(t)
	defer ts.Close()

	output, err := runLeaf(root, []string{"df020-test-api", "widgets", "list-widgets"})
	if err != nil {
		t.Fatalf("list-widgets: %v", err)
	}

	if strings.Contains(output, "output truncated") {
		t.Errorf("narrow table should not carry a truncation hint, got: %q", output)
	}
	if !strings.Contains(output, "alpha") || !strings.Contains(output, "beta") {
		t.Errorf("table rows missing: %q", output)
	}
}

// TestDF038_HintAbsentFromJSONOutput proves the hint never leaks into the
// machine-readable formats: --output json stays parseable JSON.
func TestDF038_HintAbsentFromJSONOutput(t *testing.T) {
	root, ts := df020SetupWith(t, df038WideWidgets())
	defer ts.Close()

	output, err := runLeaf(root, []string{"df020-test-api", "widgets", "list-widgets", "-o", "json"})
	if err != nil {
		t.Fatalf("list-widgets -o json: %v", err)
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(output), &rows); err != nil {
		t.Fatalf("JSON output is not parseable (hint leaked?): %v (output: %q)", err, output)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if blob, _ := rows[0]["blob"].(string); len(blob) != 400 {
		t.Errorf("JSON output should carry the full value, got %d chars", len(blob))
	}
}

// TestDF038_HintAbsentFromRawOutput proves the hint never leaks into --raw.
func TestDF038_HintAbsentFromRawOutput(t *testing.T) {
	root, ts := df020SetupWith(t, df038WideWidgets())
	defer ts.Close()

	output, err := runLeaf(root, []string{"df020-test-api", "widgets", "list-widgets", "--raw"})
	if err != nil {
		t.Fatalf("list-widgets --raw: %v", err)
	}

	if strings.Contains(output, "output truncated") {
		t.Errorf("--raw output should not carry a truncation hint, got: %q", output)
	}
	if !strings.Contains(output, strings.Repeat("x", 400)) {
		t.Errorf("--raw output should carry the full body, got %d bytes", len(output))
	}
}

// TestDF038_HintAbsentFromTableWrittenToFile keeps files byte-clean: when the
// table is written to --output-file the hint is not injected into the file.
func TestDF038_HintAbsentFromTableWrittenToFile(t *testing.T) {
	root, ts := df020SetupWith(t, df038WideWidgets())
	defer ts.Close()

	outPath := t.TempDir() + "/widgets.txt"
	if _, err := runLeaf(root, []string{
		"df020-test-api", "widgets", "list-widgets",
		"--output-file", outPath,
	}); err != nil {
		t.Fatalf("list-widgets --output-file x.txt: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if strings.Contains(string(content), "output truncated") {
		t.Errorf("table written to a file should not carry the hint, got: %q", string(content))
	}
	if !strings.Contains(string(content), "alpha") {
		t.Errorf("table file missing row: %q", string(content))
	}
}

// TestTableOutputTruncated pins the detector to the engine's exact truncation
// shape: a cell that is at least as wide as the engine cap AND ends in the
// "..." tail.  A cell that merely ends in an ellipsis is real data, not a
// truncation.
func TestTableOutputTruncated(t *testing.T) {
	wide := strings.Repeat("x", engineTableCellLimit-3) + "..."

	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{"empty", "", false},
		{"narrow rows", "id  name\n1   alpha\n2   beta\n", false},
		{"ellipsis in narrow data", "note  loading...\n", false},
		{"truncated middle column", "blob" + strings.Repeat(" ", 40) + "id\n" + wide + "  1\n", true},
		{"truncated last column", "id  blob\n1   " + wide + "\n", true},
		{"truncated object row", "answer  " + wide + "\n", true},
		{"wide value without ellipsis", strings.Repeat("y", 100) + "  id\n", false},
		{"longer than cap with ellipsis", strings.Repeat("z", 120) + "...\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tableOutputTruncated(tt.output); got != tt.want {
				t.Errorf("tableOutputTruncated(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}
