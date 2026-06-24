// Package cli provides output format writers for MusterFlow.
// Supported formats: table, json, yaml, csv, jsonl, parquet.
package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// FormatWriter converts structured data to a specific output format.
type FormatWriter func(w io.Writer, data interface{}) error

// FormatWriters maps format names to their writers.
var FormatWriters = map[string]FormatWriter{
	"csv":   writeCSV,
	"jsonl": writeJSONL,
	"json":  writeJSON,
	"table": nil, // handled inline in ExecuteAndFormat
}

// writeCSV writes JSON data as CSV. Arrays of objects become rows with
// keys as headers. Single objects become two-column key/value rows.
func writeCSV(w io.Writer, data interface{}) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	switch v := data.(type) {
	case []interface{}:
		if len(v) == 0 {
			return nil
		}
		// Collect all unique keys across all objects for headers
		keys := collectKeys(v)
		if err := cw.Write(keys); err != nil {
			return fmt.Errorf("write CSV header: %w", err)
		}
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				row := make([]string, len(keys))
				for i, k := range keys {
					row[i] = csvValue(m[k])
				}
				if err := cw.Write(row); err != nil {
					return fmt.Errorf("write CSV row: %w", err)
				}
			}
		}
	case map[string]interface{}:
		// Single object — key/value pairs
		for k, val := range v {
			if err := cw.Write([]string{k, csvValue(val)}); err != nil {
				return fmt.Errorf("write CSV row: %w", err)
			}
		}
	default:
		// Scalar — single cell
		if err := cw.Write([]string{csvValue(v)}); err != nil {
			return fmt.Errorf("write CSV cell: %w", err)
		}
	}
	return nil
}

// writeJSONL writes one JSON object per line.
func writeJSONL(w io.Writer, data interface{}) error {
	switch v := data.(type) {
	case []interface{}:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "") // compact
		for _, item := range v {
			if err := enc.Encode(item); err != nil {
				return fmt.Errorf("write JSONL: %w", err)
			}
		}
	default:
		// Non-array — write as a single JSON line
		enc := json.NewEncoder(w)
		if err := enc.Encode(v); err != nil {
			return fmt.Errorf("write JSONL: %w", err)
		}
	}
	return nil
}

// writeJSON writes pretty-printed JSON.
func writeJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// csvValue converts an arbitrary value to its CSV string representation.
// Nested objects/arrays are JSON-encoded within the cell.
func csvValue(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", val), "0"), ".")
	case bool:
		if val {
			return "true"
		}
		return "false"
	case map[string]interface{}, []interface{}:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// DetectFormat infers the output format from a file path extension.
// Returns the format name and whether auto-detection was successful.
// Supported: .csv, .jsonl, .parquet, .json, .yaml, .yml.
// Returns "" if the extension doesn't match a known format.
func DetectFormat(path string) string {
	ext := strings.ToLower(path)
	if idx := strings.LastIndex(ext, "."); idx >= 0 {
		ext = ext[idx:]
	}
	switch ext {
	case ".csv":
		return "csv"
	case ".jsonl":
		return "jsonl"
	case ".parquet":
		return "parquet"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return ""
	}
}
