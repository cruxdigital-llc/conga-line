package discovery

import (
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

func TestChannelBinding_Found(t *testing.T) {
	a := &provider.AgentConfig{
		Name: "test",
		Type: "user",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "U123"},
			{Platform: "discord", ID: "D456"},
		},
	}

	b := a.ChannelBinding("slack")
	if b == nil {
		t.Fatal("expected to find slack binding")
	}
	if b.ID != "U123" {
		t.Errorf("expected ID U123, got %s", b.ID)
	}
}

func TestChannelBinding_NotFound(t *testing.T) {
	a := &provider.AgentConfig{
		Name: "test",
		Type: "user",
		Channels: []channels.ChannelBinding{
			{Platform: "slack", ID: "U123"},
		},
	}

	if a.ChannelBinding("discord") != nil {
		t.Error("expected nil for missing platform")
	}
}

func TestChannelBinding_Empty(t *testing.T) {
	a := &provider.AgentConfig{Name: "test", Type: "user"}

	if a.ChannelBinding("slack") != nil {
		t.Error("expected nil for agent with no channels")
	}
}

func TestParseAgentConfig_WithChannels(t *testing.T) {
	json := `{"type":"team","channels":[{"platform":"slack","id":"C0ABC123"}],"gateway_port":18790}`
	cfg, err := parseAgentConfig("/conga/agents/myteam", json)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "myteam" {
		t.Errorf("expected name myteam, got %s", cfg.Name)
	}
	if cfg.Type != provider.AgentTypeTeam {
		t.Errorf("expected type team, got %s", cfg.Type)
	}
	if len(cfg.Channels) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(cfg.Channels))
	}
	if cfg.Channels[0].Platform != "slack" || cfg.Channels[0].ID != "C0ABC123" {
		t.Errorf("unexpected channel: %+v", cfg.Channels[0])
	}
}

func TestParseAgentConfig_WithoutChannels(t *testing.T) {
	json := `{"type":"user","gateway_port":18789}`
	cfg, err := parseAgentConfig("/conga/agents/solo", json)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "solo" {
		t.Errorf("expected name solo, got %s", cfg.Name)
	}
	if len(cfg.Channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(cfg.Channels))
	}
	if cfg.ChannelBinding("slack") != nil {
		t.Error("expected nil for agent with no channels")
	}
}
