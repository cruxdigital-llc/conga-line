// Package channels defines the Channel interface and shared types for
// pluggable messaging platform integrations (Slack, Discord, etc.).
package channels

// ChannelBinding links an agent to a specific endpoint on a messaging platform.
type ChannelBinding struct {
	Platform string `json:"platform"`        // registered channel name: "slack"
	ID       string `json:"id"`              // platform-specific identifier
	Label    string `json:"label,omitempty"` // optional human label (e.g. "#general")
}

// SecretDef declares a secret that a channel needs during admin setup.
type SecretDef struct {
	Name       string // file/key name: "slack-bot-token"
	EnvVar     string // env var: "SLACK_BOT_TOKEN"
	Prompt     string // interactive prompt: "Slack bot token (xoxb-...)"
	Required   bool   // true = channel cannot function without it
	RouterOnly bool   // true = only needed by the router, not agent containers
}

// RoutingEntry is one entry for a channel's routing config.
type RoutingEntry struct {
	Section string // routing.json top-level key: "channels", "members"
	Key     string // platform identifier: Slack channel/member ID
	URL     string // webhook URL: "http://conga-name:port/slack/events"
}

// Channel defines the contract for a messaging platform integration.
type Channel interface {
	// Name returns the platform identifier used in ChannelBinding.Platform.
	Name() string

	// ValidateBinding checks whether id is valid for the given agent type.
	// agentType is "user" or "team".
	ValidateBinding(agentType string, id string) error

	// SharedSecrets returns the secrets this channel needs during admin setup.
	SharedSecrets() []SecretDef

	// HasCredentials returns true if secretValues contains the required secrets.
	HasCredentials(secretValues map[string]string) bool

	// OpenClawChannelConfig returns the channels.{platform} section for openclaw.json.
	OpenClawChannelConfig(agentType string, binding ChannelBinding, secretValues map[string]string) (map[string]any, error)

	// OpenClawPluginConfig returns the plugins.entries.{platform} section.
	OpenClawPluginConfig(enabled bool) map[string]any

	// RoutingEntries returns routing.json entries for this agent+binding.
	RoutingEntries(agentType string, binding ChannelBinding, agentName string, port int) []RoutingEntry

	// AgentEnvVars returns env vars for the agent container's env file.
	AgentEnvVars(secretValues map[string]string) map[string]string

	// RouterEnvVars returns env vars for the channel proxy's env file.
	RouterEnvVars(secretValues map[string]string) map[string]string

	// WebhookPath returns the container endpoint path (e.g., "/slack/events").
	WebhookPath() string

	// BehaviorTemplateVars returns template substitution vars for behavior files.
	BehaviorTemplateVars(agentType string, binding ChannelBinding) map[string]string
}
