package localprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
)

const (
	egressProxyContainer = "conga-egress-proxy"
	egressProxyImage     = "conga-egress-proxy"
	egressNetwork        = "conga-egress"
	routerContainer      = "conga-router"
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
func (p *LocalProvider) configDir() string              { return filepath.Join(p.dataDir, "config") }
func (p *LocalProvider) dataSubDir(name string) string  { return filepath.Join(p.dataDir, "data", name) }
func (p *LocalProvider) routerDir() string              { return filepath.Join(p.dataDir, "router") }
func (p *LocalProvider) behaviorDir() string            { return filepath.Join(p.dataDir, "behavior") }
func (p *LocalProvider) logsDir() string                { return filepath.Join(p.dataDir, "logs") }
func (p *LocalProvider) egressProxyDir() string         { return filepath.Join(p.dataDir, "egress-proxy") }

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
	if err := os.MkdirAll(p.agentsDir(), 0700); err != nil {
		return err
	}
	agentJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(p.agentsDir(), cfg.Name+".json"), agentJSON, 0600); err != nil {
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

	// 5. Create Docker network (internal — no external access)
	netName := networkName(cfg.Name)
	if !networkExists(ctx, netName) {
		fmt.Printf("Creating network %s...\n", netName)
		if err := createNetwork(ctx, netName); err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}
	}

	// 6. Connect egress proxy to agent network (for outbound HTTPS/DNS)
	if containerExists(ctx, egressProxyContainer) {
		connectNetwork(ctx, netName, egressProxyContainer)
	}

	// 7. Start container
	cName := containerName(cfg.Name)
	if containerExists(ctx, cName) {
		removeContainer(ctx, cName)
	}

	fmt.Printf("Starting container %s...\n", cName)
	if err := runAgentContainer(ctx, agentContainerOpts{
		Name:        cName,
		AgentName:   cfg.Name,
		Network:     netName,
		EnvFile:     envPath,
		DataDir:     dataDir,
		GatewayPort: cfg.GatewayPort,
		Image:       image,
	}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// 8. Update routing.json
	if err := p.regenerateRouting(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update routing: %v\n", err)
	}

	// 9. Ensure router is running and connected (only if Slack configured)
	if shared.HasSlack() {
		p.ensureRouter(ctx)
		if containerExists(ctx, routerContainer) {
			connectNetwork(ctx, netName, routerContainer)
		}
	}

	// 10. Save config hash baseline
	p.saveConfigBaseline(cfg.Name)

	return nil
}

func (p *LocalProvider) RemoveAgent(ctx context.Context, name string, deleteSecrets bool) error {
	cName := containerName(name)
	netName := networkName(name)

	if containerExists(ctx, cName) {
		stopContainer(ctx, cName)
		removeContainer(ctx, cName)
	}

	// Disconnect router and egress proxy from network before removing it
	if containerExists(ctx, routerContainer) {
		disconnectNetwork(ctx, netName, routerContainer)
	}
	if containerExists(ctx, egressProxyContainer) {
		disconnectNetwork(ctx, netName, egressProxyContainer)
	}

	if networkExists(ctx, netName) {
		removeNetwork(ctx, netName)
	}

	os.Remove(filepath.Join(p.agentsDir(), name+".json"))
	os.Remove(filepath.Join(p.configDir(), name+".env"))
	os.Remove(filepath.Join(p.configDir(), name+".sha256"))

	if deleteSecrets {
		os.RemoveAll(p.agentSecretsDir(name))
	}

	p.regenerateRouting(ctx)
	return nil
}

// --- Container Operations ---

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
	} else {
		status.ReadyPhase = "stopped"
	}

	return status, nil
}

func (p *LocalProvider) GetLogs(ctx context.Context, agentName string, lines int) (string, error) {
	return containerLogs(ctx, containerName(agentName), lines)
}

