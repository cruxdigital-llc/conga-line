// Package awsprovider implements the Provider interface using AWS services.
package awsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	awsutil "github.com/cruxdigital-llc/conga-line/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/discovery"
	"github.com/cruxdigital-llc/conga-line/pkg/policy"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/provider/managedhost"
	"github.com/cruxdigital-llc/conga-line/pkg/tunnel"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/cruxdigital-llc/conga-line/scripts"
)

const defaultInstanceTag = "conga-line-host"

// AWSProvider implements provider.Provider using AWS services.
type AWSProvider struct {
	clients *awsutil.Clients
	region  string
	profile string
}

// NewAWSProvider creates an AWS provider from the given config.
func NewAWSProvider(cfg *provider.Config) (provider.Provider, error) {
	c, err := awsutil.NewClients(context.Background(), cfg.Region, cfg.Profile)
	if err != nil {
		if cfg.Profile != "" {
			return nil, fmt.Errorf("failed to initialize AWS session (profile=%q): %w\nRun `aws sso login --profile %s` to authenticate", cfg.Profile, err, cfg.Profile)
		}
		return nil, fmt.Errorf("failed to initialize AWS session: %w\nRun `aws sso login --profile <your-profile>` to authenticate", err)
	}
	return &AWSProvider{
		clients: c,
		region:  cfg.Region,
		profile: cfg.Profile,
	}, nil
}

func init() {
	provider.Register(provider.ProviderAWS, NewAWSProvider)
}

func (p *AWSProvider) Name() string { return "aws" }

// --- Identity & Discovery ---

func (p *AWSProvider) WhoAmI(ctx context.Context) (*provider.Identity, error) {
	out, err := p.clients.STS.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("session expired or invalid. Run `conga auth login` to authenticate.\n%w", err)
	}

	arn := aws.ToString(out.Arn)
	accountID := aws.ToString(out.Account)

	sessionName := ""
	parts := strings.Split(arn, "/")
	if len(parts) >= 3 {
		sessionName = parts[len(parts)-1]
	}

	identity := &provider.Identity{
		Name:      sessionName,
		AccountID: accountID,
		ARN:       arn,
	}

	if sessionName != "" {
		agent, err := discovery.ResolveAgentByIAM(ctx, p.clients.SSM, sessionName)
		if err == nil {
			identity.AgentName = agent.Name
		}
	}

	return identity, nil
}

func (p *AWSProvider) ListAgents(ctx context.Context) ([]provider.AgentConfig, error) {
	return discovery.ListAgents(ctx, p.clients.SSM)
}

func (p *AWSProvider) GetAgent(ctx context.Context, name string) (*provider.AgentConfig, error) {
	return discovery.ResolveAgent(ctx, p.clients.SSM, name)
}

func (p *AWSProvider) ResolveAgentByIdentity(ctx context.Context) (*provider.AgentConfig, error) {
	identity, err := discovery.ResolveIdentity(ctx, p.clients.STS, p.clients.SSM)
	if err != nil {
		return nil, err
	}
	if identity.AgentName == "" {
		return nil, nil
	}
	return p.GetAgent(ctx, identity.AgentName)
}

// --- Agent Lifecycle ---

