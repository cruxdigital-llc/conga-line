package localprovider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/cli/internal/channels"
	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

// AddChannel configures a messaging channel platform by storing its shared
// secrets and starting the router. Idempotent: re-adding updates secrets.
func (p *LocalProvider) AddChannel(ctx context.Context, platform string, secrets map[string]string) error {
	ch, ok := channels.Get(platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q; registered: %s", platform, registeredChannelNames())
	}

	// Validate required secrets are present
	for _, def := range ch.SharedSecrets() {
		if def.Required {
			if v, ok := secrets[def.Name]; !ok || v == "" {
				return fmt.Errorf("missing required secret %q for %s", def.Name, platform)
			}
		}
	}

	// Write each secret
	for _, def := range ch.SharedSecrets() {
		val, ok := secrets[def.Name]
		if !ok || val == "" {
			continue
		}
		path := filepath.Join(p.sharedSecretsDir(), def.Name)
		if err := writeSecret(path, val); err != nil {
			return fmt.Errorf("failed to save %s: %w", def.Name, err)
		}
	}

	// Build router.env from all configured channels
	if err := p.writeRouterEnv(); err != nil {
		return fmt.Errorf("failed to write router env: %w", err)
	}

	// Start the router
	p.ensureRouter(ctx)

	return nil
}

// RemoveChannel removes a channel platform: stops the router, strips bindings
// from all agents, regenerates configs, and deletes shared secrets.
func (p *LocalProvider) RemoveChannel(ctx context.Context, platform string) error {
	ch, ok := channels.Get(platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q", platform)
	}

	// Check if actually configured
	shared, _ := p.readSharedSecrets()
	if !ch.HasCredentials(shared.Values) {
		return nil // not configured, no-op
	}

	// 1. Stop and remove router
	if containerExists(ctx, routerContainer) {
		removeContainer(ctx, routerContainer)
	}

	// 2. Strip bindings from all agents and regenerate their configs
	agents, _ := p.ListAgents(ctx)
	for _, a := range agents {
		if a.ChannelBinding(platform) != nil {
			a.Channels = filterBindings(a.Channels, platform)
			if err := p.saveAgentConfig(&a); err != nil {
				return fmt.Errorf("failed to update agent %s: %w", a.Name, err)
			}
			if !a.Paused {
				if err := p.regenerateAgentConfig(ctx, a); err != nil {
					return fmt.Errorf("failed to regenerate config for %s: %w", a.Name, err)
				}
			}
		}
	}

	// 3. Regenerate routing.json
	if err := p.regenerateRouting(ctx); err != nil {
		return fmt.Errorf("failed to regenerate routing: %w", err)
	}

	// 4. Delete shared secrets for this platform
	for _, def := range ch.SharedSecrets() {
		os.Remove(filepath.Join(p.sharedSecretsDir(), def.Name))
	}

	// 5. Remove router.env
	os.Remove(filepath.Join(p.configDir(), "router.env"))

	return nil
}

// ListChannels returns the status of all registered channel platforms.
func (p *LocalProvider) ListChannels(ctx context.Context) ([]provider.ChannelStatus, error) {
	shared, _ := p.readSharedSecrets()

	routerRunning := false
	if containerExists(ctx, routerContainer) {
		state, err := inspectState(ctx, routerContainer)
		if err == nil && state.Running {
			routerRunning = true
		}
	}

	agents, _ := p.ListAgents(ctx)

	var result []provider.ChannelStatus
	for _, ch := range channels.All() {
		status := provider.ChannelStatus{
			Platform:   ch.Name(),
			Configured: ch.HasCredentials(shared.Values),
		}
		status.RouterRunning = routerRunning && status.Configured
		for _, a := range agents {
			if a.ChannelBinding(ch.Name()) != nil {
				status.BoundAgents = append(status.BoundAgents, a.Name)
			}
		}
		result = append(result, status)
	}
	return result, nil
}

