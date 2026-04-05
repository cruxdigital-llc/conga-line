package awsprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	awsutil "github.com/cruxdigital-llc/conga-line/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/discovery"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// AddChannel configures a messaging channel platform by storing shared secrets
// in Secrets Manager, generating router config, and starting the router.
func (p *AWSProvider) AddChannel(ctx context.Context, platform string, secrets map[string]string) error {
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

	// Store each secret in Secrets Manager
	for _, def := range ch.SharedSecrets() {
		val, ok := secrets[def.Name]
		if !ok || val == "" {
			continue
		}
		secretPath := fmt.Sprintf("conga/shared/%s", def.Name)
		if err := awsutil.SetSecret(ctx, p.clients.SecretsManager, secretPath, val); err != nil {
			return fmt.Errorf("failed to save %s: %w", def.Name, err)
		}
	}

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	// Generate and upload router.env
	shared, err := p.readSharedSecrets(ctx)
	if err != nil {
		return fmt.Errorf("failed to read shared secrets: %w", err)
	}
	routerEnv := common.BuildRouterEnvContent(shared)
	if err := p.uploadFile(ctx, instanceID, "/opt/conga/config/router.env", []byte(routerEnv), "0400"); err != nil {
		return fmt.Errorf("failed to upload router.env: %w", err)
	}

	// Generate and upload routing.json
	if err := p.regenerateRoutingOnInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("failed to regenerate routing: %w", err)
	}

	// Start (or restart) the router
	if err := p.restartRouterOnInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("failed to restart router: %w", err)
	}

	return nil
}

// RemoveChannel removes a channel platform: stops the router, strips bindings
// from all agents, deletes shared secrets from Secrets Manager.
func (p *AWSProvider) RemoveChannel(ctx context.Context, platform string) error {
	ch, ok := channels.Get(platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q", platform)
	}

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	// 1. Stop router on instance
	if _, err := p.runOnInstance(ctx, instanceID, "docker rm -f conga-router 2>/dev/null || true", 30*time.Second); err != nil {
		return fmt.Errorf("failed to stop router (instance may be unreachable): %w", err)
	}

	// 2. Strip bindings from all agents, regenerate configs, update SSM
	agents, err := discovery.ListAgents(ctx, p.clients.SSM)
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}
	var warnings []string
	for _, a := range agents {
		if a.ChannelBinding(platform) != nil {
			a.Channels = channels.FilterBindings(a.Channels, platform)
			if err := p.saveAgentToSSM(ctx, a); err != nil {
				return fmt.Errorf("failed to update agent %s: %w", a.Name, err)
			}
			if !a.Paused {
				if err := p.regenerateAgentConfigOnInstance(ctx, instanceID, a); err != nil {
					warnings = append(warnings, fmt.Sprintf("failed to regenerate config for %s: %v", a.Name, err))
				}
			}
		}
	}

	// 3. Regenerate routing.json (now without the removed channel's entries)
	if err := p.regenerateRoutingOnInstance(ctx, instanceID); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to regenerate routing: %v", err))
	}

	// 4. Delete shared secrets for this platform
	for _, def := range ch.SharedSecrets() {
		secretPath := fmt.Sprintf("conga/shared/%s", def.Name)
		if err := awsutil.DeleteSecret(ctx, p.clients.SecretsManager, secretPath); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to delete secret %s: %v", def.Name, err))
		}
	}

	// 5. Remove router.env
	if _, err := p.runOnInstance(ctx, instanceID, "rm -f /opt/conga/config/router.env", 30*time.Second); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to remove router.env: %v", err))
	}

	if len(warnings) > 0 {
		return fmt.Errorf("channel removed but cleanup incomplete: %s", strings.Join(warnings, "; "))
	}
	return nil
}

