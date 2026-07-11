package config

import (
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
	port, err := FindPort(19876)
	if err != nil {
		t.Fatalf("FindPort: %v", err)
	}
	if port != 19876 {
		t.Errorf("expected 19876, got %d", port)
	}
}

func TestFindPort_Occupied(t *testing.T) {
	// Occupy 19877
	ln, err := net.Listen("tcp", ":19877")
	if err != nil {
		t.Skipf("cannot bind for test: %v", err)
	}
	defer ln.Close()

	port, err := FindPort(19877)
	if err != nil {
		t.Fatalf("FindPort: %v", err)
	}
	if port != 19878 {
		t.Errorf("expected next port 19878, got %d", port)
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
