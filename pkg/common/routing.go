package common

import (
	"encoding/json"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// RoutingConfig is the JSON structure for routing.json.
type RoutingConfig struct {
	Channels map[string]string `json:"channels"`
	Members  map[string]string `json:"members"`
}

// GenerateRoutingJSON builds routing.json from a list of agents.
// Delegates to each agent's channel implementations for routing entries.
func GenerateRoutingJSON(agents []provider.AgentConfig) ([]byte, error) {
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
			for _, entry := range ch.RoutingEntries(string(a.Type), binding, a.Name, a.GatewayPort) {
				switch entry.Section {
				case "channels":
					cfg.Channels[entry.Key] = entry.URL
				case "members":
					cfg.Members[entry.Key] = entry.URL
				}
			}
		}
	}

	return json.MarshalIndent(cfg, "", "  ")
}
