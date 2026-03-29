// Package awsprovider implements the Provider interface using AWS services.
package awsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	awsutil "github.com/cruxdigital-llc/conga-line/cli/internal/aws"
	"github.com/cruxdigital-llc/conga-line/cli/internal/channels"
	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/cruxdigital-llc/conga-line/cli/internal/discovery"
	"github.com/cruxdigital-llc/conga-line/cli/internal/policy"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/cruxdigital-llc/conga-line/cli/internal/tunnel"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/cruxdigital-llc/conga-line/cli/scripts"
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
	provider.Register("aws", NewAWSProvider)
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
	agents, err := discovery.ListAgents(ctx, p.clients.SSM)
	if err != nil {
		return nil, err
	}
	return convertAgentList(agents), nil
}

func (p *AWSProvider) GetAgent(ctx context.Context, name string) (*provider.AgentConfig, error) {
	a, err := discovery.ResolveAgent(ctx, p.clients.SSM, name)
	if err != nil {
		return nil, err
	}
	result := convertAgent(*a)
	return &result, nil
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
		fmt.Fprintf(os.Stderr, "Warning: could not resolve state bucket — behavior files will not sync. Run 'terraform apply' first.\n")
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
	egressPolicy, _ := policy.LoadEgressPolicy(provider.DefaultDataDir(), cfg.Name)
	egressMode := policy.EgressModeEnforce
	if egressPolicy != nil {
		egressMode = egressPolicy.Mode
	}
	envoyConfig, err := policy.GenerateProxyConf(egressPolicy)
	if err != nil {
		return fmt.Errorf("failed to generate egress config: %w", err)
	}
	proxyBootstrapJS := policy.ProxyBootstrapJS()

	var scriptTemplate string
	var templateData interface{}
	type provisionData struct {
		AgentName, SlackMemberID, SlackChannel, AWSRegion, StateBucket string
		GatewayPort                                                    int
		EnvoyConfig, EgressMode, ProxyBootstrapJS                      string
	}
	switch cfg.Type {
	case provider.AgentTypeUser:
		scriptTemplate = scripts.AddUserScript
		templateData = provisionData{
			AgentName: cfg.Name, SlackMemberID: slackID, AWSRegion: p.region,
			StateBucket: stateBucket, GatewayPort: cfg.GatewayPort,
			EnvoyConfig: envoyConfig, EgressMode: string(egressMode), ProxyBootstrapJS: proxyBootstrapJS,
		}
	case provider.AgentTypeTeam:
		scriptTemplate = scripts.AddTeamScript
		templateData = provisionData{
			AgentName: cfg.Name, SlackChannel: slackID, AWSRegion: p.region,
			StateBucket: stateBucket, GatewayPort: cfg.GatewayPort,
			EnvoyConfig: envoyConfig, EgressMode: string(egressMode), ProxyBootstrapJS: proxyBootstrapJS,
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
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, buf.String(), 180*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		fmt.Fprintf(os.Stderr, "Setup output:\n%s\n%s\n", result.Stdout, result.Stderr)
		return fmt.Errorf("provisioning agent %s failed on instance", cfg.Name)
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

func (p *AWSProvider) UnpauseAgent(ctx context.Context, name string) error {
	agent, err := discovery.ResolveAgent(ctx, p.clients.SSM, name)
	if err != nil {
		return err
	}
	if !agent.Paused {
		fmt.Printf("Agent %s is not paused.\n", name)
		return nil
	}

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	script, err := p.renderAgentScript(scripts.UnpauseAgentScript, "unpause", name, agent)
	if err != nil {
		return err
	}

	spin := ui.NewSpinner(fmt.Sprintf("Unpausing agent %s...", name))
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, script, 60*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}
	if result.Status != "Success" {
		fmt.Fprintf(os.Stderr, "Output:\n%s\n%s\n", result.Stdout, result.Stderr)
		return fmt.Errorf("unpause failed on instance")
	}

	if err := p.setAgentPaused(ctx, name, agent, false); err != nil {
		return fmt.Errorf("agent started but failed to update SSM: %w", err)
	}

	return nil
}

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

	tmpl, err := template.New("refresh").Parse(scripts.RefreshUserScript)
	if err != nil {
		return fmt.Errorf("failed to parse refresh template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ AgentName, AWSRegion string }{agentName, p.region}); err != nil {
		return fmt.Errorf("failed to render refresh script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Refreshing secrets for %s...", agentName))
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, buf.String(), 120*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		return fmt.Errorf("refresh failed:\n%s\n%s", result.Stdout, result.Stderr)
	}
	return nil
}

func (p *AWSProvider) RefreshAll(ctx context.Context) error {
	agents, err := discovery.ListAgents(ctx, p.clients.SSM)
	if err != nil {
		return err
	}

	var activeAgents []discovery.AgentConfig
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

	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
	}

	tmpl, err := template.New("refresh-all").Parse(scripts.RefreshAllScript)
	if err != nil {
		return fmt.Errorf("failed to parse refresh-all template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		Agents    []discovery.AgentConfig
		AWSRegion string
	}{activeAgents, p.region}); err != nil {
		return fmt.Errorf("failed to render refresh-all script: %w", err)
	}

	spin := ui.NewSpinner("Refreshing all agents...")
	result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, buf.String(), 300*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		fmt.Fprintf(os.Stderr, "Output:\n%s\n%s\n", result.Stdout, result.Stderr)
		return fmt.Errorf("refresh-all failed on instance")
	}
	return nil
}

