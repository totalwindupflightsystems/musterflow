package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	engineauth "github.com/wojons/muster/pkg/auth"

	"github.com/totalwindupflightsystems/musterflow/internal/config"
)

// fakeKeychain is an in-memory engineauth.Keychain.
type fakeKeychain struct {
	secrets map[string]string
	setErr  error
}

func newFakeKeychain() *fakeKeychain {
	return &fakeKeychain{secrets: map[string]string{}}
}

func keychainKey(service, account string) string { return service + "\x00" + account }

func (f *fakeKeychain) Set(service, account, password string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.secrets[keychainKey(service, account)] = password
	return nil
}

func (f *fakeKeychain) Get(service, account string) (string, error) {
	v, ok := f.secrets[keychainKey(service, account)]
	if !ok {
		return "", engineauth.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeychain) Delete(service, account string) error {
	k := keychainKey(service, account)
	if _, ok := f.secrets[k]; !ok {
		return engineauth.ErrNotFound
	}
	delete(f.secrets, k)
	return nil
}

func (f *fakeKeychain) List(service string) ([]engineauth.Account, error) {
	return nil, nil
}

// useFakeEngine points the engine seams at fc and a temp config dir.
func useFakeEngine(t *testing.T, fc *fakeKeychain) string {
	t.Helper()
	dir := t.TempDir()
	origKC, origDir := newEngineKeychain, engineCredentialDir
	newEngineKeychain = func() (engineauth.Keychain, error) { return fc, nil }
	engineCredentialDir = func() string { return dir }
	t.Cleanup(func() {
		newEngineKeychain = origKC
		engineCredentialDir = origDir
	})
	return dir
}

func cfgWith(authType, key string) config.Config {
	return config.Config{Auth: map[string]config.AuthConfig{"probe": {Type: authType, Key: key}}}
}

// AC1: a credential stored by `musterflow auth add` becomes resolvable through
// the engine's --auth path (keychain secret + credentials.json type).
func TestEnsureEngineCredentialAttachesStoredCredential(t *testing.T) {
	tests := []struct {
		name       string
		credType   string
		wantType   string
		wantHeader string
		wantValue  string
	}{
		{"api key", "apikey", "api-key", "X-API-Key", "LIVEKEY"},
		{"bearer", "bearer", "bearer", "Authorization", "Bearer tok123"},
		{"oauth2", "oauth2", "bearer", "Authorization", "Bearer oauth-tok"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fc := newFakeKeychain()
			dir := useFakeEngine(t, fc)

			if err := EnsureEngineCredential(cfgWith(tc.credType, strings.TrimPrefix(tc.wantValue, "Bearer ")), "probe"); err != nil {
				t.Fatalf("EnsureEngineCredential: %v", err)
			}

			got, err := fc.Get(engineCredentialService, "probe")
			if err != nil {
				t.Fatalf("secret not exported to the engine keychain: %v", err)
			}
			if want := strings.TrimPrefix(tc.wantValue, "Bearer "); got != want {
				t.Fatalf("exported secret = %q, want %q", got, want)
			}

			var types map[string]string
			data, err := os.ReadFile(filepath.Join(dir, engineauth.CredentialMetaFileName))
			if err != nil {
				t.Fatalf("credentials.json not written: %v", err)
			}
			if err := json.Unmarshal(data, &types); err != nil {
				t.Fatalf("credentials.json is not a flat map: %v", err)
			}
			if types["probe"] != tc.wantType {
				t.Fatalf("credential type = %q, want %q", types["probe"], tc.wantType)
			}

			header, err := engineauth.ResolveCredentialHeaderFrom(fc, dir, "probe")
			if err != nil {
				t.Fatalf("engine cannot resolve the exported credential: %v", err)
			}
			if header.Name != tc.wantHeader || header.Value != tc.wantValue {
				t.Fatalf("header = %s: %q, want %s: %q", header.Name, header.Value, tc.wantHeader, tc.wantValue)
			}
		})
	}
}

