package localprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cruxdigital-llc/conga-line/cli/internal/channels"
	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/cruxdigital-llc/conga-line/cli/internal/policy"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
)

const (
	egressProxyImage = "conga-egress-proxy"
	routerContainer  = "conga-router"
)

// LocalProvider implements provider.Provider using local Docker.
type LocalProvider struct {
	dataDir string
}

// NewLocalProvider creates a local provider.
func NewLocalProvider(cfg *provider.Config) (provider.Provider, error) {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = provider.DefaultDataDir()
	}
	return &LocalProvider{dataDir: dataDir}, nil
}

func init() {
	provider.Register("local", NewLocalProvider)
}

func (p *LocalProvider) Name() string { return "local" }

// --- paths ---

func (p *LocalProvider) agentsDir() string             { return filepath.Join(p.dataDir, "agents") }
func (p *LocalProvider) configDir() string             { return filepath.Join(p.dataDir, "config") }
func (p *LocalProvider) dataSubDir(name string) string { return filepath.Join(p.dataDir, "data", name) }
func (p *LocalProvider) routerDir() string             { return filepath.Join(p.dataDir, "router") }
func (p *LocalProvider) behaviorDir() string           { return filepath.Join(p.dataDir, "behavior") }
func (p *LocalProvider) logsDir() string               { return filepath.Join(p.dataDir, "logs") }
func (p *LocalProvider) egressProxyDir() string        { return filepath.Join(p.dataDir, "egress-proxy") }

// --- Identity & Discovery ---

func (p *LocalProvider) WhoAmI(ctx context.Context) (*provider.Identity, error) {
	u, err := user.Current()
	if err != nil {
		return &provider.Identity{Name: "local-user"}, nil
	}
	return &provider.Identity{Name: u.Username}, nil
}

func (p *LocalProvider) ListAgents(ctx context.Context) ([]provider.AgentConfig, error) {
	dir := p.agentsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var agents []provider.AgentConfig
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var cfg provider.AgentConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		cfg.Name = strings.TrimSuffix(e.Name(), ".json")
		agents = append(agents, cfg)
	}

	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}

func (p *LocalProvider) GetAgent(ctx context.Context, name string) (*provider.AgentConfig, error) {
	path := filepath.Join(p.agentsDir(), name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("agent %q not found. Use `conga admin add-user` or `add-team` to provision", name)
		}
		return nil, err
	}
	var cfg provider.AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.Name = name
	return &cfg, nil
}

func (p *LocalProvider) ResolveAgentByIdentity(ctx context.Context) (*provider.AgentConfig, error) {
	// Local provider has no IAM-style identity mapping.
	// If there's exactly one agent, auto-resolve to it for convenience.
	agents, err := p.ListAgents(ctx)
	if err != nil || len(agents) != 1 {
		return nil, nil
	}
	return &agents[0], nil
}

// --- Agent Lifecycle ---

