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
	return loadFrom(defaultDataDir())
}

// LoadWithDataDir reads config from <dir>/config.yaml, using dir as the data
// directory. Returns defaults if file doesn't exist. Returns an error only for
// parse failures. The returned Config has DataDir set to dir.
func LoadWithDataDir(dir string) (Config, error) {
	if dir == "" {
		return Load()
	}
	return loadFrom(dir)
}

func loadFrom(dir string) (Config, error) {
	cfg := Defaults()
	cfg.DataDir = dir
	path := configPathFor(dir)

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
		d := Defaults()
		d.DataDir = dir
		return d, nil
	}

	return cfg, nil
}

// Save writes the config to the config file in cfg.DataDir if non-empty,
// otherwise to ~/.musterflow/config.yaml. Creates the directory if needed.
func Save(cfg Config) error {
	dir := cfg.DataDir
	if dir == "" {
		dir = defaultDataDir()
	}
	return SaveWithDataDir(cfg, dir)
}

// configFileMode is the permission for the config file. It holds plaintext
// bearer tokens and API keys, so it must never be readable by group or others.
const configFileMode os.FileMode = 0600

// SaveWithDataDir writes the config to <dir>/config.yaml.
// Creates the directory if needed. The file is always left at 0600; a
// pre-existing group/world-readable file is tightened on save and its previous
// mode is reported on stderr.
func SaveWithDataDir(cfg Config, dir string) error {
	if dir == "" {
		dir = defaultDataDir()
	}
	path := configPathFor(dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	warnIfLooseConfigPerms(path)

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// os.WriteFile only applies the mode when it CREATES the file, so an
	// existing 0644 config (written by an older release) would keep its loose
	// mode. Chmod after the write to tighten it.
	if err := os.WriteFile(path, data, configFileMode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Chmod(path, configFileMode); err != nil {
		return fmt.Errorf("secure config permissions: %w", err)
	}
	return nil
}

// warnIfLooseConfigPerms reports an existing config file that group or others
// can read. The file is rewritten 0600 immediately after, so this is a notice
// rather than a refusal. A missing file (first save) is not a warning.
func warnIfLooseConfigPerms(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if mode := info.Mode().Perm(); mode&0077 != 0 {
		fmt.Fprintf(os.Stderr, "Warning: config at %s was readable by group/others (%04o) — rewriting with 0600\n", path, mode)
	}
}

// ConfigPath returns the path to the config file in the default data directory.
func ConfigPath() string {
	return configPathFor(defaultDataDir())
}

// ConfigPathFor returns the path to the config file for the given data directory.
func ConfigPathFor(dir string) string {
	return configPathFor(dir)
}

// FindPort finds an available port starting from the configured port.
// Tries up to 10 ports before giving up.
func FindPort(startPort int) (int, error) {
	for offset := 0; offset < 10; offset++ {
		port := startPort + offset
		addr := fmt.Sprintf(":%d", port)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			_ = ln.Close()
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

func configPathFor(dir string) string {
	return filepath.Join(dir, "config.yaml")
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".musterflow")
	}
	return filepath.Join(home, ".musterflow")
}
