package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ProviderName identifies a deployment target.
type ProviderName string

const (
	ProviderAWS    ProviderName = "aws"
	ProviderLocal  ProviderName = "local"
	ProviderRemote ProviderName = "remote"
)

// Config holds provider-agnostic configuration.
type Config struct {
	Provider ProviderName `json:"provider"`           // "aws", "local", or "remote"
	Runtime  string       `json:"runtime,omitempty"`  // default runtime: "openclaw", "hermes"
	DataDir  string       `json:"data_dir,omitempty"` // override for ~/.conga/
	Region   string       `json:"region,omitempty"`   // AWS region (aws provider)
	Profile  string       `json:"profile,omitempty"`  // AWS profile (aws provider)
	// Remote provider (SSH)
	SSHHost    string `json:"ssh_host,omitempty"`     // Remote hostname or IP
	SSHPort    int    `json:"ssh_port,omitempty"`     // SSH port (default 22)
	SSHUser    string `json:"ssh_user,omitempty"`     // SSH user (default "root")
	SSHKeyPath string `json:"ssh_key_path,omitempty"` // Path to SSH private key
	RemoteDir  string `json:"remote_dir,omitempty"`  // Remote base directory (default: /opt/conga)
}

// DefaultDataDir returns ~/.conga/.
func DefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".conga")
	}
	return filepath.Join(home, ".conga")
}

// DefaultConfigPath returns ~/.conga/config.json.
func DefaultConfigPath() string {
	return filepath.Join(DefaultDataDir(), "config.json")
}

// ConfigPathForDataDir returns <dataDir>/config.json if dataDir is set,
// otherwise falls back to DefaultConfigPath.
func ConfigPathForDataDir(dataDir string) string {
	if dataDir != "" {
		return filepath.Join(dataDir, "config.json")
	}
	return DefaultConfigPath()
}

// LoadConfig reads provider config from disk. Returns defaults if file doesn't exist.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig writes provider config to disk atomically.
func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	return os.WriteFile(path, append(data, '\n'), 0600)
}
