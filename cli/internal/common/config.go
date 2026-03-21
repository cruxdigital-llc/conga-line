package common

import (
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

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
// The format matches the working AWS bootstrap template in user-data.sh.tftpl.
// When Slack credentials are not provided, the channels section is omitted
// and the agent operates in gateway-only mode (web UI).
func GenerateOpenClawConfig(agent provider.AgentConfig, secrets SharedSecrets, gatewayToken string) ([]byte, error) {
	config := map[string]interface{}{
		"agents": map[string]interface{}{
			"defaults": map[string]interface{}{
				"model":     map[string]interface{}{"primary": "anthropic/claude-opus-4-6", "fallbacks": []string{}},
				"models":    map[string]interface{}{"anthropic/claude-opus-4-6": map[string]interface{}{"params": map[string]interface{}{}}},
				"workspace": "/home/node/.openclaw/data/workspace",
				"heartbeat": map[string]interface{}{
					"every":        "55m",
					"lightContext": true,
					"target":       "none",
				},
				"contextPruning": map[string]interface{}{
					"mode": "cache-ttl",
					"ttl":  "5m",
				},
				"compaction": map[string]interface{}{
					"mode": "safeguard",
				},
			},
		},
		"tools":    map[string]interface{}{"profile": "coding"},
		"commands": map[string]interface{}{"native": "auto", "nativeSkills": "auto", "restart": true, "ownerDisplay": "raw"},
		"session":  map[string]interface{}{"dmScope": "per-channel-peer"},
		"hooks": map[string]interface{}{
			"internal": map[string]interface{}{
				"enabled": true,
				"entries": map[string]interface{}{
					"command-logger": map[string]interface{}{"enabled": true},
					"session-memory": map[string]interface{}{"enabled": true},
				},
			},
		},
		"gateway": buildGatewayConfig(agent.GatewayPort, gatewayToken),
		"skills":  map[string]interface{}{"install": map[string]interface{}{"nodeManager": "pnpm"}},
		"plugins": map[string]interface{}{"entries": map[string]interface{}{"slack": map[string]interface{}{"enabled": secrets.HasSlack()}}},
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

	// Preserve existing token if provided (OpenClaw generates on first boot)
	if token != "" {
		gw["auth"] = map[string]interface{}{
			"mode":  "token",
			"token": token,
		}
	}

	return gw
}
