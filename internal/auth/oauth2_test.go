package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wojons/muster/pkg/auth"
)

func TestNewYAMLTokenStore(t *testing.T) {
	dir := t.TempDir()
	store := NewYAMLTokenStore(dir)

	if store == nil {
		t.Fatal("NewYAMLTokenStore returned nil")
	}
	if store.Dir != dir {
		t.Errorf("Dir = %q, want %q", store.Dir, dir)
	}
}

func TestYAMLTokenStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewYAMLTokenStore(dir)

	expiresAt := time.Now().Add(1 * time.Hour).Truncate(time.Second)
	token := &auth.Token{
		AccessToken:  "access-token-abc123",
		RefreshToken: "refresh-token-xyz789",
		TokenType:    "Bearer",
		ExpiresAt:    expiresAt,
	}

	if err := store.Save("github", token); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load("github")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.AccessToken != token.AccessToken {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, token.AccessToken)
	}
	if loaded.RefreshToken != token.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", loaded.RefreshToken, token.RefreshToken)
	}
	if loaded.TokenType != token.TokenType {
		t.Errorf("TokenType = %q, want %q", loaded.TokenType, token.TokenType)
	}
	if !loaded.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", loaded.ExpiresAt, expiresAt)
	}
}

func TestYAMLTokenStore_SaveOverwrite(t *testing.T) {
	dir := t.TempDir()
	store := NewYAMLTokenStore(dir)

	// Save first token
	token1 := &auth.Token{
		AccessToken:  "old-token",
		RefreshToken: "old-refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Truncate(time.Second),
	}
	if err := store.Save("service", token1); err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	// Overwrite
	token2 := &auth.Token{
		AccessToken:  "new-token",
		RefreshToken: "new-refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(2 * time.Hour).Truncate(time.Second),
	}
	if err := store.Save("service", token2); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	loaded, err := store.Load("service")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AccessToken != "new-token" {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, "new-token")
	}
}

func TestYAMLTokenStore_LoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	store := NewYAMLTokenStore(dir)

	_, err := store.Load("nonexistent-service")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
	if !strings.Contains(err.Error(), "token not found") {
		t.Errorf("error = %q, want containing 'token not found'", err.Error())
	}
}

func TestYAMLTokenStore_SaveCreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "tokens")
	store := NewYAMLTokenStore(dir)

	token := &auth.Token{
		AccessToken: "tok",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	if err := store.Save("api", token); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify directory was created
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}

	// Verify file exists
	tokenFile := filepath.Join(dir, "api.yaml")
	if _, err := os.Stat(tokenFile); err != nil {
		t.Errorf("token file not created: %v", err)
	}
}

func TestYAMLTokenStore_SaveEmptyService(t *testing.T) {
	dir := t.TempDir()
	store := NewYAMLTokenStore(dir)

	token := &auth.Token{
		AccessToken: "tok",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	err := store.Save("", token)
	if err != nil {
		t.Errorf("Save with empty service: %v (should be allowed, creates .yaml file)", err)
	}

	// Should be able to load it back
	loaded, err := store.Load("")
	if err != nil {
		t.Fatalf("Load empty service: %v", err)
	}
	if loaded.AccessToken != "tok" {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, "tok")
	}
}

func TestYAMLTokenStore_LoadFileNotYAML(t *testing.T) {
	dir := t.TempDir()
	store := NewYAMLTokenStore(dir)

	// Write a non-YAML file
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("not valid yaml: [[["), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := store.Load("bad")
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestOpenBrowser_Linux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("OpenBrowser only tested on Linux")
	}

	// xdg-open should exist on any real Linux system
	// but if it somehow doesn't, Skip rather than Fail
	if _, err := os.Stat("/usr/bin/xdg-open"); os.IsNotExist(err) {
		t.Skip("xdg-open not found — skipping OpenBrowser test")
	}

	err := OpenBrowser("https://example.com")
	if err != nil {
		t.Errorf("OpenBrowser failed on Linux: %v", err)
	}
}

func TestYAMLTokenStore_SaveMultipleServices(t *testing.T) {
	dir := t.TempDir()
	store := NewYAMLTokenStore(dir)

	services := []struct {
		name  string
		token *auth.Token
	}{
		{"github", &auth.Token{AccessToken: "gh-tok", TokenType: "Bearer", ExpiresAt: time.Now().Add(1 * time.Hour)}},
		{"stripe", &auth.Token{AccessToken: "sk-tok", TokenType: "Bearer", ExpiresAt: time.Now().Add(2 * time.Hour)}},
		{"slack", &auth.Token{AccessToken: "sl-tok", TokenType: "Bot", ExpiresAt: time.Now().Add(3 * time.Hour)}},
	}

	for _, svc := range services {
		if err := store.Save(svc.name, svc.token); err != nil {
			t.Fatalf("Save %s: %v", svc.name, err)
		}
	}

	for _, svc := range services {
		loaded, err := store.Load(svc.name)
		if err != nil {
			t.Fatalf("Load %s: %v", svc.name, err)
		}
		if loaded.AccessToken != svc.token.AccessToken {
			t.Errorf("%s AccessToken = %q, want %q", svc.name, loaded.AccessToken, svc.token.AccessToken)
		}
	}
}