func (p *LocalProvider) ProvisionAgent(ctx context.Context, cfg provider.AgentConfig) error {
	// 1. Save agent config
	if err := p.saveAgentConfig(&cfg); err != nil {
		return err
	}

	// 2. Read secrets and generate config files
	shared, err := p.readSharedSecrets()
	if err != nil {
		return fmt.Errorf("failed to read shared secrets: %w", err)
	}
	perAgent, err := p.readAgentSecrets(cfg.Name)
	if err != nil {
		return fmt.Errorf("failed to read agent secrets: %w", err)
	}

	openClawJSON, err := common.GenerateOpenClawConfig(cfg, shared, "")
	if err != nil {
		return fmt.Errorf("failed to generate config: %w", err)
	}

	dataDir := p.dataSubDir(cfg.Name)
	// Pre-create the full directory structure that OpenClaw expects
	// (matches AWS bootstrap: mkdir -p {workspace,memory,logs,agents,canvas,cron,devices,identity,media})
	for _, sub := range []string{"data/workspace", "memory", "logs", "agents", "canvas", "cron", "devices", "identity", "media"} {
		os.MkdirAll(filepath.Join(dataDir, sub), 0755)
	}
	// Create empty MEMORY.md so OpenClaw doesn't error on first read
	memoryPath := filepath.Join(dataDir, "data", "workspace", "MEMORY.md")
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		os.WriteFile(memoryPath, []byte("# Memory\n"), 0644)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "openclaw.json"), openClawJSON, 0644); err != nil {
		return err
	}

	envContent := common.GenerateEnvFile(cfg, shared, perAgent)
	if err := os.MkdirAll(p.configDir(), 0700); err != nil {
		return err
	}
	envPath := filepath.Join(p.configDir(), cfg.Name+".env")
	if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old env file %s: %w", envPath, err)
	}
	if err := os.WriteFile(envPath, envContent, 0400); err != nil {
		return err
	}

	// 3. Deploy behavior files
	if err := p.deployBehavior(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: behavior file deployment failed: %v\n", err)
	}

	// 4. Read image
	image := p.getConfigValue("image")
	if image == "" {
		image = "ghcr.io/openclaw/openclaw:latest"
	}

	// 5. Load egress policy — proxy always deployed (deny-all when no policy)
	egressPolicy, policyErr := policy.LoadEgressPolicy(p.dataDir, cfg.Name)
	if policyErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load egress policy: %v\n", policyErr)
	}
	if egressPolicy != nil && egressPolicy.Mode == policy.EgressModeValidate {
		fmt.Fprintf(os.Stderr, "Egress proxy active in validate mode (logging violations, not blocking). iptables still forces all traffic through the proxy.\n")
	} else if egressPolicy == nil {
		fmt.Fprintf(os.Stderr, "No egress policy configured — proxy will deny all outbound traffic. Use 'conga policy set-egress' to allow domains.\n")
	}

	// 6. Create Docker network
	netName := networkName(cfg.Name)
	if !networkExists(ctx, netName) {
		fmt.Printf("Creating network %s...\n", netName)
		if err := createNetwork(ctx, netName); err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}
	}

	// 7. Start per-agent egress proxy (always — deny-all when no policy)
	if err := p.startAgentEgressProxy(ctx, cfg.Name, egressPolicy); err != nil {
		return fmt.Errorf("failed to start egress proxy: %w", err)
	}

	// 8. Start container
	cName := containerName(cfg.Name)
	if containerExists(ctx, cName) {
		if err := stopContainer(ctx, cName); err != nil {
			return fmt.Errorf("failed to stop existing container %s: %w", cName, err)
		}
		if err := removeContainer(ctx, cName); err != nil {
			return fmt.Errorf("failed to remove existing container %s: %w", cName, err)
		}
	}

	// Ensure all files are owned by the container user (node, uid 1000).
	// Best-effort: fails on macOS where uid 1000 doesn't exist, but Docker Desktop
	// handles ownership mapping transparently via its VM layer.
	exec.CommandContext(ctx, "chown", "-R", "1000:1000", dataDir).Run() //nolint:errcheck

	// Write proxy bootstrap script for Node.js CONNECT tunneling
	bootstrapPath := filepath.Join(p.configDir(), "proxy-bootstrap.js")
	if err := os.Remove(bootstrapPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old proxy bootstrap %s: %w", bootstrapPath, err)
	}
	if err := os.WriteFile(bootstrapPath, []byte(policy.ProxyBootstrapJS()), 0444); err != nil {
		return fmt.Errorf("failed to write proxy bootstrap: %w", err)
	}

	egressProxyName := policy.EgressProxyName(cfg.Name)
	fmt.Printf("Starting container %s...\n", cName)
	if err := runAgentContainer(ctx, agentContainerOpts{
		Name:               cName,
		AgentName:          cfg.Name,
		Network:            netName,
		EnvFile:            envPath,
		DataDir:            dataDir,
		GatewayPort:        cfg.GatewayPort,
		Image:              image,
		EgressProxyName:    egressProxyName,
		ProxyBootstrapPath: bootstrapPath,
	}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Apply iptables egress rules (defense-in-depth, best-effort on macOS).
	// Always applied — in validate mode the proxy logs+allows, but iptables ensures
	// nothing bypasses the proxy (e.g. tools ignoring HTTP_PROXY).
	agentIP, err := containerIPOnNetwork(ctx, cName, netName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not get agent IP for iptables: %v\n", err)
	} else if cidr, err := networkSubnetCIDR(ctx, netName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not get network CIDR for iptables: %v\n", err)
	} else if err := addEgressIptablesRules(ctx, agentIP, cidr); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: iptables egress rules not applied (expected on macOS): %v\n", err)
	} else {
		fmt.Printf("  Egress iptables: DROP rules applied for %s (%s)\n", cName, agentIP)
	}

	// 9. Update routing.json
	if err := p.regenerateRouting(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update routing: %v\n", err)
	}

	// 10. Ensure router is running and connected (only if any channel has credentials)
	if common.HasAnyChannel(shared) {
		if err := p.ensureRouter(ctx, false); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: router not started: %v\n", err)
		}
		if containerExists(ctx, routerContainer) {
			if err := connectNetwork(ctx, netName, routerContainer); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to connect router to agent network: %v\n", err)
			}
		}
	}

	// 11. Save config hash baseline
	p.saveConfigBaseline(cfg.Name)

	return nil
}

func (p *LocalProvider) RemoveAgent(ctx context.Context, name string, deleteSecrets bool) error {
	cName := containerName(name)
	netName := networkName(name)

	// Remove iptables egress rules before stopping container
	if containerExists(ctx, cName) && networkExists(ctx, netName) {
		if ip, err := containerIPOnNetwork(ctx, cName, netName); err == nil {
			if cidr, err := networkSubnetCIDR(ctx, netName); err == nil {
				removeEgressIptablesRules(ctx, ip, cidr)
			}
		}
	}

	if containerExists(ctx, cName) {
		stopContainer(ctx, cName)
		removeContainer(ctx, cName)
	}

	p.stopAgentEgressProxy(ctx, name)

	if containerExists(ctx, routerContainer) {
		disconnectNetwork(ctx, netName, routerContainer)
	}

	if networkExists(ctx, netName) {
		removeNetwork(ctx, netName)
	}

	os.Remove(filepath.Join(p.agentsDir(), name+".json"))
	os.Remove(filepath.Join(p.configDir(), name+".env"))
	os.Remove(filepath.Join(p.configDir(), name+".sha256"))
	os.Remove(filepath.Join(p.configDir(), fmt.Sprintf("egress-%s.yaml", name)))
	os.Remove(filepath.Join(p.configDir(), fmt.Sprintf("egress-%s-entrypoint.sh", name)))

	if deleteSecrets {
		os.RemoveAll(p.agentSecretsDir(name))
	}

	if err := p.regenerateRouting(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update routing: %v\n", err)
	}
	return nil
}

// --- Container Operations ---

