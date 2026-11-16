package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	engineauth "github.com/wojons/muster/pkg/auth"

	"github.com/totalwindupflightsystems/musterflow/internal/config"
)

// engineCredentialService is the keychain service the engine's stored-credential
// resolver reads. It mirrors credentialServiceName in the engine's
// pkg/auth/credential.go, which is unexported, so it has to be repeated here.
const engineCredentialService = "muster-cli"

// engineCredentialTypeAPIKey is the engine's credentials.json type whose header
// is X-API-Key (see pkg/auth.HeaderFor).
const engineCredentialTypeAPIKey = "api-key"

// Seams so tests can substitute the engine keychain and config directory.
var (
	newEngineKeychain   = engineauth.NewKeychain
	engineCredentialDir = engineauth.CredentialConfigDir
)

// EngineCredentialType maps a MusterFlow credential type onto the type
// vocabulary the engine stores in credentials.json. ok is false when the
// credential has no header secret to hand over (mTLS, none).
//
// oauth2 maps to bearer: the engine has no oauth2 type, and an OAuth2 access
// token is attached exactly like a bearer token.
func EngineCredentialType(t CredentialType) (string, bool) {
	switch t {
	case CredentialAPIKey:
		return engineCredentialTypeAPIKey, true
	case CredentialBearer, CredentialOAuth2:
		return "bearer", true
	default:
		return "", false
	}
}

// EnsureEngineCredential makes the generated-call flag `--auth <name>` work for
// a credential stored by `musterflow auth add`.
//
// MusterFlow keeps credentials in config.yaml, but the engine resolves --auth
// from the system keychain (service muster-cli) plus the credentials.json type
// metadata in its config dir, so musterflow-stored credentials were never
// attached to any request (DF-040). This function bridges the two stores:
//
//   - a credential the engine can already resolve (e.g. added with the engine
//     CLI) is left untouched;
//   - a credential only MusterFlow knows about is exported — secret to the
//     keychain, type to credentials.json — so the engine's own resolution path
//     attaches it;
//   - an unknown name returns a not-found error naming MusterFlow's real flags.
func EnsureEngineCredential(cfg config.Config, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}

	kc, err := newEngineKeychain()
	if err != nil {
		return fmt.Errorf("init engine keychain: %w", err)
	}
	dir := engineCredentialDir()
	if _, err := engineauth.ResolveCredentialHeaderFrom(kc, dir, name); err == nil {
		return nil
	}

	cred, err := NewManager(cfg).Get(name)
	if err != nil {
		return &CredentialNotFoundError{Name: name}
	}
	return exportEngineCredential(kc, dir, name, cred)
}

// PurgeEngineCredential removes a credential exported by EnsureEngineCredential
// so `auth remove` does not leave the secret behind in the engine keychain.
// Missing entries are not an error; the keychain being unavailable is reported
// to the caller, which may ignore it.
func PurgeEngineCredential(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	kc, err := newEngineKeychain()
	if err != nil {
		return fmt.Errorf("init engine keychain: %w", err)
	}
	if err := kc.Delete(engineCredentialService, name); err != nil && err != engineauth.ErrNotFound {
		return fmt.Errorf("delete engine credential %q: %w", name, err)
	}
	if err := deleteEngineCredentialType(engineCredentialDir(), name); err != nil {
		return fmt.Errorf("delete engine credential type %q: %w", name, err)
	}
	return nil
}

// CredentialNotFoundError reports a --auth name with no stored credential. Its
// hint names MusterFlow's real flags (--type/--key); the engine's own message
// suggested --name/--value, which this CLI does not have.
type CredentialNotFoundError struct {
	Name string
}

func (e *CredentialNotFoundError) Error() string {
	return fmt.Sprintf(
		"credential %q not found: add it first with 'musterflow auth add %s --type apikey --key <secret>'",
		e.Name, e.Name,
	)
}

// exportEngineCredential copies cred into the engine's credential store.
func exportEngineCredential(kc engineauth.Keychain, dir, name string, cred *Credential) error {
	engType, ok := EngineCredentialType(cred.Type)
	if !ok {
		return fmt.Errorf(
			"credential %q is %s auth: the engine attaches stored credentials as HTTP headers only, so --auth cannot use it — pass --cert/--key-path on the call instead",
			name, cred.Type,
		)
	}
	if cred.Key == "" {
		return fmt.Errorf("credential %q has no secret to attach: re-add it with 'musterflow auth add %s --type %s --key <secret>'", name, name, cred.Type)
	}
	if err := kc.Set(engineCredentialService, name, cred.Key); err != nil {
		return fmt.Errorf("store credential %q in the engine keychain: %w", name, err)
	}
	if err := saveEngineCredentialType(dir, name, engType); err != nil {
		return fmt.Errorf("record engine credential type for %q: %w", name, err)
	}
	return nil
}

// saveEngineCredentialType merges name → type into the engine's credentials.json
// (a flat JSON object), preserving entries written by the engine CLI.
func saveEngineCredentialType(dir, name, credType string) error {
	if dir == "" {
		return fmt.Errorf("engine config directory is unknown")
	}
	path := filepath.Join(dir, engineauth.CredentialMetaFileName)
	types := map[string]string{}
	if data, err := os.ReadFile(path); err == nil {
		// Malformed metadata is rewritten: the entry we add is the one we need.
		_ = json.Unmarshal(data, &types)
	} else if !os.IsNotExist(err) {
		return err
	}
	if types == nil {
		types = map[string]string{}
	}
	types[name] = credType

	data, err := json.MarshalIndent(types, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

// deleteEngineCredentialType drops name from the engine's credentials.json.
func deleteEngineCredentialType(dir, name string) error {
	if dir == "" {
		return nil
	}
	path := filepath.Join(dir, engineauth.CredentialMetaFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	types := map[string]string{}
	if err := json.Unmarshal(data, &types); err != nil {
		return nil // unreadable metadata holds nothing we can confidently edit
	}
	if _, ok := types[name]; !ok {
		return nil
	}
	delete(types, name)
	data, err = json.MarshalIndent(types, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
