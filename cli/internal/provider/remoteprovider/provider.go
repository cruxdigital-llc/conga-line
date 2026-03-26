package remoteprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	posixpath "path"
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
	egressProxyContainer = "conga-egress-proxy"
	egressProxyImage     = "conga-egress-proxy"
	egressNetwork        = "conga-egress"
	routerContainer      = "conga-router"
)

// RemoteProvider implements provider.Provider for any SSH-accessible host.
// Works with VPS instances, bare metal servers (Raspberry Pi, Mac Mini),
// colocated servers, or any Linux machine reachable via SSH with Docker installed.
type RemoteProvider struct {
	ssh       *SSHClient
	dataDir   string // local ~/.conga/ (for remote-config.json)
	remoteDir string // /opt/conga/ on the remote host
}

// NewRemoteProvider creates a remote provider.
func NewRemoteProvider(cfg *provider.Config) (provider.Provider, error) {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = provider.DefaultDataDir()
	}

	p := &RemoteProvider{
		dataDir:   dataDir,
		remoteDir: "/opt/conga",
	}

	// Allow creation without SSH connection for setup (which prompts for details)
	host := cfg.SSHHost
	if host == "" {
		return p, nil
	}

	sshClient, err := SSHConnect(host, cfg.SSHPort, cfg.SSHUser, cfg.SSHKeyPath)
	if err != nil {
		return nil, err
	}
	p.ssh = sshClient

	return p, nil
}

func init() {
	provider.Register("remote", NewRemoteProvider)
}

func (p *RemoteProvider) Name() string { return "remote" }

// Close releases the SSH connection. TODO: wire into CLI framework shutdown.
func (p *RemoteProvider) Close() error {
	if p.ssh != nil {
		return p.ssh.Close()
	}
	return nil
}

// --- remote paths ---

func (p *RemoteProvider) remoteAgentsDir() string { return posixpath.Join(p.remoteDir, "agents") }
func (p *RemoteProvider) remoteConfigDir() string { return posixpath.Join(p.remoteDir, "config") }
func (p *RemoteProvider) remoteDataSubDir(name string) string {
	return posixpath.Join(p.remoteDir, "data", name)
}
func (p *RemoteProvider) remoteRouterDir() string   { return posixpath.Join(p.remoteDir, "router") }
func (p *RemoteProvider) remoteBehaviorDir() string { return posixpath.Join(p.remoteDir, "behavior") }
func (p *RemoteProvider) remoteEgressProxyDir() string {
	return posixpath.Join(p.remoteDir, "egress-proxy")
}

// requireSSH returns an error if the SSH connection is not established.
func (p *RemoteProvider) requireSSH() error {
	if p.ssh == nil {
		return fmt.Errorf("SSH not configured. Run `conga admin setup --provider remote` first")
	}
	return nil
}

// --- Identity & Discovery ---

func (p *RemoteProvider) WhoAmI(ctx context.Context) (*provider.Identity, error) {
	if err := p.requireSSH(); err != nil {
		return nil, err
	}
	user, err := p.ssh.Run(ctx, "whoami")
	if err != nil {
		return &provider.Identity{Name: fmt.Sprintf("%s@%s", p.ssh.user, p.ssh.host)}, nil
	}
	return &provider.Identity{Name: fmt.Sprintf("%s@%s", strings.TrimSpace(user), p.ssh.host)}, nil
}

func (p *RemoteProvider) ListAgents(ctx context.Context) ([]provider.AgentConfig, error) {
	dir := p.remoteAgentsDir()
	output, err := p.ssh.Run(ctx, fmt.Sprintf("ls %s/*.json 2>/dev/null || true", shellQuote(dir)))
	if err != nil || strings.TrimSpace(output) == "" {
		return nil, nil
	}

	var agents []provider.AgentConfig
	for _, path := range strings.Split(strings.TrimSpace(output), "\n") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		data, err := p.ssh.Download(path)
		if err != nil {
			continue
		}
		var cfg provider.AgentConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		// Extract name from filename
		base := posixpath.Base(path)
		cfg.Name = strings.TrimSuffix(base, ".json")
		agents = append(agents, cfg)
	}

	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}