// GetStatus returns the current status of an agent.
//
// SIDE EFFECT: When the container is running, this calls ensureEgressIptables
// which may modify host iptables rules to re-apply egress enforcement lost
// after a reboot or container IP change. This is intentional — status checks
// double as a self-healing mechanism for egress security.
func (p *LocalProvider) GetStatus(ctx context.Context, agentName string) (*provider.AgentStatus, error) {
	cName := containerName(agentName)
	status := &provider.AgentStatus{
		AgentName:    agentName,
		ServiceState: "docker",
	}

	if !containerExists(ctx, cName) {
		status.Container.State = "not found"
		return status, nil
	}

	state, err := inspectState(ctx, cName)
	if err != nil {
		return nil, err
	}
	status.Container.State = state.Status
	status.Container.StartedAt = state.StartedAt

	if state.Running {
		if stats, err := containerStats(ctx, cName); err == nil {
			status.Container.CPUPercent = stats.CPUPercent
			status.Container.MemoryUsage = stats.MemoryUsage
		}
		logs, _ := containerLogs(ctx, cName, 50)
		status.ReadyPhase = detectReadyPhase(logs)

		// Re-apply iptables egress rules if they were lost (e.g., after reboot or IP change).
		p.ensureEgressIptables(ctx, agentName)
	} else {
		status.ReadyPhase = "stopped"
	}

	return status, nil
}

// ensureEgressIptables checks if iptables egress rules are in place for a running
// container and re-applies them if missing. Handles IP changes after container restart.
// Always applied — in validate mode the proxy logs+allows, but iptables ensures
// nothing bypasses the proxy (e.g. tools ignoring HTTP_PROXY).
func (p *LocalProvider) ensureEgressIptables(ctx context.Context, agentName string) {
	cName := containerName(agentName)
	netName := networkName(agentName)
	if !networkExists(ctx, netName) {
		fmt.Fprintf(os.Stderr, "Warning: network %s not found for %s — cannot verify egress iptables\n", netName, agentName)
		return
	}

	ip, err := containerIPOnNetwork(ctx, cName, netName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get container IP for %s: %v\n", agentName, err)
		return
	}
	cidr, err := networkSubnetCIDR(ctx, netName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to get network CIDR for %s: %v\n", agentName, err)
		return
	}

	if !checkEgressIptablesRules(ctx, ip, cidr) {
		if err := addEgressIptablesRules(ctx, ip, cidr); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to re-apply egress iptables rules for %s: %v\n", agentName, err)
		}
	}
}

func (p *LocalProvider) GetLogs(ctx context.Context, agentName string, lines int) (string, error) {
	return containerLogs(ctx, containerName(agentName), lines)
}

func (p *LocalProvider) ContainerExec(ctx context.Context, agentName string, command []string) (string, error) {
	args := append([]string{"exec", containerName(agentName)}, command...)
	return dockerRun(ctx, args...)
}

func (p *LocalProvider) PauseAgent(ctx context.Context, name string) error {
	cfg, err := p.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if cfg.Paused {
		fmt.Printf("Agent %s is already paused.\n", name)
		return nil
	}

	// Mark paused first so state is consistent even if container ops fail
	cfg.Paused = true
	if err := p.saveAgentConfig(cfg); err != nil {
		return err
	}

	// Stop container (preserve data)
	cName := containerName(name)
	netName := networkName(name)

	// Remove iptables egress rules before stopping container
	if containerExists(ctx, cName) && networkExists(ctx, netName) {
		if ip, err := containerIPOnNetwork(ctx, cName, netName); err == nil {
			if cidr, err := networkSubnetCIDR(ctx, netName); err == nil {
				removeEgressIptablesRules(ctx, ip, cidr)
			}
		}
	}

	if containerExists(ctx, cName) {
		stopContainer(ctx, cName)
		removeContainer(ctx, cName)
	}

	p.stopAgentEgressProxy(ctx, name)

	if containerExists(ctx, routerContainer) {
		disconnectNetwork(ctx, netName, routerContainer)
	}

	// Regenerate routing (excludes paused agents)
	if err := p.regenerateRouting(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update routing: %v\n", err)
	}

	return nil
}

func (p *LocalProvider) UnpauseAgent(ctx context.Context, name string) error {
	cfg, err := p.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if !cfg.Paused {
		fmt.Printf("Agent %s is not paused.\n", name)
		return nil
	}

	// Update agent config first (so RefreshAgent sees active state)
	cfg.Paused = false
	if err := p.saveAgentConfig(cfg); err != nil {
		return err
	}

	// Refresh agent (regenerates config, starts container, reconnects router)
	if err := p.RefreshAgent(ctx, name); err != nil {
		return err
	}

	// Regenerate routing (includes this agent again)
	if err := p.regenerateRouting(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update routing: %v\n", err)
	}

	return nil
}

func (p *LocalProvider) saveAgentConfig(cfg *provider.AgentConfig) error {
	if err := os.MkdirAll(p.agentsDir(), 0700); err != nil {
		return err
	}
	agentJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.agentsDir(), cfg.Name+".json"), agentJSON, 0600)
}

