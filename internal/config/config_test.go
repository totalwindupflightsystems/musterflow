package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Port != 9876 {
		t.Errorf("expected port 9876, got %d", cfg.Port)
	}
	if cfg.DefaultFormat != "table" {
		t.Errorf("expected default format 'table', got %q", cfg.DefaultFormat)
	}
	if cfg.AutoCompletion != true {
		t.Error("expected AutoCompletion=true")
	}
	if cfg.Auth == nil {
		t.Error("expected non-nil Auth map")
	}
	if cfg.DataDir == "" {
		t.Error("expected non-empty DataDir")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	// Use a temp home to isolate from real config
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on missing file should not error: %v", err)
	}
	if cfg.Port != 9876 {
		t.Errorf("expected default port 9876, got %d", cfg.Port)
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create musterflow dir and config
	cfgDir := filepath.Join(tmpDir, ".musterflow")
	_ = os.MkdirAll(cfgDir, 0755)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("port: 9999\ndefault_format: json\nauto_completion: false\n"), 0644)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9999 {
		t.Errorf("expected port 9999, got %d", cfg.Port)
	}
	if cfg.DefaultFormat != "json" {
		t.Errorf("expected json format, got %q", cfg.DefaultFormat)
	}
	if cfg.AutoCompletion != false {
		t.Error("expected AutoCompletion=false")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfgDir := filepath.Join(tmpDir, ".musterflow")
	_ = os.MkdirAll(cfgDir, 0755)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("::: this is not yaml :::"), 0644)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() on invalid YAML should fall back to defaults, not error: %v", err)
	}
	if cfg.Port != 9876 {
		t.Errorf("expected fallback to default port 9876, got %d", cfg.Port)
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	cfg := Config{
		Port:           8888,
		DataDir:        filepath.Join(tmpDir, ".musterflow"),
		DefaultFormat:  "jsonl",
		AutoCompletion: true,
		Auth: map[string]AuthConfig{
			"gh": {Type: "bearer", Key: "ghp_secret123456789"},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if loaded.Port != 8888 {
		t.Errorf("expected port 8888, got %d", loaded.Port)
	}
	if loaded.DefaultFormat != "jsonl" {
		t.Errorf("expected jsonl, got %q", loaded.DefaultFormat)
	}
	if loaded.Auth["gh"].Type != "bearer" {
		t.Errorf("expected auth type bearer, got %q", loaded.Auth["gh"].Type)
	}
}

func TestFindPort_Available(t *testing.T) {
	// Find a free base port dynamically — hardcoded ports (19876) collide
	// with sibling processes (hivemind sitrep server on 127.0.0.1:19876).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	base := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	port, err := FindPort(base)
	if err != nil {
		t.Fatalf("FindPort: %v", err)
	}
	if port != base {
		t.Errorf("expected %d, got %d", base, port)
	}
}

func TestFindPort_Occupied(t *testing.T) {
	// Occupy TWO dynamically-chosen ports (base and base+1) so FindPort
	// must skip both. The returned port must not be base or base+1, and
	// must be bindable at assertion time.
	//
	// The old test asserted port == base+1, which flaked under parallel
	// test load: another package could grab base+1 between FindPort's
	// bind-check and the assertion. By occupying two ports and only
	// asserting "not base, not base+1, and bindable", the test is
	// deterministic regardless of what other tests grab. See DF-014.
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind for test: %v", err)
	}
	defer func() { _ = ln1.Close() }()
	base := ln1.Addr().(*net.TCPAddr).Port

	// Bind base+1 as a second listener. If base+1 is unavailable (rare
	// under OS port allocation), skip — we need both occupied.
	ln2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base+1))
	if err != nil {
		t.Skipf("cannot bind base+1 for test: %v", err)
	}
	defer func() { _ = ln2.Close() }()

	port, err := FindPort(base)
	if err != nil {
		t.Fatalf("FindPort: %v", err)
	}

	// The returned port must not be either of the occupied ports.
	if port == base || port == base+1 {
		t.Errorf("FindPort(%d) = %d, expected a port != %d and != %d", base, port, base, base+1)
	}

	// The returned port must actually be bindable at assertion time,
	// proving FindPort returned a genuinely free port.
	ln3, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Errorf("FindPort(%d) returned %d which is not bindable: %v", base, port, err)
	} else {
		_ = ln3.Close()
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"sk-1234567890abcdef", "sk-1****cdef"},
		{"short", "****"},
		{"ab", "****"},
		{"", "****"},
		{"12345678", "****"},
		{"123456789", "1234****6789"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := MaskKey(tt.key)
			if got != tt.expected {
				t.Errorf("MaskKey(%q) = %q, want %q", tt.key, got, tt.expected)
			}
		})
	}
}

func TestConfigPath(t *testing.T) {
	path := ConfigPath()
	if path == "" {
		t.Error("expected non-empty config path")
	}
}

func TestLoadWithDataDir_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := LoadWithDataDir(tmpDir)
	if err != nil {
		t.Fatalf("LoadWithDataDir on missing file should not error: %v", err)
	}
	if cfg.Port != 9876 {
		t.Errorf("expected default port 9876, got %d", cfg.Port)
	}
	if cfg.DataDir != tmpDir {
		t.Errorf("expected DataDir=%s, got %q", tmpDir, cfg.DataDir)
	}
}

func TestSaveAndLoadWithDataDir(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Port:           7777,
		DataDir:        tmpDir,
		DefaultFormat:  "jsonl",
		AutoCompletion: false,
		Auth: map[string]AuthConfig{
			"testapi": {Type: "apikey", Key: "sk-test1234567890"},
		},
	}

	if err := SaveWithDataDir(cfg, tmpDir); err != nil {
		t.Fatalf("SaveWithDataDir: %v", err)
	}

	// Verify the file landed in <tmp>/config.yaml
	path := filepath.Join(tmpDir, "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected config file at %s", path)
	}

	// Verify ConfigPathFor matches
	if got := ConfigPathFor(tmpDir); got != path {
		t.Errorf("ConfigPathFor(%q) = %q, want %q", tmpDir, got, path)
	}

	// Load it back
	loaded, err := LoadWithDataDir(tmpDir)
	if err != nil {
		t.Fatalf("LoadWithDataDir: %v", err)
	}
	if loaded.Port != 7777 {
		t.Errorf("expected port 7777, got %d", loaded.Port)
	}
	if loaded.DefaultFormat != "jsonl" {
		t.Errorf("expected jsonl format, got %q", loaded.DefaultFormat)
	}
	if loaded.Auth["testapi"].Type != "apikey" {
		t.Errorf("expected auth type apikey, got %q", loaded.Auth["testapi"].Type)
	}
	if loaded.Auth["testapi"].Key != "sk-test1234567890" {
		t.Errorf("expected auth key sk-test1234567890, got %q", loaded.Auth["testapi"].Key)
	}
}

func TestSave_HonorsDataDir(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Port:           6666,
		DataDir:        tmpDir,
		DefaultFormat:  "json",
		AutoCompletion: true,
		Auth:           make(map[string]AuthConfig),
	}

	// Save() should write to cfg.DataDir (tmpDir), not home
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(tmpDir, "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected Save() to write to %s", path)
	}

	// Load back with LoadWithDataDir to confirm
	loaded, err := LoadWithDataDir(tmpDir)
	if err != nil {
		t.Fatalf("LoadWithDataDir: %v", err)
	}
	if loaded.Port != 6666 {
		t.Errorf("expected port 6666, got %d", loaded.Port)
	}
}
