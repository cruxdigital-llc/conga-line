package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SetupConfig provides values for non-interactive setup.
// When passed to Setup(), any non-empty field skips the corresponding prompt.
type SetupConfig struct {
	// Connection (remote provider)
	SSHHost    string `json:"ssh_host,omitempty"`
	SSHPort    int    `json:"ssh_port,omitempty"`
	SSHUser    string `json:"ssh_user,omitempty"`
	SSHKeyPath string `json:"ssh_key_path,omitempty"`

	// Config values
	Runtime         string `json:"runtime,omitempty"` // "openclaw", "hermes"
	RuntimeOverride string `json:"-"`                 // Set by --runtime flag; not serialized
	RepoPath        string `json:"repo_path,omitempty"`
	Image           string `json:"image,omitempty"`

	// Shared secrets — generic map. Keys are secret names: "slack-bot-token", "google-client-id", etc.
	Secrets map[string]string `json:"secrets,omitempty"`

	// InstallDocker skips the Docker install confirmation prompt.
	InstallDocker bool `json:"install_docker,omitempty"`
}

// SecretValue returns the config value for a given secret name, or empty string.
func (c *SetupConfig) SecretValue(name string) string {
	if c == nil || c.Secrets == nil {
		return ""
	}
	return c.Secrets[name]
}

// ParseSetupConfig parses a JSON string or reads a JSON file.
func ParseSetupConfig(input string) (*SetupConfig, error) {
	var data []byte
	if strings.HasPrefix(strings.TrimSpace(input), "{") {
		data = []byte(input)
	} else {
		var err error
		data, err = os.ReadFile(input)
		if err != nil {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}
	var cfg SetupConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return &cfg, nil
}
