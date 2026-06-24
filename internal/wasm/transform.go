// Package wasm provides WASM transform management for MusterFlow.
// Transforms are WebAssembly modules that process API data (PII redaction,
// reshaping, enrichment). Sandboxed via wazero with no network access by default.
package wasm

import (
	"fmt"
	"os"
	"path/filepath"
)

// Transform represents an installed WASM transform.
type Transform struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Hash        string `json:"hash"`
	Path        string `json:"path"`
}

// Registry manages installed WASM transforms.
type Registry struct {
	dir string
}

// NewRegistry creates a transform registry at the given directory.
func NewRegistry(dir string) *Registry {
	return &Registry{dir: dir}
}

// List returns all installed transforms.
func (r *Registry) List() ([]Transform, error) {
	if err := os.MkdirAll(r.dir, 0755); err != nil {
		return nil, fmt.Errorf("create transform dir: %w", err)
	}

	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, fmt.Errorf("read transform dir: %w", err)
	}

	var transforms []Transform
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".wasm" {
			continue
		}
		transforms = append(transforms, Transform{
			Name: entry.Name(),
			Path: filepath.Join(r.dir, entry.Name()),
		})
	}
	if transforms == nil {
		transforms = []Transform{}
	}
	return transforms, nil
}

// InstallFromCatalog downloads and installs a transform from the catalog.
// This is a placeholder — full catalog integration in Phase 2.
func (r *Registry) InstallFromCatalog(entryID string) error {
	return fmt.Errorf(
		"WASM transform catalog integration is not yet implemented. "+
			"Place .wasm files in %s to install transforms manually.", r.dir)
}

// Run executes a transform with the given input JSON.
// This is a placeholder — full sandboxed execution in Phase 2.
// Uses muster's pkg/wasm runtime (wazero, pure Go, no CGO).
func Run(transformPath string, inputJSON string) (string, error) {
	return "", fmt.Errorf(
		"WASM transform execution is not yet compiled in. "+
			"This is a Phase 2 feature. Transform path: %s", transformPath)
}
