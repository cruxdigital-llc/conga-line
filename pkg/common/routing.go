package common

import (
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// RoutingConfig is the JSON structure for routing.json.
type RoutingConfig struct {
	Channels map[string]string `json:"channels"`
	Members  map[string]string `json:"members"`
}

// WebhookPathResolver returns the webhook path for a given agent runtime and
// channel platform. Used by GenerateRoutingJSON to construct per-runtime URLs.
// When nil, the channel's default WebhookPath() is used.
type WebhookPathResolver func(agentRuntime, platform string) string

// GenerateRoutingJSON builds routing.json from a list of agents.
// The resolver maps (runtime, platform) → webhook path so that different
// runtimes receive events at their expected endpoints.
// Pass nil for resolver to use each channel's default webhook path.
func GenerateRoutingJSON(agents []provider.AgentConfig, resolver WebhookPathResolver) ([]byte, error) {
	cfg := RoutingConfig{
		Channels: make(map[string]string),
		Members:  make(map[string]string),
	}

	for _, a := range agents {
		if a.Paused {
			continue
		}
		for _, binding := range a.Channels {
			ch, ok := channels.Get(binding.Platform)
			if !ok {
				continue
			}

			// Resolve the webhook path: runtime-specific if resolver provided,
			// otherwise fall back to the channel's default.
			webhookPath := ch.WebhookPath()
			if resolver != nil {
				webhookPath = resolver(a.Runtime, binding.Platform)
			}

			url := fmt.Sprintf("http://conga-%s:%d%s", a.Name, a.GatewayPort, webhookPath)

			switch string(a.Type) {
			case "user":
				if binding.ID != "" {
					cfg.Members[binding.ID] = url
				}
			case "team":
				if binding.ID != "" {
					cfg.Channels[binding.ID] = url
				}
			}
		}
	}

	return json.MarshalIndent(cfg, "", "  ")
}