// --- Egress Policy Deployment ---

// DeployEgress deploys the egress proxy for a single agent without requiring a host cycle.
// It uploads the policy file and pre-generated Envoy config, starts the proxy container,
// restarts the agent container with HTTPS_PROXY, and applies iptables rules (enforce mode only).
func (p *AWSProvider) DeployEgress(ctx context.Context, agentName, policyContent, envoyConfig string, mode policy.EgressMode) error {
	instanceID, err := p.findInstance(ctx)
	if err != nil {
		return err
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
	}{agentName, string(mode), policyContent, envoyConfig, policy.ProxyBootstrapJS()}); err != nil {
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
	heredocDelimiters := []string{"POLICYEOF", "ENVOYEOF", "BOOTSTRAPEOF", "PROXYDF"}
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
		return fmt.Errorf("setup manifest not found in SSM. Run `terraform apply` first to create infrastructure")
	}

	var manifest struct {
		Config   map[string]string `json:"config"`
		Defaults map[string]string `json:"defaults"`
		Secrets  map[string]string `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return fmt.Errorf("failed to parse setup manifest: %w", err)
	}

	fmt.Println("Reading setup manifest...")
	changed := 0

	// Process config values — sorted for deterministic order
	configKeys := sortedKeys(manifest.Config)
	for _, key := range configKeys {
		description := manifest.Config[key]
		paramName := fmt.Sprintf("/conga/config/%s", key)
		current, err := awsutil.GetParameter(ctx, p.clients.SSM, paramName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not read %s: %v\n", paramName, err)
			current = ""
		}

		status := "set"
		if current == "" {
			status = "not set"
		}
		fmt.Printf("\n[config] %s — %s (%s)\n", key, description, status)

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
		current, err := awsutil.GetSecretValue(ctx, p.clients.SecretsManager, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not read secret %s: %v\n", path, err)
			current = ""
		}

		status := "set"
		if current == "" || current == "REPLACE_ME" {
			status = "not set"
		}
		fmt.Printf("\n[secret] %s — %s (%s)\n", path, description, status)

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
func (p *AWSProvider) renderAgentScript(tmplStr, tmplName, agentName string, agent *discovery.AgentConfig) (string, error) {
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
	}{agentName, agent.Type, slackID}); err != nil {
		return "", fmt.Errorf("failed to render %s script: %w", tmplName, err)
	}
	return buf.String(), nil
}

func (p *AWSProvider) setAgentPaused(ctx context.Context, name string, agent *discovery.AgentConfig, paused bool) error {
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

func convertAgent(a discovery.AgentConfig) provider.AgentConfig {
	return provider.AgentConfig{
		Name:        a.Name,
		Type:        provider.AgentType(a.Type),
		Channels:    a.Channels,
		GatewayPort: a.GatewayPort,
		IAMIdentity: a.IAMIdentity,
		Paused:      a.Paused,
	}
}

func convertAgentList(agents []discovery.AgentConfig) []provider.AgentConfig {
	result := make([]provider.AgentConfig, len(agents))
	for i, a := range agents {
		result[i] = convertAgent(a)
	}
	return result
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

func (p *AWSProvider) AddChannel(_ context.Context, _ string, _ map[string]string) error {
	return fmt.Errorf("channel management not yet implemented for AWS provider; use --provider local or --provider remote instead")
}

func (p *AWSProvider) RemoveChannel(_ context.Context, _ string) error {
	return fmt.Errorf("channel management not yet implemented for AWS provider; use --provider local or --provider remote instead")
}

func (p *AWSProvider) ListChannels(_ context.Context) ([]provider.ChannelStatus, error) {
	return nil, fmt.Errorf("channel management not yet implemented for AWS provider; use --provider local or --provider remote instead")
}

func (p *AWSProvider) BindChannel(_ context.Context, _ string, _ channels.ChannelBinding) error {
	return fmt.Errorf("channel management not yet implemented for AWS provider; use --provider local or --provider remote instead")
}

func (p *AWSProvider) UnbindChannel(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("channel management not yet implemented for AWS provider; use --provider local or --provider remote instead")
}