func (p *RemoteProvider) GetAgent(ctx context.Context, name string) (*provider.AgentConfig, error) {
	path := posixpath.Join(p.remoteAgentsDir(), name+".json")
	data, err := p.ssh.Download(path)
	if err != nil {
		return nil, fmt.Errorf("agent %q not found. Use `conga admin add-user` or `add-team` to provision", name)
	}
	var cfg provider.AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.Name = name
	return &cfg, nil
}

func (p *RemoteProvider) ResolveAgentByIdentity(ctx context.Context) (*provider.AgentConfig, error) {
	agents, err := p.ListAgents(ctx)
	if err != nil || len(agents) != 1 {
		return nil, nil
	}
	return &agents[0], nil
}

// --- Agent Lifecycle ---

func (p *RemoteProvider) ProvisionAgent(ctx context.Context, cfg provider.AgentConfig) error {
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

	dataDir := p.remoteDataSubDir(cfg.Name)
	// Create directory structure on remote
	for _, sub := range []string{"data/workspace", "memory", "logs", "agents", "canvas", "cron", "devices", "identity", "media"} {
		p.ssh.MkdirAll(posixpath.Join(dataDir, sub), 0755)
	}
	// Create empty MEMORY.md
	memoryPath := posixpath.Join(dataDir, "data", "workspace", "MEMORY.md")
	p.ssh.Run(ctx, fmt.Sprintf("test -f %s || echo '# Memory' > %s", shellQuote(memoryPath), shellQuote(memoryPath)))

	if err := p.ssh.Upload(posixpath.Join(dataDir, "openclaw.json"), openClawJSON, 0644); err != nil {
		return err
	}

	envContent := common.GenerateEnvFile(cfg, shared, perAgent)
	p.ssh.MkdirAll(p.remoteConfigDir(), 0700)
	envPath := posixpath.Join(p.remoteConfigDir(), cfg.Name+".env")
	if err := p.ssh.Upload(envPath, envContent, 0400); err != nil {
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

	// 5. Load egress policy (from local ~/.conga/, not the remote host)
	egressPolicy, policyErr := policy.LoadEgressPolicy(p.dataDir, cfg.Name)
	if policyErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load egress policy: %v\n", policyErr)
	}
	egressEnforce := false
	if egressPolicy != nil && len(egressPolicy.AllowedDomains) > 0 {
		egressEnforce = true // Remote always enforces when domains defined
	}

	// 6. Create Docker network
	netName := networkName(cfg.Name)
	if !p.networkExists(ctx, netName) {
		fmt.Printf("Creating network %s...\n", netName)
		if err := p.createNetwork(ctx, netName); err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}
	}

	// 7. Start per-agent egress proxy (when policy defines domains)
	if egressEnforce {
		domains := policy.EffectiveAllowedDomains(egressPolicy)
		if err := p.startAgentEgressProxy(ctx, cfg.Name, domains); err != nil {
			return fmt.Errorf("failed to start egress proxy: %w", err)
		}
	}

	// Connect shared egress proxy if running (legacy / passthrough)
	if p.containerExists(ctx, egressProxyContainer) {
		p.connectNetwork(ctx, netName, egressProxyContainer)
	}

	// 8. Start container
	cName := containerName(cfg.Name)
	if p.containerExists(ctx, cName) {
		p.removeContainer(ctx, cName)
	}

	// Ensure all files in the data directory are owned by the container user (node, uid 1000).
	// SFTP uploads create files as root — this must run after all uploads and before starting the container.
	if _, err := p.ssh.Run(ctx, fmt.Sprintf("chown -R 1000:1000 %s", shellQuote(dataDir))); err != nil {
		return fmt.Errorf("failed to chown data directory: %w", err)
	}

	fmt.Printf("Starting container %s...\n", cName)
	if err := p.runAgentContainer(ctx, agentContainerOpts{
		Name:            cName,
		AgentName:       cfg.Name,
		Network:         netName,
		EnvFile:         envPath,
		DataDir:         dataDir,
		GatewayPort:     cfg.GatewayPort,
		Image:           image,
		EgressEnforce:   egressEnforce,
		EgressProxyName: policy.EgressProxyName(cfg.Name),
	}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// 9. Update routing.json
	if err := p.regenerateRouting(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update routing: %v\n", err)
	}

	// 10. Restart router if any channel has credentials so it picks up updated routing.json
	if hasAnyChannel(shared) {
		p.restartRouter(ctx)
	}

	// 11. Save config hash baseline
	p.saveConfigBaseline(cfg.Name)

	return nil
}

