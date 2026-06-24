// Package auth manages per-API credentials for MusterFlow.
package auth

import "fmt"

// CredentialType identifies the authentication mechanism.
type CredentialType string

const (
	CredentialNone   CredentialType = "none"
	CredentialAPIKey CredentialType = "apikey"
	CredentialBearer CredentialType = "bearer"
	CredentialOAuth2 CredentialType = "oauth2"
	CredentialMTLS   CredentialType = "mtls"
)

// Credential holds authentication material for a single API.
type Credential struct {
	Type     CredentialType `json:"type"`
	Key      string         `json:"key,omitempty"`
	CertPath string         `json:"cert_path,omitempty"`
	KeyPath  string         `json:"key_path,omitempty"`
}

// Validate checks that the credential has the required fields for its type.
func (c Credential) Validate() error {
	switch c.Type {
	case CredentialNone:
		return nil
	case CredentialAPIKey, CredentialBearer, CredentialOAuth2:
		if c.Key == "" {
			return fmt.Errorf("%s auth requires a key", c.Type)
		}
	case CredentialMTLS:
		if c.CertPath == "" {
			return fmt.Errorf("mTLS auth requires cert_path")
		}
		if c.KeyPath == "" {
			return fmt.Errorf("mTLS auth requires key_path")
		}
	default:
		return fmt.Errorf("unknown credential type: %s", c.Type)
	}
	return nil
}

// String returns a human-readable representation of the credential type.
func (c Credential) String() string {
	return fmt.Sprintf("Credential{type=%s, key=%s}", c.Type, maskDisplay(c.Key))
}

// IsConfigured returns true if the credential has any usable auth material.
func (c Credential) IsConfigured() bool {
	return c.Type != CredentialNone && c.Validate() == nil
}

func maskDisplay(key string) string {
	if len(key) <= 8 {
		if key == "" {
			return "(empty)"
		}
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
