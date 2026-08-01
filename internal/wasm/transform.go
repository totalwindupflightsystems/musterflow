// Package wasm provides WASM transform management for MusterFlow.
// Transforms are WebAssembly modules that process API data (PII redaction,
// reshaping, enrichment). Sandboxed via wazero with no network access by default.
package wasm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	musterwasm "github.com/wojons/muster/pkg/wasm"
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
		"WASM transform catalog integration is not yet implemented; "+
			"place .wasm files in %s to install transforms manually", r.dir)
}

// Run executes a WASM transform at transformPath with the given inputJSON.
// The module must follow the WASI stdin/stdout convention: it reads JSON from
// stdin and writes the transformed output to stdout. Execution is sandboxed by
// wazero via muster's pkg/wasm ModuleManager (default limits: 128MB memory,
// 30s timeout, no network). Returns the stdout output as a string.
func Run(transformPath string, inputJSON string) (string, error) {
	if _, err := os.Stat(transformPath); err != nil {
		return "", fmt.Errorf("wasm transform: module %q not found: %w", transformPath, err)
	}

	ctx := context.Background()
	mgr := musterwasm.NewModuleManager(nil)
	defer func() { _ = mgr.Close(ctx) }()

	lm, err := mgr.Load(ctx, transformPath)
	if err != nil {
		return "", fmt.Errorf("wasm transform: load module %q: %w", transformPath, err)
	}

	out, err := mgr.Transform(ctx, lm, []byte(inputJSON))
	if err != nil {
		return "", fmt.Errorf("wasm transform: execute module %q: %w", transformPath, err)
	}

	return string(out), nil
}
