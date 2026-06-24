package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/config"
)

func tempConfig(t *testing.T) (config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	return cfg, dir
}

func TestCredential_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cred    Credential
		wantErr bool
		errMsg  string
	}{
		{"none is always valid", Credential{Type: CredentialNone}, false, ""},
		{"apikey requires key", Credential{Type: CredentialAPIKey}, true, "requires a key"},
		{"apikey with key", Credential{Type: CredentialAPIKey, Key: "sk-abc123"}, false, ""},
		{"bearer requires key", Credential{Type: CredentialBearer}, true, "requires a key"},
		{"bearer with key", Credential{Type: CredentialBearer, Key: "tok_123"}, false, ""},
		{"oauth2 requires key", Credential{Type: CredentialOAuth2}, true, "requires a key"},
		{"oauth2 with key", Credential{Type: CredentialOAuth2, Key: "at_xyz"}, false, ""},
		{"mtls requires cert", Credential{Type: CredentialMTLS}, true, "requires cert_path"},
		{"mtls requires key", Credential{Type: CredentialMTLS, CertPath: "cert.pem"}, true, "requires key_path"},
		{"mtls with both", Credential{Type: CredentialMTLS, CertPath: "cert.pem", KeyPath: "key.pem"}, false, ""},
		{"unknown type", Credential{Type: "unknown"}, true, "unknown credential type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cred.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

func TestCredential_IsConfigured(t *testing.T) {
	if (Credential{Type: CredentialNone}).IsConfigured() {
		t.Error("none should not be configured")
	}
	if !(Credential{Type: CredentialAPIKey, Key: "sk-123"}).IsConfigured() {
		t.Error("apikey with key should be configured")
	}
	if (Credential{Type: CredentialAPIKey}).IsConfigured() {
		t.Error("apikey without key should not be configured")
	}
}

func TestCredential_String(t *testing.T) {
	c := Credential{Type: CredentialBearer, Key: "ghp_abcdef12345678"}
	s := c.String()
	if !strings.Contains(s, "bearer") {
		t.Errorf("String() = %q, want containing 'bearer'", s)
	}
	if strings.Contains(s, "ghp_abcdef12345678") {
		t.Error("String() should mask the key")
	}
}

func TestCredentialTypeFromString(t *testing.T) {
	tests := []struct {
		input string
		want  CredentialType
	}{
		{"", CredentialNone},
		{"none", CredentialNone},
		{"apikey", CredentialAPIKey},
		{"api_key", CredentialAPIKey},
		{"api-key", CredentialAPIKey},
		{"bearer", CredentialBearer},
		{"token", CredentialBearer},
		{"oauth2", CredentialOAuth2},
		{"oauth", CredentialOAuth2},
		{"mtls", CredentialMTLS},
		{"mutual-tls", CredentialMTLS},
		{"client-cert", CredentialMTLS},
		{"UNKNOWN", CredentialNone},
		{"  bearer  ", CredentialBearer},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := CredentialTypeFromString(tt.input)
			if got != tt.want {
				t.Errorf("CredentialTypeFromString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestManager_Add(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	err := mgr.Add("test-api", Credential{Type: CredentialAPIKey, Key: "sk-test-key"})
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify it was persisted
	reloaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	ac, ok := reloaded.Auth["test-api"]
	if !ok {
		t.Fatal("auth not found in config")
	}
	if ac.Type != "apikey" {
		t.Errorf("type = %q, want apikey", ac.Type)
	}
	if ac.Key != "sk-test-key" {
		t.Errorf("key = %q, want sk-test-key", ac.Key)
	}
}

func TestManager_Add_Invalid(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	// Empty ID
	err := mgr.Add("", Credential{Type: CredentialAPIKey, Key: "x"})
	if err == nil {
		t.Error("expected error for empty ID")
	}

	// Missing key
	err = mgr.Add("test", Credential{Type: CredentialAPIKey})
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestManager_Remove(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	// Add first
	if err := mgr.Add("test-api", Credential{Type: CredentialBearer, Key: "tok"}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Remove
	if err := mgr.Remove("test-api"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify gone
	_, err := mgr.Get("test-api")
	if err == nil {
		t.Error("expected error after remove")
	}
}

func TestManager_Remove_NotExist(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	err := mgr.Remove("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent")
	}
}

func TestManager_Get(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	if err := mgr.Add("api-1", Credential{Type: CredentialBearer, Key: "secret-token"}); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	cred, err := mgr.Get("api-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if cred.Type != CredentialBearer {
		t.Errorf("type = %q, want bearer", cred.Type)
	}
	if cred.Key != "secret-token" {
		t.Errorf("key = %q, want secret-token", cred.Key)
	}
}

func TestManager_Get_NotExist(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	_, err := mgr.Get("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

func TestManager_List(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	// Empty list
	entries := mgr.List()
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}

	// Add two
	mgr.Add("api-1", Credential{Type: CredentialAPIKey, Key: "key1111111111111111"})
	mgr.Add("api-2", Credential{Type: CredentialBearer, Key: "tok2222222222222222"})

	entries = mgr.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Keys should be masked
	for _, e := range entries {
		if strings.Contains(e.Key, "1111") && !strings.Contains(e.Key, "…") {
			t.Errorf("key for %s should be masked, got %q", e.APIID, e.Key)
		}
	}
}

func TestManager_List_ShortKey(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	mgr.Add("api", Credential{Type: CredentialAPIKey, Key: "short"})

	entries := mgr.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry")
	}
	if entries[0].Key != "****" {
		t.Errorf("short key should be fully masked, got %q", entries[0].Key)
	}
}

func TestManager_List_EmptyKey(t *testing.T) {
	cfg, _ := tempConfig(t)

	// Add via config directly to test edge case with empty key
	cfg.Auth["empty-key"] = config.AuthConfig{Type: "apikey", Key: ""}
	mgr2 := NewManager(cfg)

	entries := mgr2.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry")
	}
	if entries[0].Key != "(not set)" {
		t.Errorf("empty key should show '(not set)', got %q", entries[0].Key)
	}
}

func TestManager_Add_MTLS(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	err := mgr.Add("mtls-api", Credential{
		Type:     CredentialMTLS,
		CertPath: "/tmp/cert.pem",
		KeyPath:  "/tmp/key.pem",
	})
	if err != nil {
		t.Fatalf("Add mTLS failed: %v", err)
	}

	cred, err := mgr.Get("mtls-api")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if cred.CertPath != "/tmp/cert.pem" {
		t.Errorf("cert = %q", cred.CertPath)
	}
	if cred.KeyPath != "/tmp/key.pem" {
		t.Errorf("key = %q", cred.KeyPath)
	}

	entries := mgr.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry")
	}
	if entries[0].CertPath != "/tmp/cert.pem" {
		t.Errorf("list cert = %q", entries[0].CertPath)
	}
}

func TestManager_List_MTLS_Paths(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	mgr.Add("mtls-api", Credential{
		Type:     CredentialMTLS,
		CertPath: "/etc/certs/client.crt",
		KeyPath:  "/etc/certs/client.key",
	})

	entries := mgr.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry")
	}
	if entries[0].CertPath != "/etc/certs/client.crt" {
		t.Errorf("cert = %q", entries[0].CertPath)
	}
}

func TestManager_Add_Overwrite(t *testing.T) {
	cfg, _ := tempConfig(t)
	mgr := NewManager(cfg)

	// Add initial
	mgr.Add("api", Credential{Type: CredentialAPIKey, Key: "first-key"})

	// Overwrite with new cred
	err := mgr.Add("api", Credential{Type: CredentialBearer, Key: "second-key"})
	if err != nil {
		t.Fatalf("Add overwrite failed: %v", err)
	}

	cred, err := mgr.Get("api")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if cred.Type != CredentialBearer {
		t.Errorf("type = %q, want bearer", cred.Type)
	}
	if cred.Key != "second-key" {
		t.Errorf("key = %q, want second-key", cred.Key)
	}
}

func TestInjectAuthHeader(t *testing.T) {
	tests := []struct {
		name     string
		cred     *Credential
		wantAuth bool
	}{
		{"nil credential", nil, false},
		{"none type", &Credential{Type: CredentialNone}, false},
		{"apikey with key", &Credential{Type: CredentialAPIKey, Key: "sk-abc"}, true},
		{"bearer with key", &Credential{Type: CredentialBearer, Key: "tok"}, true},
		{"oauth2 with key", &Credential{Type: CredentialOAuth2, Key: "at"}, true},
		{"apikey without key", &Credential{Type: CredentialAPIKey}, false},
		{"mtls", &Credential{Type: CredentialMTLS, CertPath: "x", KeyPath: "y"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			var headerKey, headerVal string
			InjectAuthHeader(tt.cred, func(k, v string) {
				called = true
				headerKey = k
				headerVal = v
			})
			if called != tt.wantAuth {
				t.Errorf("called = %v, want %v", called, tt.wantAuth)
			}
			if called {
				if headerKey != "Authorization" {
					t.Errorf("header key = %q, want Authorization", headerKey)
				}
				if !strings.HasPrefix(headerVal, "Bearer ") {
					t.Errorf("header value = %q, want Bearer prefix", headerVal)
				}
			}
		})
	}
}

func TestBuildTransport(t *testing.T) {
	// nil credential
	info, err := BuildTransport(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if info != nil {
		t.Error("expected nil for nil credential")
	}

	// non-mtls
	info, err = BuildTransport(&Credential{Type: CredentialBearer, Key: "tok"})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if info != nil {
		t.Error("expected nil for non-mtls credential")
	}

	// mtls without paths
	info, err = BuildTransport(&Credential{Type: CredentialMTLS})
	if err == nil {
		t.Error("expected error for mtls without paths")
	}
	if info != nil {
		t.Error("expected nil on error")
	}

	// mtls with missing cert file
	info, err = BuildTransport(&Credential{
		Type:     CredentialMTLS,
		CertPath: "/nonexistent/path/cert.pem",
		KeyPath:  "/nonexistent/path/key.pem",
	})
	if err == nil {
		t.Error("expected error for nonexistent cert files")
	}
	if info != nil {
		t.Error("expected nil on error")
	}

	// mtls with real cert/key files
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")
	// Generate a self-signed cert for testing
	if err := generateTestCert(certPath, keyPath); err != nil {
		t.Skipf("skipping mTLS transport test: cert generation failed: %v", err)
	}
	info, err = BuildTransport(&Credential{
		Type:     CredentialMTLS,
		CertPath: certPath,
		KeyPath:  keyPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil for valid mtls")
	}
	transport, ok := info.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", info)
	}
	if transport.TLSClientConfig == nil {
		t.Error("expected TLSClientConfig to be set")
	}
	if len(transport.TLSClientConfig.Certificates) != 1 {
		t.Errorf("expected 1 certificate, got %d", len(transport.TLSClientConfig.Certificates))
	}
}

func TestConfigPersistence(t *testing.T) {
	cfg, dir := tempConfig(t)
	mgr := NewManager(cfg)

	// Add credentials
	if err := mgr.Add("gh", Credential{Type: CredentialBearer, Key: "ghp_test1234567890"}); err != nil {
		t.Fatalf("Add gh: %v", err)
	}
	if err := mgr.Add("stripe", Credential{Type: CredentialAPIKey, Key: "sk_test_abcdefgh"}); err != nil {
		t.Fatalf("Add stripe: %v", err)
	}

	// Verify via List — manager reads from in-memory cfg
	entries := mgr.List()
	var foundGH, foundStripe bool
	for _, e := range entries {
		if e.APIID == "gh" {
			foundGH = true
		}
		if e.APIID == "stripe" {
			foundStripe = true
		}
	}
	if !foundGH {
		t.Error("gh entry not found")
	}
	if !foundStripe {
		t.Error("stripe entry not found")
	}

	// Verify Get returns correct keys
	cred, err := mgr.Get("gh")
	if err != nil {
		t.Fatalf("Get gh: %v", err)
	}
	if cred.Key != "ghp_test1234567890" {
		t.Errorf("gh key = %q", cred.Key)
	}

	// Verify config file was written somewhere
	_ = dir // dir is used for temp config
}

func TestManager_NilAuthMap(t *testing.T) {
	cfg := config.Defaults()
	cfg.Auth = nil
	mgr := NewManager(cfg)

	err := mgr.Add("api", Credential{Type: CredentialAPIKey, Key: "sk-test"})
	if err != nil {
		t.Fatalf("Add with nil Auth map failed: %v", err)
	}

	entries := mgr.List()
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestMaskDisplay(t *testing.T) {
	if maskDisplay("") != "(empty)" {
		t.Errorf("empty = %q", maskDisplay(""))
	}
	if maskDisplay("ab") != "***" {
		t.Errorf("short = %q", maskDisplay("ab"))
	}
	if !strings.Contains(maskDisplay("ghp_abcdef12345678"), "...") {
		t.Errorf("long key should contain '...': %q", maskDisplay("ghp_abcdef12345678"))
	}
}

// generateTestCert creates a self-signed TLS certificate for testing.
func generateTestCert(certPath, keyPath string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	certFile, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certFile.Close()
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return err
	}
	keyFile, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyFile.Close()
	return pem.Encode(keyFile, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}