// ListChannels returns the status of all registered channel platforms.
func (p *AWSProvider) ListChannels(ctx context.Context) ([]provider.ChannelStatus, error) {
	shared, err := p.readSharedSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read shared secrets: %w", err)
	}

	// Check router status on instance
	routerStates := map[string]bool{}
	instanceID, findErr := p.findInstance(ctx)
	if findErr == nil {
		result, err := p.runOnInstance(ctx, instanceID,
			`docker inspect conga-router --format '{{.State.Running}}' 2>/dev/null || echo "false"`,
			30*time.Second)
		if err == nil && result != nil && result.Status == "Success" {
			routerStates["slack"] = strings.TrimSpace(result.Stdout) == "true"
		}
	}

	agents, err := p.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return common.BuildChannelStatuses(agents, shared, routerStates), nil
}

// BindChannel adds a channel binding to an existing agent.
func (p *AWSProvider) BindChannel(ctx context.Context, agentName string, binding channels.ChannelBinding) error {
	ch, ok := channels.Get(binding.Platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q", binding.Platform)
	}

	// Check channel is configured
	shared, err := p.readSharedSecrets(ctx)
	if err != nil {
		return fmt.Errorf("failed to read shared secrets: %w", err)
	}
	if !ch.HasCredentials(shared.Values) {
		return fmt.Errorf("%s is not configured; run 'conga channels add %s' first", binding.Platform, binding.Platform)
	}

	// Load agent from SSM
	a, err := discovery.ResolveAgent(ctx, p.clients.SSM, agentName)
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

	// Add binding and save to SSM
	a.Channels = append(a.Channels, binding)
	if err := p.saveAgentToSSM(ctx, *a); err != nil {
		return err
	}

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	// Regenerate agent config files (openclaw.json, .env) on instance
	if err := p.regenerateAgentConfigOnInstance(ctx, instanceID, *a); err != nil {
		return fmt.Errorf("failed to regenerate config for %s: %w", agentName, err)
	}

	// Regenerate routing.json
	if err := p.regenerateRoutingOnInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("failed to regenerate routing: %w", err)
	}

	// Connect router to agent network and restart
	if _, err := p.runOnInstance(ctx, instanceID,
		fmt.Sprintf("docker network connect conga-%s conga-router 2>/dev/null || true", agentName),
		30*time.Second); err != nil {
		return fmt.Errorf("failed to connect router to agent network: %w", err)
	}

	if err := p.restartRouterOnInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("binding saved but router restart failed: %w", err)
	}

	// Refresh agent to restart container with new config
	if !a.Paused {
		if err := p.RefreshAgent(ctx, agentName); err != nil {
			return fmt.Errorf("binding saved but agent refresh failed (restart manually): %w", err)
		}
	}

	return nil
}

// UnbindChannel removes a channel binding from an agent.
func (p *AWSProvider) UnbindChannel(ctx context.Context, agentName string, platform string) error {
	if _, ok := channels.Get(platform); !ok {
		return fmt.Errorf("unknown channel platform %q", platform)
	}

	// Load agent from SSM
	a, err := discovery.ResolveAgent(ctx, p.clients.SSM, agentName)
	if err != nil {
		return err
	}

	// Check if agent has this binding
	if a.ChannelBinding(platform) == nil {
		return fmt.Errorf("agent %q has no %s binding", agentName, platform)
	}

	// Remove binding and save to SSM
	a.Channels = channels.FilterBindings(a.Channels, platform)
	if err := p.saveAgentToSSM(ctx, *a); err != nil {
		return err
	}

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	// Regenerate agent config files on instance
	if err := p.regenerateAgentConfigOnInstance(ctx, instanceID, *a); err != nil {
		return fmt.Errorf("failed to regenerate config for %s: %w", agentName, err)
	}

	// Regenerate routing.json
	if err := p.regenerateRoutingOnInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("failed to regenerate routing: %w", err)
	}

	// Restart router
	if err := p.restartRouterOnInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("unbind saved but router restart failed: %w", err)
	}

	// Refresh agent
	if !a.Paused {
		if err := p.RefreshAgent(ctx, agentName); err != nil {
			return fmt.Errorf("unbind saved but agent refresh failed (restart manually): %w", err)
		}
	}

	return nil
}

// --- helpers ---

