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
	RepoPath string `json:"repo_path,omitempty"`
	Image    string `json:"image,omitempty"`

	// Shared secrets
	SlackBotToken      string `json:"slack_bot_token,omitempty"`
	SlackSigningSecret string `json:"slack_signing_secret,omitempty"`
	SlackAppToken      string `json:"slack_app_token,omitempty"`
	GoogleClientID     string `json:"google_client_id,omitempty"`
	GoogleClientSecret string `json:"google_client_secret,omitempty"`

	// InstallDocker skips the Docker install confirmation prompt.
	InstallDocker bool `json:"install_docker,omitempty"`
}

// SecretValue returns the config value for a given secret name, or empty string.
func (c *SetupConfig) SecretValue(name string) string {
	if c == nil {
		return ""
	}
	switch name {
	case "slack-bot-token":
		return c.SlackBotToken
	case "slack-signing-secret":
		return c.SlackSigningSecret
	case "slack-app-token":
		return c.SlackAppToken
	case "google-client-id":
		return c.GoogleClientID
	case "google-client-secret":
		return c.GoogleClientSecret
	default:
		return ""
	}
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