func (p *RemoteProvider) RemoveAgent(ctx context.Context, name string, deleteSecrets bool) error {
	cName := containerName(name)
	netName := networkName(name)

	if p.containerExists(ctx, cName) {
		p.stopContainer(ctx, cName)
		p.removeContainer(ctx, cName)
	}

	// Stop per-agent egress proxy
	p.stopAgentEgressProxy(ctx, name)

	if p.containerExists(ctx, routerContainer) {
		p.disconnectNetwork(ctx, netName, routerContainer)
	}
	if p.containerExists(ctx, egressProxyContainer) {
		p.disconnectNetwork(ctx, netName, egressProxyContainer)
	}

	if p.networkExists(ctx, netName) {
		p.removeNetwork(ctx, netName)
	}

	// Remove remote config files
	p.ssh.Run(ctx, fmt.Sprintf("rm -f %s %s %s %s",
		shellQuote(posixpath.Join(p.remoteAgentsDir(), name+".json")),
		shellQuote(posixpath.Join(p.remoteConfigDir(), name+".env")),
		shellQuote(posixpath.Join(p.remoteConfigDir(), name+".sha256")),
		shellQuote(posixpath.Join(p.remoteConfigDir(), fmt.Sprintf("egress-%s.conf", name))),
	))

	if deleteSecrets {
		p.ssh.Run(ctx, fmt.Sprintf("rm -rf %s", shellQuote(p.agentSecretsDir(name))))
	}

	if err := p.regenerateRouting(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update routing: %v\n", err)
	}
	// Restart router to pick up removed agent from routing.json
	shared, _ := p.readSharedSecrets()
	if hasAnyChannel(shared) {
		p.restartRouter(ctx)
	}
	return nil
}

// --- Container Operations ---

func (p *RemoteProvider) GetStatus(ctx context.Context, agentName string) (*provider.AgentStatus, error) {
	cName := containerName(agentName)
	status := &provider.AgentStatus{
		AgentName:    agentName,
		ServiceState: "docker",
	}

	if !p.containerExists(ctx, cName) {
		status.Container.State = "not found"
		return status, nil
	}

	state, err := p.inspectState(ctx, cName)
	if err != nil {
		return nil, err
	}
	status.Container.State = state.Status
	status.Container.StartedAt = state.StartedAt

	if state.Running {
		if stats, err := p.containerStats(ctx, cName); err == nil {
			status.Container.CPUPercent = stats.CPUPercent
			status.Container.MemoryUsage = stats.MemoryUsage
		}
		logs, _ := p.containerLogs(ctx, cName, 50)
		status.ReadyPhase = detectReadyPhase(logs)
	} else {
		status.ReadyPhase = "stopped"
	}

	return status, nil
}

func (p *RemoteProvider) GetLogs(ctx context.Context, agentName string, lines int) (string, error) {
	return p.containerLogs(ctx, containerName(agentName), lines)
}

func (p *RemoteProvider) ContainerExec(ctx context.Context, agentName string, command []string) (string, error) {
	args := append([]string{"exec", containerName(agentName)}, command...)
	return p.dockerRun(ctx, args...)
}