func (p *AWSProvider) ProvisionAgent(ctx context.Context, cfg provider.AgentConfig) error {
	agentConfigJSON, err := json.Marshal(map[string]interface{}{
		"type":         string(cfg.Type),
		"channels":     cfg.Channels,
		"gateway_port": cfg.GatewayPort,
		"iam_identity": cfg.IAMIdentity,
	})
	if err != nil {
		return fmt.Errorf("failed to serialize agent config: %w", err)
	}

	fmt.Println("Creating SSM parameter...")
	if err := awsutil.PutParameter(ctx, p.clients.SSM, fmt.Sprintf("/conga/agents/%s", cfg.Name), string(agentConfigJSON)); err != nil {
		return fmt.Errorf("failed to create agent config parameter: %w", err)
	}

	stateBucket, err := awsutil.GetParameter(ctx, p.clients.SSM, "/conga/config/state-bucket")
	if err != nil {
		// Only treat a genuine "parameter doesn't exist" as recoverable
		// (infra not provisioned yet — log a warning and continue without
		// agent overlay sync). Auth/network failures must abort instead
		// of silently provisioning a broken agent.
		var notFound *ssmtypes.ParameterNotFound
		if !errors.As(err, &notFound) {
			return fmt.Errorf("failed to resolve state bucket from SSM: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Warning: state bucket parameter not found — agent overlay files will not sync. Run 'terraform apply' first.\n")
		stateBucket = ""
	}

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	// Extract the Slack binding ID (if any) for the shell provisioning scripts.
	slackBinding := cfg.ChannelBinding("slack")
	slackID := ""
	if slackBinding != nil {
		slackID = slackBinding.ID
	}

	// Generate egress proxy config (deny-all when no policy, or from existing policy)
	egressPolicy, policyErr := policy.LoadEgressPolicy(provider.DefaultDataDir(), cfg.Name)
	if policyErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load egress policy: %v\n", policyErr)
	}
	egressMode := policy.EgressModeEnforce
	if egressPolicy != nil {
		egressMode = egressPolicy.Mode
	}
	envoyConfig, err := policy.GenerateProxyConf(egressPolicy)
	if err != nil {
		return fmt.Errorf("failed to generate egress config: %w", err)
	}
	proxyBootstrapJS := policy.ProxyBootstrapJS()

	// Pre-flight: warn if the overlay's primary or subagent endpoints are
	// not in the effective egress allowlist. Best-effort — if the local
	// agents/ overlay directory isn't resolvable (operator invoking from
	// outside the repo), skip the check. Provisioning itself proceeds via
	// the shell scripts uploaded to the instance; the overlay is consumed
	// inside the bootstrap heredoc rather than here.
	if behaviorDir := resolveAWSBehaviorDir(); behaviorDir != "" {
		overlay, overlayErr := common.LoadAgentOverlay(behaviorDir, cfg)
		if overlayErr != nil {
			common.Warn(ctx, "failed to load agent overlay for egress check: %v", overlayErr)
		} else {
			common.WarnOverlayEgressGaps(ctx, overlay, policy.EffectiveAllowedDomains(egressPolicy), cfg.Name)
			common.WarnCustomConfigEgressGaps(ctx, common.ResolveCustomConfigSources(behaviorDir, cfg), policy.EffectiveAllowedDomains(egressPolicy), cfg.Name)
		}
	}

	// Deterministic network plan so the infra-only provision script creates the
	// per-agent network on its static subnet and pins the egress proxy to .3 (R1 —
	// reboot-collision safety). The agent itself binds .2 later via the Go unit.
	provNet, err := managedhost.PlanAgentNetwork(cfg.GatewayPort, common.BaseGatewayPort)
	if err != nil {
		return fmt.Errorf("failed to plan network for %s: %w", cfg.Name, err)
	}

	var scriptTemplate string
	var templateData interface{}
	type provisionData struct {
		AgentName, SlackMemberID, SlackChannel, AWSRegion, StateBucket string
		GatewayPort                                                    int
		EnvoyConfig, EgressMode, ProxyBootstrapJS                      string
		SubnetCIDR, GatewayIP, ProxyIP                                 string
	}
	switch cfg.Type {
	case provider.AgentTypeUser:
		scriptTemplate = scripts.AddUserScript
		templateData = provisionData{
			AgentName: cfg.Name, SlackMemberID: slackID, AWSRegion: p.region,
			StateBucket: stateBucket, GatewayPort: cfg.GatewayPort,
			EnvoyConfig: envoyConfig, EgressMode: string(egressMode), ProxyBootstrapJS: proxyBootstrapJS,
			SubnetCIDR: provNet.SubnetCIDR, GatewayIP: provNet.GatewayIP, ProxyIP: provNet.ProxyIP,
		}
	case provider.AgentTypeTeam:
		scriptTemplate = scripts.AddTeamScript
		templateData = provisionData{
			AgentName: cfg.Name, SlackChannel: slackID, AWSRegion: p.region,
			StateBucket: stateBucket, GatewayPort: cfg.GatewayPort,
			EnvoyConfig: envoyConfig, EgressMode: string(egressMode), ProxyBootstrapJS: proxyBootstrapJS,
			SubnetCIDR: provNet.SubnetCIDR, GatewayIP: provNet.GatewayIP, ProxyIP: provNet.ProxyIP,
		}
	default:
		return fmt.Errorf("unknown agent type: %s", cfg.Type)
	}

	tmpl, err := template.New("provision").Parse(scriptTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse provision template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, templateData); err != nil {
		return fmt.Errorf("failed to render provision script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Provisioning agent %s...", cfg.Name))
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, buf.String(), 300*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		fmt.Fprintf(os.Stderr, "Setup output:\n%s\n%s\n", result.Stdout, result.Stderr)
		return fmt.Errorf("provisioning agent %s failed on instance", cfg.Name)
	}

	// Generate config + start the agent via the proven refresh path. The provision
	// scripts now set up per-agent INFRASTRUCTURE ONLY (env, data dir, behavior,
	// network, egress proxy) — they no longer generate openclaw.json or create/start
	// the systemd unit (managed-host engine, slice 2b). RefreshAgent does that:
	// regenerateAgentConfigOnInstance writes openclaw.json + the $include layers +
	// env in Go (canonical model + per-agent agent.yaml overlay), then
	// defineAndStartAgentService builds the docker-run command + systemd unit in Go
	// (with the ExecStartPost/ExecStopPost egress-iptables lifecycle), starts the
	// container, reconciles loopback routing.json, and deploys egress policy.
	//
	// FATAL: RefreshAgent is now the only thing that produces a running agent, so a
	// failure means provisioning failed (no config/unit/container). It errors hard if
	// the agents/ overlay dir isn't resolvable — the correct contract: provisioning
	// requires the overlay to generate config. The infra above is idempotent, so the
	// operator can fix the cwd/overlay and re-run `conga admin add-user`.
	if err := p.RefreshAgent(ctx, cfg.Name); err != nil {
		return fmt.Errorf("agent %s infrastructure was provisioned, but config generation + container start (via refresh) failed: %w", cfg.Name, err)
	}
	return nil
}

func (p *AWSProvider) RemoveAgent(ctx context.Context, name string, deleteSecrets bool) error {
	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	tmpl, err := template.New("removeagent").Parse(scripts.RemoveAgentScript)
	if err != nil {
		return fmt.Errorf("failed to parse remove-agent template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ ContainerID string }{ContainerID: name}); err != nil {
		return fmt.Errorf("failed to render remove-agent script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Removing agent %s...", name))
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, buf.String(), 60*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	var cleanupErrs []string
	if result.Status != "Success" {
		cleanupErrs = append(cleanupErrs, fmt.Sprintf("instance cleanup: %s", result.Stderr))
	}

	if err := awsutil.DeleteParameter(ctx, p.clients.SSM, fmt.Sprintf("/conga/agents/%s", name)); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Sprintf("SSM parameter: %v", err))
	}

	if deleteSecrets {
		secretPrefix := fmt.Sprintf("conga/agents/%s/", name)
		secrets, err := awsutil.ListSecrets(ctx, p.clients.SecretsManager, secretPrefix)
		if err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Sprintf("list secrets: %v", err))
		} else {
			for _, s := range secrets {
				if err := awsutil.DeleteSecret(ctx, p.clients.SecretsManager, fmt.Sprintf("conga/agents/%s/%s", name, s.Name)); err != nil {
					cleanupErrs = append(cleanupErrs, fmt.Sprintf("delete secret %s: %v", s.Name, err))
				}
			}
		}
	}

	if _, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, "/opt/conga/bin/update-dashboard.sh", 30*time.Second); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Sprintf("dashboard update: %v", err))
	}

	// Reconcile routing.json now that the agent's SSM param is deleted: the engine
	// regenerates it (loopback form) from the remaining agents, dropping the removed
	// agent's entry, then restarts the router. The removal script no longer edits
	// routing.json itself — its jq used the stale bridge-URL form, which never
	// matched the loopback routing, so the entry was left dangling until some other
	// refresh happened to rebuild it. Non-fatal (mirrors RefreshAgent step 3).
	if err := p.regenerateRoutingOnInstance(ctx, instanceID); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Sprintf("routing reconcile: %v", err))
	} else if err := p.restartRouterOnInstance(ctx, instanceID); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Sprintf("router restart: %v", err))
	}

	if len(cleanupErrs) > 0 {
		fmt.Fprintf(os.Stderr, "Agent %s removed, but %d cleanup operation(s) failed:\n", name, len(cleanupErrs))
		for _, e := range cleanupErrs {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		return fmt.Errorf("agent removed but %d cleanup step(s) failed", len(cleanupErrs))
	}
	return nil
}

