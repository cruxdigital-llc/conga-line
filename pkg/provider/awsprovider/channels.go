package awsprovider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	awsutil "github.com/cruxdigital-llc/conga-line/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/discovery"
	"github.com/cruxdigital-llc/conga-line/pkg/policy"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/provider/managedhost"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
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

	// Runtime-compatibility gate: refuse unsupported channel × runtime
	// combinations (e.g. telegram on openclaw) before ValidateBinding.
	resolvedRuntime := string(runtime.ResolveRuntime(a.Runtime, ""))
	if supported, reason := ch.SupportsRuntime(resolvedRuntime); !supported {
		return fmt.Errorf("channel %s is not supported for the %s runtime: %s", binding.Platform, resolvedRuntime, reason)
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

	// Restart the router so it picks up the new routing.json. It runs
	// --network host and reaches the agent via its published 127.0.0.1:<hostPort>,
	// so there is no per-agent bridge attach.
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

// manifestMissingSentinel is echoed by ReadProxyManifest's shell script when
// the manifest file does not exist on the host. Chosen so it cannot collide
// with a valid manifest (which is pretty-printed JSON starting with '{').
const manifestMissingSentinel = "__CONGA_MANIFEST_MISSING__"

// ReadProxyManifest runs a `cat` on the instance to fetch the deployed
// egress policy manifest for an agent. Returns (nil, ErrNotFound) when the
// manifest file is absent — e.g. if the agent has not been deployed with
// the drift-aware pipeline yet. Other read errors (permission denied, SSM
// failure, disk I/O) surface as distinct errors so the drift UI can tell
// the operator "error" vs "not deployed".
func (p *AWSProvider) ReadProxyManifest(ctx context.Context, agentName string) ([]byte, error) {
	if !isValidAgentName(agentName) {
		return nil, fmt.Errorf("invalid agent name %q", agentName)
	}
	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return nil, err
	}
	manifestPath := fmt.Sprintf("/opt/conga/config/%s", policy.EgressManifestFileName(agentName))
	// isValidAgentName restricts agentName to [a-z0-9-] so the Go-quoted
	// string produced by %q contains no shell-special characters. Keep these
	// two facts together when reviewing changes to either site.
	//
	// The script always exits 0 with Success. "Missing" is communicated by
	// the sentinel on stdout so the caller can distinguish a missing file
	// (benign — report "not deployed") from an SSM/permission error
	// (operator needs to know).
	script := fmt.Sprintf(
		`if [ -f %q ]; then cat %q; else echo %s; fi`,
		manifestPath, manifestPath, manifestMissingSentinel,
	)
	result, err := p.runOnInstance(ctx, instanceID, script, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("reading manifest for %q: %w", agentName, err)
	}
	if result == nil || result.Status != "Success" {
		stderr := ""
		if result != nil {
			stderr = strings.TrimSpace(result.Stderr)
		}
		return nil, fmt.Errorf("reading manifest for %q: SSM command failed (stderr: %s)", agentName, stderr)
	}
	if strings.TrimSpace(result.Stdout) == manifestMissingSentinel {
		return nil, fmt.Errorf("manifest for agent %q: %w", agentName, provider.ErrNotFound)
	}
	return []byte(result.Stdout), nil
}

// isValidAgentName mirrors the agent-name safety check in deploy-egress.sh.tmpl
// and keeps injection-prone characters out of the interpolated shell command.
// Any change here must be mirrored at call sites that interpolate agentName
// into a shell command (see ReadProxyManifest).
func isValidAgentName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !ok {
			return false
		}
		if i == 0 && r == '-' {
			return false
		}
	}
	return true
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

// mcpOAuthExistsMarker is echoed by the on-instance existence probe only when the
// target file is present.
const mcpOAuthExistsMarker = "__conga_mcp_oauth_exists__"

// mcpFileExists interprets the result of a `test -f … && printf MARKER` SSM probe.
// It deliberately reads stdout, not the error: awsutil.RunCommand returns
// (result, nil) for both "Success" and "Failed" invocation statuses, so a missing
// file (non-zero exit) does not surface as a Go error. Reading err instead of
// stdout here would make the probe always report "present", silently disabling
// restore on AWS.
func mcpFileExists(res *awsutil.RunCommandResult) bool {
	return res != nil && strings.Contains(res.Stdout, mcpOAuthExistsMarker)
}

