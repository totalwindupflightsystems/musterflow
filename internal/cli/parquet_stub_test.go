package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// TestParquetTypedColumns verifies that writeParquet infers physical column
// types from the Go values in the records rather than writing every column as
// untyped BYTE_ARRAY (string).  This is a regression test for GAP-015 where
// numeric/bool API data read back as strings in DuckDB/pandas, breaking
// filtering and aggregation.
func TestParquetTypedColumns(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{
			"id":       int64(1),
			"price":    float64(19.99),
			"active":   true,
			"name":     "widget",
			"optional": nil,
		},
		map[string]interface{}{
			"id":       int64(2),
			"price":    float64(5.49),
			"active":   false,
			"name":     "gadget",
			"optional": nil,
		},
	}

	var buf bytes.Buffer
	if err := writeParquet(&buf, data); err != nil {
		t.Fatalf("writeParquet: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty parquet output")
	}

	// Read the parquet file back and inspect the schema leaf types.
	file, err := parquet.OpenFile(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	schema := file.Schema()
	if schema == nil {
		t.Fatal("nil schema from parquet file")
	}

	// Map of column name → expected parquet.Kind (physical type).
	expect := map[string]parquet.Kind{
		"id":     parquet.Int64,
		"price":  parquet.Double,
		"active": parquet.Boolean,
		"name":   parquet.ByteArray,
	}

	root := file.Root()
	if root == nil {
		t.Fatal("nil root column")
	}

	for _, col := range root.Columns() {
		name := col.Name()
		if name == "optional" {
			// optional is all-nil → falls back to STRING
			if got := col.Type().Kind(); got != parquet.ByteArray {
				t.Errorf("column %q: expected ByteArray (string fallback), got %s", name, got)
			}
			continue
		}
		want, ok := expect[name]
		if !ok {
			t.Logf("unexpected column %q in schema (skipping)", name)
			continue
		}
		if !col.Leaf() {
			t.Errorf("column %q is not a leaf node", name)
			continue
		}
		if got := col.Type().Kind(); got != want {
			t.Errorf("column %q: physical type = %s, want %s", name, got, want)
		}
	}
}

// TestParquetTypedColumnsMixedType verifies that a column with mixed value
// types (e.g. int and string) falls back to STRING for JSON compatibility.
func TestParquetTypedColumnsMixedType(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"mixed": int64(1)},
		map[string]interface{}{"mixed": "hello"},
	}

	var buf bytes.Buffer
	if err := writeParquet(&buf, data); err != nil {
		t.Fatalf("writeParquet: %v", err)
	}

	file, err := parquet.OpenFile(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	root := file.Root()
	for _, col := range root.Columns() {
		if col.Name() == "mixed" {
			if got := col.Type().Kind(); got != parquet.ByteArray {
				t.Errorf("mixed column: physical type = %s, want ByteArray (string fallback)", got)
			}
		}
	}
}

// TestParquetTypedColumnsJSONNumber verifies that json.Number values (which
// occur when a JSON decoder uses UseNumber) are mapped to INT64 when integral
// and DOUBLE when not.
func TestParquetTypedColumnsJSONNumber(t *testing.T) {
	data := []interface{}{
		map[string]interface{}{"count": jsonNumber("42"), "ratio": jsonNumber("3.14")},
	}

	var buf bytes.Buffer
	if err := writeParquet(&buf, data); err != nil {
		t.Fatalf("writeParquet: %v", err)
	}

	file, err := parquet.OpenFile(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	expect := map[string]parquet.Kind{
		"count": parquet.Int64,
		"ratio": parquet.Double,
	}

	root := file.Root()
	for _, col := range root.Columns() {
		want, ok := expect[col.Name()]
		if !ok {
			continue
		}
		if got := col.Type().Kind(); got != want {
			t.Errorf("column %q: physical type = %s, want %s", col.Name(), got, want)
		}
	}
}

// jsonNumber is a test helper that constructs an encoding/json.Number value.
func jsonNumber(s string) json.Number {
	return json.Number(s)
}
