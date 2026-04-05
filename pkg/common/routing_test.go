package common

import (
	"encoding/json"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

func TestGenerateRoutingJSON(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "myagent", Type: provider.AgentTypeUser, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0123456789"}}, GatewayPort: 18789},
		{Name: "leadership", Type: provider.AgentTypeTeam, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C9876543210"}}, GatewayPort: 18790},
	}

	data, err := GenerateRoutingJSON(agents, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if got := cfg.Members["U0123456789"]; got != "http://conga-myagent:18789/slack/events" {
		t.Errorf("member route = %q, want http://conga-myagent:18789/slack/events", got)
	}
	if got := cfg.Channels["C9876543210"]; got != "http://conga-leadership:18790/slack/events" {
		t.Errorf("channel route = %q, want http://conga-leadership:18790/slack/events", got)
	}
}

func TestGenerateRoutingJSON_PausedExcluded(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "myagent", Type: provider.AgentTypeUser, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0123456789"}}, GatewayPort: 18789},
		{Name: "paused-user", Type: provider.AgentTypeUser, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U9999999999"}}, Paused: true, GatewayPort: 18790},
		{Name: "leadership", Type: provider.AgentTypeTeam, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C9876543210"}}, GatewayPort: 18791},
		{Name: "paused-team", Type: provider.AgentTypeTeam, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C0000000000"}}, Paused: true, GatewayPort: 18792},
	}

	data, err := GenerateRoutingJSON(agents, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if len(cfg.Members) != 1 {
		t.Errorf("expected 1 member, got %d", len(cfg.Members))
	}
	if len(cfg.Channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(cfg.Channels))
	}
	if _, ok := cfg.Members["U9999999999"]; ok {
		t.Error("paused user should not be in routing")
	}
	if _, ok := cfg.Channels["C0000000000"]; ok {
		t.Error("paused team should not be in routing")
	}
}

func TestGenerateRoutingJSON_Empty(t *testing.T) {
	data, err := GenerateRoutingJSON(nil, nil)
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

func TestGenerateRoutingJSON_GatewayOnly(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "myagent", Type: provider.AgentTypeUser, GatewayPort: 18789}, // no channels
	}

	data, err := GenerateRoutingJSON(agents, nil)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	if len(cfg.Members) != 0 || len(cfg.Channels) != 0 {
		t.Errorf("expected empty routing for gateway-only agent, got %d members, %d channels", len(cfg.Members), len(cfg.Channels))
	}
}

func TestGenerateRoutingJSON_MixedRuntimes(t *testing.T) {
	agents := []provider.AgentConfig{
		{Name: "ocagent", Type: provider.AgentTypeUser, Runtime: "openclaw",
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0001111111"}}, GatewayPort: 18789},
		{Name: "hermes1", Type: provider.AgentTypeUser, Runtime: "hermes",
			Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0002222222"}}, GatewayPort: 18790},
	}

	resolver := func(agentRuntime, platform string) string {
		if agentRuntime == "hermes" {
			return "/webhooks/" + platform
		}
		return "/slack/events"
	}

	data, err := GenerateRoutingJSON(agents, resolver)
	if err != nil {
		t.Fatalf("GenerateRoutingJSON() error: %v", err)
	}

	var cfg RoutingConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	// OpenClaw agent should get /slack/events
	if got := cfg.Members["U0001111111"]; got != "http://conga-ocagent:18789/slack/events" {
		t.Errorf("OpenClaw route = %q, want /slack/events path", got)
	}
	// Hermes agent should get /webhooks/slack
	if got := cfg.Members["U0002222222"]; got != "http://conga-hermes1:18790/webhooks/slack" {
		t.Errorf("Hermes route = %q, want /webhooks/slack path", got)
	}
}
