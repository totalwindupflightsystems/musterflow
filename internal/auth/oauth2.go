package auth

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/wojons/muster/pkg/auth"
	"gopkg.in/yaml.v3"
)

// OAuth2Config holds the parameters for an OAuth2 authorization code flow.
type OAuth2Config struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	Scopes       []string
	RedirectPort int // default 19876
}

// StartLogin initiates the OAuth2 authorization code flow and returns the
// authorization URL the user should visit.
func StartLogin(cfg OAuth2Config) (*auth.AuthRequest, error) {
	if cfg.RedirectPort == 0 {
		cfg.RedirectPort = 19876
	}
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", cfg.RedirectPort)

	flow := &auth.AuthorizationCodeFlow{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		AuthURL:      cfg.AuthURL,
		TokenURL:     cfg.TokenURL,
		RedirectURL:  redirectURL,
		Scopes:       cfg.Scopes,
		PKCE:         false,
	}
	return flow.Start()
}

// CompleteLogin exchanges an authorization code for a token and stores it.
func CompleteLogin(store auth.TokenStorage, cfg OAuth2Config, req *auth.AuthRequest, code string) (*auth.Token, error) {
	if cfg.RedirectPort == 0 {
		cfg.RedirectPort = 19876
	}
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", cfg.RedirectPort)

	flow := &auth.AuthorizationCodeFlow{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		AuthURL:      cfg.AuthURL,
		TokenURL:     cfg.TokenURL,
		RedirectURL:  redirectURL,
		Scopes:       cfg.Scopes,
		PKCE:         false,
	}

	resp, err := flow.Exchange(code, req.State, req.RequestID)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	token := &auth.Token{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		TokenType:    resp.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
	}

	// Store the token
	if store != nil {
		if err := store.Save(cfg.ClientID, token); err != nil {
			return token, fmt.Errorf("store token: %w", err)
		}
	}

	return token, nil
}

// OpenBrowser opens the given URL in the default browser.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// YAMLTokenStore implements auth.TokenStorage using YAML files on disk.
type YAMLTokenStore struct {
	Dir string
}

// NewYAMLTokenStore creates a token store at the given directory.
func NewYAMLTokenStore(dir string) *YAMLTokenStore {
	return &YAMLTokenStore{Dir: dir}
}

// Save persists a token to <dir>/<service>.yaml.
func (s *YAMLTokenStore) Save(service string, token *auth.Token) error {
	if err := os.MkdirAll(s.Dir, 0755); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}
	path := filepath.Join(s.Dir, service+".yaml")
	data, err := yaml.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// Load reads a token from <dir>/<service>.yaml.
func (s *YAMLTokenStore) Load(service string) (*auth.Token, error) {
	path := filepath.Join(s.Dir, service+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("token not found for service %q", service)
		}
		return nil, fmt.Errorf("read token file: %w", err)
	}
	var token auth.Token
	if err := yaml.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}
	return &token, nil
}
