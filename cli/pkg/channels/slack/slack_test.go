package slack

import (
	"testing"

	"github.com/cruxdigital-llc/conga-line/cli/pkg/channels"
)

func TestValidateBinding_User(t *testing.T) {
	s := &Slack{}
	tests := []struct {
		id      string
		wantErr bool
	}{
		{"U0123456789", false},   // 10 chars
		{"UABCDEFGHIJ", false},   // 10 chars
		{"UA13HEGTS", false},     // 8 chars (older workspace)
		{"U012345678", false},    // 9 chars
		{"U01234567890", false},  // 11 chars
		{"C0123456789", true},    // channel ID, not member
		{"U01234", true},         // too short (< 8)
		{"U0123456789ABC", true}, // too long (> 12)
		{"u0123456789", true},    // lowercase
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			err := s.ValidateBinding("user", tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBinding(user, %q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestValidateBinding_Team(t *testing.T) {
	s := &Slack{}
	tests := []struct {
		id      string
		wantErr bool
	}{
		{"C0123456789", false},   // 10 chars
		{"CABCDEFGHIJ", false},   // 10 chars
		{"C0AQG67NPG9", false},   // 10 chars (real workspace ID)
		{"C012345678", false},    // 9 chars
		{"C01234567890", false},  // 11 chars
		{"U0123456789", true},    // member ID, not channel
		{"C01234", true},         // too short (< 8)
		{"C0123456789ABC", true}, // too long (> 12)
		{"c0123456789", true},    // lowercase
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			err := s.ValidateBinding("team", tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBinding(team, %q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestHasCredentials(t *testing.T) {
	s := &Slack{}
	tests := []struct {
		name string
		sv   map[string]string
		want bool
	}{
		{"both present", map[string]string{"slack-bot-token": "xoxb-123", "slack-signing-secret": "abc"}, true},
		{"missing bot token", map[string]string{"slack-signing-secret": "abc"}, false},
		{"missing signing secret", map[string]string{"slack-bot-token": "xoxb-123"}, false},
		{"both missing", map[string]string{}, false},
		{"empty values", map[string]string{"slack-bot-token": "", "slack-signing-secret": ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.HasCredentials(tt.sv); got != tt.want {
				t.Errorf("HasCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenClawChannelConfig_User(t *testing.T) {
	s := &Slack{}
	sv := map[string]string{"slack-bot-token": "xoxb-test", "slack-signing-secret": "secret"}
	binding := channels.ChannelBinding{Platform: "slack", ID: "U0123456789"}

	cfg, err := s.OpenClawChannelConfig("user", binding, sv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg["mode"] != "http" {
		t.Errorf("mode = %v, want http", cfg["mode"])
	}
	if cfg["dmPolicy"] != "allowlist" {
		t.Errorf("dmPolicy = %v, want allowlist", cfg["dmPolicy"])
	}
	if cfg["groupPolicy"] != "disabled" {
		t.Errorf("groupPolicy = %v, want disabled", cfg["groupPolicy"])
	}
	allowFrom, ok := cfg["allowFrom"].([]string)
	if !ok || len(allowFrom) != 1 || allowFrom[0] != "U0123456789" {
		t.Errorf("allowFrom = %v, want [U0123456789]", cfg["allowFrom"])
	}
	dm, ok := cfg["dm"].(map[string]any)
	if !ok || dm["enabled"] != true {
		t.Errorf("dm = %v, want {enabled: true}", cfg["dm"])
	}
}

func TestOpenClawChannelConfig_Team(t *testing.T) {
	s := &Slack{}
	sv := map[string]string{"slack-bot-token": "xoxb-test", "slack-signing-secret": "secret"}
	binding := channels.ChannelBinding{Platform: "slack", ID: "C9876543210"}

	cfg, err := s.OpenClawChannelConfig("team", binding, sv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg["groupPolicy"] != "allowlist" {
		t.Errorf("groupPolicy = %v, want allowlist", cfg["groupPolicy"])
	}
	if cfg["dmPolicy"] != "disabled" {
		t.Errorf("dmPolicy = %v, want disabled", cfg["dmPolicy"])
	}
	chans, ok := cfg["channels"].(map[string]any)
	if !ok {
		t.Fatalf("channels not a map: %v", cfg["channels"])
	}
	entry, ok := chans["C9876543210"].(map[string]any)
	if !ok {
		t.Fatalf("channel entry not a map: %v", chans["C9876543210"])
	}
	if entry["allow"] != true {
		t.Errorf("allow = %v, want true", entry["allow"])
	}
}

func TestOpenClawChannelConfig_NoID(t *testing.T) {
	s := &Slack{}
	sv := map[string]string{"slack-bot-token": "xoxb-test", "slack-signing-secret": "secret"}
	binding := channels.ChannelBinding{Platform: "slack", ID: ""}

	cfg, err := s.OpenClawChannelConfig("user", binding, sv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should still produce valid config, just without allowFrom
	if _, ok := cfg["allowFrom"]; ok {
		t.Error("expected no allowFrom when ID is empty")
	}
}

func TestRoutingEntries_User(t *testing.T) {
	s := &Slack{}
	binding := channels.ChannelBinding{Platform: "slack", ID: "U0123456789"}
	entries := s.RoutingEntries("user", binding, "myagent", 18789)

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Section != "members" {
		t.Errorf("section = %q, want members", entries[0].Section)
	}
	if entries[0].Key != "U0123456789" {
		t.Errorf("key = %q, want U0123456789", entries[0].Key)
	}
	if entries[0].URL != "http://conga-myagent:18789/slack/events" {
		t.Errorf("url = %q, want http://conga-myagent:18789/slack/events", entries[0].URL)
	}
}

func TestRoutingEntries_Team(t *testing.T) {
	s := &Slack{}
	binding := channels.ChannelBinding{Platform: "slack", ID: "C9876543210"}
	entries := s.RoutingEntries("team", binding, "leadership", 18790)

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Section != "channels" {
		t.Errorf("section = %q, want channels", entries[0].Section)
	}
}

func TestRoutingEntries_NoID(t *testing.T) {
	s := &Slack{}
	binding := channels.ChannelBinding{Platform: "slack", ID: ""}
	entries := s.RoutingEntries("user", binding, "myagent", 18789)

	if entries != nil {
		t.Errorf("expected nil entries for empty ID, got %v", entries)
	}
}

func TestAgentEnvVars(t *testing.T) {
	s := &Slack{}
	sv := map[string]string{"slack-bot-token": "xoxb-123", "slack-signing-secret": "sec"}
	vars := s.AgentEnvVars(sv)

	if vars["SLACK_BOT_TOKEN"] != "xoxb-123" {
		t.Errorf("SLACK_BOT_TOKEN = %q, want xoxb-123", vars["SLACK_BOT_TOKEN"])
	}
	if vars["SLACK_SIGNING_SECRET"] != "sec" {
		t.Errorf("SLACK_SIGNING_SECRET = %q, want sec", vars["SLACK_SIGNING_SECRET"])
	}
}

func TestRouterEnvVars(t *testing.T) {
	s := &Slack{}
	sv := map[string]string{"slack-app-token": "xapp-123", "slack-signing-secret": "sec"}
	vars := s.RouterEnvVars(sv)

	if vars["SLACK_APP_TOKEN"] != "xapp-123" {
		t.Errorf("SLACK_APP_TOKEN = %q, want xapp-123", vars["SLACK_APP_TOKEN"])
	}
	if vars["SLACK_SIGNING_SECRET"] != "sec" {
		t.Errorf("SLACK_SIGNING_SECRET = %q, want sec", vars["SLACK_SIGNING_SECRET"])
	}
}

func TestBehaviorTemplateVars(t *testing.T) {
	s := &Slack{}
	binding := channels.ChannelBinding{Platform: "slack", ID: "U0123456789"}
	vars := s.BehaviorTemplateVars("user", binding)

	if vars["SLACK_ID"] != "U0123456789" {
		t.Errorf("SLACK_ID = %q, want U0123456789", vars["SLACK_ID"])
	}
}

func TestSharedSecrets(t *testing.T) {
	s := &Slack{}
	secrets := s.SharedSecrets()

	if len(secrets) != 3 {
		t.Fatalf("expected 3 secrets, got %d", len(secrets))
	}

	// bot token: required, not router-only
	if secrets[0].Name != "slack-bot-token" || !secrets[0].Required || secrets[0].RouterOnly {
		t.Errorf("secret[0] = %+v", secrets[0])
	}
	// signing secret: required, not router-only
	if secrets[1].Name != "slack-signing-secret" || !secrets[1].Required || secrets[1].RouterOnly {
		t.Errorf("secret[1] = %+v", secrets[1])
	}
	// app token: not required, router-only
	if secrets[2].Name != "slack-app-token" || secrets[2].Required || !secrets[2].RouterOnly {
		t.Errorf("secret[2] = %+v", secrets[2])
	}
}