func (p *RemoteProvider) PauseAgent(ctx context.Context, name string) error {
	cfg, err := p.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if cfg.Paused {
		fmt.Printf("Agent %s is already paused.\n", name)
		return nil
	}

	cfg.Paused = true
	if err := p.saveAgentConfig(cfg); err != nil {
		return err
	}

	cName := containerName(name)
	if p.containerExists(ctx, cName) {
		p.stopContainer(ctx, cName)
		p.removeContainer(ctx, cName)
	}

	netName := networkName(name)
	if p.containerExists(ctx, routerContainer) {
		p.disconnectNetwork(ctx, netName, routerContainer)
	}

	if err := p.regenerateRouting(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update routing: %v\n", err)
	}

	return nil
}

func (p *RemoteProvider) UnpauseAgent(ctx context.Context, name string) error {
	cfg, err := p.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if !cfg.Paused {
		fmt.Printf("Agent %s is not paused.\n", name)
		return nil
	}

	cfg.Paused = false
	if err := p.saveAgentConfig(cfg); err != nil {
		return err
	}

	if err := p.RefreshAgent(ctx, name); err != nil {
		return err
	}

	if err := p.regenerateRouting(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update routing: %v\n", err)
	}

	return nil
}

func (p *RemoteProvider) saveAgentConfig(cfg *provider.AgentConfig) error {
	p.ssh.MkdirAll(p.remoteAgentsDir(), 0700)
	agentJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return p.ssh.Upload(posixpath.Join(p.remoteAgentsDir(), cfg.Name+".json"), agentJSON, 0600)
}

func (p *RemoteProvider) RefreshAgent(ctx context.Context, agentName string) error {
	cfg, err := p.GetAgent(ctx, agentName)
	if err != nil {
		return err
	}
	if cfg.Paused {
		return fmt.Errorf("agent %s is paused. Use `conga admin unpause %s` first", agentName, agentName)
	}

	// Errors reading secrets are non-fatal — agent starts with whatever secrets are available.
	// Missing secrets surface as runtime errors in the container (check via `conga logs`).
	shared, _ := p.readSharedSecrets()
	perAgent, _ := p.readAgentSecrets(agentName)

	dataDir := p.remoteDataSubDir(agentName)
	existingToken := ""
	if err := p.checkConfigIntegrity(agentName); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		fmt.Fprintf(os.Stderr, "Generating fresh gateway token instead of preserving existing one.\n")
		existingToken, _ = generateToken()
	} else {
		existingToken = p.readExistingGatewayToken(posixpath.Join(dataDir, "openclaw.json"))
	}

	openClawJSON, err := common.GenerateOpenClawConfig(*cfg, shared, existingToken)
	if err != nil {
		return fmt.Errorf("failed to generate config: %w", err)
	}
	p.ssh.MkdirAll(dataDir, 0755)
	if err := p.ssh.Upload(posixpath.Join(dataDir, "openclaw.json"), openClawJSON, 0644); err != nil {
		return err
	}

	p.saveConfigBaseline(agentName)

	envContent := common.GenerateEnvFile(*cfg, shared, perAgent)
	envPath := posixpath.Join(p.remoteConfigDir(), agentName+".env")
	p.ssh.Run(ctx, fmt.Sprintf("rm -f %s", shellQuote(envPath)))
	if err := p.ssh.Upload(envPath, envContent, 0400); err != nil {
		return err
	}

	// Load egress policy (from local ~/.conga/, not the remote host)
	egressPolicy, policyErr := policy.LoadEgressPolicy(p.dataDir, agentName)
	if policyErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load egress policy: %v\n", policyErr)
	}
	egressEnforce := false
	if egressPolicy != nil && len(egressPolicy.AllowedDomains) > 0 {
		egressEnforce = true
	}

	cName := containerName(agentName)
	if p.containerExists(ctx, cName) {
		p.stopContainer(ctx, cName)
		p.removeContainer(ctx, cName)
	}

	// Stop old egress proxy
	p.stopAgentEgressProxy(ctx, agentName)

	image := p.getConfigValue("image")
	if image == "" {
		image = "ghcr.io/openclaw/openclaw:latest"
	}

	netName := networkName(agentName)
	if !p.networkExists(ctx, netName) {
		p.createNetwork(ctx, netName)
	}

	// Start per-agent egress proxy (when policy defines domains)
	if egressEnforce {
		domains := policy.EffectiveAllowedDomains(egressPolicy)
		if err := p.startAgentEgressProxy(ctx, agentName, domains); err != nil {
			return fmt.Errorf("failed to start egress proxy: %w", err)
		}
	}

	// Ensure all files are owned by the container user before starting.
	if _, err := p.ssh.Run(ctx, fmt.Sprintf("chown -R 1000:1000 %s", shellQuote(dataDir))); err != nil {
		return fmt.Errorf("failed to chown data directory: %w", err)
	}

	if err := p.runAgentContainer(ctx, agentContainerOpts{
		Name:            cName,
		AgentName:       agentName,
		Network:         netName,
		EnvFile:         envPath,
		DataDir:         dataDir,
		GatewayPort:     cfg.GatewayPort,
		Image:           image,
		EgressEnforce:   egressEnforce,
		EgressProxyName: policy.EgressProxyName(agentName),
	}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}

	if p.containerExists(ctx, egressProxyContainer) {
		p.connectNetwork(ctx, netName, egressProxyContainer)
	}
	if p.containerExists(ctx, routerContainer) {
		p.connectNetwork(ctx, netName, routerContainer)
	}
	return nil
}

