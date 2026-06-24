// Package auth manages per-API credentials for MusterFlow.
// Credentials are stored in ~/.musterflow/config.yaml and never leave
// the user's machine. Supported types: none, apikey, bearer, oauth2, mtls.
package auth

import (
	"fmt"
	"strings"

	"github.com/totalwindupflightsystems/musterflow/internal/config"
)

// Manager wraps credential storage operations.
type Manager struct {
	cfg config.Config
}

// NewManager creates an auth manager from an existing config.
func NewManager(cfg config.Config) *Manager {
	return &Manager{cfg: cfg}
}

// Add stores a credential for an API.
func (m *Manager) Add(apiID string, cred Credential) error {
	if apiID == "" {
		return fmt.Errorf("api ID is required")
	}
	if err := cred.Validate(); err != nil {
		return fmt.Errorf("invalid credential: %w", err)
	}

	ac := config.AuthConfig{
		Type:     string(cred.Type),
		Key:      cred.Key,
		CertPath: cred.CertPath,
		KeyPath:  cred.KeyPath,
	}

	if m.cfg.Auth == nil {
		m.cfg.Auth = make(map[string]config.AuthConfig)
	}
	m.cfg.Auth[apiID] = ac

	return config.Save(m.cfg)
}

// Remove deletes a credential for an API.
func (m *Manager) Remove(apiID string) error {
	if apiID == "" {
		return fmt.Errorf("api ID is required")
	}
	if _, ok := m.cfg.Auth[apiID]; !ok {
		return fmt.Errorf("no auth configured for %q", apiID)
	}
	delete(m.cfg.Auth, apiID)
	return config.Save(m.cfg)
}

// Get returns the credential for an API, or nil if not configured.
func (m *Manager) Get(apiID string) (*Credential, error) {
	ac, ok := m.cfg.Auth[apiID]
	if !ok {
		return nil, fmt.Errorf("no auth configured for %q", apiID)
	}
	return &Credential{
		Type:     CredentialType(ac.Type),
		Key:      ac.Key,
		CertPath: ac.CertPath,
		KeyPath:  ac.KeyPath,
	}, nil
}

// List returns all configured API IDs with masked credential info.
func (m *Manager) List() []AuthEntry {
	entries := make([]AuthEntry, 0, len(m.cfg.Auth))
	for id, ac := range m.cfg.Auth {
		entries = append(entries, AuthEntry{
			APIID:    id,
			Type:     ac.Type,
			Key:      maskKey(ac.Key),
			CertPath: ac.CertPath,
			KeyPath:  ac.KeyPath,
		})
	}
	return entries
}

// AuthEntry is a masked view of a stored credential.
type AuthEntry struct {
	APIID    string
	Type     string
	Key      string
	CertPath string
	KeyPath  string
}

// maskKey shows first 4 + last 4 chars of a key, or "****" if too short.
func maskKey(key string) string {
	if len(key) <= 8 {
		if key == "" {
			return "(not set)"
		}
		return "****"
	}
	return key[:4] + "…" + key[len(key)-4:]
}

// InjectAuthHeader adds the appropriate Authorization header to an HTTP request
// based on the credential type. Returns nil if auth type is "none" or no auth configured.
func InjectAuthHeader(cred *Credential, headerFn func(key, value string)) {
	if cred == nil || cred.Type == CredentialNone {
		return
	}
	switch cred.Type {
	case CredentialAPIKey, CredentialBearer:
		if cred.Key != "" {
			headerFn("Authorization", "Bearer "+cred.Key)
		}
	case CredentialOAuth2:
		if cred.Key != "" {
			headerFn("Authorization", "Bearer "+cred.Key)
		}
	default:
		// mTLS is handled at the transport layer, not as a header
	}
}

// BuildTransport creates an http.RoundTripper for mTLS credentials.
// Returns nil if mTLS is not configured.
func BuildTransport(cred *Credential) (interface{}, error) {
	if cred == nil || cred.Type != CredentialMTLS {
		return nil, nil
	}
	if cred.CertPath == "" || cred.KeyPath == "" {
		return nil, fmt.Errorf("mTLS requires both cert_path and key_path")
	}
	// The actual tls.Config creation happens in the caller, which has access
	// to crypto/tls and can load the cert+key files. This function just
	// validates and returns the paths.
	return map[string]string{
		"cert_path": cred.CertPath,
		"key_path":  cred.KeyPath,
	}, nil
}

// CredentialTypeFromString converts a string to a CredentialType, defaulting to
// CredentialNone for unrecognized values.
func CredentialTypeFromString(s string) CredentialType {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "none":
		return CredentialNone
	case "apikey", "api_key", "api-key":
		return CredentialAPIKey
	case "bearer", "token":
		return CredentialBearer
	case "oauth2", "oauth":
		return CredentialOAuth2
	case "mtls", "mutual-tls", "client-cert":
		return CredentialMTLS
	default:
		return CredentialNone
	}
}