// readSharedSecrets reads channel shared secrets from AWS Secrets Manager.
func (p *AWSProvider) readSharedSecrets(ctx context.Context) (common.SharedSecrets, error) {
	secrets := common.SharedSecrets{Values: make(map[string]string)}

	for _, ch := range channels.All() {
		for _, def := range ch.SharedSecrets() {
			secretPath := fmt.Sprintf("conga/shared/%s", def.Name)
			val, err := awsutil.GetSecretValue(ctx, p.clients.SecretsManager, secretPath)
			if err != nil {
				return secrets, fmt.Errorf("failed to read shared secret %s: %w", def.Name, err)
			}
			if val != "" && val != "REPLACE_ME" {
				secrets.Values[def.Name] = val
			}
		}
	}

	// Google OAuth secrets are optional (gateway authentication only) and independent
	// of channel functionality. Errors reading them should not block channel operations.
	if id, err := awsutil.GetSecretValue(ctx, p.clients.SecretsManager, "conga/shared/google-client-id"); err == nil {
		if id != "" && id != "REPLACE_ME" {
			secrets.GoogleClientID = id
		}
	}
	if secret, err := awsutil.GetSecretValue(ctx, p.clients.SecretsManager, "conga/shared/google-client-secret"); err == nil {
		if secret != "" && secret != "REPLACE_ME" {
			secrets.GoogleClientSecret = secret
		}
	}

	return secrets, nil
}

// readAgentSecrets reads per-agent secrets from Secrets Manager.
func (p *AWSProvider) readAgentSecrets(ctx context.Context, agentName string) (map[string]string, error) {
	prefix := fmt.Sprintf("conga/agents/%s/", agentName)
	entries, err := awsutil.ListSecrets(ctx, p.clients.SecretsManager, prefix)
	if err != nil {
		return nil, err
	}

	secrets := make(map[string]string)
	for _, e := range entries {
		val, err := awsutil.GetSecretValue(ctx, p.clients.SecretsManager, fmt.Sprintf("conga/agents/%s/%s", agentName, e.Name))
		if err != nil {
			return nil, fmt.Errorf("failed to read agent secret %s/%s: %w", agentName, e.Name, err)
		}
		if val != "" {
			secrets[e.Name] = val
		}
	}
	return secrets, nil
}

// saveAgentToSSM writes an agent config to SSM Parameter Store.
// Name is excluded from JSON because it's derived from the SSM parameter path.
func (p *AWSProvider) saveAgentToSSM(ctx context.Context, a provider.AgentConfig) error {
	// SSM derives name from parameter path, so exclude it from the JSON body.
	type ssmAgent struct {
		Type        provider.AgentType        `json:"type"`
		Channels    []channels.ChannelBinding `json:"channels,omitempty"`
		GatewayPort int                       `json:"gateway_port"`
		IAMIdentity string                    `json:"iam_identity,omitempty"`
		Paused      bool                      `json:"paused,omitempty"`
	}
	agentConfigJSON, err := json.Marshal(ssmAgent{
		Type:        a.Type,
		Channels:    a.Channels,
		GatewayPort: a.GatewayPort,
		IAMIdentity: a.IAMIdentity,
		Paused:      a.Paused,
	})
	if err != nil {
		return fmt.Errorf("failed to serialize agent config: %w", err)
	}

	paramName := fmt.Sprintf("/conga/agents/%s", a.Name)
	return awsutil.PutParameter(ctx, p.clients.SSM, paramName, string(agentConfigJSON))
}

