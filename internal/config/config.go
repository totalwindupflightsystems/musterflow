// Package config provides YAML configuration management for MusterFlow.
// Config is stored at ~/.musterflow/config.yaml with sensible defaults.
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all MusterFlow configuration.
type Config struct {
	Port           int                   `yaml:"port"`
	DataDir        string                `yaml:"data_dir"`
	DefaultFormat  string                `yaml:"default_format"`
	AutoCompletion bool                  `yaml:"auto_completion"`
	Auth           map[string]AuthConfig `yaml:"auth,omitempty"`
}

// AuthConfig holds per-API authentication settings.
type AuthConfig struct {
	Type     string `yaml:"type"`               // none, apikey, bearer, oauth2, mtls
	Key      string `yaml:"key"`                // API key or bearer token (masked in display)
	CertPath string `yaml:"cert,omitempty"`     // mTLS client cert
	KeyPath  string `yaml:"key_path,omitempty"` // mTLS client key
}

// Defaults returns a Config with sensible defaults.
func Defaults() Config {
	return Config{
		Port:           9876,
		DataDir:        defaultDataDir(),
		DefaultFormat:  "table",
		AutoCompletion: true,
		Auth:           make(map[string]AuthConfig),
	}
}

// Load reads config from ~/.musterflow/config.yaml.
// Returns defaults if file doesn't exist. Returns an error only for parse failures.
func Load() (Config, error) {
	cfg := Defaults()
	path := configPath()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		// Invalid YAML — warn and fall back to defaults
		fmt.Fprintf(os.Stderr, "Warning: invalid config at %s: %v — using defaults\n", path, err)
		return Defaults(), nil
	}

	return cfg, nil
}

// Save writes the config to ~/.musterflow/config.yaml.
// Creates the directory if needed.
func Save(cfg Config) error {
	path := configPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// ConfigPath returns the path to the config file.
func ConfigPath() string {
	return configPath()
}

// FindPort finds an available port starting from the configured port.
// Tries up to 10 ports before giving up.
func FindPort(startPort int) (int, error) {
	for offset := 0; offset < 10; offset++ {
		port := startPort + offset
		addr := fmt.Sprintf(":%d", port)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			ln.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port in range %d-%d", startPort, startPort+9)
}

// MaskKey masks an API key for display, showing only the first 4 and last 4 chars.
func MaskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func configPath() string {
	dir := defaultDataDir()
	return filepath.Join(dir, "config.yaml")
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".musterflow")
	}
	return filepath.Join(home, ".musterflow")
}