// --- Container Operations ---

func (p *AWSProvider) GetStatus(ctx context.Context, agentName string) (*provider.AgentStatus, error) {
	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return nil, err
	}

	script := fmt.Sprintf(`
SVC=conga-%s
SVC_STATE=$(systemctl is-active $SVC 2>/dev/null || echo "inactive")
echo "SERVICE_STATE=$SVC_STATE"
if docker inspect $SVC >/dev/null 2>&1; then
  echo "CONTAINER_STATUS=$(docker inspect --format '{{.State.Status}}' $SVC)"
  echo "CONTAINER_STARTED=$(docker inspect --format '{{.State.StartedAt}}' $SVC)"
  echo "CONTAINER_RESTARTS=$(docker inspect --format '{{.RestartCount}}' $SVC)"
  STATS=$(docker stats --no-stream --format '{{.CPUPerc}}|{{.MemUsage}}|{{.PIDs}}' $SVC 2>/dev/null)
  echo "CONTAINER_STATS=$STATS"
  LOGS=$(docker logs $SVC --tail 50 2>&1)
  echo "BOOT_GATEWAY=$(echo "$LOGS" | grep -c '\[gateway\] listening on')"
  echo "BOOT_SLACK_START=$(echo "$LOGS" | grep -c '\[slack\].*starting provider')"
  echo "BOOT_SLACK_HTTP=$(echo "$LOGS" | grep -c '\[slack\] http mode listening')"
  echo "BOOT_SLACK_CHANNELS=$(echo "$LOGS" | grep -c '\[slack\] channels resolved')"
  echo "BOOT_ERROR=$(echo "$LOGS" | grep -ci 'error\|fatal\|panic' || true)"
else
  echo "CONTAINER_STATUS=not found"
fi
`, agentName)

	spin := ui.NewSpinner("Checking status...")
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, script, 30*time.Second)
	spin.Stop()
	if err != nil {
		return nil, err
	}

	kv := parseKeyValues(result.Stdout)
	return buildAgentStatus(agentName, kv), nil
}

func (p *AWSProvider) GetLogs(ctx context.Context, agentName string, lines int) (string, error) {
	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return "", err
	}

	script := fmt.Sprintf("docker logs conga-%s --tail %d 2>&1", agentName, lines)

	spin := ui.NewSpinner("Fetching logs...")
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, script, 30*time.Second)
	spin.Stop()
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

func (p *AWSProvider) ContainerExec(ctx context.Context, agentName string, command []string) (string, error) {
	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return "", err
	}

	// Build shell command: docker exec conga-<name> <args...>
	// Arguments are controlled (OpenClaw CLI commands), not user input.
	quoted := make([]string, len(command))
	for i, arg := range command {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	script := fmt.Sprintf("docker exec conga-%s %s 2>&1", agentName, strings.Join(quoted, " "))

	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, script, 30*time.Second)
	if err != nil {
		return "", err
	}
	if result.Status != "Success" {
		return "", fmt.Errorf("command failed: %s", result.Stderr)
	}
	return result.Stdout, nil
}

func (p *AWSProvider) PauseAgent(ctx context.Context, name string) error {
	agent, err := discovery.ResolveAgent(ctx, p.clients.SSM, name)
	if err != nil {
		return err
	}
	if agent.Paused {
		fmt.Printf("Agent %s is already paused.\n", name)
		return nil
	}

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	script, err := p.renderAgentScript(scripts.PauseAgentScript, "pause", name, agent)
	if err != nil {
		return err
	}

	spin := ui.NewSpinner(fmt.Sprintf("Pausing agent %s...", name))
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, script, 60*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}
	if result.Status != "Success" {
		fmt.Fprintf(os.Stderr, "Output:\n%s\n%s\n", result.Stdout, result.Stderr)
		return fmt.Errorf("pause failed on instance")
	}

	if err := p.setAgentPaused(ctx, name, agent, true); err != nil {
		return fmt.Errorf("container stopped but failed to update SSM: %w", err)
	}

	return nil
}