func (p *LocalProvider) RefreshAgent(ctx context.Context, agentName string) error {
	cfg, err := p.GetAgent(ctx, agentName)
	if err != nil {
		return err
	}
	if cfg.Paused {
		return fmt.Errorf("agent %s is paused. Use `conga admin unpause %s` first", agentName, agentName)
	}

	shared, _ := p.readSharedSecrets()
	perAgent, _ := p.readAgentSecrets(agentName)

	// Check config integrity before trusting the existing token
	dataDir := p.dataSubDir(agentName)
	existingToken := ""
	if err := p.checkConfigIntegrity(agentName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		fmt.Fprintf(os.Stderr, "Generating fresh gateway token instead of preserving existing one.\n")
		existingToken, _ = generateToken()
	} else {
		existingToken = readExistingGatewayToken(filepath.Join(dataDir, "openclaw.json"))
	}

	// Regenerate openclaw.json with current config format
	openClawJSON, err := common.GenerateOpenClawConfig(*cfg, shared, existingToken)
	if err != nil {
		return fmt.Errorf("failed to generate config: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory %s: %w", dataDir, err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "openclaw.json"), openClawJSON, 0644); err != nil {
		return err
	}

	// Update baseline hash after writing new config
	p.saveConfigBaseline(agentName)

	// Regenerate env file
	envContent := common.GenerateEnvFile(*cfg, shared, perAgent)
	envPath := filepath.Join(p.configDir(), agentName+".env")
	if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old env file %s: %w", envPath, err)
	}
	if err := os.WriteFile(envPath, envContent, 0400); err != nil {
		return err
	}

	// Load egress policy — proxy always deployed (deny-all when no policy)
	egressPolicy, policyErr := policy.LoadEgressPolicy(p.dataDir, agentName)
	if policyErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load egress policy: %v\n", policyErr)
	}
	if egressPolicy != nil && egressPolicy.Mode == policy.EgressModeValidate {
		fmt.Fprintf(os.Stderr, "Egress proxy active in validate mode (logging violations, not blocking). iptables still forces all traffic through the proxy.\n")
	} else if egressPolicy == nil {
		fmt.Fprintf(os.Stderr, "No egress policy configured — proxy will deny all outbound traffic. Use 'conga policy set-egress' to allow domains.\n")
	}

	cName := containerName(agentName)
	netName := networkName(agentName)

	// Remove old iptables egress rules before stopping container (need IP while running)
	if containerExists(ctx, cName) && networkExists(ctx, netName) {
		if ip, err := containerIPOnNetwork(ctx, cName, netName); err == nil {
			if cidr, err := networkSubnetCIDR(ctx, netName); err == nil {
				removeEgressIptablesRules(ctx, ip, cidr)
			}
		}
	}

	if containerExists(ctx, cName) {
		if err := stopContainer(ctx, cName); err != nil {
			return fmt.Errorf("failed to stop container %s: %w", cName, err)
		}
		if err := removeContainer(ctx, cName); err != nil {
			return fmt.Errorf("failed to remove container %s: %w", cName, err)
		}
	}

	p.stopAgentEgressProxy(ctx, agentName)

	image := p.getConfigValue("image")
	if image == "" {
		image = "ghcr.io/openclaw/openclaw:latest"
	}

	// Recreate network. TODO: consider keeping the network if egress policy
	// hasn't changed — currently we always recreate for a clean slate, which
	// causes brief connectivity loss during refresh.
	if networkExists(ctx, netName) {
		if containerExists(ctx, routerContainer) {
			disconnectNetwork(ctx, netName, routerContainer)
		}
		if err := removeNetwork(ctx, netName); err != nil {
			return fmt.Errorf("failed to remove network %s: %w", netName, err)
		}
	}
	if err := createNetwork(ctx, netName); err != nil {
		return fmt.Errorf("failed to create network %s: %w", netName, err)
	}

	// Start per-agent egress proxy (always — deny-all when no policy)
	if err := p.startAgentEgressProxy(ctx, agentName, egressPolicy); err != nil {
		return fmt.Errorf("failed to start egress proxy: %w", err)
	}

	// Write proxy bootstrap script for Node.js CONNECT tunneling
	bootstrapPath := filepath.Join(p.configDir(), "proxy-bootstrap.js")
	if err := os.Remove(bootstrapPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old proxy bootstrap %s: %w", bootstrapPath, err)
	}
	if err := os.WriteFile(bootstrapPath, []byte(policy.ProxyBootstrapJS()), 0444); err != nil {
		return fmt.Errorf("failed to write proxy bootstrap: %w", err)
	}

	// Ensure all files are owned by the container user before starting.
	// Best-effort: chown fails on macOS where uid 1000 doesn't exist (Docker Desktop remaps).
	exec.CommandContext(ctx, "chown", "-R", "1000:1000", dataDir).Run() //nolint:errcheck

	refreshEgressProxyName := policy.EgressProxyName(agentName)
	if err := runAgentContainer(ctx, agentContainerOpts{
		Name:               cName,
		AgentName:          agentName,
		Network:            netName,
		EnvFile:            envPath,
		DataDir:            dataDir,
		GatewayPort:        cfg.GatewayPort,
		Image:              image,
		EgressProxyName:    refreshEgressProxyName,
		ProxyBootstrapPath: bootstrapPath,
	}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}

	// Apply iptables egress rules (defense-in-depth, best-effort on macOS).
	// Always applied — in validate mode the proxy logs+allows, but iptables ensures
	// nothing bypasses the proxy (e.g. tools ignoring HTTP_PROXY).
	agentIP, err := containerIPOnNetwork(ctx, cName, netName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not get agent IP for iptables: %v\n", err)
	} else if cidr, err := networkSubnetCIDR(ctx, netName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not get network CIDR for iptables: %v\n", err)
	} else if err := addEgressIptablesRules(ctx, agentIP, cidr); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: iptables egress rules not applied (expected on macOS): %v\n", err)
	} else {
		fmt.Printf("  Egress iptables: DROP rules applied for %s (%s)\n", cName, agentIP)
	}

	// Reconnect router
	if containerExists(ctx, routerContainer) {
		if err := connectNetwork(ctx, netName, routerContainer); err != nil {
			return fmt.Errorf("failed to reconnect router to network %s: %w", netName, err)
		}
	}
	return nil
}

func (p *LocalProvider) RefreshAll(ctx context.Context) error {
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return err
	}

	spin := ui.NewSpinner("Refreshing all agents...")
	for _, a := range agents {
		if a.Paused {
			spin.Stop()
			fmt.Printf("Skipping paused agent: %s\n", a.Name)
			spin = ui.NewSpinner("Refreshing all agents...")
			continue
		}
		if err := p.RefreshAgent(ctx, a.Name); err != nil {
			spin.Stop()
			return fmt.Errorf("failed to refresh %s: %w", a.Name, err)
		}
	}
	spin.Stop()
	return nil
}

