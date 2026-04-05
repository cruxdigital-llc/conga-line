package hermes

import (
	"github.com/cruxdigital-llc/conga-line/pkg/channels"
)

func (r *Runtime) ChannelConfig(agentType string, binding channels.ChannelBinding, secretValues map[string]string) (map[string]any, error) {
	// Hermes doesn't embed channel config in config.yaml the way OpenClaw does.
	// Channel configuration is done via env vars and the webhook adapter.
	return nil, nil
}

func (r *Runtime) PluginConfig(platform string, enabled bool) map[string]any {
	// Hermes doesn't have OpenClaw's plugin system.
	return nil
}

func (r *Runtime) WebhookPath(platform string) string {
	return "/webhooks/" + platform
}