// BindChannel adds a channel binding to an existing agent.
func (p *LocalProvider) BindChannel(ctx context.Context, agentName string, binding channels.ChannelBinding) error {
	ch, ok := channels.Get(binding.Platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q", binding.Platform)
	}

	// Check channel is configured
	shared, _ := p.readSharedSecrets()
	if !ch.HasCredentials(shared.Values) {
		return fmt.Errorf("%s is not configured; run 'conga channels add %s' first", binding.Platform, binding.Platform)
	}

	// Load agent
	a, err := p.GetAgent(ctx, agentName)
	if err != nil {
		return err
	}

	// Check for duplicate binding
	if a.ChannelBinding(binding.Platform) != nil {
		return fmt.Errorf("agent %q already has a %s binding; use 'conga channels unbind %s %s' first",
			agentName, binding.Platform, agentName, binding.Platform)
	}

	// Validate binding
	if err := ch.ValidateBinding(string(a.Type), binding.ID); err != nil {
		return err
	}

	// Add binding
	a.Channels = append(a.Channels, binding)
	if err := p.saveAgentConfig(a); err != nil {
		return err
	}

	// Regenerate config files
	if err := p.regenerateAgentConfig(ctx, *a); err != nil {
		return fmt.Errorf("failed to regenerate config for %s: %w", agentName, err)
	}

	// Regenerate routing
	if err := p.regenerateRouting(ctx); err != nil {
		return fmt.Errorf("failed to regenerate routing: %w", err)
	}

	// Ensure router is connected to this agent's network
	if containerExists(ctx, routerContainer) {
		connectNetwork(ctx, networkName(agentName), routerContainer)
	}

	// Restart agent to pick up new config
	if !a.Paused {
		if err := p.RefreshAgent(ctx, agentName); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to refresh agent %s: %v (config updated, restart manually)\n", agentName, err)
		}
	}

	return nil
}

// UnbindChannel removes a channel binding from an agent.
func (p *LocalProvider) UnbindChannel(ctx context.Context, agentName string, platform string) error {
	if _, ok := channels.Get(platform); !ok {
		return fmt.Errorf("unknown channel platform %q", platform)
	}

	// Load agent
	a, err := p.GetAgent(ctx, agentName)
	if err != nil {
		return err
	}

	// Check if agent has this binding
	if a.ChannelBinding(platform) == nil {
		return nil // no-op
	}

	// Remove binding
	a.Channels = filterBindings(a.Channels, platform)
	if err := p.saveAgentConfig(a); err != nil {
		return err
	}

	// Regenerate config files
	if err := p.regenerateAgentConfig(ctx, *a); err != nil {
		return fmt.Errorf("failed to regenerate config for %s: %w", agentName, err)
	}

	// Regenerate routing
	if err := p.regenerateRouting(ctx); err != nil {
		return fmt.Errorf("failed to regenerate routing: %w", err)
	}

	// Restart agent to pick up new config
	if !a.Paused {
		if err := p.RefreshAgent(ctx, agentName); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to refresh agent %s: %v (config updated, restart manually)\n", agentName, err)
		}
	}

	return nil
}

// --- helpers ---

// regenerateAgentConfig regenerates an agent's openclaw.json and .env file.
func (p *LocalProvider) regenerateAgentConfig(ctx context.Context, cfg provider.AgentConfig) error {
	shared, err := p.readSharedSecrets()
	if err != nil {
		return err
	}
	perAgent, err := p.readAgentSecrets(cfg.Name)
	if err != nil {
		return err
	}

	openClawJSON, err := common.GenerateOpenClawConfig(cfg, shared, "")
	if err != nil {
		return err
	}
	dataDir := p.dataSubDir(cfg.Name)
	if err := os.WriteFile(filepath.Join(dataDir, "openclaw.json"), openClawJSON, 0644); err != nil {
		return err
	}

	envContent := common.GenerateEnvFile(cfg, shared, perAgent)
	envPath := filepath.Join(p.configDir(), cfg.Name+".env")
	if err := os.WriteFile(envPath, envContent, 0400); err != nil {
		return err
	}

	// Re-chown data dir for container user (node, uid 1000)
	exec.CommandContext(ctx, "chown", "-R", "1000:1000", dataDir).Run()
	return nil
}

// writeRouterEnv builds and writes the router.env file from all configured channels.
func (p *LocalProvider) writeRouterEnv() error {
	shared, err := p.readSharedSecrets()
	if err != nil {
		return err
	}

	var buf strings.Builder
	for _, ch := range channels.All() {
		if ch.HasCredentials(shared.Values) {
			for k, v := range ch.RouterEnvVars(shared.Values) {
				fmt.Fprintf(&buf, "%s=%s\n", k, v)
			}
		}
	}

	routerEnvPath := filepath.Join(p.configDir(), "router.env")
	return os.WriteFile(routerEnvPath, []byte(buf.String()), 0400)
}

// filterBindings returns bindings with the given platform removed.
func filterBindings(bindings []channels.ChannelBinding, platform string) []channels.ChannelBinding {
	var result []channels.ChannelBinding
	for _, b := range bindings {
		if b.Platform != platform {
			result = append(result, b)
		}
	}
	return result
}

// registeredChannelNames returns a comma-separated list of registered channel platforms.
func registeredChannelNames() string {
	var names []string
	for _, ch := range channels.All() {
		names = append(names, ch.Name())
	}
	return strings.Join(names, ", ")
}