// regenerateAgentConfigOnInstance generates openclaw.json and .env in Go using
// common.GenerateAgentFiles(), then uploads them to the EC2 instance via SSM.
// This ensures the same config generation logic as local and remote providers.
func (p *AWSProvider) regenerateAgentConfigOnInstance(ctx context.Context, instanceID string, cfg provider.AgentConfig) error {
	// Resolve the per-agent overlay directory FIRST, before any AWS calls.
	// AWS doesn't persist a repo_path the way the remote provider does, so we
	// resolve the agents directory by:
	//   1. Trying ./agents (cwd-relative — works when operator is at repo root).
	//   2. Walking up from cwd to find the congaline go.mod, then trying
	//      <repo-root>/agents (works when operator is in any subdir of the repo).
	// If neither resolves we fail closed: writing a defaults-only openclaw.json
	// over the live one would silently strip per-agent model and runtime
	// overrides (e.g. agent.yaml pointing at a self-hosted LLM). MCP-launched
	// invocations are especially susceptible because the operator never sees
	// the stderr warning, so silent skip is unsafe.
	behaviorDir := resolveAWSBehaviorDir()
	if behaviorDir == "" {
		cwd, _ := os.Getwd()
		return fmt.Errorf("cannot locate the congaline agents/ overlay directory from cwd %q. Refusing to regenerate %s/openclaw.json without overlay context: doing so would silently strip per-agent model and runtime overrides on the host. Run `conga` from the congaline repo root or any subdirectory of it", cwd, cfg.Name)
	}
	overlay, err := common.LoadAgentOverlay(behaviorDir, cfg)
	if err != nil {
		return fmt.Errorf("failed to load agent overlay: %w", err)
	}

	shared, err := p.readSharedSecrets(ctx)
	if err != nil {
		return err
	}
	perAgent, err := p.readAgentSecrets(ctx, cfg.Name)
	if err != nil {
		return err
	}

	// Preserve the gateway token if it already exists on the instance;
	// otherwise generate a fresh one. OpenClaw v2026.3.22+ refuses to bind a
	// non-loopback gateway without auth, so writing a token-less config would
	// break the next container restart for any existing agent.
	dataDir := fmt.Sprintf("/opt/conga/data/%s", cfg.Name)
	gatewayToken, err := p.readExistingGatewayTokenOnInstance(ctx, instanceID, dataDir+"/openclaw.json")
	if err != nil {
		return fmt.Errorf("failed to read existing gateway token: %w", err)
	}
	if gatewayToken == "" {
		gatewayToken, err = generateAWSToken()
		if err != nil {
			return fmt.Errorf("failed to generate gateway token: %w", err)
		}
	}

	openClawJSON, envContent, err := common.RuntimeGenerateAgentFilesWithOverlay(
		runtime.ResolveRuntime(cfg.Runtime, ""), cfg, shared, perAgent, gatewayToken, overlay,
	)
	if err != nil {
		return err
	}

	// Upload openclaw.json
	if err := p.uploadFile(ctx, instanceID, dataDir+"/openclaw.json", openClawJSON, "0644"); err != nil {
		return fmt.Errorf("failed to upload openclaw.json: %w", err)
	}

	// Ensure the admin-owned include exists (the "$include" target). A missing
	// target invalidates the whole OpenClaw config. Create "{}" only if absent —
	// never clobber admin customization. (Filename = openclaw.AgentCustomConfigFile.)
	customPath := dataDir + "/agent-custom.json"
	if _, err := p.runOnInstance(ctx, instanceID,
		fmt.Sprintf("test -e '%s' || printf '{}\\n' > '%s'", customPath, customPath), 30*time.Second); err != nil {
		return fmt.Errorf("failed to ensure agent-custom.json: %w", err)
	}

	// Deploy the Conga-managed declarative custom-config layers (#31) from their
	// committed sources (or "{}" if absent): fleet-custom.json (all agents) +
	// agent-managed-custom.json (per-agent). These re-sync every refresh, so
	// fleet/per-agent changes propagate. Re-protected root:root 0444 below.
	srcs := common.ResolveCustomConfigSources(behaviorDir, cfg)
	// Fail closed before uploading anything: a reserved-key violation in the fleet
	// source would break/compromise every agent (blast radius). #31 T6.1.
	if err := common.ValidateManagedConfigSources(srcs); err != nil {
		return err
	}
	managedLayers := []struct {
		name    string
		content []byte
	}{
		{"fleet-custom.json", srcs.Fleet},
		{"agent-managed-custom.json", srcs.PerAgent},
	}
	for _, l := range managedLayers {
		content := l.content
		if content == nil {
			content = []byte("{}\n")
		}
		if err := p.uploadFile(ctx, instanceID, dataDir+"/"+l.name, content, "0444"); err != nil {
			return fmt.Errorf("failed to upload %s: %w", l.name, err)
		}
		// Refresh the managed-include integrity baseline so check-config-integrity.sh
		// doesn't flag this propagated change as tampering. Mirrors deploy-agents.sh
		// (which seeds it on the bash path) and the local/remote
		// saveManagedIncludeBaselines. Without this, the first content-changing
		// `conga refresh` on AWS leaves the baseline stale → false violation. (#31 P5)
		mh := sha256.Sum256(content)
		mBaselinePath := fmt.Sprintf("/opt/conga/config/%s-%s.sha256", cfg.Name, l.name)
		if err := p.uploadFile(ctx, instanceID, mBaselinePath, []byte(hex.EncodeToString(mh[:])+"\n"), "0444"); err != nil {
			return fmt.Errorf("failed to upload integrity baseline for %s: %w", l.name, err)
		}
	}

	// Upload .env
	envPath := fmt.Sprintf("/opt/conga/config/%s.env", cfg.Name)
	if err := p.uploadFile(ctx, instanceID, envPath, envContent, "0400"); err != nil {
		return fmt.Errorf("failed to upload env file: %w", err)
	}

	// Restore any persisted MCP OAuth credential blobs into the data dir
	// (cold-only) before the container starts, so an OAuth server comes up
	// authenticated after a fresh provision / data-dir loss. Uploaded 0600
	// before the chown below, which makes them container-user-owned on the
	// encrypted EBS data volume — the managed-host secure posture. On-disk copies
	// (kept fresh by the running runtime) are authoritative and never overwritten.
	if rt, rtErr := runtime.Get(runtime.ResolveRuntime(cfg.Runtime, "")); rtErr == nil {
		if oauthDir := rt.OAuthStateDir(); oauthDir != "" {
			targetDir := dataDir + "/" + oauthDir
			if _, err := p.runOnInstance(ctx, instanceID, fmt.Sprintf("mkdir -p '%s'", targetDir), 30*time.Second); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not create MCP OAuth dir for %s: %v\n", cfg.Name, err)
			} else {
				n, rerr := common.RestoreMCPOAuth(perAgent,
					func(f string) bool {
						// Presence must be read from stdout, NOT err: RunCommand
						// returns (result, nil) for both "Success" and "Failed" SSM
						// statuses, so a missing file (non-zero exit → "Failed")
						// still yields err == nil. Echo a marker only when present.
						res, err := p.runOnInstance(ctx, instanceID,
							fmt.Sprintf("test -f '%s/%s' && printf '%%s' %s", targetDir, f, mcpOAuthExistsMarker), 30*time.Second)
						if err != nil {
							// Couldn't verify (transport error) — assume present so we
							// never clobber a possibly-authoritative on-disk copy;
							// best-effort, the next refresh retries.
							return true
						}
						return mcpFileExists(res)
					},
					func(f string, d []byte) error {
						return p.uploadFile(ctx, instanceID, targetDir+"/"+f, d, "0600")
					},
				)
				if rerr != nil {
					fmt.Fprintf(os.Stderr, "Warning: restoring MCP OAuth credentials for %s: %v\n", cfg.Name, rerr)
				} else if n > 0 {
					fmt.Fprintf(os.Stderr, "Restored %d MCP OAuth credential(s) for %s from Secrets Manager.\n", n, cfg.Name)
				}
			}
		}
	}

	// Fix ownership for container user (SFTP uploads create root-owned files)
	if _, err := p.runOnInstance(ctx, instanceID, fmt.Sprintf("chown -R 1000:1000 '%s'", dataDir), 30*time.Second); err != nil {
		return fmt.Errorf("failed to fix ownership on %s: %w", dataDir, err)
	}

	// Re-protect the admin include as read-only to the agent uid. The recursive
	// chown above made it 1000-owned; root:root 0444 means a prompt-injected
	// agent (uid 1000) cannot edit agent-custom.json to inject a channel binding
	// (the allowlist is a security boundary). Admins edit it via SSM as root.
	// Defense-in-depth — the effective-allowlist integrity check is the
	// load-bearing control. uid 1000 still reads it (0444), so $include resolves.
	// Re-protect all managed include files (admin-drift + the two #31 managed
	// layers) root:root 0444 after the recursive chown — read-only to the agent,
	// still readable for $include resolution.
	if _, err := p.runOnInstance(ctx, instanceID,
		fmt.Sprintf("chown root:root '%s' '%s/fleet-custom.json' '%s/agent-managed-custom.json' && chmod 0444 '%s' '%s/fleet-custom.json' '%s/agent-managed-custom.json'",
			customPath, dataDir, dataDir, customPath, dataDir, dataDir), 30*time.Second); err != nil {
		return fmt.Errorf("failed to protect managed include files: %w", err)
	}

	// Refresh the integrity baseline so check-config-integrity.sh doesn't
	// flag our own regeneration as tampering. Bootstrap writes the .sha256
	// from the bash-emitted config; Go regen writes differently-formatted
	// JSON, so without this rewrite every refresh produces a baseline
	// mismatch.
	hash := sha256.Sum256(openClawJSON)
	baseline := hex.EncodeToString(hash[:]) + "\n"
	baselinePath := fmt.Sprintf("/opt/conga/config/%s-config.json.sha256", cfg.Name)
	if err := p.uploadFile(ctx, instanceID, baselinePath, []byte(baseline), "0444"); err != nil {
		return fmt.Errorf("failed to upload integrity baseline: %w", err)
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

	// Generate routing.json in Go and ship it through the managed-host transport
	// seam. The router runs with --network host (see routerRestartScript), so the
	// loopback resolver targets each agent's published 127.0.0.1:<hostPort> rather
	// than a per-agent Docker bridge network.
	return managedhost.WriteRoutingJSON(ctx, p.transport(instanceID),
		"/opt/conga/config/routing.json", agents, common.LoopbackWebhookResolver(""))
}

