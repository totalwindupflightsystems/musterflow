// Package cli provides output format writers.
package cli

import (
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go"
)

// writeParquet writes data as a Parquet file to w.
//
// Supported data shapes:
//   - []interface{} where each element is map[string]interface{} (primary case)
//   - map[string]interface{} (single record)
//
// All columns are written as BYTE_ARRAY (string) for maximum compatibility
// with the JSON-derived data produced by MusterFlow API calls.
func writeParquet(w io.Writer, data interface{}) error {
	records, err := toRecords(data)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	// Convert to []interface{} for the shared collectKeys in execute.go
	items := make([]interface{}, len(records))
	for i, r := range records {
		items[i] = r
	}
	keys := collectKeys(items)
	if len(keys) == 0 {
		return nil
	}

	schema := buildParquetSchema(keys)
	writer := parquet.NewWriter(w, schema)
	defer func() { _ = writer.Close() }()

	for _, rec := range records {
		if err := writer.Write(rec); err != nil {
			return fmt.Errorf("write parquet row: %w", err)
		}
	}

	return writer.Close()
}

// toRecords converts data into a []map[string]interface{} slice.
func toRecords(data interface{}) ([]map[string]interface{}, error) {
	switch v := data.(type) {
	case []interface{}:
		records := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				records = append(records, m)
			}
		}
		return records, nil
	case map[string]interface{}:
		return []map[string]interface{}{v}, nil
	default:
		return nil, fmt.Errorf("unsupported data type %T for parquet output", data)
	}
}

// buildParquetSchema creates a parquet schema from column names.
// All columns use BYTE_ARRAY (string) for universal JSON compatibility.
func buildParquetSchema(keys []string) *parquet.Schema {
	nodes := make(parquet.Group, len(keys))
	for _, k := range keys {
		nodes[k] = parquet.String()
	}
	return parquet.NewSchema("record", nodes)
}
