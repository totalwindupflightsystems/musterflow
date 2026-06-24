package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ExportJSONL writes all API connections to a JSONL file (one JSON object per line).
func ExportJSONL(store *Store, path string) error {
	conns, err := store.List()
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, conn := range conns {
		if err := enc.Encode(conn); err != nil {
			return fmt.Errorf("encode: %w", err)
		}
	}
	return nil
}

// ImportJSONL reads a JSONL file and imports all connections into the store.
// Existing connections with the same ID are replaced.
func ImportJSONL(store *Store, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	count := 0
	for dec.More() {
		var conn APIConnection
		if err := dec.Decode(&conn); err != nil {
			return count, fmt.Errorf("decode line %d: %w", count+1, err)
		}
		if err := store.Add(&conn); err != nil {
			return count, fmt.Errorf("add %s: %w", conn.ID, err)
		}
		count++
	}
	return count, nil
}

// MigrateJSONToStore reads the legacy JSON registry file and imports all connections
// into the DuckDB store. Returns the number of migrated connections.
// Does nothing if the JSON file doesn't exist.
func MigrateJSONToStore(store *Store, dataDir string) (int, error) {
	jsonPath := filepath.Join(dataDir, "registry.json")
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		return 0, nil // nothing to migrate
	}

	conns, err := loadJSONRegistry(jsonPath)
	if err != nil {
		return 0, fmt.Errorf("load JSON registry: %w", err)
	}

	if len(conns) == 0 {
		return 0, nil
	}

	for _, conn := range conns {
		if err := store.Add(conn); err != nil {
			return 0, fmt.Errorf("migrate %s: %w", conn.ID, err)
		}
	}

	// Rename the old file so we don't migrate again
	backupPath := jsonPath + ".bak"
	if err := os.Rename(jsonPath, backupPath); err != nil {
		return len(conns), fmt.Errorf("backup old registry: %w", err)
	}

	return len(conns), nil
}

// loadJSONRegistry reads the legacy JSON registry file.
func loadJSONRegistry(path string) (map[string]*APIConnection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var conns map[string]*APIConnection
	if err := json.Unmarshal(data, &conns); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return conns, nil
}