// restartRouterOnInstance restarts the router container on the EC2 instance.
// Assumes router.env and routing.json are already uploaded.
// routerRestartScript stops, reinstalls deps for, and restarts the conga-router
// container. The router runs with --network host and reaches each agent through
// its published 127.0.0.1:<hostPort> (the loopback topology, see
// common.GenerateRoutingJSON) — it is NOT attached to per-agent bridge networks.
// The dep-check, the npm-install mount, and the run-step volume mount MUST all
// target /opt/conga/router/slack — the slack/telegram split moved the router
// source (and package.json) there, and the parent dir has none.
// TestRouterRestartScriptUsesSlackPath guards against regressing to the
// pre-split /opt/conga/router path and against re-introducing the bridge attach.
const routerRestartScript = `set -euo pipefail

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

# Install npm deps if needed. The router source + package.json live under
# /opt/conga/router/slack (the slack/telegram split — matching the bootstrap and
# the run-step volume mount below); the parent dir has no package.json.
if [ ! -d /opt/conga/router/slack/node_modules ]; then
  docker run --rm -v /opt/conga/router/slack:/app -w /app node:22-alpine npm install --production
fi

# Start router on the host network. With --network host the router reaches each
# agent through its published 127.0.0.1:<hostPort> (routing.json uses loopback
# URLs), so no per-agent bridge attach is needed — and on Docker 25 +
# kernel 6.1.174 that bridge attach is impossible (route conflict). See
# specs/2026-06-11_bugfix_router-host-networking/.
docker run -d \
  --name conga-router \
  --restart unless-stopped \
  --network host \
  --env-file /opt/conga/config/router.env \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --memory 128m \
  -v /opt/conga/router/slack:/app:ro \
  -v /opt/conga/config/routing.json:/opt/conga/config/routing.json:ro \
  node:22-alpine node /app/src/index.js

echo "Router restarted"
`