// --- Connectivity ---

func (p *LocalProvider) Connect(ctx context.Context, agentName string, localPort int) (*provider.ConnectInfo, error) {
	cfg, err := p.GetAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	if localPort == 0 {
		localPort = cfg.GatewayPort
	}

	// Try to extract the gateway token from the running container's config.
	// OpenClaw auto-generates the token at first boot and writes it back.
	cName := containerName(agentName)
	token := ""

	// Read from the data dir (OpenClaw writes token back to config on disk)
	configPath := filepath.Join(p.dataSubDir(agentName), "openclaw.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var config map[string]interface{}
		if err := json.Unmarshal(data, &config); err == nil {
			if gw, ok := config["gateway"].(map[string]interface{}); ok {
				if t, ok := gw["token"].(string); ok && t != "" {
					token = t
				}
				if auth, ok := gw["auth"].(map[string]interface{}); ok {
					if t, ok := auth["token"].(string); ok && t != "" {
						token = t
					}
				}
			}
		}
	}

	// Fallback: try docker exec to read it from inside the container
	if token == "" && containerExists(ctx, cName) {
		output, err := dockerRun(ctx, "exec", cName, "node", "-e",
			`try{const c=require('/home/node/.openclaw/openclaw.json');console.log(c.gateway?.token||c.gateway?.auth?.token||'')}catch(e){console.log('')}`)
		if err == nil {
			token = strings.TrimSpace(output)
		}
	}

	if token == "" {
		fmt.Println("Note: gateway token not found. The web UI may prompt for authentication.")
		return &provider.ConnectInfo{
			URL:       fmt.Sprintf("http://localhost:%d", localPort),
			LocalPort: localPort,
			Waiter:    nil,
		}, nil
	}

	return &provider.ConnectInfo{
		URL:       fmt.Sprintf("http://localhost:%d#token=%s", localPort, token),
		LocalPort: localPort,
		Token:     token,
		Waiter:    nil,
	}, nil
}

// --- Environment Management ---

