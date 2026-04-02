package common

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
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

// BuildChannelStatuses builds the channel status list from the given agents,
// shared secrets, and router state. This is the shared logic used by both
// local and remote providers' ListChannels implementations.
func BuildChannelStatuses(agents []provider.AgentConfig, shared SharedSecrets, routerRunning bool) []provider.ChannelStatus {
	var result []provider.ChannelStatus
	for _, ch := range channels.All() {
		status := provider.ChannelStatus{
			Platform:   ch.Name(),
			Configured: ch.HasCredentials(shared.Values),
		}
		status.RouterRunning = routerRunning && status.Configured
		for _, a := range agents {
			if a.ChannelBinding(ch.Name()) != nil {
				status.BoundAgents = append(status.BoundAgents, a.Name)
			}
		}
		result = append(result, status)
	}
	return result
}

// BuildRouterEnvContent generates the router.env file content from all
// configured channels' router env vars.
func BuildRouterEnvContent(shared SharedSecrets) string {
	var buf strings.Builder
	for _, ch := range channels.All() {
		if ch.HasCredentials(shared.Values) {
			for k, v := range ch.RouterEnvVars(shared.Values) {
				fmt.Fprintf(&buf, "%s=%s\n", k, v)
			}
		}
	}
	return buf.String()
}

// GenerateAgentFiles produces the openclaw.json and .env file content for an agent.
// Returns the raw bytes for each file, leaving I/O to the caller.
func GenerateAgentFiles(cfg provider.AgentConfig, shared SharedSecrets, perAgent map[string]string) (openclawJSON []byte, envContent []byte, err error) {
	openclawJSON, err = GenerateOpenClawConfig(cfg, shared, "")
	if err != nil {
		return nil, nil, err
	}
	envContent = GenerateEnvFile(cfg, shared, perAgent)
	return openclawJSON, envContent, nil
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
	// Base NODE_OPTIONS for heap size. When egress proxy is active, local/remote
	// providers override this via Docker -e flag to add --require proxy-bootstrap.js.
	// On AWS, the systemd unit is patched by deploy-egress.sh to add the require flag.
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