// UnpauseAgent brings a paused agent back online by:
//  1. Clearing the paused flag in SSM so RefreshAgent will run.
//  2. Calling RefreshAgent, which regenerates openclaw.json + .env,
//     (re)writes the systemd unit if missing, restarts the container,
//     reconciles routing.json, and (re)deploys the egress proxy.
//
// This replaces the previous bash-based unpause script that simply ran
// `systemctl start`. The script failed silently when the systemd unit
// file had been deleted (e.g. by manual cleanup), leaving the agent
// stuck in "paused but unstartable" state — a catch-22 because Refresh
// refuses to run on paused agents.
//
// On refresh failure, the paused flag is rolled back so the SSM state
// matches reality.
//
// (Designed as part of the delegation-routing spec follow-up work; see
// specs/2026-05-22_feature_delegation-routing/spec.md for the broader
// surface area.)
func (p *AWSProvider) UnpauseAgent(ctx context.Context, name string) error {
	agent, err := discovery.ResolveAgent(ctx, p.clients.SSM, name)
	if err != nil {
		return err
	}
	if !agent.Paused {
		fmt.Printf("Agent %s is not paused.\n", name)
		return nil
	}

	// Step 1: clear paused flag. RefreshAgent refuses paused agents.
	if err := p.setAgentPaused(ctx, name, agent, false); err != nil {
		return fmt.Errorf("failed to clear paused flag: %w", err)
	}

	// Step 2: refresh. This handles unit-file (re)creation, container
	// start, routing.json reconciliation, and egress proxy redeploy.
	spin := ui.NewSpinner(fmt.Sprintf("Unpausing agent %s (via refresh)...", name))
	refreshErr := p.RefreshAgent(ctx, name)
	spin.Stop()
	if refreshErr != nil {
		// Roll back the paused flag so SSM state matches reality.
		if rollbackErr := p.setAgentPaused(ctx, name, agent, true); rollbackErr != nil {
			return fmt.Errorf("refresh failed (%w) and rollback of paused flag also failed: %v", refreshErr, rollbackErr)
		}
		return fmt.Errorf("unpause failed during refresh: %w", refreshErr)
	}
	return nil
}

// RefreshAgent brings an agent's runtime state in sync with SSM:
//
//  1. Regenerate openclaw.json + .env via the Go path → upload via SSM
//  2. Build the docker-run command + systemd unit in Go (managed-host engine)
//     and apply via SSM: reconcile the static-IP network, write+enable the unit,
//     restart the container.
//  3. Reconcile routing.json from current SSM agent state + restart
//     router so changed bindings take effect. Refresh used to skip
//     routing.json, leaving the router with stale routes after a
//     pause/unpause via SSM put-parameter — that's why this step exists.
//  4. Redeploy egress proxy with the current policy file. This pushes
//     the latest allowlist (and validate/enforce mode) to the proxy on
//     disk + restarts it. Closes the bootstrap-time-config-never-updates
//     gap: without this step, the proxy keeps whatever Envoy config the
//     EC2 boot script wrote (deny-all until `conga policy deploy` runs).
//
// Each step is independent: a failure in routing.json reconciliation
// shouldn't undo the openclaw.json update. We log + continue with a
// warning rather than aborting the whole refresh.
func (p *AWSProvider) RefreshAgent(ctx context.Context, agentName string) error {
	agent, err := discovery.ResolveAgent(ctx, p.clients.SSM, agentName)
	if err != nil {
		return err
	}
	if agent.Paused {
		return fmt.Errorf("agent %s is paused. Use `conga admin unpause %s` first", agentName, agentName)
	}

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	// Step 0: refresh-time egress pre-flight. Provision-time has the
	// same check; without doing it here too, operators editing
	// agent.yaml (e.g. swapping a subagent base_url) and running
	// refresh would silently land in 403-at-runtime territory.
	//
	// Errors loading the overlay or policy are surfaced as warnings
	// (not fatal — the agent itself can still refresh) but they must
	// NOT be silently swallowed: a YAML typo in conga-policy.yaml that
	// prevents the pre-flight from running is the same silent-failure
	// pattern loadRefreshPolicy itself was extracted to prevent. The
	// missing-behavior-dir case stays a hard error because regenerate-
	// AgentConfigOnInstance (step 1) already refuses on that condition.
	if behaviorDir := resolveAWSBehaviorDir(); behaviorDir != "" {
		overlay, overlayErr := common.LoadAgentOverlay(behaviorDir, *agent)
		if overlayErr != nil {
			common.Warn(ctx, "failed to load agent overlay for refresh-time egress check: %v", overlayErr)
		} else {
			policyPath := filepath.Join(provider.DefaultDataDir(), "conga-policy.yaml")
			pf, _, policyErr := loadRefreshPolicy(ctx, policyPath)
			if policyErr != nil {
				common.Warn(ctx, "skipping refresh-time egress pre-flight: %v", policyErr)
			} else if pf != nil {
				merged := pf.MergeForAgent(agentName)
				common.WarnOverlayEgressGaps(ctx, overlay, policy.EffectiveAllowedDomains(merged.Egress), agentName)
				common.WarnCustomConfigEgressGaps(ctx, common.ResolveCustomConfigSources(behaviorDir, *agent), policy.EffectiveAllowedDomains(merged.Egress), agentName)
			}
		}
	}

	// Step 1: regenerate openclaw.json + .env via Go (uploaded to instance).
	if err := p.regenerateAgentConfigOnInstance(ctx, instanceID, *agent); err != nil {
		return fmt.Errorf("failed to regenerate config for %s: %w", agentName, err)
	}

	// Step 2: build the agent's docker-run command + systemd unit in Go and apply
	// them via the managed-host engine (shared container builder + systemd
	// supervisor). This replaced refresh-user.sh's bash unit generation: the unit
	// can no longer drift from the run command or the egress hooks, there's no
	// in-place `sed` on the unit (audit #8), and the static-IP egress iptables are
	// deterministic (no 10-retry discovery loop, audit #7). Secrets reach the
	// container via `docker run --env-file` against the .env Step 1 just wrote
	// from Secrets Manager, so a refresh still picks up fresh secrets.
	spin := ui.NewSpinner(fmt.Sprintf("Applying unit + restarting %s...", agentName))
	err = p.defineAndStartAgentService(ctx, instanceID, *agent)
	spin.Stop()
	if err != nil {
		return err
	}

	// Step 3: reconcile routing.json from current SSM agent state, then
	// restart the router so the change takes effect. Non-fatal: warn on
	// failure and continue — the agent itself is still healthy. Warn
	// routes through the context's WarningSink so MCP callers see it
	// in the tool result (stderr is invisible under MCP).
	if err := p.regenerateRoutingOnInstance(ctx, instanceID); err != nil {
		common.Warn(ctx, "routing.json regeneration failed for %s: %v", agentName, err)
	} else if err := p.restartRouterOnInstance(ctx, instanceID); err != nil {
		common.Warn(ctx, "router restart failed after %s refresh: %v", agentName, err)
	}

	// Step 4: redeploy egress proxy with current policy. Non-fatal: warn
	// + continue. The bootstrap-time Envoy config is empty (deny-all)
	// until this step succeeds at least once, so it's important — but
	// we don't want a transient policy-deploy failure to fail an
	// otherwise-successful refresh.
	if err := p.redeployEgressDuringRefresh(ctx, agentName); err != nil {
		common.Warn(ctx, "egress redeploy failed for %s: %v", agentName, err)
	}

	return nil
}