func (p *LocalProvider) Setup(ctx context.Context, cfg *provider.SetupConfig) error {
	if err := dockerCheck(ctx); err != nil {
		return err
	}

	fmt.Println("Setting up local Conga Line deployment...")

	// Create directory structure
	dirs := []string{
		p.agentsDir(),
		p.sharedSecretsDir(),
		filepath.Join(p.dataDir, "secrets", "agents"),
		filepath.Join(p.dataDir, "data"),
		p.configDir(),
		p.routerDir(),
		filepath.Join(p.routerDir(), "src"),
		p.behaviorDir(),
		p.logsDir(),
		p.egressProxyDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	changed := 0

	// --- Repo path (for copying router source and behavior files) ---
	repoPath := p.getConfigValue("repo_path")
	if cfg != nil && cfg.RepoPath != "" {
		repoPath = cfg.RepoPath
	}
	repoStatus := "set"
	if repoPath == "" {
		repoStatus = "not set"
		// Try to auto-detect from git
		repoPath = detectRepoRoot()
	}
	fmt.Printf("\n[config] repo_path — Conga Line repo root for router/behavior files (%s)\n", repoStatus)
	if cfg == nil {
		newRepoPath, err := ui.TextPromptWithDefault("  Repo path", repoPath)
		if err != nil {
			return err
		}
		if newRepoPath != "" {
			repoPath = newRepoPath
		}
	}
	if repoPath != "" {
		// Validate the path has router/ and behavior/ directories
		if _, err := os.Stat(filepath.Join(repoPath, "router", "src", "index.js")); err != nil {
			return fmt.Errorf("invalid repo path: %s/router/src/index.js not found", repoPath)
		}
		p.setConfigValue("repo_path", repoPath)
		changed++
	}

	// --- Docker image ---
	image := p.getConfigValue("image")
	if cfg != nil && cfg.Image != "" {
		image = cfg.Image
	}
	imageStatus := "set"
	if image == "" {
		imageStatus = "not set"
	}
	fmt.Printf("\n[config] image — OpenClaw Docker image (%s)\n", imageStatus)
	if cfg != nil {
		if image == "" {
			image = "ghcr.io/openclaw/openclaw:2026.3.11"
		}
	} else if image == "" || ui.Confirm("  Update this value?") {
		defaultImage := "ghcr.io/openclaw/openclaw:2026.3.11"
		if image != "" {
			defaultImage = image
		}
		newImage, err := ui.TextPromptWithDefault("  Docker image", defaultImage)
		if err != nil {
			return err
		}
		if newImage != "" {
			image = newImage
		}
	}
	if image != "" {
		p.setConfigValue("image", image)
		changed++
	}

	// --- Shared secrets (non-channel only — channel secrets are managed via `conga channels add`) ---
	type secretItem struct {
		name, description string
		isSecret          bool
	}
	secretItems := []secretItem{
		{"google-client-id", "Google OAuth client ID", false},
		{"google-client-secret", "Google OAuth client secret", true},
	}

	for _, item := range secretItems {
		path := filepath.Join(p.sharedSecretsDir(), item.name)
		current, _ := readSecret(path)

		// Check for config-provided value first
		cfgValue := cfg.SecretValue(item.name)
		status := "set"
		if current == "" && cfgValue == "" {
			status = "not set"
		}
		optLabel := " (optional)"
		fmt.Printf("\n[secret] %s — %s%s (%s)\n", item.name, item.description, optLabel, status)
		var value string
		if cfgValue != "" {
			value = cfgValue
		} else if cfg != nil {
			fmt.Println("  Skipped (not in config)")
			continue
		} else {
			if current != "" {
				if !ui.Confirm("  Update this value?") {
					continue
				}
			}

			var err error
			if item.isSecret {
				value, err = ui.SecretPrompt(fmt.Sprintf("  Enter %s", item.name))
			} else {
				value, err = ui.TextPrompt(fmt.Sprintf("  Enter %s", item.name))
			}
			if err != nil {
				return err
			}
			if value == "" {
				fmt.Println("  Skipped (empty value)")
				continue
			}
		}

		if err := writeSecret(path, value); err != nil {
			return fmt.Errorf("failed to save %s: %w", item.name, err)
		}
		fmt.Printf("  Saved (%s).\n", common.MaskSecret(value))
		changed++
	}

	// --- Copy router source from repo ---
	if repoPath != "" {
		fmt.Println("\nCopying router source files...")
		if err := copyDir(filepath.Join(repoPath, "router"), p.routerDir()); err != nil {
			return fmt.Errorf("failed to copy router files: %w", err)
		}
		fmt.Println("  Router source copied to ~/.conga/router/")

		fmt.Println("Copying behavior files...")
		if err := copyDir(filepath.Join(repoPath, "behavior"), p.behaviorDir()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to copy behavior files: %v\n", err)
		} else {
			fmt.Println("  Behavior files copied to ~/.conga/behavior/")
		}

		// Copy egress proxy files
		fmt.Println("Copying egress proxy config...")
		if err := copyDir(filepath.Join(repoPath, "deploy", "egress-proxy"), p.egressProxyDir()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to copy egress proxy files: %v\n", err)
		} else {
			fmt.Println("  Egress proxy config copied to ~/.conga/egress-proxy/")
		}
	}

	// --- Pull images ---
	if image != "" {
		fmt.Printf("\nPulling OpenClaw image %s...\n", image)
		spin := ui.NewSpinner("Pulling Docker image...")
		err := pullImage(ctx, image)
		spin.Stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to pull image: %v\nYou can pull it manually: docker pull %s\n", err, image)
		} else {
			fmt.Println("  Image pulled.")
		}
	}

	// Pull node:22-alpine for router
	fmt.Println("Pulling node:22-alpine for router...")
	spin := ui.NewSpinner("Pulling router image...")
	pullImage(ctx, "node:22-alpine")
	spin.Stop()

	// --- Build egress proxy image ---
	if _, err := os.Stat(filepath.Join(p.egressProxyDir(), "Dockerfile")); err == nil {
		fmt.Println("Building egress proxy image...")
		spin := ui.NewSpinner("Building egress proxy...")
		err := buildImage(ctx, p.egressProxyDir(), egressProxyImage)
		spin.Stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to build egress proxy: %v\n", err)
		} else {
			fmt.Println("  Egress proxy image built.")
		}
	}

	// --- Create initial empty routing.json ---
	routingPath := filepath.Join(p.configDir(), "routing.json")
	if _, err := os.Stat(routingPath); os.IsNotExist(err) {
		os.WriteFile(routingPath, []byte(`{"channels":{},"members":{}}`), 0644)
	}

	// --- Auto-configure channels if secrets were provided in SetupConfig (backwards compat) ---
	if cfg != nil {
		for _, ch := range channels.All() {
			channelSecrets := map[string]string{}
			hasRequired := true
			for _, def := range ch.SharedSecrets() {
				val := cfg.SecretValue(def.Name)
				if val != "" {
					channelSecrets[def.Name] = val
				} else if def.Required {
					hasRequired = false
					break
				}
			}
			if hasRequired && len(channelSecrets) > 0 {
				fmt.Printf("\nAuto-configuring %s channel from provided secrets...\n", ch.Name())
				if err := p.AddChannel(ctx, ch.Name(), channelSecrets); err != nil {
					return fmt.Errorf("auto-configure %s channel: %w", ch.Name(), err)
				}
			}
		}
	}

	// --- Save provider config ---
	provCfg := &provider.Config{
		Provider: "local",
		DataDir:  p.dataDir,
	}
	if err := provider.SaveConfig(provider.DefaultConfigPath(), provCfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if changed > 0 {
		fmt.Printf("\n%d value(s) configured.\n", changed)
	} else {
		fmt.Println("\nAll values already configured.")
	}
	fmt.Println("\nLocal deployment ready! Next steps:")
	fmt.Println("  conga channels add slack                                     # optional: add Slack integration")
	fmt.Println("  conga admin add-user <name>                                  # provision an agent")
	fmt.Println("  conga channels bind <name> slack:<id>                        # optional: bind agent to Slack")
	return nil
}

func (p *LocalProvider) CycleHost(ctx context.Context) error {
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Stopping all containers...")
	for _, a := range agents {
		if !a.Paused {
			stopContainer(ctx, containerName(a.Name))
		}
	}
	stopContainer(ctx, routerContainer)

	fmt.Println("Restarting...")

	if err := p.ensureRouter(ctx, false); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: router not started: %v\n", err)
	}

	// Restart agents
	for _, a := range agents {
		if a.Paused {
			fmt.Printf("Skipping paused agent: %s\n", a.Name)
			continue
		}
		if err := p.RefreshAgent(ctx, a.Name); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to restart %s: %v\n", a.Name, err)
		}
	}

	fmt.Println("All containers restarted.")
	return nil
}

