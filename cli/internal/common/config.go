package common

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

//go:embed openclaw-defaults.json
var openclawDefaults []byte

// SharedSecrets holds the shared secrets needed to generate agent config and env files.
type SharedSecrets struct {
	SlackBotToken      string
	SlackSigningSecret string
	SlackAppToken      string // router only
	GoogleClientID     string
	GoogleClientSecret string
}

// HasSlack returns true if Slack credentials are configured.
func (s SharedSecrets) HasSlack() bool {
	return s.SlackBotToken != "" && s.SlackSigningSecret != ""
}

// GenerateOpenClawConfig produces the openclaw.json content for an agent.
// Static settings (model, heartbeat, pruning, etc.) are loaded from the embedded
// openclaw-defaults.json — the single source of truth for OpenClaw config structure.
// Dynamic fields (gateway, Slack channels, plugins) are overlaid per-agent.
func GenerateOpenClawConfig(agent provider.AgentConfig, secrets SharedSecrets, gatewayToken string) ([]byte, error) {
	// Load static defaults from embedded JSON
	var config map[string]interface{}
	if err := json.Unmarshal(openclawDefaults, &config); err != nil {
		return nil, fmt.Errorf("failed to parse openclaw-defaults.json: %w", err)
	}

	// Overlay dynamic fields
	config["gateway"] = buildGatewayConfig(agent.GatewayPort, gatewayToken)
	config["plugins"] = map[string]interface{}{
		"entries": map[string]interface{}{
			"slack": map[string]interface{}{"enabled": secrets.HasSlack()},
		},
	}

	// Only add Slack channel config when credentials are present
	if secrets.HasSlack() {
		slackChannel := map[string]interface{}{
			"mode":              "http",
			"enabled":           true,
			"botToken":          secrets.SlackBotToken,
			"signingSecret":     secrets.SlackSigningSecret,
			"webhookPath":       "/slack/events",
			"userTokenReadOnly": true,
			"streaming":         "partial",
			"nativeStreaming":   true,
		}

		switch agent.Type {
		case provider.AgentTypeUser:
			slackChannel["groupPolicy"] = "disabled"
			slackChannel["dmPolicy"] = "allowlist"
			if agent.SlackMemberID != "" {
				slackChannel["allowFrom"] = []string{agent.SlackMemberID}
			}
			slackChannel["dm"] = map[string]interface{}{"enabled": true}
		case provider.AgentTypeTeam:
			slackChannel["groupPolicy"] = "allowlist"
			slackChannel["dmPolicy"] = "disabled"
			if agent.SlackChannel != "" {
				slackChannel["channels"] = map[string]interface{}{
					agent.SlackChannel: map[string]interface{}{"allow": true, "requireMention": false},
				}
			}
		}

		config["channels"] = map[string]interface{}{
			"slack": slackChannel,
		}
	}

	return json.MarshalIndent(config, "", "  ")
}

// GenerateEnvFile produces the .env file content for an agent container.
// Format: KEY=VALUE\n (one per line, no quoting).
func GenerateEnvFile(agent provider.AgentConfig, shared SharedSecrets, perAgent map[string]string) []byte {
	var buf []byte

	appendEnv := func(key, val string) {
		if val != "" {
			buf = append(buf, []byte(fmt.Sprintf("%s=%s\n", key, val))...)
		}
	}

	appendEnv("SLACK_BOT_TOKEN", shared.SlackBotToken)
	appendEnv("SLACK_SIGNING_SECRET", shared.SlackSigningSecret)
	appendEnv("GOOGLE_CLIENT_ID", shared.GoogleClientID)
	appendEnv("GOOGLE_CLIENT_SECRET", shared.GoogleClientSecret)
	appendEnv("NODE_OPTIONS", "--max-old-space-size=1536")

	for name, value := range perAgent {
		appendEnv(SecretNameToEnvVar(name), value)
	}

	return buf
}

func buildGatewayConfig(port int, token string) map[string]interface{} {
	gw := map[string]interface{}{
		"port": port,
		"mode": "local",
		"bind": "lan",
		"controlUi": map[string]interface{}{
			"allowedOrigins": []string{
				fmt.Sprintf("http://localhost:%d", port),
				fmt.Sprintf("http://127.0.0.1:%d", port),
			},
		},
	}

	if token != "" {
		gw["auth"] = map[string]interface{}{
			"mode":  "token",
			"token": token,
		}
	}

	return gw
}
