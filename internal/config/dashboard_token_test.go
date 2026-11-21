package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- dashboard_token: file key + MUSTERFLOW_DASHBOARD_TOKEN override ---

func writeConfigFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestDashboardTokenFromFile(t *testing.T) {
	t.Setenv(DashboardTokenEnvVar, "")
	dir := t.TempDir()
	writeConfigFile(t, dir, "port: 9876\ndashboard_token: file-token-0123456789\n")

	cfg, err := LoadWithDataDir(dir)
	if err != nil {
		t.Fatalf("LoadWithDataDir: %v", err)
	}
	if cfg.DashboardToken != "file-token-0123456789" {
		t.Fatalf("DashboardToken = %q, want the config file value", cfg.DashboardToken)
	}
}

func TestDashboardTokenEnvWinsOverFile(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "port: 9876\ndashboard_token: file-token-0123456789\n")
	t.Setenv(DashboardTokenEnvVar, "env-token-0123456789")

	cfg, err := LoadWithDataDir(dir)
	if err != nil {
		t.Fatalf("LoadWithDataDir: %v", err)
	}
	if cfg.DashboardToken != "env-token-0123456789" {
		t.Fatalf("DashboardToken = %q, want the environment value to win", cfg.DashboardToken)
	}
}

func TestDashboardTokenEnvWithNoConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DashboardTokenEnvVar, "env-token-0123456789")

	cfg, err := LoadWithDataDir(dir)
	if err != nil {
		t.Fatalf("LoadWithDataDir: %v", err)
	}
	if cfg.DashboardToken != "env-token-0123456789" {
		t.Fatalf("DashboardToken = %q, want the environment value", cfg.DashboardToken)
	}
}

func TestDashboardTokenEmptyByDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DashboardTokenEnvVar, "")

	cfg, err := LoadWithDataDir(dir)
	if err != nil {
		t.Fatalf("LoadWithDataDir: %v", err)
	}
	if cfg.DashboardToken != "" {
		t.Fatalf("DashboardToken = %q, want empty (auth disabled by default on loopback)", cfg.DashboardToken)
	}
}

func TestDashboardTokenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DashboardTokenEnvVar, "")

	cfg := Defaults()
	cfg.DataDir = dir
	cfg.DashboardToken = "round-trip-token-0123456789"
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), "dashboard_token:") {
		t.Fatalf("saved config does not carry dashboard_token:\n%s", raw)
	}
	if info, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("stat config: %v", err)
	} else if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("config mode = %04o, want 0600 (it holds a bearer token)", perm)
	}

	got, err := LoadWithDataDir(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.DashboardToken != cfg.DashboardToken {
		t.Fatalf("reloaded DashboardToken = %q, want %q", got.DashboardToken, cfg.DashboardToken)
	}
}

// An operator who exports MUSTERFLOW_DASHBOARD_TOKEN means "keep this out of
// config.yaml"; an unrelated `config set port` (which saves the whole config)
// must not write it to disk.
func TestDashboardTokenFromEnvIsNotPersistedOnSave(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "port: 9876\n")
	t.Setenv(DashboardTokenEnvVar, "env-only-token-0123456789")

	cfg, err := LoadWithDataDir(dir)
	if err != nil {
		t.Fatalf("LoadWithDataDir: %v", err)
	}
	cfg.Port = 9999
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "dashboard_token") {
		t.Fatalf("env-supplied token was persisted to disk:\n%s", raw)
	}
	if !strings.Contains(string(raw), "9999") {
		t.Fatalf("the unrelated setting was not saved:\n%s", raw)
	}
}

// A token that already lives in the file survives a save while the environment
// overrides it in memory.
func TestDashboardTokenFileValueSurvivesSaveUnderEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "port: 9876\ndashboard_token: file-token-0123456789\n")
	t.Setenv(DashboardTokenEnvVar, "env-token-0123456789")

	cfg, err := LoadWithDataDir(dir)
	if err != nil {
		t.Fatalf("LoadWithDataDir: %v", err)
	}
	if cfg.DashboardToken != "env-token-0123456789" {
		t.Fatalf("in-memory token = %q, want the env value", cfg.DashboardToken)
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), "file-token-0123456789") {
		t.Fatalf("the file's own token was dropped on save:\n%s", raw)
	}
	if strings.Contains(string(raw), "env-token-0123456789") {
		t.Fatalf("the env token was written to disk:\n%s", raw)
	}
}

// An explicit SetDashboardToken (what `config set dashboard_token` calls) is a
// request to persist it, even with the environment set.
func TestSetDashboardTokenPersistsUnderEnvOverride(t *testing.T) {
	dir := t.TempDir()
	writeConfigFile(t, dir, "port: 9876\n")
	t.Setenv(DashboardTokenEnvVar, "env-token-0123456789")

	cfg, err := LoadWithDataDir(dir)
	if err != nil {
		t.Fatalf("LoadWithDataDir: %v", err)
	}
	cfg.SetDashboardToken("explicit-token-0123456789")
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), "explicit-token-0123456789") {
		t.Fatalf("config set dashboard_token did not persist:\n%s", raw)
	}
}

func TestDashboardTokenOmittedFromFileWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DashboardTokenEnvVar, "")

	cfg := Defaults()
	cfg.DataDir = dir
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "dashboard_token") {
		t.Fatalf("empty dashboard_token was written to disk:\n%s", raw)
	}
}

// MaskKey is the helper the display paths use for both auth keys and the
// dashboard token; pin that it never returns the raw secret.
func TestMaskKeyNeverReturnsFullToken(t *testing.T) {
	for _, tok := range []string{"short", "0123456789", "s3cr3t-dashboard-token-0123456789"} {
		masked := MaskKey(tok)
		if masked == tok {
			t.Errorf("MaskKey(%q) returned the raw value", tok)
		}
		if strings.Contains(masked, tok) {
			t.Errorf("MaskKey(%q) = %q contains the raw value", tok, masked)
		}
	}
}
