package common

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/channels"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

//go:embed openclaw-defaults.json
var openclawDefaults []byte

// SharedSecrets holds the shared secrets needed to generate agent config and env files.
type SharedSecrets struct {
	Values             map[string]string // keyed by secret name: "slack-bot-token" → value
	GoogleClientID     string
	GoogleClientSecret string
}

// HasAnyChannel returns true if any registered channel has its required credentials present.
func HasAnyChannel(shared SharedSecrets) bool {
	for _, ch := range channels.All() {
		if ch.HasCredentials(shared.Values) {
			return true
		}
	}
	return false
}

// GenerateOpenClawConfig produces the openclaw.json content for an agent.
// Static settings (model, heartbeat, pruning, etc.) are loaded from the embedded
// openclaw-defaults.json — the single source of truth for OpenClaw config structure.
// Dynamic fields (gateway, channels, plugins) are overlaid per-agent.
func GenerateOpenClawConfig(agent provider.AgentConfig, secrets SharedSecrets, gatewayToken string) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(openclawDefaults, &config); err != nil {
		return nil, fmt.Errorf("failed to parse openclaw-defaults.json: %w", err)
	}

	config["gateway"] = buildGatewayConfig(agent.GatewayPort, gatewayToken)

	channelsCfg := map[string]any{}
	pluginsCfg := map[string]any{}

	for _, binding := range agent.Channels {
		ch, ok := channels.Get(binding.Platform)
		if !ok {
			continue
		}
		hasCreds := ch.HasCredentials(secrets.Values)
		pluginsCfg[binding.Platform] = ch.OpenClawPluginConfig(hasCreds)
		if hasCreds {
			section, err := ch.OpenClawChannelConfig(string(agent.Type), binding, secrets.Values)
			if err != nil {
				return nil, fmt.Errorf("channel %s config: %w", binding.Platform, err)
			}
			channelsCfg[binding.Platform] = section
		}
	}

	if len(channelsCfg) > 0 {
		config["channels"] = channelsCfg
	}
	if len(pluginsCfg) > 0 {
		config["plugins"] = map[string]any{"entries": pluginsCfg}
	}

	return json.MarshalIndent(config, "", "  ")
}

// GenerateEnvFile produces the .env file content for an agent container.
// Format: KEY=VALUE\n (one per line, no quoting).
func GenerateEnvFile(agent provider.AgentConfig, secrets SharedSecrets, perAgent map[string]string) []byte {
	var buf []byte

	appendEnv := func(key, val string) {
		if val != "" {
			buf = append(buf, []byte(fmt.Sprintf("%s=%s\n", key, val))...)
		}
	}

	// Channel-provided env vars (deduplicated)
	seen := map[string]bool{}
	for _, binding := range agent.Channels {
		ch, ok := channels.Get(binding.Platform)
		if !ok {
			continue
		}
		for k, v := range ch.AgentEnvVars(secrets.Values) {
			if !seen[k] {
				appendEnv(k, v)
				seen[k] = true
			}
		}
	}

	// Non-channel shared secrets
	appendEnv("GOOGLE_CLIENT_ID", secrets.GoogleClientID)
	appendEnv("GOOGLE_CLIENT_SECRET", secrets.GoogleClientSecret)
	appendEnv("NODE_OPTIONS", "--max-old-space-size=1536")

	for name, value := range perAgent {
		appendEnv(SecretNameToEnvVar(name), value)
	}

	return buf
}

func buildGatewayConfig(port int, token string) map[string]any {
	gw := map[string]any{
		"port": port,
		"mode": "local",
		"bind": "lan",
		"controlUi": map[string]any{
			"allowedOrigins": []string{
				fmt.Sprintf("http://localhost:%d", port),
				fmt.Sprintf("http://127.0.0.1:%d", port),
			},
		},
	}

	if token != "" {
		gw["auth"] = map[string]any{
			"mode":  "token",
			"token": token,
		}
	}

	return gw
}