func (p *LocalProvider) Teardown(ctx context.Context) error {
	// Remove agents from config (if config still exists)
	agents, _ := p.ListAgents(ctx)
	for _, a := range agents {
		fmt.Printf("Removing agent %s...\n", a.Name)
		p.removeAgentDocker(ctx, a.Name)
	}

	// Also find any conga-* containers/networks directly from Docker
	// in case config was already deleted or is out of sync
	p.cleanupDockerByPrefix(ctx)

	// Delete all local state
	if _, err := os.Stat(p.dataDir); err == nil {
		fmt.Printf("Removing %s...\n", p.dataDir)
		if err := os.RemoveAll(p.dataDir); err != nil {
			return fmt.Errorf("failed to remove data directory: %w", err)
		}
	}

	fmt.Println("Local deployment torn down.")
	return nil
}

// removeAgentDocker removes a single agent's container, iptables rules, proxy, and network.
func (p *LocalProvider) removeAgentDocker(ctx context.Context, name string) {
	cName := containerName(name)
	netName := networkName(name)
	// Remove iptables rules before stopping (need container IP while running)
	if containerExists(ctx, cName) && networkExists(ctx, netName) {
		if ip, err := containerIPOnNetwork(ctx, cName, netName); err == nil {
			if cidr, err := networkSubnetCIDR(ctx, netName); err == nil {
				removeEgressIptablesRules(ctx, ip, cidr)
			}
		}
	}
	if containerExists(ctx, cName) {
		stopContainer(ctx, cName)
		removeContainer(ctx, cName)
	}
	p.stopAgentEgressProxy(ctx, name)
	if networkExists(ctx, netName) {
		removeNetwork(ctx, netName)
	}
}

// cleanupDockerByPrefix finds and removes all conga-* containers and networks
// directly from Docker, regardless of local config state.
func (p *LocalProvider) cleanupDockerByPrefix(ctx context.Context) {
	// Find all conga-* containers
	output, err := dockerRun(ctx, "ps", "-a", "--filter", "name=conga-", "--format", "{{.Names}}")
	if err == nil {
		for _, name := range strings.Split(strings.TrimSpace(output), "\n") {
			if name == "" {
				continue
			}
			fmt.Printf("Removing container %s...\n", name)
			stopContainer(ctx, name)
			removeContainer(ctx, name)
		}
	}

	// Find all conga-* networks
	output, err = dockerRun(ctx, "network", "ls", "--filter", "name=conga-", "--format", "{{.Name}}")
	if err == nil {
		for _, name := range strings.Split(strings.TrimSpace(output), "\n") {
			if name == "" {
				continue
			}
			fmt.Printf("Removing network %s...\n", name)
			removeNetwork(ctx, name)
		}
	}

}

// --- infrastructure helpers ---

// ensureRouter starts or restarts the router container.
// If restart is true and the router is already running, it is replaced to pick up config changes.
func (p *LocalProvider) ensureRouter(ctx context.Context, restart bool) error {
	if containerExists(ctx, routerContainer) {
		state, err := inspectState(ctx, routerContainer)
		if err == nil && state.Running && !restart {
			return nil // already running, no restart requested
		}
		// Exists but not running (or restart requested) — remove and recreate
		if err := removeContainer(ctx, routerContainer); err != nil {
			return fmt.Errorf("failed to remove existing router container: %w", err)
		}
	}

	routerEnvPath := filepath.Join(p.configDir(), "router.env")
	routingPath := filepath.Join(p.configDir(), "routing.json")

	// Check required files exist
	if _, err := os.Stat(filepath.Join(p.routerDir(), "src", "index.js")); err != nil {
		return fmt.Errorf("router source not found at %s", p.routerDir())
	}
	if _, err := os.Stat(routerEnvPath); err != nil {
		return fmt.Errorf("router.env not found — run 'conga channels add' first")
	}

	fmt.Println("Starting router...")
	if err := runRouterContainer(ctx, routerContainerOpts{
		EnvFile:     routerEnvPath,
		RouterDir:   p.routerDir(),
		RoutingJSON: routingPath,
	}); err != nil {
		return fmt.Errorf("failed to start router: %w", err)
	}

	// Connect router to all existing agent networks
	agents, err := p.ListAgents(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not list agents for router network connections: %v\n", err)
	}
	for _, a := range agents {
		if err := connectNetwork(ctx, networkName(a.Name), routerContainer); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to connect router to %s network: %v\n", a.Name, err)
		}
	}

	fmt.Println("  Router started.")
	return nil
}