func (p *AWSProvider) restartRouterOnInstance(ctx context.Context, instanceID string) error {
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, routerRestartScript, 120*time.Second)
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
	// Write atomically: stage in <path>.tmp.$$, then mv into place. The
	// rename(2) is atomic on the same filesystem, so a killed or failing
	// decode/chmod/cp leaves the existing file untouched rather than
	// half-written. The previous version is preserved as <path>.bak so a
	// botched refresh can be recovered without an SSH session. The trap
	// sweeps the staging file on any error path; after a successful mv it
	// no longer exists and `rm -f` is a no-op.
	script := fmt.Sprintf(`set -euo pipefail
target=%q
tmp="${target}.tmp.$$"
trap 'rm -f "$tmp"' EXIT
mkdir -p "$(dirname "$target")"
echo %q | base64 -d > "$tmp"
chmod %s "$tmp"
if [ -f "$target" ]; then
  cp -p "$target" "${target}.bak"
fi
mv "$tmp" "$target"`, path, encoded, mode)

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

// ResetAgentCustomConfig backs up the admin-owned customization file on the
// instance and resets it to "{}" (re-protected root:root 0444). Discards admin
// drift; the caller refreshes to reload the gateway.
func (p *AWSProvider) ResetAgentCustomConfig(ctx context.Context, name string) error {
	cfg, err := p.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	rt, err := runtime.Get(runtime.ResolveRuntime(cfg.Runtime, ""))
	if err != nil {
		return err
	}
	fname := rt.CustomConfigFileName()
	if fname == "" {
		return fmt.Errorf("runtime %s has no customization file to reset", rt.Name())
	}
	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/opt/conga/data/%s/%s", name, fname)
	cmd := fmt.Sprintf("if [ -e '%s' ]; then cp -p '%s' '%s.bak.'$(date +%%s); fi; "+
		"printf '{}\\n' > '%s' && chown root:root '%s' && chmod 0444 '%s'",
		path, path, path, path, path, path)
	if _, err := p.runOnInstance(ctx, instanceID, cmd, 30*time.Second); err != nil {
		return fmt.Errorf("reset %s: %w", fname, err)
	}
	return nil
}