// redeployEgressDuringRefresh loads the current policy, generates the
// Envoy config + manifest, and pushes them to the instance via the
// existing DeployEgress code path. Used by RefreshAgent to keep the
// egress proxy in sync with policy changes after the operator runs
// `conga policy set-egress` and then `conga refresh` (without this,
// only `conga policy deploy` would push the new config and operators
// would have to remember the two-step).
func (p *AWSProvider) redeployEgressDuringRefresh(ctx context.Context, agentName string) error {
	policyPath := filepath.Join(provider.DefaultDataDir(), "conga-policy.yaml")
	pf, policyContent, err := loadRefreshPolicy(ctx, policyPath)
	if err != nil {
		return err
	}

	var egressPolicy *policy.EgressPolicy
	mode := policy.EgressModeEnforce
	if pf != nil {
		merged := pf.MergeForAgent(agentName)
		egressPolicy = merged.Egress
		if egressPolicy != nil {
			mode = egressPolicy.Mode
		}
	}

	envoyConfig, err := policy.GenerateProxyConf(egressPolicy)
	if err != nil {
		return fmt.Errorf("generate proxy conf: %w", err)
	}
	manifest := policy.BuildManifest(egressPolicy)
	manifestBytes, err := manifest.MarshalForDeploy()
	if err != nil {
		return fmt.Errorf("marshal manifest for %s: %w", agentName, err)
	}

	return p.DeployEgress(ctx, agentName, policyContent, envoyConfig, string(manifestBytes), mode)
}

// loadRefreshPolicy loads conga-policy.yaml for the refresh egress-redeploy
// step. Splits "file missing" (acceptable: pre-first-deploy state, fall
// back to nil policy + deny-all warning) from "file broken / unreadable"
// (YAML typo, empty file, permission denied, I/O error) — the latter
// must surface as an error so a typo doesn't silently regress every
// refreshed agent to a deny-all proxy config.
//
// Reads the file once and parses from the resulting bytes so the parsed
// PolicyFile and the raw content are guaranteed to agree (closes a TOCTOU
// window where a concurrent `conga policy set-egress` could land between
// a parse-read and a content-read, producing mismatched Envoy config vs
// uploaded policy YAML). Returns:
//   - (pf, content, nil) on successful load
//   - (nil, "", nil) when the file genuinely does not exist; warning emitted
//   - (nil, "", err) on any other failure (empty, YAML, permission, I/O)
func loadRefreshPolicy(ctx context.Context, policyPath string) (*policy.PolicyFile, string, error) {
	data, err := os.ReadFile(policyPath)
	if err != nil {
		if os.IsNotExist(err) {
			common.Warn(ctx, "no conga-policy.yaml found at %s; egress proxy will be deployed with deny-all config", policyPath)
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("read policy %s: %w", policyPath, err)
	}
	pf, err := policy.LoadFromBytes(data)
	if err != nil {
		return nil, "", fmt.Errorf("parse policy %s: %w", policyPath, err)
	}
	return pf, string(data), nil
}

// perAgentRefreshTimeout bounds a single agent's refresh during `refresh-all`. A
// full refresh (config-gen + network reconcile + unit + restart-with-plugin-install
// + routing + egress) runs ~3-4m; 6m is headroom. It is applied PER AGENT, not once
// across the fleet (R3).
const perAgentRefreshTimeout = 6 * time.Minute

// perAgentRefreshCtx derives a per-agent refresh context with a fresh deadline that
// is independent of the parent's global --timeout. context.WithoutCancel strips the
// parent's deadline (and cancellation — the RefreshAll loop guards operator cancel
// separately via ctx.Err()), so one slow agent can't starve the rest and the whole
// fleet isn't capped by a single --timeout window.
func perAgentRefreshCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), perAgentRefreshTimeout)
}

func (p *AWSProvider) RefreshAll(ctx context.Context) error {
	agents, err := discovery.ListAgents(ctx, p.clients.SSM)
	if err != nil {
		return err
	}

	var activeAgents []provider.AgentConfig
	for _, a := range agents {
		if a.Paused {
			fmt.Printf("Skipping paused agent: %s\n", a.Name)
			continue
		}
		activeAgents = append(activeAgents, a)
	}

	if len(activeAgents) == 0 {
		fmt.Println("No active agents to refresh.")
		return nil
	}

	// Refresh each agent individually so env files are regenerated from
	// Secrets Manager. The old approach used a bulk shell script that only
	// restarted systemd units without regenerating env files, causing stale
	// secrets after secret changes.
	//
	// Each agent gets its OWN timeout (perAgentRefreshCtx), not the single global
	// --timeout shared across the whole fleet — otherwise a fleet larger than one
	// --timeout window dies partway through with `context deadline exceeded` on the
	// unprocessed agents (managed-host migration hardening, R3). An explicit operator
	// cancel (Ctrl-C → context.Canceled) still stops the loop; the global deadline
	// (DeadlineExceeded) deliberately does NOT bound the fleet.
	var failed []string
	spin := ui.NewSpinner("Refreshing all agents...")
	for _, a := range activeAgents {
		spin.Stop()
		if errors.Is(ctx.Err(), context.Canceled) {
			fmt.Fprintln(os.Stderr, "refresh-all cancelled; remaining agents not refreshed")
			failed = append(failed, a.Name)
			continue
		}
		aCtx, cancel := perAgentRefreshCtx(ctx)
		refreshErr := p.RefreshAgent(aCtx, a.Name)
		cancel()
		if refreshErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to refresh %s: %v\n", a.Name, refreshErr)
			failed = append(failed, a.Name)
		}
		spin = ui.NewSpinner("Refreshing all agents...")
	}
	spin.Stop()

	if len(failed) > 0 {
		return fmt.Errorf("failed to refresh %d agent(s): %s",
			len(failed), strings.Join(failed, ", "))
	}
	return nil
}

