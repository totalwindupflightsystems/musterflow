// Package cli provides output format writers.
package cli

import (
	"encoding/json"
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
// Column physical types are inferred from the Go values in the records: int64
// values map to INT64, float64 to DOUBLE, bool to BOOLEAN, string to STRING.
// Columns that are absent, nil, or have mixed types fall back to STRING
// (BYTE_ARRAY) for JSON compatibility.
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

	// Infer per-column physical types from the record values so that numeric
	// and boolean data round-trips with proper INT64/DOUBLE/BOOLEAN types
	// instead of everything being untyped BYTE_ARRAY strings.
	typeMap := inferColumnTypes(records, keys)
	schema := buildParquetSchema(keys, typeMap)
	writer := parquet.NewWriter(w, schema)
	defer func() { _ = writer.Close() }()

	for _, rec := range records {
		// Normalize record values to match the schema leaf types so that
		// parquet-go's reflection-based writer does not reject type
		// mismatches (e.g. json.Number where INT64 is expected).
		normalized := normalizeRecord(rec, typeMap)
		if err := writer.Write(normalized); err != nil {
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

// parquetType is an enumerated type identifying the inferred physical type
// for a parquet column.
type parquetType int

const (
	parquetTypeString parquetType = iota
	parquetTypeInt64
	parquetTypeDouble
	parquetTypeBoolean
)

// inferColumnTypes scans the records and determines the physical parquet type
// for each key.  Keys that never have a non-nil value default to STRING.
// Columns where non-nil values have inconsistent Go types fall back to STRING
// (JSON compatibility) so that a mixed-type column does not panic parquet-go's
// writer at runtime.
func inferColumnTypes(records []map[string]interface{}, keys []string) map[string]parquetType {
	types := make(map[string]parquetType, len(keys))
	for _, k := range keys {
		types[k] = parquetTypeString // default
		var firstType parquetType
		seen := false
		consistent := true
		for _, rec := range records {
			val, ok := rec[k]
			if !ok || val == nil {
				continue
			}
			vt := valueTypeOf(val)
			if !seen {
				firstType = vt
				seen = true
			} else if vt != firstType {
				consistent = false
			}
		}
		if seen && consistent {
			types[k] = firstType
		}
	}
	return types
}

// valueTypeOf maps a Go value to its inferred parquet type category.
func valueTypeOf(v interface{}) parquetType {
	switch n := v.(type) {
	case json.Number:
		if _, err := n.Int64(); err == nil {
			return parquetTypeInt64
		}
		return parquetTypeDouble
	case int:
		return parquetTypeInt64
	case int64:
		return parquetTypeInt64
	case float64:
		return parquetTypeDouble
	case bool:
		return parquetTypeBoolean
	default:
		return parquetTypeString
	}
}

// buildParquetSchema creates a parquet schema from column names and inferred
// physical types.
func buildParquetSchema(keys []string, typeMap map[string]parquetType) *parquet.Schema {
	nodes := make(parquet.Group, len(keys))
	for _, k := range keys {
		switch typeMap[k] {
		case parquetTypeInt64:
			nodes[k] = parquet.Int(64)
		case parquetTypeDouble:
			nodes[k] = parquet.Leaf(parquet.DoubleType)
		case parquetTypeBoolean:
			nodes[k] = parquet.Leaf(parquet.BooleanType)
		default:
			nodes[k] = parquet.String()
		}
	}
	return parquet.NewSchema("record", nodes)
}

// normalizeRecord returns a copy of rec with values coerced to the Go types
// expected by the inferred parquet schema, so that parquet-go's reflection-based
// writer does not reject type mismatches (e.g. json.Number for an INT64
// column, or int for INT64).
func normalizeRecord(rec map[string]interface{}, typeMap map[string]parquetType) map[string]interface{} {
	out := make(map[string]interface{}, len(rec))
	for k, v := range rec {
		out[k] = normalizeValue(v, typeMap[k])
	}
	return out
}

func normalizeValue(v interface{}, pt parquetType) interface{} {
	if v == nil {
		return nil
	}
	switch pt {
	case parquetTypeInt64:
		switch n := v.(type) {
		case json.Number:
			i, err := n.Int64()
			if err == nil {
				return i
			}
			// Not integral — fall back to string to avoid data loss.
			return n.String()
		case int:
			return int64(n)
		case int64:
			return v
		case float64:
			// Integral float from JSON decode without UseNumber.
			return int64(n)
		default:
			return v
		}
	case parquetTypeDouble:
		switch n := v.(type) {
		case json.Number:
			f, err := n.Float64()
			if err == nil {
				return f
			}
			return n.String()
		case float64:
			return v
		case int:
			return float64(n)
		case int64:
			return float64(n)
		default:
			return v
		}
	case parquetTypeBoolean:
		return v
	default:
		// String column: convert non-string values to their string form so
		// the parquet String() leaf does not reject them.
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
}
