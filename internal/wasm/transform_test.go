package wasm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	if reg == nil {
		t.Fatal("expected non-nil Registry")
	}
	if reg.dir != dir {
		t.Errorf("expected dir=%q, got %q", dir, reg.dir)
	}
}

func TestList_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	transforms, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(transforms) != 0 {
		t.Errorf("expected 0 transforms, got %d", len(transforms))
	}
}

func TestList_CreatesDirectory(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "transforms")
	reg := NewRegistry(dir)

	// Directory doesn't exist yet
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir to not exist yet")
	}

	_, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Directory should now exist
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("expected dir to exist after List()")
	}
}

func TestList_WithWasmFiles(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	// Create some .wasm files
	wasm1 := filepath.Join(dir, "redact.wasm")
	wasm2 := filepath.Join(dir, "enrich.wasm")
	if err := os.WriteFile(wasm1, []byte("fake-wasm"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wasm2, []byte("fake-wasm"), 0644); err != nil {
		t.Fatal(err)
	}

	transforms, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(transforms) != 2 {
		t.Fatalf("expected 2 transforms, got %d", len(transforms))
	}

	names := make(map[string]bool)
	for _, tr := range transforms {
		names[tr.Name] = true
	}
	if !names["redact.wasm"] {
		t.Error("expected redact.wasm in transforms")
	}
	if !names["enrich.wasm"] {
		t.Error("expected enrich.wasm in transforms")
	}
}

func TestList_FiltersNonWasmFiles(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	// Create a mix of files
	os.WriteFile(filepath.Join(dir, "valid.wasm"), []byte("wasm"), 0644)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("docs"), 0644)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("config"), 0644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)

	transforms, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(transforms) != 1 {
		t.Errorf("expected 1 transform (only .wasm), got %d", len(transforms))
	}
	if len(transforms) > 0 && transforms[0].Name != "valid.wasm" {
		t.Errorf("expected valid.wasm, got %q", transforms[0].Name)
	}
}

func TestList_ReturnsCorrectPaths(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	wasmFile := filepath.Join(dir, "test.wasm")
	os.WriteFile(wasmFile, []byte("fake"), 0644)

	transforms, err := reg.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(transforms) != 1 {
		t.Fatalf("expected 1 transform, got %d", len(transforms))
	}
	if transforms[0].Path != wasmFile {
		t.Errorf("expected path=%q, got %q", wasmFile, transforms[0].Path)
	}
}

func TestInstallFromCatalog_NotImplemented(t *testing.T) {
	reg := NewRegistry(t.TempDir())
	err := reg.InstallFromCatalog("some-entry")
	if err == nil {
		t.Error("expected error (not yet implemented)")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("expected 'not yet implemented' in error, got: %v", err)
	}
}

func TestRun_NotImplemented(t *testing.T) {
	output, err := Run("/some/path.wasm", `{"key":"value"}`)
	if err == nil {
		t.Error("expected error (not yet implemented)")
	}
	if output != "" {
		t.Errorf("expected empty output string, got %q", output)
	}
	if !strings.Contains(err.Error(), "not yet compiled") {
		t.Errorf("expected 'not yet compiled' in error, got: %v", err)
	}
}