// --- Egress Policy Deployment ---

// DeployEgress deploys the egress proxy for a single agent without requiring a host cycle.
// It uploads the policy file and pre-generated Envoy config, starts the proxy container,
// restarts the agent container with HTTPS_PROXY, and applies iptables rules (all modes).
//
// manifestJSON is an optional pre-rendered deployment manifest (see
// policy.BuildManifest → MarshalForDeploy) written alongside the Envoy
// config so drift detection can inspect what was deployed without parsing
// the Lua filter. When empty, an empty manifest file is written; callers
// that want drift detection should always supply a manifest.
func (p *AWSProvider) DeployEgress(ctx context.Context, agentName, policyContent, envoyConfig, manifestJSON string, mode policy.EgressMode) error {
	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	// Resolve the agent's deterministic network plan so the proxy can be pinned to
	// its reserved static IP (.3). Pinning prevents the --restart-always proxy from
	// grabbing the agent's .2 on a simultaneous restart (reboot / docker daemon
	// restart) → exit-125 crash-loop (managed-host migration hardening, R1).
	agent, err := discovery.ResolveAgent(ctx, p.clients.SSM, agentName)
	if err != nil {
		return fmt.Errorf("failed to resolve agent %s for egress deploy: %w", agentName, err)
	}
	agentNet, err := managedhost.PlanAgentNetwork(agent.GatewayPort, common.BaseGatewayPort)
	if err != nil {
		return fmt.Errorf("failed to plan network for %s: %w", agentName, err)
	}

	tmpl, err := template.New("deploy-egress").Parse(scripts.DeployEgressScript)
	if err != nil {
		return fmt.Errorf("failed to parse deploy-egress template: %w", err)
	}

	if err := validateHeredocSafety(map[string]string{
		"AgentName":        agentName,
		"Mode":             string(mode),
		"PolicyContent":    policyContent,
		"EnvoyConfig":      envoyConfig,
		"ProxyBootstrapJS": policy.ProxyBootstrapJS(),
		"ManifestJSON":     manifestJSON,
	}); err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		AgentName        string
		Mode             string
		PolicyContent    string
		EnvoyConfig      string
		ProxyBootstrapJS string
		ManifestJSON     string
		ProxyIP          string
	}{agentName, string(mode), policyContent, envoyConfig, policy.ProxyBootstrapJS(), manifestJSON, agentNet.ProxyIP}); err != nil {
		return fmt.Errorf("failed to render deploy-egress script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Deploying egress proxy for %s (mode=%s)...", agentName, mode))
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, buf.String(), 180*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		return fmt.Errorf("deploy-egress failed:\n%s\n%s", result.Stdout, result.Stderr)
	}
	fmt.Fprintln(os.Stderr, result.Stdout)
	return nil
}

// validateHeredocSafety checks that template values don't contain heredoc delimiters.
// A line containing only the delimiter would terminate the heredoc early and allow
// arbitrary shell execution. This check conservatively rejects any value containing
// the delimiter string, even as a substring.
func validateHeredocSafety(values map[string]string) error {
	heredocDelimiters := []string{"POLICYEOF", "ENVOYEOF", "BOOTSTRAPEOF", "PROXYDF", "MANIFESTEOF"}
	for _, delim := range heredocDelimiters {
		for name, val := range values {
			if strings.Contains(val, delim) {
				return fmt.Errorf("%s contains heredoc delimiter %q — refusing to render (possible injection)", name, delim)
			}
		}
	}
	return nil
}

// --- Secrets ---

func (p *AWSProvider) SetSecret(ctx context.Context, agentName, secretName, value string) error {
	secretPath := fmt.Sprintf("conga/agents/%s/%s", agentName, secretName)
	return awsutil.SetSecret(ctx, p.clients.SecretsManager, secretPath, value)
}

func (p *AWSProvider) ListSecrets(ctx context.Context, agentName string) ([]provider.SecretEntry, error) {
	prefix := fmt.Sprintf("conga/agents/%s/", agentName)
	entries, err := awsutil.ListSecrets(ctx, p.clients.SecretsManager, prefix)
	if err != nil {
		return nil, err
	}

	result := make([]provider.SecretEntry, len(entries))
	for i, e := range entries {
		result[i] = provider.SecretEntry{
			Name:   e.Name,
			EnvVar: common.SecretNameToEnvVar(e.Name),
			Path:   fmt.Sprintf("conga/agents/%s/%s", agentName, e.Name),
		}
		if e.LastChanged != "" {
			result[i].LastChanged, _ = time.Parse(time.RFC3339, e.LastChanged)
		}
	}
	return result, nil
}

func (p *AWSProvider) DeleteSecret(ctx context.Context, agentName, secretName string) error {
	secretPath := fmt.Sprintf("conga/agents/%s/%s", agentName, secretName)
	return awsutil.DeleteSecret(ctx, p.clients.SecretsManager, secretPath)
}

// --- Connectivity ---