func (p *LocalProvider) RefreshAgent(ctx context.Context, agentName string) error {
	cfg, err := p.GetAgent(ctx, agentName)
	if err != nil {
		return err
	}

	shared, _ := p.readSharedSecrets()
	perAgent, _ := p.readAgentSecrets(agentName)

	// Preserve existing gateway auth token before regenerating config
	dataDir := p.dataSubDir(agentName)
	existingToken := readExistingGatewayToken(filepath.Join(dataDir, "openclaw.json"))

	// Regenerate openclaw.json with current config format
	openClawJSON, err := common.GenerateOpenClawConfig(*cfg, shared, existingToken)
	if err != nil {
		return fmt.Errorf("failed to generate config: %w", err)
	}
	os.MkdirAll(dataDir, 0755)
	if err := os.WriteFile(filepath.Join(dataDir, "openclaw.json"), openClawJSON, 0644); err != nil {
		return err
	}

	// Regenerate env file
	envContent := common.GenerateEnvFile(*cfg, shared, perAgent)
	envPath := filepath.Join(p.configDir(), agentName+".env")
	os.Remove(envPath) // remove old 0400 file before rewriting
	if err := os.WriteFile(envPath, envContent, 0400); err != nil {
		return err
	}

	cName := containerName(agentName)
	if containerExists(ctx, cName) {
		stopContainer(ctx, cName)
		removeContainer(ctx, cName)
	}

	image := p.getConfigValue("image")
	if image == "" {
		image = "ghcr.io/openclaw/openclaw:latest"
	}

	netName := networkName(agentName)
	if !networkExists(ctx, netName) {
		createNetwork(ctx, netName)
	}

	if err := runAgentContainer(ctx, agentContainerOpts{
		Name:        cName,
		AgentName:   agentName,
		Network:     netName,
		EnvFile:     envPath,
		DataDir:     p.dataSubDir(agentName),
		GatewayPort: cfg.GatewayPort,
		Image:       image,
	}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}

	// Reconnect egress proxy and router
	if containerExists(ctx, egressProxyContainer) {
		connectNetwork(ctx, netName, egressProxyContainer)
	}
	if containerExists(ctx, routerContainer) {
		connectNetwork(ctx, netName, routerContainer)
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

func (p *LocalProvider) Setup(ctx context.Context) error {
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
	repoStatus := "set"
	if repoPath == "" {
		repoStatus = "not set"
		// Try to auto-detect from git
		repoPath = detectRepoRoot()
	}
	fmt.Printf("\n[config] repo_path — Conga Line repo root for router/behavior files (%s)\n", repoStatus)
	newRepoPath, err := ui.TextPromptWithDefault("  Repo path", repoPath)
	if err != nil {
		return err
	}
	if newRepoPath != "" {
		// Validate the path has router/ and behavior/ directories
		if _, err := os.Stat(filepath.Join(newRepoPath, "router", "src", "index.js")); err != nil {
			return fmt.Errorf("invalid repo path: %s/router/src/index.js not found", newRepoPath)
		}
		p.setConfigValue("repo_path", newRepoPath)
		repoPath = newRepoPath
		changed++
	}

	// --- Docker image ---
	image := p.getConfigValue("image")
	imageStatus := "set"
	if image == "" {
		imageStatus = "not set"
	}
	fmt.Printf("\n[config] image — OpenClaw Docker image (%s)\n", imageStatus)
	if image == "" || ui.Confirm("  Update this value?") {
		defaultImage := "ghcr.io/openclaw/openclaw:2026.3.11"
		if image != "" {
			defaultImage = image
		}
		newImage, err := ui.TextPromptWithDefault("  Docker image", defaultImage)
		if err != nil {
			return err
		}
		if newImage != "" {
			p.setConfigValue("image", newImage)
			image = newImage
			changed++
		}
	}

	// --- Shared secrets (all optional — Slack not required for gateway-only mode) ---
	secretItems := []struct {
		name, description string
		isSecret           bool
		group              string // "slack" or "google"
	}{
		{"slack-bot-token", "Slack bot token (xoxb-...)", true, "slack"},
		{"slack-signing-secret", "Slack signing secret", true, "slack"},
		{"slack-app-token", "Slack app token (xapp-...)", true, "slack"},
		{"google-client-id", "Google OAuth client ID", false, "google"},
		{"google-client-secret", "Google OAuth client secret", true, "google"},
	}

	fmt.Println("\nSlack integration is optional. Skip all Slack tokens to run in gateway-only mode (web UI).")

	for _, item := range secretItems {
		path := filepath.Join(p.sharedSecretsDir(), item.name)
		current, _ := readSecret(path)

		status := "set"
		if current == "" {
			status = "not set"
		}
		optLabel := " (optional)"
		fmt.Printf("\n[secret] %s — %s%s (%s)\n", item.name, item.description, optLabel, status)

		if current != "" {
			if !ui.Confirm("  Update this value?") {
				continue
			}
		}

		var value string
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

		if err := writeSecret(path, value); err != nil {
			return fmt.Errorf("failed to save %s: %w", item.name, err)
		}
		fmt.Println("  Saved.")
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

	// --- Start egress proxy ---
	p.ensureEgressProxy(ctx)

	// --- Router (only if Slack is configured) ---
	shared, _ := p.readSharedSecrets()
	if shared.HasSlack() {
		routerEnvPath := filepath.Join(p.configDir(), "router.env")
		routerEnv := fmt.Sprintf("SLACK_APP_TOKEN=%s\nSLACK_SIGNING_SECRET=%s\n", shared.SlackAppToken, shared.SlackSigningSecret)
		if err := os.WriteFile(routerEnvPath, []byte(routerEnv), 0400); err != nil {
			return fmt.Errorf("failed to write router env file: %w", err)
		}
		p.ensureRouter(ctx)
	} else {
		fmt.Println("\nSlack not configured — router skipped. Agents will run in gateway-only mode (web UI).")
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
	fmt.Println("  conga admin add-user <name> <slack_member_id>")
	fmt.Println("  conga admin add-team <name> <slack_channel_id>")
	return nil
}

func (p *LocalProvider) CycleHost(ctx context.Context) error {
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Stopping all containers...")
	for _, a := range agents {
		stopContainer(ctx, containerName(a.Name))
	}
	stopContainer(ctx, routerContainer)
	stopContainer(ctx, egressProxyContainer)

	fmt.Println("Restarting...")
	time.Sleep(2 * time.Second)

	// Restart infrastructure containers
	p.ensureEgressProxy(ctx)
	p.ensureRouter(ctx)

	// Restart agents
	for _, a := range agents {
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

// removeAgentDocker removes a single agent's container and network.
func (p *LocalProvider) removeAgentDocker(ctx context.Context, name string) {
	cName := containerName(name)
	netName := networkName(name)
	if containerExists(ctx, cName) {
		stopContainer(ctx, cName)
		removeContainer(ctx, cName)
	}
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

	// Also clean the egress network
	if networkExists(ctx, egressNetwork) {
		fmt.Printf("Removing network %s...\n", egressNetwork)
		removeNetwork(ctx, egressNetwork)
	}
}

// --- infrastructure helpers ---

// ensureRouter starts the router container if it's not already running.
func (p *LocalProvider) ensureRouter(ctx context.Context) {
	if containerExists(ctx, routerContainer) {
		state, err := inspectState(ctx, routerContainer)
		if err == nil && state.Running {
			return // already running
		}
		// Exists but not running — remove and recreate
		removeContainer(ctx, routerContainer)
	}

	routerEnvPath := filepath.Join(p.configDir(), "router.env")
	routingPath := filepath.Join(p.configDir(), "routing.json")

	// Check required files exist
	if _, err := os.Stat(filepath.Join(p.routerDir(), "src", "index.js")); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: router source not found at %s — router not started\n", p.routerDir())
		return
	}
	if _, err := os.Stat(routerEnvPath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: router.env not found — router not started\n")
		return
	}

	fmt.Println("Starting router...")
	if err := runRouterContainer(ctx, routerContainerOpts{
		EnvFile:     routerEnvPath,
		RouterDir:   p.routerDir(),
		RoutingJSON: routingPath,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to start router: %v\n", err)
		return
	}

	// Connect router to all existing agent networks
	agents, _ := p.ListAgents(ctx)
	for _, a := range agents {
		connectNetwork(ctx, networkName(a.Name), routerContainer)
	}

	fmt.Println("  Router started.")
}

// ensureEgressProxy starts the egress proxy if not already running.
func (p *LocalProvider) ensureEgressProxy(ctx context.Context) {
	if containerExists(ctx, egressProxyContainer) {
		state, err := inspectState(ctx, egressProxyContainer)
		if err == nil && state.Running {
			return
		}
		removeContainer(ctx, egressProxyContainer)
	}

	// Check if the image exists
	if !imageExists(ctx, egressProxyImage) {
		// Try to build it
		if _, err := os.Stat(filepath.Join(p.egressProxyDir(), "Dockerfile")); err == nil {
			buildImage(ctx, p.egressProxyDir(), egressProxyImage)
		} else {
			fmt.Fprintf(os.Stderr, "Warning: egress proxy image not found and Dockerfile not available — proxy not started\n")
			return
		}
	}

	// Create egress network (non-internal — has external access)
	if !networkExists(ctx, egressNetwork) {
		dockerRun(ctx, "network", "create", egressNetwork, "--driver", "bridge")
	}

	fmt.Println("Starting egress proxy...")
	_, err := dockerRun(ctx, "run", "-d",
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

	// Connect proxy to all existing agent networks
	agents, _ := p.ListAgents(ctx)
	for _, a := range agents {
		connectNetwork(ctx, networkName(a.Name), egressProxyContainer)
	}

	fmt.Println("  Egress proxy started.")
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

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
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
