package remoteprovider

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	posixpath "path"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/policy"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// ReadProxyManifest downloads the deployed egress policy manifest for an
// agent from the remote host. Returns (nil, ErrNotFound) when the
// manifest file is absent on the host.
func (p *RemoteProvider) ReadProxyManifest(ctx context.Context, agentName string) ([]byte, error) {
	if err := p.requireSSH(); err != nil {
		return nil, err
	}
	path := posixpath.Join(p.remoteConfigDir(), policy.EgressManifestFileName(agentName))
	data, err := p.ssh.Download(path)
	if err != nil {
		if isNotExist(err) {
			return nil, fmt.Errorf("manifest for agent %q: %w", agentName, provider.ErrNotFound)
		}
		return nil, fmt.Errorf("reading manifest for %q: %w", agentName, err)
	}
	return data, nil
}

// isNotExist reports whether err indicates a missing file, across the two
// code paths in SSHClient.Download:
//   - SFTP path: wraps os.ErrNotExist / fs.ErrNotExist via *sftp.StatusError,
//     which satisfies errors.Is.
//   - cat fallback: wraps the shell's "No such file or directory" stderr
//     plus a session exit error. Detected here via substring match on the
//     common variants.
func isNotExist(err error) bool {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, fs.ErrNotExist) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "file does not exist") ||
		strings.Contains(msg, "No such file") ||
		strings.Contains(msg, "not found")
}

// AddChannel configures a messaging channel platform on the remote host by
// uploading its shared secrets and starting (or restarting) the router.
func (p *RemoteProvider) AddChannel(ctx context.Context, platform string, secrets map[string]string) error {
	ch, ok := channels.Get(platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q; registered: %s", platform, channels.RegisteredNames())
	}

	// Validate required secrets are present
	for _, def := range ch.SharedSecrets() {
		if def.Required {
			if v, ok := secrets[def.Name]; !ok || v == "" {
				return fmt.Errorf("missing required secret %q for %s", def.Name, platform)
			}
		}
	}

	// Upload each secret
	for _, def := range ch.SharedSecrets() {
		val, ok := secrets[def.Name]
		if !ok || val == "" {
			continue
		}
		if err := p.writeSharedSecret(def.Name, val); err != nil {
			return fmt.Errorf("failed to save %s: %w", def.Name, err)
		}
	}

	// Build and upload router.env
	if err := p.writeRouterEnv(); err != nil {
		return fmt.Errorf("failed to write router env: %w", err)
	}

	// Start (or restart) the router to pick up the new config
	if err := p.ensureRouter(ctx, true); err != nil {
		return fmt.Errorf("failed to start router: %w", err)
	}

	return nil
}

// RemoveChannel removes a channel platform from the remote host: stops the
// router, strips bindings from all agents, regenerates configs, deletes shared
// secrets, and removes router.env.
func (p *RemoteProvider) RemoveChannel(ctx context.Context, platform string) error {
	ch, ok := channels.Get(platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q", platform)
	}

	// Check if actually configured
	shared, err := p.readSharedSecrets()
	if err != nil {
		return fmt.Errorf("failed to read shared secrets: %w", err)
	}
	if !ch.HasCredentials(shared.Values) {
		return nil // not configured, no-op
	}

	// 1. Stop and remove router
	if p.containerExists(ctx, routerContainer) {
		if err := p.removeContainer(ctx, routerContainer); err != nil {
			return fmt.Errorf("failed to remove router container: %w", err)
		}
	}

	// 2. Strip bindings from all agents and regenerate their configs
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}
	for _, a := range agents {
		if a.ChannelBinding(platform) != nil {
			a.Channels = channels.FilterBindings(a.Channels, platform)
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
		path := shellQuote(posixpath.Join(p.sharedSecretsDir(), def.Name))
		if _, err := p.ssh.Run(ctx, fmt.Sprintf("rm -f %s", path)); err != nil {
			return fmt.Errorf("failed to delete secret %s: %w", def.Name, err)
		}
	}

	// 5. Remove router.env
	routerEnvPath := shellQuote(posixpath.Join(p.remoteConfigDir(), "router.env"))
	if _, err := p.ssh.Run(ctx, fmt.Sprintf("rm -f %s", routerEnvPath)); err != nil {
		return fmt.Errorf("failed to remove router.env: %w", err)
	}

	return nil
}