func (p *AWSProvider) Connect(ctx context.Context, agentName string, localPort int) (*provider.ConnectInfo, error) {
	if err := tunnel.CheckPlugin(); err != nil {
		return nil, err
	}

	agentCfg, err := discovery.ResolveAgent(ctx, p.clients.SSM, agentName)
	if err != nil {
		return nil, err
	}

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return nil, err
	}

	// Fetch gateway token
	tokenScript := fmt.Sprintf(`python3 -c "import json; c=json.load(open('/opt/conga/data/%s/openclaw.json')); print(c.get('gateway',{}).get('auth',{}).get('token','NOT_FOUND'))"`, agentName)
	spin := ui.NewSpinner("Fetching gateway token...")
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, tokenScript, 30*time.Second)
	spin.Stop()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch gateway token: %w", err)
	}

	token := strings.TrimSpace(result.Stdout)
	if token == "" || token == "NOT_FOUND" {
		return nil, fmt.Errorf("gateway token not found. Container may not have started yet.\nTry: conga status")
	}

	if localPort == 0 {
		localPort = agentCfg.GatewayPort
	}

	fmt.Printf("Starting tunnel: localhost:%d → instance:%d\n", localPort, agentCfg.GatewayPort)
	tun, err := tunnel.StartTunnel(ctx, p.clients.SSM, instanceID, agentCfg.GatewayPort, localPort, p.region, p.profile)
	if err != nil {
		return nil, err
	}

	waiter := make(chan error, 1)
	go func() { waiter <- tun.Wait() }()

	return &provider.ConnectInfo{
		URL:       fmt.Sprintf("http://localhost:%d#token=%s", localPort, token),
		LocalPort: localPort,
		Token:     token,
		Waiter:    waiter,
	}, nil
}

// --- Environment Management ---