// readExistingGatewayTokenOnInstance reads the gateway auth token out of an
// agent's openclaw.json on the EC2 instance via SSM. Returns "" when the
// file is missing or the token is unset (the agent is fresh and a new token
// must be minted).
func (p *AWSProvider) readExistingGatewayTokenOnInstance(ctx context.Context, instanceID, configPath string) (string, error) {
	// Quote the path for shell safety. jq -e returns 1 when the field is null
	// or missing; we swallow that into "" so callers don't see it as an error.
	script := fmt.Sprintf(`if [ -r %s ]; then jq -er '.gateway.auth.token // ""' %s 2>/dev/null || true; fi`,
		shellSingleQuote(configPath), shellSingleQuote(configPath))
	result, err := p.runOnInstance(ctx, instanceID, script, 30*time.Second)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

// generateAWSToken returns a cryptographically-random 32-byte token rendered
// as 64 lowercase hex characters. Matches the local/remote provider format.
func generateAWSToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// shellSingleQuote wraps s in single quotes for safe inclusion in a shell
// command. Escapes embedded single quotes via the standard '\” idiom.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// resolveAWSBehaviorDir locates the live agent-overlays directory for the
// AWS provider's config-gen path. Delegates to
// `common.ResolveOperatorBehaviorDir`, which walks up from cwd to find the
// congaline repo root and (critically) detects git worktrees, redirecting
// to the main worktree's agents/ when invoked from inside one.
//
// Callers MUST treat "" as a hard error and refuse to regenerate agent
// config: writing a defaults-only openclaw.json over the live one silently
// strips per-agent model and runtime overrides (e.g. agent.yaml pointing
// at a self-hosted LLM). Emitting a warning and proceeding is unsafe under
// MCP because stderr is invisible to the operator.
func resolveAWSBehaviorDir() string {
	return common.ResolveOperatorBehaviorDir()
}
