package openclaw

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

//go:embed openclaw-defaults.json
var openclawDefaults []byte

func (r *Runtime) GenerateConfig(params runtime.ConfigParams) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(openclawDefaults, &config); err != nil {
		return nil, fmt.Errorf("failed to parse openclaw-defaults.json: %w", err)
	}

	config["gateway"] = buildGatewayConfig(ContainerPort, params.Agent.GatewayPort, params.GatewayToken)

	channelsCfg := map[string]any{}
	pluginsCfg := map[string]any{}

	for _, binding := range params.Agent.Channels {
		ch, ok := channels.Get(binding.Platform)
		if !ok {
			continue
		}
		hasCreds := ch.HasCredentials(params.Secrets.Values)
		pluginsCfg[binding.Platform] = ch.OpenClawPluginConfig(hasCreds)
		if hasCreds {
			section, err := ch.OpenClawChannelConfig(string(params.Agent.Type), binding, params.Secrets.Values)
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

func (r *Runtime) ConfigFileName() string { return "openclaw.json" }

// buildGatewayConfig produces the gateway section of openclaw.json.
func buildGatewayConfig(containerPort, hostPort int, token string) map[string]any {
	origins := []string{
		fmt.Sprintf("http://localhost:%d", containerPort),
		fmt.Sprintf("http://127.0.0.1:%d", containerPort),
	}
	if hostPort != containerPort {
		origins = append(origins,
			fmt.Sprintf("http://localhost:%d", hostPort),
			fmt.Sprintf("http://127.0.0.1:%d", hostPort),
		)
	}

	gw := map[string]any{
		"port": containerPort,
		"mode": "remote",
		"bind": "lan",
		"remote": map[string]any{
			"url": fmt.Sprintf("http://localhost:%d", containerPort),
		},
		"controlUi": map[string]any{
			"allowedOrigins": origins,
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
