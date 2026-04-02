// Package slack implements the Channel interface for Slack integration.
package slack

import (
	"fmt"
	"regexp"

	"github.com/cruxdigital-llc/conga-line/cli/pkg/channels"
)

func init() {
	channels.Register(&Slack{})
}

var (
	memberIDPattern  = regexp.MustCompile(`^U[A-Z0-9]{8,12}$`)
	channelIDPattern = regexp.MustCompile(`^C[A-Z0-9]{8,12}$`)
)

// Slack implements the channels.Channel interface.
type Slack struct{}

func (s *Slack) Name() string { return "slack" }

func (s *Slack) ValidateBinding(agentType, id string) error {
	switch agentType {
	case "user":
		if !memberIDPattern.MatchString(id) {
			return fmt.Errorf("invalid Slack member ID %q: must match U + 8-12 alphanumeric chars (e.g., U0123456789)", id)
		}
	case "team":
		if !channelIDPattern.MatchString(id) {
			return fmt.Errorf("invalid Slack channel ID %q: must match C + 8-12 alphanumeric chars (e.g., C0123456789)", id)
		}
	}
	return nil
}

func (s *Slack) SharedSecrets() []channels.SecretDef {
	return []channels.SecretDef{
		{Name: "slack-bot-token", EnvVar: "SLACK_BOT_TOKEN", Prompt: "Slack bot token (xoxb-...)", Required: true},
		{Name: "slack-signing-secret", EnvVar: "SLACK_SIGNING_SECRET", Prompt: "Slack signing secret", Required: true},
		{Name: "slack-app-token", EnvVar: "SLACK_APP_TOKEN", Prompt: "Slack app-level token (xapp-...)", Required: false, RouterOnly: true},
	}
}

func (s *Slack) HasCredentials(sv map[string]string) bool {
	return sv["slack-bot-token"] != "" && sv["slack-signing-secret"] != ""
}

func (s *Slack) OpenClawChannelConfig(agentType string, binding channels.ChannelBinding, sv map[string]string) (map[string]any, error) {
	cfg := map[string]any{
		"mode":              "http",
		"enabled":           true,
		"botToken":          sv["slack-bot-token"],
		"signingSecret":     sv["slack-signing-secret"],
		"webhookPath":       "/slack/events",
		"userTokenReadOnly": true,
		"streaming":         "partial",
		"nativeStreaming":   true,
	}

	switch agentType {
	case "user":
		cfg["groupPolicy"] = "disabled"
		cfg["dmPolicy"] = "allowlist"
		if binding.ID != "" {
			cfg["allowFrom"] = []string{binding.ID}
		}
		cfg["dm"] = map[string]any{"enabled": true}
	case "team":
		cfg["groupPolicy"] = "allowlist"
		cfg["dmPolicy"] = "disabled"
		if binding.ID != "" {
			cfg["channels"] = map[string]any{
				binding.ID: map[string]any{"allow": true, "requireMention": false},
			}
		}
	}

	return cfg, nil
}

func (s *Slack) OpenClawPluginConfig(enabled bool) map[string]any {
	return map[string]any{"enabled": enabled}
}

func (s *Slack) RoutingEntries(agentType string, binding channels.ChannelBinding, agentName string, port int) []channels.RoutingEntry {
	if binding.ID == "" {
		return nil
	}
	url := fmt.Sprintf("http://conga-%s:%d/slack/events", agentName, port)
	switch agentType {
	case "user":
		return []channels.RoutingEntry{{Section: "members", Key: binding.ID, URL: url}}
	case "team":
		return []channels.RoutingEntry{{Section: "channels", Key: binding.ID, URL: url}}
	}
	return nil
}

func (s *Slack) AgentEnvVars(sv map[string]string) map[string]string {
	vars := map[string]string{}
	if v := sv["slack-bot-token"]; v != "" {
		vars["SLACK_BOT_TOKEN"] = v
	}
	if v := sv["slack-signing-secret"]; v != "" {
		vars["SLACK_SIGNING_SECRET"] = v
	}
	return vars
}

func (s *Slack) RouterEnvVars(sv map[string]string) map[string]string {
	vars := map[string]string{}
	if v := sv["slack-app-token"]; v != "" {
		vars["SLACK_APP_TOKEN"] = v
	}
	if v := sv["slack-signing-secret"]; v != "" {
		vars["SLACK_SIGNING_SECRET"] = v
	}
	return vars
}

func (s *Slack) WebhookPath() string { return "/slack/events" }

func (s *Slack) BehaviorTemplateVars(agentType string, binding channels.ChannelBinding) map[string]string {
	return map[string]string{"SLACK_ID": binding.ID}
}