// regenerateAgentConfigOnInstance generates openclaw.json and .env in Go using
// common.GenerateAgentFiles(), then uploads them to the EC2 instance via SSM.
// This ensures the same config generation logic as local and remote providers.
func (p *AWSProvider) regenerateAgentConfigOnInstance(ctx context.Context, instanceID string, cfg provider.AgentConfig) error {
	shared, err := p.readSharedSecrets(ctx)
	if err != nil {
		return err
	}
	perAgent, err := p.readAgentSecrets(ctx, cfg.Name)
	if err != nil {
		return err
	}

	openClawJSON, envContent, err := common.GenerateAgentFiles(cfg, shared, perAgent)
	if err != nil {
		return err
	}

	// Upload openclaw.json
	dataDir := fmt.Sprintf("/opt/conga/data/%s", cfg.Name)
	if err := p.uploadFile(ctx, instanceID, dataDir+"/openclaw.json", openClawJSON, "0644"); err != nil {
		return fmt.Errorf("failed to upload openclaw.json: %w", err)
	}

	// Upload .env
	envPath := fmt.Sprintf("/opt/conga/config/%s.env", cfg.Name)
	if err := p.uploadFile(ctx, instanceID, envPath, envContent, "0400"); err != nil {
		return fmt.Errorf("failed to upload env file: %w", err)
	}

	// Fix ownership for container user (SFTP uploads create root-owned files)
	if _, err := p.runOnInstance(ctx, instanceID, fmt.Sprintf("chown -R 1000:1000 '%s'", dataDir), 30*time.Second); err != nil {
		return fmt.Errorf("failed to fix ownership on %s: %w", dataDir, err)
	}

	return nil
}

// regenerateRoutingOnInstance generates routing.json in Go using
// common.GenerateRoutingJSON(), then uploads it to the EC2 instance.
func (p *AWSProvider) regenerateRoutingOnInstance(ctx context.Context, instanceID string) error {
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	routingJSON, err := common.GenerateRoutingJSON(agents, nil)
	if err != nil {
		return fmt.Errorf("failed to generate routing: %w", err)
	}

	return p.uploadFile(ctx, instanceID, "/opt/conga/config/routing.json", routingJSON, "0644")
}

// restartRouterOnInstance restarts the router container on the EC2 instance.
// Assumes router.env and routing.json are already uploaded.
func (p *AWSProvider) restartRouterOnInstance(ctx context.Context, instanceID string) error {
	script := `set -euo pipefail

# Skip if no router.env (channel not configured)
if [ ! -f /opt/conga/config/router.env ]; then
  echo "No router.env — skipping router"
  exit 0
fi

# Stop and remove old router — retry until name is released
docker stop conga-router 2>/dev/null || true
for i in 1 2 3; do
  docker rm -f conga-router 2>/dev/null || true
  docker inspect conga-router >/dev/null 2>&1 || break
  sleep "$i"
done

# Install npm deps if needed
if [ ! -d /opt/conga/router/node_modules ]; then
  docker run --rm -v /opt/conga/router:/app -w /app node:22-alpine npm install --production
fi

# Start router
docker run -d \
  --name conga-router \
  --restart unless-stopped \
  --env-file /opt/conga/config/router.env \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --memory 128m \
  -v /opt/conga/router:/app:ro \
  -v /opt/conga/config/routing.json:/opt/conga/config/routing.json:ro \
  node:22-alpine node /app/src/index.js

# Connect router to each agent's Docker network
for NET in $(docker network ls --filter name=conga- --format '{{.Name}}' | grep -v '^conga-router$'); do
  docker network connect "$NET" conga-router 2>/dev/null || true
done

echo "Router restarted"
`

	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, script, 120*time.Second)
	if err != nil {
		return err
	}
	if result.Status != "Success" {
		return fmt.Errorf("router restart failed:\n%s\n%s", result.Stdout, result.Stderr)
	}
	return nil
}

// uploadFile writes content to a file on the EC2 instance via SSM RunCommand.
// Uses base64 encoding to safely transmit binary/special-character content.
func (p *AWSProvider) uploadFile(ctx context.Context, instanceID, path string, content []byte, mode string) error {
	encoded := base64.StdEncoding.EncodeToString(content)
	script := fmt.Sprintf(
		"mkdir -p \"$(dirname '%s')\" && echo '%s' | base64 -d > '%s' && chmod %s '%s'",
		path, encoded, path, mode, path,
	)

	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, script, 60*time.Second)
	if err != nil {
		return err
	}
	if result.Status != "Success" {
		return fmt.Errorf("failed to write %s: %s", path, result.Stderr)
	}
	return nil
}

// runOnInstance runs a command on the EC2 instance via SSM.
func (p *AWSProvider) runOnInstance(ctx context.Context, instanceID, script string, timeout time.Duration) (*awsutil.RunCommandResult, error) {
	return awsutil.RunCommand(ctx, p.clients.SSM, instanceID, script, timeout)
}