func (p *RemoteProvider) RefreshAll(ctx context.Context) error {
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return err
	}

	// Regenerate routing.json before restarting the router
	shared, _ := p.readSharedSecrets()
	if hasAnyChannel(shared) {
		if err := p.regenerateRouting(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to regenerate routing: %v\n", err)
		}
		p.restartRouter(ctx)
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

func (p *RemoteProvider) Connect(ctx context.Context, agentName string, localPort int) (*provider.ConnectInfo, error) {
	cfg, err := p.GetAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}

	if localPort == 0 {
		localPort = cfg.GatewayPort
	}

	// Read gateway token from remote config
	token := p.readExistingGatewayToken(posixpath.Join(p.remoteDataSubDir(agentName), "openclaw.json"))

	// Fallback: try docker exec
	if token == "" {
		cName := containerName(agentName)
		if p.containerExists(ctx, cName) {
			output, err := p.dockerRun(ctx, "exec", cName, "node", "-e",
				`try{const c=require('/home/node/.openclaw/openclaw.json');console.log(c.gateway?.token||c.gateway?.auth?.token||'')}catch(e){console.log('')}`)
			if err == nil {
				token = strings.TrimSpace(output)
			}
		}
	}

	// Start SSH tunnel
	tunnel, err := p.ssh.ForwardPort(ctx, localPort, cfg.GatewayPort)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH tunnel: %w", err)
	}

	waiter := make(chan error, 1)
	go func() {
		waiter <- tunnel.Wait()
	}()

	url := fmt.Sprintf("http://localhost:%d", localPort)
	if token != "" {
		url = fmt.Sprintf("http://localhost:%d#token=%s", localPort, token)
	} else {
		fmt.Println("Note: gateway token not found. The web UI may prompt for authentication.")
	}

	return &provider.ConnectInfo{
		URL:       url,
		LocalPort: localPort,
		Token:     token,
		Waiter:    waiter,
	}, nil
}

// --- Environment Management ---