// startAgentEgressProxy starts a per-agent Envoy proxy for egress domain filtering.
func (p *LocalProvider) startAgentEgressProxy(ctx context.Context, agentName string, ep *policy.EgressPolicy) error {
	proxyName := policy.EgressProxyName(agentName)
	netName := networkName(agentName)

	// Stop existing proxy if any
	if containerExists(ctx, proxyName) {
		removeContainer(ctx, proxyName)
	}

	// Build proxy image if not present or missing Envoy.
	if !imageExists(ctx, policy.EgressProxyImage) || !imageHasBinary(ctx, policy.EgressProxyImage, "envoy") {
		fmt.Printf("  Building egress proxy image...\n")
		if err := buildEgressProxyImage(ctx); err != nil {
			return fmt.Errorf("building egress proxy image: %w", err)
		}
	}

	// Generate Envoy config
	conf, err := policy.GenerateProxyConf(ep)
	if err != nil {
		return fmt.Errorf("generating envoy config: %w", err)
	}
	confPath := filepath.Join(p.configDir(), fmt.Sprintf("egress-%s.yaml", agentName))
	if err := os.MkdirAll(filepath.Dir(confPath), 0700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	if err := os.Remove(confPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old egress config %s: %w", confPath, err)
	}
	if err := os.WriteFile(confPath, []byte(conf), 0444); err != nil {
		return fmt.Errorf("writing egress config: %w", err)
	}

	// Ensure agent network exists (caller should have created it, but be safe)
	if !networkExists(ctx, netName) {
		if err := createNetwork(ctx, netName); err != nil {
			return fmt.Errorf("creating network: %w", err)
		}
	}

	// Write entrypoint script for Envoy
	entrypointPath := filepath.Join(p.configDir(), fmt.Sprintf("egress-%s-entrypoint.sh", agentName))
	if err := os.Remove(entrypointPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old entrypoint %s: %w", entrypointPath, err)
	}
	if err := os.WriteFile(entrypointPath, []byte(policy.GenerateProxyEntrypoint()), 0555); err != nil {
		return fmt.Errorf("writing entrypoint: %w", err)
	}

	// Start Envoy proxy on agent's network.
	// --entrypoint overrides the default Envoy entrypoint which tries to chown /dev/stdout.
	// --user 101:101 runs as the envoy user (non-root) for reduced blast radius.
	args := []string{"run", "-d",
		"--name", proxyName,
		"--network", netName,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--memory", "128m",
		"--read-only",
		"--user", "101:101",
		"--tmpfs", "/tmp:rw,noexec,nosuid",
		"--entrypoint", "",
		"-v", fmt.Sprintf("%s:/etc/envoy/envoy.yaml:ro", confPath),
		"-v", fmt.Sprintf("%s:/opt/entrypoint.sh:ro", entrypointPath),
	}
	args = append(args, policy.EgressProxyImage, "sh", "/opt/entrypoint.sh")

	if _, err := dockerRun(ctx, args...); err != nil {
		return fmt.Errorf("starting egress proxy: %w", err)
	}

	fmt.Printf("  Egress proxy started for %s (%d domains allowed)\n", agentName, len(policy.EffectiveAllowedDomains(ep)))
	return nil
}

// buildEgressProxyImage builds the egress proxy Docker image locally from Envoy.
func buildEgressProxyImage(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "conga-egress-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(policy.EgressProxyDockerfile()), 0644); err != nil {
		return err
	}
	return buildImage(ctx, dir, policy.EgressProxyImage)
}

// stopAgentEgressProxy removes the per-agent egress proxy container.
func (p *LocalProvider) stopAgentEgressProxy(ctx context.Context, agentName string) {
	proxyName := policy.EgressProxyName(agentName)
	if containerExists(ctx, proxyName) {
		if err := removeContainer(ctx, proxyName); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to remove egress proxy %s: %v\n", proxyName, err)
		}
	}
}

// --- file helpers ---

func (p *LocalProvider) regenerateRouting(ctx context.Context) error {
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return err
	}
	data, err := common.GenerateRoutingJSON(agents)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p.configDir(), 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(p.configDir(), "routing.json"), data, 0644)
}

func (p *LocalProvider) deployBehavior(cfg provider.AgentConfig) error {
	behaviorDir := p.behaviorDir()
	if _, err := os.Stat(behaviorDir); os.IsNotExist(err) {
		return nil
	}

	files, err := common.ComposeBehaviorFiles(behaviorDir, cfg)
	if err != nil {
		return err
	}

	targetDir := filepath.Join(p.dataSubDir(cfg.Name), "data", "workspace")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(targetDir, name), content, 0644); err != nil {
			return err
		}
	}
	return nil
}

func (p *LocalProvider) getConfigValue(key string) string {
	extraPath := filepath.Join(p.dataDir, "local-config.json")
	data, err := os.ReadFile(extraPath)
	if err != nil {
		return ""
	}
	var extra map[string]string
	if err := json.Unmarshal(data, &extra); err != nil {
		return ""
	}
	return extra[key]
}

func (p *LocalProvider) setConfigValue(key, value string) error {
	if err := os.MkdirAll(p.dataDir, 0700); err != nil {
		return err
	}
	extraPath := filepath.Join(p.dataDir, "local-config.json")
	extra := make(map[string]string)
	if data, err := os.ReadFile(extraPath); err == nil {
		json.Unmarshal(data, &extra)
	}
	extra[key] = value
	data, err := json.MarshalIndent(extra, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(extraPath, data, 0600)
}

// --- utility functions ---

func detectReadyPhase(logs string) string {
	phase := "starting"
	if strings.Contains(logs, "[gateway] listening") {
		phase = "gateway up, waiting for plugins"
	}
	if strings.Contains(logs, "[slack]") && strings.Contains(logs, "starting provider") {
		phase = "slack plugin loading"
	}
	if strings.Contains(logs, "[slack] http mode listening") {
		phase = "slack endpoint ready, resolving channels"
	}
	if strings.Contains(logs, "[slack] channels resolved") {
		phase = "ready"
	}
	if strings.Contains(strings.ToLower(logs), "error") || strings.Contains(strings.ToLower(logs), "fatal") {
		phase += " (errors in logs — check `conga logs`)"
	}
	return phase
}

// readExistingGatewayToken extracts the gateway auth token from an existing openclaw.json.
func readExistingGatewayToken(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return ""
	}
	if gw, ok := config["gateway"].(map[string]interface{}); ok {
		if auth, ok := gw["auth"].(map[string]interface{}); ok {
			if t, ok := auth["token"].(string); ok {
				return t
			}
		}
		if t, ok := gw["token"].(string); ok {
			return t
		}
	}
	return ""
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// detectRepoRoot tries to find the conga-line repo root from the current working directory.
func detectRepoRoot() string {
	// Walk up looking for CLAUDE.md (a reliable marker for this repo)
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "router", "src", "index.js")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// copyDir recursively copies src to dst, overwriting existing files.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0700)
		}

		// Skip node_modules if present
		if strings.Contains(relPath, "node_modules") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, 0644)
	})
}