func (p *AWSProvider) Setup(ctx context.Context, cfg *provider.SetupConfig) error {
	manifestJSON, err := awsutil.GetParameter(ctx, p.clients.SSM, "/conga/config/setup-manifest")
	if err != nil {
		var notFound *ssmtypes.ParameterNotFound
		if errors.As(err, &notFound) {
			return fmt.Errorf("setup manifest not found in SSM. Run `terraform apply` first to create infrastructure")
		}
		return fmt.Errorf("failed to read setup manifest from SSM: %w", err)
	}

	var manifest struct {
		Config   map[string]string `json:"config"`
		Defaults map[string]string `json:"defaults"`
		Secrets  map[string]string `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return fmt.Errorf("failed to parse setup manifest: %w", err)
	}

	// Non-interactive mode: when SetupConfig is provided, use its values for
	// unset config and skip prompts entirely. This enables Terraform and
	// programmatic callers to run Setup without a terminal.
	nonInteractive := cfg != nil

	fmt.Println("Reading setup manifest...")
	changed := 0

	// Process config values — sorted for deterministic order
	configKeys := sortedKeys(manifest.Config)
	for _, key := range configKeys {
		description := manifest.Config[key]
		paramName := fmt.Sprintf("/conga/config/%s", key)
		current, err := awsutil.GetParameter(ctx, p.clients.SSM, paramName)
		if err != nil {
			// Only "the parameter doesn't exist yet" is a recoverable
			// state for the loop (it means "ask the user / use the
			// default"). Auth/network failures must abort: silently
			// treating them as "not set" would overwrite already-set
			// values in SSM with whatever cfg.Secrets or the prompt
			// supplies — silent data loss on transient SSM blips.
			var notFound *ssmtypes.ParameterNotFound
			if !errors.As(err, &notFound) {
				return fmt.Errorf("failed to read config %s: %w", paramName, err)
			}
			current = ""
		}

		status := "set"
		if current == "" {
			status = "not set"
		}
		fmt.Printf("\n[config] %s — %s (%s)\n", key, description, status)

		if nonInteractive {
			// In non-interactive mode: skip if already set, otherwise use SetupConfig or default
			if current != "" {
				continue
			}
			value := ""
			if cfg.Secrets != nil {
				value = cfg.Secrets[key]
			}
			if value == "" {
				value = manifest.Defaults[key]
			}
			if value == "" {
				fmt.Printf("  Skipped (no value provided)\n")
				continue
			}
			if err := awsutil.PutParameter(ctx, p.clients.SSM, paramName, value); err != nil {
				return fmt.Errorf("failed to set config %s: %w", key, err)
			}
			fmt.Printf("  Saved to SSM: %s\n", paramName)
			changed++
			continue
		}

		if current != "" {
			if !ui.Confirm("  Update this value?") {
				continue
			}
		}

		defaultVal := manifest.Defaults[key]
		value, err := ui.TextPromptWithDefault(fmt.Sprintf("  Enter value for %s", key), defaultVal)
		if err != nil {
			return err
		}
		if value == "" {
			fmt.Println("  Skipped (empty value)")
			continue
		}

		if err := awsutil.PutParameter(ctx, p.clients.SSM, paramName, value); err != nil {
			return fmt.Errorf("failed to set config %s: %w", key, err)
		}
		fmt.Printf("  Saved to SSM: %s\n", paramName)
		changed++
	}

	// Process secrets — sorted for deterministic order
	secretPaths := sortedKeys(manifest.Secrets)
	for _, path := range secretPaths {
		description := manifest.Secrets[path]
		// awsutil.GetSecretValue returns ("", nil) for a missing secret,
		// so any non-nil error here is a real failure (auth, network,
		// throttling). Aborting prevents the same silent-overwrite
		// scenario as the config loop above.
		current, err := awsutil.GetSecretValue(ctx, p.clients.SecretsManager, path)
		if err != nil {
			return fmt.Errorf("failed to read secret %s: %w", path, err)
		}

		status := "set"
		if current == "" || current == "REPLACE_ME" {
			status = "not set"
		}
		fmt.Printf("\n[secret] %s — %s (%s)\n", path, description, status)

		if nonInteractive {
			// In non-interactive mode: skip if already set
			if current != "" && current != "REPLACE_ME" {
				continue
			}
			value := ""
			if cfg.Secrets != nil {
				value = cfg.Secrets[path]
			}
			if value == "" {
				fmt.Printf("  Skipped (no value provided)\n")
				continue
			}
			if err := awsutil.SetSecret(ctx, p.clients.SecretsManager, path, value); err != nil {
				return fmt.Errorf("failed to set secret %s: %w", path, err)
			}
			fmt.Printf("  Saved to Secrets Manager\n")
			changed++
			continue
		}

		if current != "" && current != "REPLACE_ME" {
			if !ui.Confirm("  Update this value?") {
				continue
			}
		}

		value, err := ui.SecretPrompt(fmt.Sprintf("  Enter value for %s", path))
		if err != nil {
			return err
		}
		if value == "" {
			fmt.Println("  Skipped (empty value)")
			continue
		}

		if err := awsutil.SetSecret(ctx, p.clients.SecretsManager, path, value); err != nil {
			return fmt.Errorf("failed to set secret %s: %w", path, err)
		}
		fmt.Printf("  Saved to Secrets Manager\n")
		changed++
	}

	if changed > 0 {
		fmt.Printf("\n%d value(s) updated. Run `conga admin cycle-host` to apply.\n", changed)
	} else {
		fmt.Println("\nAll values already configured. No changes needed.")
	}
	return nil
}

func (p *AWSProvider) CycleHost(ctx context.Context) error {
	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Stopping instance %s...\n", instanceID)
	if err := awsutil.StopInstance(ctx, p.clients.EC2, instanceID); err != nil {
		return fmt.Errorf("failed to stop instance: %w", err)
	}

	spin := ui.NewSpinner("Waiting for instance to stop...")
	err = awsutil.WaitForState(ctx, p.clients.EC2, instanceID, "stopped")
	spin.Stop()
	if err != nil {
		return fmt.Errorf("instance failed to stop: %w", err)
	}
	fmt.Println("Instance stopped.")

	fmt.Printf("Starting instance %s...\n", instanceID)
	if err := awsutil.StartInstance(ctx, p.clients.EC2, instanceID); err != nil {
		return fmt.Errorf("failed to start instance: %w", err)
	}

	spin = ui.NewSpinner("Waiting for instance to start...")
	err = awsutil.WaitForState(ctx, p.clients.EC2, instanceID, "running")
	spin.Stop()
	if err != nil {
		return fmt.Errorf("instance failed to start: %w", err)
	}

	fmt.Println("Instance running. SSM agent may take 1-2 minutes to reconnect.")
	fmt.Println("Use `conga status` to verify your container is healthy.")
	return nil
}

func (p *AWSProvider) Teardown(ctx context.Context) error {
	return fmt.Errorf("teardown for AWS is managed by Terraform.\nRun: cd terraform && terraform destroy")
}

// --- helpers ---

func (p *AWSProvider) findInstance(ctx context.Context) (string, error) {
	return discovery.FindInstance(ctx, p.clients.EC2, defaultInstanceTag)
}

// renderAgentScript parses a Go template script and renders it with the agent's
// name, type, and Slack identifier. Gateway-only agents (no Slack) render with
// an empty SlackID; the scripts guard routing updates with [ -n "$SLACK_ID" ].
func (p *AWSProvider) renderAgentScript(tmplStr, tmplName, agentName string, agent *provider.AgentConfig) (string, error) {
	tmpl, err := template.New(tmplName).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse %s template: %w", tmplName, err)
	}

	slackID := ""
	for _, ch := range agent.Channels {
		if ch.Platform == "slack" {
			slackID = ch.ID
			break
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		AgentName string
		AgentType string
		SlackID   string
	}{agentName, string(agent.Type), slackID}); err != nil {
		return "", fmt.Errorf("failed to render %s script: %w", tmplName, err)
	}
	return buf.String(), nil
}

func (p *AWSProvider) setAgentPaused(ctx context.Context, name string, agent *provider.AgentConfig, paused bool) error {
	// Read-modify-write: read current JSON, toggle "paused", write back.
	// This preserves any fields that exist in SSM but aren't in the AgentConfig struct.
	paramName := fmt.Sprintf("/conga/agents/%s", name)
	raw, err := awsutil.GetParameter(ctx, p.clients.SSM, paramName)
	if err != nil {
		return fmt.Errorf("failed to read agent parameter: %w", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return fmt.Errorf("failed to parse agent parameter JSON: %w", err)
	}

	if paused {
		data["paused"] = true
	} else {
		delete(data, "paused")
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return awsutil.PutParameter(ctx, p.clients.SSM, paramName, string(jsonBytes))
}

func parseKeyValues(output string) map[string]string {
	kv := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if i := strings.IndexByte(line, '='); i > 0 {
			kv[line[:i]] = strings.TrimSpace(line[i+1:])
		}
	}
	return kv
}

func buildAgentStatus(agentName string, kv map[string]string) *provider.AgentStatus {
	status := &provider.AgentStatus{
		AgentName:    agentName,
		ServiceState: kv["SERVICE_STATE"],
		Container: provider.ContainerStatus{
			State: kv["CONTAINER_STATUS"],
		},
	}

	if kv["CONTAINER_STATUS"] == "not found" || kv["CONTAINER_STATUS"] == "" {
		status.Container.State = "not found"
		return status
	}

	status.Container.StartedAt = kv["CONTAINER_STARTED"]

	phase := "starting"
	if kv["BOOT_GATEWAY"] != "0" {
		phase = "gateway up, waiting for plugins"
	}
	if kv["BOOT_SLACK_START"] != "0" {
		phase = "slack plugin loading"
	}
	if kv["BOOT_SLACK_HTTP"] != "0" {
		phase = "slack endpoint ready, resolving channels"
	}
	if kv["BOOT_SLACK_CHANNELS"] != "0" {
		phase = "ready"
	}
	if kv["BOOT_ERROR"] != "0" && kv["BOOT_ERROR"] != "" {
		phase += " (errors in logs — check `conga logs`)"
	}
	status.ReadyPhase = phase

	if stats := kv["CONTAINER_STATS"]; stats != "" {
		parts := strings.SplitN(stats, "|", 3)
		if len(parts) == 3 {
			status.Container.CPUPercent = strings.TrimSpace(parts[0])
			status.Container.MemoryUsage = strings.TrimSpace(parts[1])
		}
	}

	return status
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- Channel Management (stubs — not yet implemented for AWS) ---

// Channel methods are implemented in channels.go.