// AC3: an unknown name must hint at MusterFlow's real flags, not the engine's
// --name/--value.
func TestEnsureEngineCredentialNotFoundHintUsesRealFlags(t *testing.T) {
	fc := newFakeKeychain()
	useFakeEngine(t, fc)

	err := EnsureEngineCredential(config.Config{Auth: map[string]config.AuthConfig{}}, "probe")
	if err == nil {
		t.Fatal("expected a not-found error for an unstored credential")
	}
	msg := err.Error()
	if !strings.Contains(msg, "musterflow auth add probe --type apikey --key") {
		t.Errorf("hint does not name the real flags: %s", msg)
	}
	if strings.Contains(msg, "--name") || strings.Contains(msg, "--value") {
		t.Errorf("hint still advertises nonexistent flags: %s", msg)
	}
}

// A credential the engine already holds (e.g. `muster auth add`) is left alone.
func TestEnsureEngineCredentialLeavesEngineCredentialAlone(t *testing.T) {
	fc := newFakeKeychain()
	dir := useFakeEngine(t, fc)
	if err := fc.Set(engineCredentialService, "wirecred", "engine-secret"); err != nil {
		t.Fatal(err)
	}

	if err := EnsureEngineCredential(config.Config{Auth: map[string]config.AuthConfig{}}, "wirecred"); err != nil {
		t.Fatalf("EnsureEngineCredential: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, engineauth.CredentialMetaFileName)); !os.IsNotExist(err) {
		t.Fatalf("unexpected metadata write for an engine-owned credential: %v", err)
	}
}

// mTLS has no header secret to export; say so instead of failing obscurely.
func TestEnsureEngineCredentialRejectsMTLS(t *testing.T) {
	fc := newFakeKeychain()
	useFakeEngine(t, fc)

	err := EnsureEngineCredential(config.Config{Auth: map[string]config.AuthConfig{
		"probe": {Type: "mtls", CertPath: "client.pem", KeyPath: "client-key.pem"},
	}}, "probe")
	if err == nil {
		t.Fatal("expected an error for an mTLS credential")
	}
	if !strings.Contains(err.Error(), "--cert/--key-path") {
		t.Fatalf("error does not point at the mTLS flags: %v", err)
	}
}

// Exporting must not clobber type metadata the engine CLI wrote for other
// credentials, and `auth remove` must clean up both halves of the export.
func TestExportPreservesMetadataAndPurgeRemoves(t *testing.T) {
	fc := newFakeKeychain()
	dir := useFakeEngine(t, fc)
	metaPath := filepath.Join(dir, engineauth.CredentialMetaFileName)
	if err := os.WriteFile(metaPath, []byte(`{"other":"api-key"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := EnsureEngineCredential(cfgWith("apikey", "LIVEKEY"), "probe"); err != nil {
		t.Fatalf("EnsureEngineCredential: %v", err)
	}

	var types map[string]string
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &types); err != nil {
		t.Fatal(err)
	}
	if types["other"] != "api-key" || types["probe"] != "api-key" {
		t.Fatalf("metadata merge lost entries: %v", types)
	}

	if err := PurgeEngineCredential("probe"); err != nil {
		t.Fatalf("PurgeEngineCredential: %v", err)
	}
	if _, err := fc.Get(engineCredentialService, "probe"); err != engineauth.ErrNotFound {
		t.Fatalf("secret survived purge: %v", err)
	}
	data, err = os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	types = map[string]string{}
	if err := json.Unmarshal(data, &types); err != nil {
		t.Fatal(err)
	}
	if _, ok := types["probe"]; ok {
		t.Fatalf("metadata for the purged credential survived: %v", types)
	}
	if types["other"] != "api-key" {
		t.Fatalf("purge dropped another credential's metadata: %v", types)
	}
}
