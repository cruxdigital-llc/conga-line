package common

import (
	"encoding/json"
	"testing"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

func TestGenerateRoutingJSON(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "aaron", Type: provider.AgentTypeUser, SlackMemberID: "U0123456789"},
		{Name: "leadership", Type: provider.AgentTypeTeam, SlackChannel: "C9876543210"},
	}

	data, err := GenerateRoutingJSON(agents)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if got := cfg.Members["U0123456789"]; got != "http://conga-aaron:18789/slack/events" {
		t.Errorf("member route = %q, want http://conga-aaron:18789/slack/events", got)
	}
	if got := cfg.Channels["C9876543210"]; got != "http://conga-leadership:18789/slack/events" {
		t.Errorf("channel route = %q, want http://conga-leadership:18789/slack/events", got)
	}
}

func TestGenerateRoutingJSON_Empty(t *testing.T) {
	data, err := GenerateRoutingJSON(nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON(nil) error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if len(cfg.Members) != 0 || len(cfg.Channels) != 0 {
		t.Errorf("expected empty routing, got %d members, %d channels", len(cfg.Members), len(cfg.Channels))
	}
}
