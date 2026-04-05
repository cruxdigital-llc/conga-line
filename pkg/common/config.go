package common

import (
	"fmt"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"

	// Import openclaw runtime so it registers via init().
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

// SharedSecrets is an alias for provider.SharedSecrets.
// Kept here for backward compatibility — callers can use either common.SharedSecrets
// or provider.SharedSecrets interchangeably.
type SharedSecrets = provider.SharedSecrets

// HasAnyChannel returns true if any registered channel has its required credentials present.
func HasAnyChannel(shared SharedSecrets) bool {
	for _, ch := range channels.All() {
		if ch.HasCredentials(shared.Values) {
			return true
		}
	}
	return false
}

// BuildChannelStatuses builds the channel status list from the given agents,
// shared secrets, and router state. This is the shared logic used by both
// local and remote providers' ListChannels implementations.
func BuildChannelStatuses(agents []provider.AgentConfig, shared SharedSecrets, routerStates map[string]bool) []provider.ChannelStatus {
	var result []provider.ChannelStatus
	for _, ch := range channels.All() {
		status := provider.ChannelStatus{
			Platform:   ch.Name(),
			Configured: ch.HasCredentials(shared.Values),
		}
		status.RouterRunning = routerStates[ch.Name()] && status.Configured
		for _, a := range agents {
			if a.ChannelBinding(ch.Name()) != nil {
				status.BoundAgents = append(status.BoundAgents, a.Name)
			}
		}
		result = append(result, status)
	}
	return result
}

// BuildRouterEnvContent generates the router.env file content from all
// configured channels' router env vars.
func BuildRouterEnvContent(shared SharedSecrets) string {
	var buf strings.Builder
	for _, ch := range channels.All() {
		if ch.HasCredentials(shared.Values) {
			for k, v := range ch.RouterEnvVars(shared.Values) {
				fmt.Fprintf(&buf, "%s=%s\n", k, v)
			}
		}
	}
	return buf.String()
}

// GenerateAgentFiles produces the config and .env file content for an agent
// using the OpenClaw runtime. For runtime-aware callers, use
// RuntimeGenerateAgentFiles instead.
func GenerateAgentFiles(cfg provider.AgentConfig, shared SharedSecrets, perAgent map[string]string) (configJSON []byte, envContent []byte, err error) {
	return RuntimeGenerateAgentFiles(runtime.RuntimeOpenClaw, cfg, shared, perAgent)
}

// RuntimeGenerateAgentFiles produces the config and .env file content for an
// agent using the specified runtime.
func RuntimeGenerateAgentFiles(rtName runtime.RuntimeName, cfg provider.AgentConfig, shared SharedSecrets, perAgent map[string]string) (configBytes []byte, envContent []byte, err error) {
	rt, err := runtime.Get(rtName)
	if err != nil {
		return nil, nil, err
	}
	configBytes, err = rt.GenerateConfig(runtime.ConfigParams{
		Agent:   cfg,
		Secrets: shared,
	})
	if err != nil {
		return nil, nil, err
	}
	envContent = rt.GenerateEnvFile(runtime.EnvParams{
		Agent:    cfg,
		Secrets:  shared,
		PerAgent: perAgent,
	})
	return configBytes, envContent, nil
}

// GenerateOpenClawConfig produces the openclaw.json content for an agent.
// Backward-compatible wrapper — delegates to the OpenClaw runtime.
func GenerateOpenClawConfig(agent provider.AgentConfig, secrets SharedSecrets, gatewayToken string) ([]byte, error) {
	rt, err := runtime.Get(runtime.RuntimeOpenClaw)
	if err != nil {
		return nil, err
	}
	return rt.GenerateConfig(runtime.ConfigParams{
		Agent:        agent,
		Secrets:      secrets,
		GatewayToken: gatewayToken,
	})
}

// GenerateEnvFile produces the .env file content for an agent container.
// Backward-compatible wrapper — delegates to the OpenClaw runtime.
func GenerateEnvFile(agent provider.AgentConfig, secrets SharedSecrets, perAgent map[string]string) []byte {
	rt, err := runtime.Get(runtime.RuntimeOpenClaw)
	if err != nil {
		return nil
	}
	return rt.GenerateEnvFile(runtime.EnvParams{
		Agent:    agent,
		Secrets:  secrets,
		PerAgent: perAgent,
	})
}