// ListChannels returns the status of all registered channel platforms.
func (p *RemoteProvider) ListChannels(ctx context.Context) ([]provider.ChannelStatus, error) {
	shared, err := p.readSharedSecrets()
	if err != nil {
		return nil, fmt.Errorf("failed to read shared secrets: %w", err)
	}

	routerStates := map[string]bool{}
	if p.containerExists(ctx, routerContainer) {
		if state, err := p.inspectState(ctx, routerContainer); err == nil && state.Running {
			routerStates["slack"] = true
		}
	}

	agents, err := p.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return common.BuildChannelStatuses(agents, shared, routerStates), nil
}

// BindChannel adds a channel binding to an existing agent on the remote host.
func (p *RemoteProvider) BindChannel(ctx context.Context, agentName string, binding channels.ChannelBinding) error {
	ch, ok := channels.Get(binding.Platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q", binding.Platform)
	}

	// Check channel is configured
	shared, err := p.readSharedSecrets()
	if err != nil {
		return fmt.Errorf("failed to read shared secrets: %w", err)
	}
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
		return fmt.Errorf("agent %q already has a %s binding: %w",
			agentName, binding.Platform, provider.ErrBindingExists)
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
	if p.containerExists(ctx, routerContainer) {
		if err := p.connectNetwork(ctx, networkName(agentName), routerContainer); err != nil {
			return fmt.Errorf("failed to connect router to agent network %s: %w", agentName, err)
		}
	}

	// Restart router to pick up updated routing.json
	if err := p.restartRouter(ctx); err != nil {
		return fmt.Errorf("binding saved but router restart failed: %w", err)
	}

	// Restart agent to pick up new config
	if !a.Paused {
		if err := p.RefreshAgent(ctx, agentName); err != nil {
			return fmt.Errorf("binding saved but agent refresh failed (restart manually): %w", err)
		}
	}

	return nil
}

// UnbindChannel removes a channel binding from an agent on the remote host.
func (p *RemoteProvider) UnbindChannel(ctx context.Context, agentName string, platform string) error {
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
		return fmt.Errorf("agent %q has no %s binding", agentName, platform)
	}

	// Remove binding
	a.Channels = channels.FilterBindings(a.Channels, platform)
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

	// Restart router to pick up updated routing.json
	if err := p.restartRouter(ctx); err != nil {
		return fmt.Errorf("unbind saved but router restart failed: %w", err)
	}

	// Restart agent to pick up new config
	if !a.Paused {
		if err := p.RefreshAgent(ctx, agentName); err != nil {
			return fmt.Errorf("unbind saved but agent refresh failed (restart manually): %w", err)
		}
	}

	return nil
}

// --- helpers ---

// regenerateAgentConfig regenerates an agent's openclaw.json and .env on the remote host.
func (p *RemoteProvider) regenerateAgentConfig(ctx context.Context, cfg provider.AgentConfig) error {
	shared, err := p.readSharedSecrets()
	if err != nil {
		return err
	}
	perAgent, err := p.readAgentSecrets(cfg.Name)
	if err != nil {
		return err
	}

	openClawJSON, envContent, err := common.GenerateAgentFiles(cfg, shared, perAgent)
	if err != nil {
		return err
	}

	dataDir := p.remoteDataSubDir(cfg.Name)
	if err := p.ssh.Upload(posixpath.Join(dataDir, "openclaw.json"), openClawJSON, 0644); err != nil {
		return err
	}
	envPath := posixpath.Join(p.remoteConfigDir(), cfg.Name+".env")
	// Remove old env file first — it's mode 0400 and can't be overwritten in place
	p.ssh.Run(ctx, fmt.Sprintf("rm -f %s", shellQuote(envPath)))
	if err := p.ssh.Upload(envPath, envContent, 0400); err != nil {
		return err
	}

	// Re-chown data dir for container user (node, uid 1000)
	if _, err := p.ssh.Run(ctx, fmt.Sprintf("chown -R 1000:1000 %s", shellQuote(dataDir))); err != nil {
		return fmt.Errorf("failed to set ownership on %s: %w", dataDir, err)
	}
	return nil
}

// writeRouterEnv builds and uploads the router.env file from all configured channels.
func (p *RemoteProvider) writeRouterEnv() error {
	shared, err := p.readSharedSecrets()
	if err != nil {
		return err
	}

	routerEnvPath := posixpath.Join(p.remoteConfigDir(), "router.env")
	return p.ssh.Upload(routerEnvPath, []byte(common.BuildRouterEnvContent(shared)), 0400)
}