func (p *RemoteProvider) CycleHost(ctx context.Context) error {
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Stopping all containers...")
	for _, a := range agents {
		if !a.Paused {
			p.stopContainer(ctx, containerName(a.Name))
		}
	}
	p.stopContainer(ctx, routerContainer)
	p.stopContainer(ctx, egressProxyContainer)

	fmt.Println("Restarting...")

	p.ensureEgressProxy(ctx)
	p.ensureRouter(ctx)

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

func (p *RemoteProvider) Teardown(ctx context.Context) error {
	agents, _ := p.ListAgents(ctx)
	for _, a := range agents {
		fmt.Printf("Removing agent %s...\n", a.Name)
		p.removeAgentDocker(ctx, a.Name)
	}

	p.cleanupDockerByPrefix(ctx)

	// Remove all remote state
	fmt.Printf("Removing %s...\n", p.remoteDir)
	p.ssh.Run(ctx, fmt.Sprintf("rm -rf %s", shellQuote(p.remoteDir)))

	// Clear local remote config
	os.Remove(filepath.Join(p.dataDir, "remote-config.json"))

	fmt.Println("Remote deployment torn down.")
	return nil
}

func (p *RemoteProvider) removeAgentDocker(ctx context.Context, name string) {
	cName := containerName(name)
	netName := networkName(name)
	if p.containerExists(ctx, cName) {
		p.stopContainer(ctx, cName)
		p.removeContainer(ctx, cName)
	}
	if p.networkExists(ctx, netName) {
		p.removeNetwork(ctx, netName)
	}
}

func (p *RemoteProvider) cleanupDockerByPrefix(ctx context.Context) {
	output, err := p.dockerRun(ctx, "ps", "-a", "--filter", "name=conga-", "--format", "{{.Names}}")
	if err == nil {
		for _, name := range strings.Split(strings.TrimSpace(output), "\n") {
			if name == "" {
				continue
			}
			fmt.Printf("Removing container %s...\n", name)
			p.stopContainer(ctx, name)
			p.removeContainer(ctx, name)
		}
	}

	output, err = p.dockerRun(ctx, "network", "ls", "--filter", "name=conga-", "--format", "{{.Name}}")
	if err == nil {
		for _, name := range strings.Split(strings.TrimSpace(output), "\n") {
			if name == "" {
				continue
			}
			fmt.Printf("Removing network %s...\n", name)
			p.removeNetwork(ctx, name)
		}
	}

	if p.networkExists(ctx, egressNetwork) {
		fmt.Printf("Removing network %s...\n", egressNetwork)
		p.removeNetwork(ctx, egressNetwork)
	}
}

// --- infrastructure helpers ---

// restartRouter removes and recreates the router container so it picks up
// the latest routing.json (which is a read-only bind mount).
func (p *RemoteProvider) restartRouter(ctx context.Context) {
	if p.containerExists(ctx, routerContainer) {
		p.removeContainer(ctx, routerContainer)
	}
	p.ensureRouter(ctx)
}

func (p *RemoteProvider) ensureRouter(ctx context.Context) {
	if p.containerExists(ctx, routerContainer) {
		state, err := p.inspectState(ctx, routerContainer)
		if err == nil && state.Running {
			return
		}
		p.removeContainer(ctx, routerContainer)
	}

	routerEnvPath := posixpath.Join(p.remoteConfigDir(), "router.env")
	routingPath := posixpath.Join(p.remoteConfigDir(), "routing.json")

	// Check router source exists
	_, err := p.ssh.Run(ctx, fmt.Sprintf("test -f %s",
		shellQuote(posixpath.Join(p.remoteRouterDir(), "src", "index.js"))))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: router source not found on remote host — router not started\n")
		return
	}
	// Check router env exists
	_, err = p.ssh.Run(ctx, fmt.Sprintf("test -f %s", shellQuote(routerEnvPath)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: router.env not found — router not started\n")
		return
	}

	// Install npm dependencies if node_modules is missing
	nodeModules := posixpath.Join(p.remoteRouterDir(), "node_modules")
	if _, err := p.ssh.Run(ctx, fmt.Sprintf("test -d %s", shellQuote(nodeModules))); err != nil {
		fmt.Println("Installing router dependencies...")
		installCmd := fmt.Sprintf("docker run --rm -v %s:/app -w /app node:22-alpine npm install --production 2>&1",
			shellQuote(p.remoteRouterDir()))
		if out, err := p.ssh.Run(ctx, installCmd); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: npm install failed: %v\n%s\n", err, out)
			return
		}
	}

	fmt.Println("Starting router...")
	if err := p.runRouterContainer(ctx, routerContainerOpts{
		EnvFile:     routerEnvPath,
		RouterDir:   p.remoteRouterDir(),
		RoutingJSON: routingPath,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to start router: %v\n", err)
		return
	}

	agents, _ := p.ListAgents(ctx)
	for _, a := range agents {
		p.connectNetwork(ctx, networkName(a.Name), routerContainer)
	}

	fmt.Println("  Router started.")
}

func (p *RemoteProvider) ensureEgressProxy(ctx context.Context) {
	if p.containerExists(ctx, egressProxyContainer) {
		state, err := p.inspectState(ctx, egressProxyContainer)
		if err == nil && state.Running {
			return
		}
		p.removeContainer(ctx, egressProxyContainer)
	}

	if !p.imageExists(ctx, egressProxyImage) {
		// Check if Dockerfile exists on remote
		_, err := p.ssh.Run(ctx, fmt.Sprintf("test -f %s",
			shellQuote(posixpath.Join(p.remoteEgressProxyDir(), "Dockerfile"))))
		if err == nil {
			p.buildImage(ctx, p.remoteEgressProxyDir(), egressProxyImage)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: egress proxy image not found and Dockerfile not available — proxy not started\n")
			return
		}
	}

	if !p.networkExists(ctx, egressNetwork) {
		p.dockerRun(ctx, "network", "create", egressNetwork, "--driver", "bridge")
	}

	fmt.Println("Starting egress proxy...")
	_, err := p.dockerRun(ctx, "run", "-d",
		"--name", egressProxyContainer,
		"--network", egressNetwork,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--memory", "64m",
		"--read-only",
		egressProxyImage,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to start egress proxy: %v\n", err)
		return
	}

	agents, _ := p.ListAgents(ctx)
	for _, a := range agents {
		p.connectNetwork(ctx, networkName(a.Name), egressProxyContainer)
	}

	fmt.Println("  Egress proxy started.")
}

// startAgentEgressProxy starts a per-agent tinyproxy for egress domain filtering on the remote host.
func (p *RemoteProvider) startAgentEgressProxy(ctx context.Context, agentName string, domains []string) error {
	proxyName := policy.EgressProxyName(agentName)
	netName := networkName(agentName)

	// Stop existing proxy if any
	p.ssh.Run(ctx, fmt.Sprintf("docker rm -f %s 2>/dev/null || true", shellQuote(proxyName)))

	// Build proxy image if not present on remote
	exists, _ := p.ssh.Run(ctx, fmt.Sprintf("docker image inspect %s >/dev/null 2>&1 && echo yes || echo no", policy.EgressProxyImage))
	if strings.TrimSpace(exists) != "yes" {
		fmt.Printf("  Building egress proxy image on remote...\n")
		buildCmd := fmt.Sprintf("mkdir -p /tmp/conga-egress-build && echo '%s' > /tmp/conga-egress-build/Dockerfile && docker build -t %s /tmp/conga-egress-build && rm -rf /tmp/conga-egress-build",
			policy.EgressProxyDockerfile(), policy.EgressProxyImage)
		if _, err := p.ssh.Run(ctx, buildCmd); err != nil {
			return fmt.Errorf("building egress proxy image: %w", err)
		}
	}

	// Generate and upload tinyproxy config
	conf := policy.GenerateProxyConf(domains)
	confPath := posixpath.Join(p.remoteConfigDir(), fmt.Sprintf("egress-%s.conf", agentName))
	if err := p.ssh.Upload(confPath, []byte(conf), 0444); err != nil {
		return fmt.Errorf("uploading egress config: %w", err)
	}

	// Upload filter file (domain allowlist)
	filterPath := posixpath.Join(p.remoteConfigDir(), fmt.Sprintf("egress-%s.filter", agentName))
	if len(domains) > 0 {
		filter := policy.GenerateProxyFilter(domains)
		if err := p.ssh.Upload(filterPath, []byte(filter), 0444); err != nil {
			return fmt.Errorf("uploading egress filter: %w", err)
		}
	}

	// Ensure network exists
	if !p.networkExists(ctx, netName) {
		if err := p.createNetwork(ctx, netName); err != nil {
			return fmt.Errorf("creating network: %w", err)
		}
	}

	// Start tinyproxy on agent's network.
	// Runs as nobody (uid 65534) — tinyproxy drops privileges internally.
	// tmpfs for /run provides writable PID file location.
	cmd := fmt.Sprintf("docker run -d --name %s --network %s "+
		"--cap-drop ALL --security-opt no-new-privileges --memory 64m --read-only "+
		"--tmpfs /run:rw,noexec,nosuid --tmpfs /var/log/tinyproxy:rw,noexec,nosuid "+
		"-v %s:/etc/tinyproxy/tinyproxy.conf:ro ",
		shellQuote(proxyName), shellQuote(netName), shellQuote(confPath))
	if len(domains) > 0 {
		cmd += fmt.Sprintf("-v %s:/etc/tinyproxy/filter:ro ", shellQuote(filterPath))
	}
	cmd += fmt.Sprintf("%s tinyproxy -d -c /etc/tinyproxy/tinyproxy.conf", policy.EgressProxyImage)

	if _, err := p.ssh.Run(ctx, cmd); err != nil {
		return fmt.Errorf("starting egress proxy: %w", err)
	}

	fmt.Printf("  Egress proxy started for %s (%d domains allowed)\n", agentName, len(domains))
	return nil
}

// stopAgentEgressProxy removes the per-agent egress proxy container on the remote host.
func (p *RemoteProvider) stopAgentEgressProxy(ctx context.Context, agentName string) {
	proxyName := policy.EgressProxyName(agentName)
	p.ssh.Run(ctx, fmt.Sprintf("docker rm -f %s 2>/dev/null || true", shellQuote(proxyName)))
}

// --- file helpers ---

func (p *RemoteProvider) regenerateRouting(ctx context.Context) error {
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return err
	}
	data, err := common.GenerateRoutingJSON(agents)
	if err != nil {
		return err
	}
	p.ssh.MkdirAll(p.remoteConfigDir(), 0700)
	return p.ssh.Upload(posixpath.Join(p.remoteConfigDir(), "routing.json"), data, 0644)
}

