package common

import (
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

// RoutingConfig is the JSON structure for routing.json.
type RoutingConfig struct {
	Channels map[string]string `json:"channels"`
	Members  map[string]string `json:"members"`
}

// GenerateRoutingJSON builds routing.json from a list of agents.
// Container URLs use the format http://conga-{name}:18789/slack/events.
func GenerateRoutingJSON(agents []provider.AgentConfig) ([]byte, error) {
	cfg := RoutingConfig{
		Channels: make(map[string]string),
		Members:  make(map[string]string),
	}

	for _, a := range agents {
		url := fmt.Sprintf("http://conga-%s:18789/slack/events", a.Name)
		switch a.Type {
		case provider.AgentTypeUser:
			if a.SlackMemberID != "" {
				cfg.Members[a.SlackMemberID] = url
			}
		case provider.AgentTypeTeam:
			if a.SlackChannel != "" {
				cfg.Channels[a.SlackChannel] = url
			}
		}
	}

	return json.MarshalIndent(cfg, "", "  ")
}
