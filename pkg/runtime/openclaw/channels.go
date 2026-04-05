package openclaw

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
)

func (r *Runtime) ChannelConfig(agentType string, binding channels.ChannelBinding, secretValues map[string]string) (map[string]any, error) {
	ch, ok := channels.Get(binding.Platform)
	if !ok {
		return nil, fmt.Errorf("unknown channel %q", binding.Platform)
	}
	return ch.OpenClawChannelConfig(agentType, binding, secretValues)
}

func (r *Runtime) PluginConfig(platform string, enabled bool) map[string]any {
	ch, ok := channels.Get(platform)
	if !ok {
		return nil
	}
	return ch.OpenClawPluginConfig(enabled)
}

func (r *Runtime) WebhookPath(platform string) string {
	ch, ok := channels.Get(platform)
	if !ok {
		return ""
	}
	return ch.WebhookPath()
}