func (p *RemoteProvider) deployBehavior(cfg provider.AgentConfig) error {
	// Read behavior files from local repo (stored in remote-config.json repo_path)
	repoPath := p.getConfigValue("repo_path")
	if repoPath == "" {
		return nil
	}
	behaviorDir := filepath.Join(repoPath, "behavior")
	if _, err := os.Stat(behaviorDir); os.IsNotExist(err) {
		return nil
	}

	files, err := common.ComposeBehaviorFiles(behaviorDir, cfg)
	if err != nil {
		return err
	}

	targetDir := posixpath.Join(p.remoteDataSubDir(cfg.Name), "data", "workspace")
	p.ssh.MkdirAll(targetDir, 0755)

	for name, content := range files {
		if err := p.ssh.Upload(posixpath.Join(targetDir, name), content, 0644); err != nil {
			return err
		}
	}
	return nil
}

func (p *RemoteProvider) getConfigValue(key string) string {
	extraPath := filepath.Join(p.dataDir, "remote-config.json")
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

func (p *RemoteProvider) setConfigValue(key, value string) error {
	if err := os.MkdirAll(p.dataDir, 0700); err != nil {
		return err
	}
	extraPath := filepath.Join(p.dataDir, "remote-config.json")
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

func (p *RemoteProvider) readExistingGatewayToken(remotePath string) string {
	data, err := p.ssh.Download(remotePath)
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

// hasAnyChannel returns true if any registered channel has its required credentials.
func hasAnyChannel(shared common.SharedSecrets) bool {
	for _, ch := range channels.All() {
		if ch.HasCredentials(shared.Values) {
			return true
		}
	}
	return false
}
