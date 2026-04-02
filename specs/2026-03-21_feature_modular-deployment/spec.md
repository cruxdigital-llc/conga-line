# Technical Specification: Modular Deployment

## 1. Overview

This spec details the refactoring of Conga Line's CLI and deployment layer into a modular provider system, and the implementation of the first non-AWS provider: **local Docker**. The spec is organized by new package, with exact type definitions, function signatures, and behavioral contracts.

---

## 2. Package: `cli/pkg/provider` — Interface & Shared Types

### 2.1 Core Types

```go
// provider.go
package provider

import (
    "context"
    "time"
)

// AgentType distinguishes user (DM-only) from team (channel-based) agents.
type AgentType string

const (
    AgentTypeUser AgentType = "user"
    AgentTypeTeam AgentType = "team"
)

// AgentConfig is the provider-agnostic representation of an agent.
// On AWS this is stored in SSM at /conga/agents/{name}.
// On local this is stored in ~/.conga/agents/{name}.json.
type AgentConfig struct {
    Name          string    `json:"name"`
    Type          AgentType `json:"type"`
    SlackMemberID string    `json:"slack_member_id,omitempty"` // user agents only
    SlackChannel  string    `json:"slack_channel,omitempty"`   // team agents only
    GatewayPort   int       `json:"gateway_port"`
    IAMIdentity   string    `json:"iam_identity,omitempty"`    // AWS SSO identity (AWS only)
}

// AgentStatus is returned by GetStatus.
type AgentStatus struct {
    AgentName    string
    ServiceState string // "running", "stopped", "not-found"
    Container    ContainerStatus
    ReadyPhase   string // "starting", "gateway up", "slack loading", "ready"
    Errors       []string
}

type ContainerStatus struct {
    State        string    // "running", "exited", "created"
    Uptime       time.Duration
    RestartCount int
    MemoryUsage  string // e.g. "512MiB / 2GiB"
    CPUPercent   string // e.g. "1.23%"
    PIDs         int
}

// SecretEntry represents a stored secret.
type SecretEntry struct {
    Name        string    // kebab-case name, e.g. "anthropic-api-key"
    EnvVar      string    // SCREAMING_SNAKE_CASE, e.g. "ANTHROPIC_API_KEY"
    Path        string    // full storage path (provider-specific)
    LastChanged time.Time
}

// Identity represents the resolved caller identity.
type Identity struct {
    Name      string // username or IAM session name
    AccountID string // AWS account ID (empty for local)
    ARN       string // AWS ARN (empty for local)
    AgentName string // mapped agent name (empty if unmapped)
}

// SetupManifest defines what the setup wizard prompts for.
// On AWS, this is read from SSM at /conga/config/setup-manifest.
// On local, it's built into the provider.
type SetupManifest struct {
    Config  []SetupItem `json:"config"`
    Secrets []SetupItem `json:"secrets"`
}

type SetupItem struct {
    Key         string `json:"key"`
    Description string `json:"description"`
    Default     string `json:"default,omitempty"`
    Required    bool   `json:"required"`
}

// ConnectInfo is returned by Connect for display to the user.
type ConnectInfo struct {
    URL       string // e.g. "http://localhost:18789#token=abc"
    LocalPort int
    Token     string
}
```

### 2.2 Provider Interface

```go
// provider.go (continued)

// Provider is the core abstraction. Each deployment target implements this interface.
// All methods must be safe for concurrent use.
type Provider interface {
    // Name returns the provider identifier ("aws", "local").
    Name() string

    // --- Identity & Discovery ---

    // WhoAmI returns the current caller's identity.
    WhoAmI(ctx context.Context) (*Identity, error)

    // ListAgents returns all configured agents.
    ListAgents(ctx context.Context) ([]AgentConfig, error)

    // GetAgent returns a single agent by name, or error if not found.
    GetAgent(ctx context.Context, name string) (*AgentConfig, error)

    // ResolveAgentByIdentity finds the agent mapped to the current caller.
    // Returns nil, nil if no mapping exists (not an error).
    ResolveAgentByIdentity(ctx context.Context) (*AgentConfig, error)

    // --- Agent Lifecycle ---

    // ProvisionAgent creates a new agent: stores config, creates container
    // infrastructure, generates openclaw.json, env file, behavior files,
    // starts the container, and updates routing.
    ProvisionAgent(ctx context.Context, cfg AgentConfig) error

    // RemoveAgent stops the container, removes network, cleans config.
    // If deleteSecrets is true, also removes all agent secrets.
    RemoveAgent(ctx context.Context, name string, deleteSecrets bool) error

    // --- Container Operations ---

    // GetStatus returns the current container status and health.
    GetStatus(ctx context.Context, agentName string) (*AgentStatus, error)

    // GetLogs returns the last N lines of container logs.
    GetLogs(ctx context.Context, agentName string, lines int) (string, error)

    // RefreshAgent restarts the agent container with fresh secrets/config.
    RefreshAgent(ctx context.Context, agentName string) error

    // RefreshAll restarts all agent containers.
    RefreshAll(ctx context.Context) error

    // --- Secrets ---

    // SetSecret creates or updates a secret for the given agent.
    SetSecret(ctx context.Context, agentName, secretName, value string) error

    // ListSecrets returns all secrets for the given agent.
    ListSecrets(ctx context.Context, agentName string) ([]SecretEntry, error)

    // DeleteSecret removes a secret.
    DeleteSecret(ctx context.Context, agentName, secretName string) error

    // --- Connectivity ---

    // Connect establishes a connection to the agent's web UI.
    // On AWS this creates an SSM tunnel; on local it's a no-op (port already bound).
    // Returns connection info for display. Blocks until interrupted.
    Connect(ctx context.Context, agentName string, localPort int) (*ConnectInfo, error)

    // --- Environment Management ---

    // Setup runs the initial environment setup wizard.
    Setup(ctx context.Context) error

    // CycleHost restarts the entire deployment environment.
    // On AWS: stop/start EC2 instance. On local: restart all containers.
    CycleHost(ctx context.Context) error
}
```

### 2.3 Provider Registry

```go
// registry.go
package provider

import "fmt"

// Factory creates a provider instance. Receives the resolved config.
type Factory func(cfg *Config) (Provider, error)

var registry = map[string]Factory{}

// Register adds a provider factory to the registry.
func Register(name string, factory Factory) {
    registry[name] = factory
}

// Get returns a provider instance by name.
func Get(name string, cfg *Config) (Provider, error) {
    factory, ok := registry[name]
    if !ok {
        return nil, fmt.Errorf("unknown provider %q (available: %v)", name, Names())
    }
    return factory(cfg)
}

// Names returns all registered provider names.
func Names() []string { /* ... */ }
```

### 2.4 Provider Config

```go
// config.go
package provider

// Config holds provider-agnostic configuration.
// Loaded from ~/.conga/config.json (local) or flags + AWS env (aws).
type Config struct {
    Provider string `json:"provider"`           // "aws" or "local"
    DataDir  string `json:"data_dir,omitempty"` // override for ~/.conga/
    Region   string `json:"region,omitempty"`   // AWS region (aws only)
    Profile  string `json:"profile,omitempty"`  // AWS profile (aws only)
}

// DefaultConfigPath returns ~/.conga/config.json (or XDG equivalent).
func DefaultConfigPath() string { /* ... */ }

// LoadConfig reads config from disk; returns defaults if file doesn't exist.
func LoadConfig(path string) (*Config, error) { /* ... */ }

// SaveConfig writes config to disk.
func SaveConfig(path string, cfg *Config) error { /* ... */ }
```

---

## 3. Package: `cli/pkg/common` — Shared Logic

### 3.1 Config Generation

```go
// config.go
package common

// SharedSecrets holds the secrets needed to generate openclaw.json and env files.
type SharedSecrets struct {
    SlackBotToken     string
    SlackSigningSecret string
    SlackAppToken     string // router only
    GoogleClientID    string
    GoogleClientSecret string
}

// GenerateOpenClawConfig produces the openclaw.json content for an agent.
// This extracts the heredoc logic from user-data.sh.tftpl into a Go template.
//
// Key config decisions (matching AWS bootstrap):
//   - model: claude-opus-4-6
//   - context_pruning: cache-ttl (5 minute TTL)
//   - compaction: safeguard
//   - heartbeat: 55 minutes
//   - session_scope: per-channel-peer
//   - mode: http (webhook from router)
//   - User agents: dmPolicy=allowlist, member in members list
//   - Team agents: groupPolicy=allowlist, channel in channels list
//   - signingSecret and botToken in config (env var override doesn't work)
func GenerateOpenClawConfig(agent AgentConfig, secrets SharedSecrets) ([]byte, error)

// GenerateEnvFile produces the .env file content for an agent.
// Includes shared secrets + per-agent secrets as SCREAMING_SNAKE_CASE env vars.
// Content format: KEY=VALUE\n (one per line, no quoting).
func GenerateEnvFile(agent AgentConfig, shared SharedSecrets, perAgent map[string]string) ([]byte, error)
```

### 3.2 Routing Generation

```go
// routing.go
package common

// RoutingConfig is the JSON structure for routing.json.
type RoutingConfig struct {
    Channels map[string]string `json:"channels"` // channel_id -> container_url
    Members  map[string]string `json:"members"`  // member_id -> container_url
}

// GenerateRoutingJSON builds routing.json from a list of agents.
// Container URLs are always http://conga-{agent_name}:3000/api/v1/webhook
func GenerateRoutingJSON(agents []AgentConfig) ([]byte, error)
```

### 3.3 Behavior Composition

```go
// behavior.go
package common

// BehaviorFiles maps filename -> content for an agent's behavior directory.
type BehaviorFiles map[string][]byte // e.g. "SOUL.md" -> content

// ComposeBehaviorFiles assembles behavior files for an agent.
// Priority: overrides/{agent_name}/ > base/ > {agent_type}/
//
// For SOUL.md and AGENTS.md: override > base, concatenate with type-specific if exists.
// For USER.md: override > render type template with agent name and Slack ID.
//
// behaviorDir is the root of the behavior/ tree (repo path or ~/.conga/behavior/).
func ComposeBehaviorFiles(behaviorDir string, agent AgentConfig) (BehaviorFiles, error)
```

### 3.4 Port Allocation

```go
// ports.go
package common

const BaseGatewayPort = 18789

// NextAvailablePort returns the next unused gateway port.
// Scans existing agents for the highest port and returns max+1.
// Starts at BaseGatewayPort if no agents exist.
func NextAvailablePort(agents []AgentConfig) int
```

### 3.5 Secret Name Conversion

```go
// secrets.go
package common

// SecretNameToEnvVar converts kebab-case to SCREAMING_SNAKE_CASE.
// Example: "anthropic-api-key" -> "ANTHROPIC_API_KEY"
func SecretNameToEnvVar(name string) string
```

---

## 4. Package: `cli/pkg/provider/aws` — AWS Provider

### 4.1 Structure

```go
// provider.go
package aws

import (
    "context"
    awsutil "github.com/cruxdigital-llc/conga-line/cli/pkg/aws"
    "github.com/cruxdigital-llc/conga-line/cli/pkg/discovery"
    "github.com/cruxdigital-llc/conga-line/cli/pkg/provider"
    "github.com/cruxdigital-llc/conga-line/cli/pkg/tunnel"
)

// AWSProvider implements provider.Provider using AWS services.
type AWSProvider struct {
    clients     *awsutil.Clients
    instanceTag string
    instanceID  string // cached after first lookup
}

func NewAWSProvider(cfg *provider.Config) (provider.Provider, error) {
    clients, err := awsutil.NewClients(context.Background(), cfg.Region, cfg.Profile)
    if err != nil {
        return nil, fmt.Errorf("aws init: %w", err)
    }
    return &AWSProvider{
        clients:     clients,
        instanceTag: "conga-line-host",
    }, nil
}

func init() {
    provider.Register("aws", NewAWSProvider)
}
```

### 4.2 Method Mapping

Each provider method delegates to existing code:

| Provider Method | Existing Code |
|----------------|---------------|
| `Name()` | returns `"aws"` |
| `WhoAmI()` | `discovery.ResolveIdentity()` → mapped to `provider.Identity` |
| `ListAgents()` | `discovery.ListAgents()` → mapped to `[]provider.AgentConfig` |
| `GetAgent()` | `discovery.ResolveAgent()` → mapped |
| `ResolveAgentByIdentity()` | `discovery.ResolveAgentByIAM()` via STS caller |
| `GetStatus()` | `awsutil.RunCommand()` with status script → parse key=value output |
| `GetLogs()` | `awsutil.RunCommand()` with `docker logs conga-{name} --tail {n}` |
| `RefreshAgent()` | `awsutil.RunCommand()` with rendered `scripts.RefreshUserScript` |
| `RefreshAll()` | `awsutil.RunCommand()` with rendered `scripts.RefreshAllScript` |
| `SetSecret()` | `awsutil.SetSecret()` at `conga/agents/{name}/{secret}` |
| `ListSecrets()` | `awsutil.ListSecrets()` with prefix `conga/agents/{name}/` |
| `DeleteSecret()` | `awsutil.DeleteSecret()` |
| `Connect()` | `tunnel.StartTunnel()` + token fetch via RunCommand |
| `Setup()` | Read manifest from SSM, prompt loop (existing admin_setup.go logic) |
| `ProvisionAgent()` | Create SSM param + `awsutil.RunCommand()` with add-user/add-team script |
| `RemoveAgent()` | Run remove script + delete SSM param + optionally delete secrets |
| `CycleHost()` | `awsutil.StopInstance()` → `WaitForState("stopped")` → `StartInstance()` → `WaitForState("running")` |

### 4.3 Type Mapping

The existing `discovery.AgentConfig` struct maps 1:1 to `provider.AgentConfig`. The AWS provider converts between them at the boundary. The existing struct is kept for backward compatibility within the `discovery` package but the provider wraps it.

### 4.4 Behavioral Contract

- **Zero behavioral change** from current CLI behavior.
- All existing timeouts, error messages, and output formatting preserved.
- The AWS provider is the **default** if no config file exists and AWS credentials are available.

---

## 5. Package: `cli/pkg/provider/local` — Local Docker Provider

### 5.1 Structure

```go
// provider.go
package local

import (
    "github.com/cruxdigital-llc/conga-line/cli/pkg/provider"
)

// LocalProvider implements provider.Provider using local Docker.
type LocalProvider struct {
    dataDir string // ~/.conga/ by default
}

func NewLocalProvider(cfg *provider.Config) (provider.Provider, error) {
    dataDir := cfg.DataDir
    if dataDir == "" {
        dataDir = defaultDataDir() // ~/.conga/
    }
    return &LocalProvider{dataDir: dataDir}, nil
}

func init() {
    provider.Register("local", NewLocalProvider)
}
```

### 5.2 Directory Layout

All state lives under `dataDir` (default `~/.conga/`):

```
~/.conga/
├── config.json                     # {"provider":"local","image":"ghcr.io/..."}
├── agents/
│   ├── {name}.json                 # AgentConfig JSON (same schema as SSM param)
├── secrets/
│   ├── shared/
│   │   ├── slack-bot-token         # file content = secret value, mode 0400
│   │   ├── slack-signing-secret
│   │   ├── slack-app-token
│   │   ├── google-client-id
│   │   └── google-client-secret
│   └── agents/
│       └── {name}/
│           └── {secret-name}       # file content = secret value, mode 0400
├── data/
│   └── {name}/
│       ├── openclaw.json           # generated config
│       └── data/workspace/         # OpenClaw data directory
├── config/
│   ├── {name}.env                  # generated env file, mode 0400
│   └── routing.json                # generated routing table
├── router/
│   ├── package.json                # copied from repo router/
│   └── src/
│       └── index.js
├── behavior/                       # copied from repo behavior/
│   ├── base/
│   ├── user/
│   ├── team/
│   └── overrides/
└── logs/
    └── integrity.log               # config integrity check output
```

### 5.3 Docker Operations

All Docker operations use `exec.CommandContext()` calling the `docker` CLI binary. No Docker SDK dependency.

```go
// docker.go
package local

// DockerClient wraps docker CLI calls.
type DockerClient struct{}

// CreateNetwork creates an internal (no-external-access) Docker network.
//   docker network create conga-{name} --internal --driver bridge
func (d *DockerClient) CreateNetwork(ctx context.Context, name string) error

// RemoveNetwork removes a Docker network.
//   docker network rm conga-{name}
func (d *DockerClient) RemoveNetwork(ctx context.Context, name string) error

// ConnectNetwork connects a container to a network.
//   docker network connect conga-{agentName} {containerName}
func (d *DockerClient) ConnectNetwork(ctx context.Context, network, container string) error

// RunContainer starts an agent container.
//   docker run -d \
//     --name conga-{name} \
//     --network conga-{name} \
//     --env-file {envPath} \
//     --cap-drop ALL \
//     --security-opt no-new-privileges \
//     --memory 2g \
//     --cpus 0.75 \
//     --pids-limit 256 \
//     --read-only \
//     --tmpfs /tmp:rw,noexec,nosuid,size=256m \
//     -v {dataDir}:/opt/conga/data/{name}:rw \
//     -v {configDir}/openclaw.json:/opt/conga/data/{name}/openclaw.json:ro \
//     -e NODE_OPTIONS="--max-old-space-size=1536" \
//     -p 127.0.0.1:{gatewayPort}:3000 \
//     {image}
func (d *DockerClient) RunContainer(ctx context.Context, opts ContainerOpts) error

type ContainerOpts struct {
    Name        string
    Image       string
    Network     string
    EnvFile     string
    DataDir     string
    ConfigPath  string
    GatewayPort int
    MemoryLimit string // "2g"
    CPULimit    string // "0.75"
}

// StopContainer stops a running container.
//   docker stop conga-{name}
func (d *DockerClient) StopContainer(ctx context.Context, name string) error

// RemoveContainer removes a container.
//   docker rm conga-{name}
func (d *DockerClient) RemoveContainer(ctx context.Context, name string) error

// RestartContainer restarts a container.
//   docker restart conga-{name}
func (d *DockerClient) RestartContainer(ctx context.Context, name string) error

// InspectContainer returns container state as JSON.
//   docker inspect conga-{name} --format '{{json .State}}'
func (d *DockerClient) InspectContainer(ctx context.Context, name string) (*DockerState, error)

// ContainerStats returns resource usage.
//   docker stats conga-{name} --no-stream --format '{{.MemUsage}}|{{.CPUPerc}}|{{.PIDs}}'
func (d *DockerClient) ContainerStats(ctx context.Context, name string) (*DockerStats, error)

// ContainerLogs returns the last N lines of logs.
//   docker logs conga-{name} --tail {n}
func (d *DockerClient) ContainerLogs(ctx context.Context, name string, lines int) (string, error)

// RunRouter starts the router container.
//   docker run -d \
//     --name conga-router \
//     --env-file {routerEnvPath} \
//     --cap-drop ALL \
//     --security-opt no-new-privileges \
//     --memory 128m \
//     --read-only \
//     --tmpfs /tmp:rw,noexec,nosuid \
//     -v {routerDir}:/app:ro \
//     -v {configDir}/routing.json:/opt/conga/config/routing.json:ro \
//     node:22-alpine \
//     node /app/src/index.js
func (d *DockerClient) RunRouter(ctx context.Context, opts RouterOpts) error
```

### 5.4 Secrets Management

Local secrets are stored as individual files with mode 0400. The `secrets/` directory itself is mode 0700.

```go
// secrets.go
package local

// SetSecret writes a secret value to disk.
// Path: {dataDir}/secrets/agents/{agentName}/{secretName}
// Or: {dataDir}/secrets/shared/{secretName} for shared secrets.
// File mode: 0400 (read-only by owner)
func (p *LocalProvider) SetSecret(ctx context.Context, agentName, secretName, value string) error

// ListSecrets reads all files in {dataDir}/secrets/agents/{agentName}/.
func (p *LocalProvider) ListSecrets(ctx context.Context, agentName string) ([]provider.SecretEntry, error)

// DeleteSecret removes the file.
func (p *LocalProvider) DeleteSecret(ctx context.Context, agentName, secretName string) error

// readSharedSecrets reads all shared secrets into a SharedSecrets struct.
func (p *LocalProvider) readSharedSecrets() (common.SharedSecrets, error)

// readAgentSecrets reads all per-agent secrets as a map[name]value.
func (p *LocalProvider) readAgentSecrets(agentName string) (map[string]string, error)
```

**Encryption**: For MVP, secrets are stored as plaintext files with strict file permissions (matching the AWS deployment where env files on EBS are also plaintext, protected by disk encryption and mode 0400). Future enhancement: optional `age` encryption with a master key stored in the OS keychain.

### 5.5 Provider Method Implementations

#### `Setup(ctx)`
1. Check Docker is installed and running (`docker info`)
2. Create directory structure under `dataDir`
3. Prompt for shared secrets interactively (same flow as AWS setup):
   - OpenClaw Docker image URL
   - Slack bot token
   - Slack signing secret
   - Slack app token
   - Google OAuth client ID + secret (optional)
4. Write shared secrets to `secrets/shared/`
5. Pull the Docker image
6. Copy router source from embedded files (or a configured repo path)
7. Copy behavior files from embedded files (or a configured repo path)
8. Start the router container
9. Save `config.json` with `{"provider":"local","image":"..."}`

#### `ProvisionAgent(ctx, cfg)`
1. Validate agent name and Slack IDs
2. Write agent config to `agents/{name}.json`
3. Read shared secrets + per-agent secrets (if any)
4. Generate `openclaw.json` via `common.GenerateOpenClawConfig()`
5. Generate `.env` file via `common.GenerateEnvFile()`
6. Compose behavior files via `common.ComposeBehaviorFiles()`
7. Write all generated files to appropriate directories
8. Create Docker network `conga-{name}` (internal)
9. Connect egress proxy network (see Section 6)
10. Start agent container
11. Regenerate `routing.json` via `common.GenerateRoutingJSON()` (reads all agents)
12. Connect router to agent network
13. Signal router to reload routing (Docker restart or file watch triggers)

#### `GetStatus(ctx, agentName)`
1. `docker inspect conga-{name}` for state
2. `docker stats conga-{name} --no-stream` for resources
3. `docker logs conga-{name} --tail 20` for boot phase detection (same markers as AWS: `[gateway] listening`, `[slack] starting provider`, etc.)
4. Map to `AgentStatus` struct

#### `GetLogs(ctx, agentName, lines)`
1. `docker logs conga-{name} --tail {lines} 2>&1`

#### `RefreshAgent(ctx, agentName)`
1. Read agent config from `agents/{name}.json`
2. Re-read shared + per-agent secrets
3. Regenerate `.env` file
4. Stop + remove old container
5. Start new container with fresh config
6. Reconnect router to agent network

#### `Connect(ctx, agentName, localPort)`
Port is already bound to localhost during container creation. This method:
1. Read `openclaw.json` to extract gateway token
2. Return `ConnectInfo{URL: "http://localhost:{port}#token={token}"}`
3. Block on context (no tunnel to maintain locally)

#### `WhoAmI(ctx)`
1. Return local OS username (`os.UserHomeDir()` + `user.Current()`)
2. No agent mapping for local (agents don't have IAM identities)

#### `RemoveAgent(ctx, name, deleteSecrets)`
1. Stop and remove container `conga-{name}`
2. Disconnect and remove network `conga-{name}`
3. Delete `agents/{name}.json`
4. If deleteSecrets: delete `secrets/agents/{name}/`
5. Optionally remove data directory (prompt user)
6. Regenerate routing.json
7. Restart router to pick up changes

#### `CycleHost(ctx)`
1. Stop all agent containers
2. Stop router
3. Re-read all configs, regenerate all env files
4. Start router
5. Start all agent containers with 5-second stagger

---

## 6. Network Isolation — Egress Proxy

### 6.1 Architecture

```
┌──────────────────────┐
│  Agent Container     │
│  network: conga-{n}  │◄── --internal (no direct external access)
│  (no gateway)        │
└──────────┬───────────┘
           │ Docker internal network
           ▼
┌──────────────────────┐
│  conga-egress-proxy  │
│  network: conga-{n}  │◄── connected to every agent network
│  + host network      │
│                      │
│  nginx stream proxy: │
│    443 → upstream    │
│    53  → upstream    │
│  All other ports     │
│    → REJECT          │
└──────────────────────┘
```

### 6.2 Implementation

A single `conga-egress-proxy` container runs an nginx stream proxy that only forwards TCP connections on ports 443 and 53. Agent containers set `HTTP_PROXY`/`HTTPS_PROXY` environment variables pointing to the proxy, and use it as their DNS resolver.

```nginx
# nginx.conf for egress proxy
stream {
    # HTTPS passthrough (SNI-based, no TLS termination)
    server {
        listen 3128;
        proxy_connect_timeout 10s;
        proxy_pass $upstream:443;
        # SNI extraction for logging
        ssl_preread on;
    }
}

# HTTP CONNECT proxy for HTTPS
http {
    server {
        listen 3129;
        # Only allow CONNECT method to port 443
        # Reject all other ports
    }
}
```

**Simpler alternative** (recommended for MVP): Use Docker's `--internal` networks, and add the egress proxy container as a network member with `--network host` capability. Agent containers get `HTTPS_PROXY=http://conga-egress-proxy:3128` in their env file.

Actually, the simplest portable approach:

1. Agent networks are `--internal` (no default gateway to host)
2. A `conga-egress` bridge network (non-internal) is created
3. The egress proxy connects to both `conga-egress` and each `conga-{agent}` network
4. Agent containers route through the proxy via `HTTPS_PROXY` / `DNS` env vars
5. The proxy only allows ports 443 and 53

### 6.3 Egress Proxy Container

```dockerfile
# deploy/egress-proxy/Dockerfile
FROM nginx:alpine
COPY nginx.conf /etc/nginx/nginx.conf
EXPOSE 3128 53
```

The proxy container is managed by the local provider alongside the router. It starts during `Setup()` and connects to new agent networks during `ProvisionAgent()`.

### 6.4 DNS

Agent containers use `--dns {proxy_ip}` pointing to the egress proxy, which forwards DNS queries to the host's configured resolver (or `8.8.8.8` by default). This ensures DNS resolution works through the internal network while still being restricted.

---

## 7. CLI Command Refactoring (`cli/cmd/`)

### 7.1 root.go Changes

Replace global `clients` with global `prov provider.Provider`:

```go
// root.go (new)
var (
    flagProvider string // --provider aws|local
    flagDataDir  string // --data-dir (local only)
    prov         provider.Provider
)

// PersistentPreRunE becomes:
func persistentPreRunE(cmd *cobra.Command, args []string) error {
    // 1. Load config from ~/.conga/config.json (if exists)
    cfg, _ := provider.LoadConfig(provider.DefaultConfigPath())

    // 2. Override with flags
    if flagProvider != "" {
        cfg.Provider = flagProvider
    }

    // 3. Auto-detect if not set:
    //    - If config.json exists with provider set → use it
    //    - If AWS credentials available → "aws"
    //    - Otherwise → error with instructions
    if cfg.Provider == "" {
        cfg.Provider = detectProvider()
    }

    // 4. AWS-specific: resolve profile and region (existing logic)
    if cfg.Provider == "aws" {
        resolveProfile()
        cfg.Region = resolvedRegion
        cfg.Profile = resolvedProfile
    }

    // 5. Initialize provider
    var err error
    prov, err = provider.Get(cfg.Provider, cfg)
    if err != nil {
        return err
    }
    return nil
}

// resolveAgentName now uses provider:
func resolveAgentName(ctx context.Context) (string, error) {
    if flagAgent != "" {
        return flagAgent, nil
    }
    agent, err := prov.ResolveAgentByIdentity(ctx)
    if err != nil {
        return "", err
    }
    if agent == nil {
        return "", fmt.Errorf("could not determine agent name; use --agent flag")
    }
    return agent.Name, nil
}
```

### 7.2 Command Refactoring Pattern

Each command file changes from calling AWS utilities directly to calling provider methods. Example for `status.go`:

**Before** (current):
```go
func statusRun(cmd *cobra.Command, args []string) error {
    ctx := commandContext()
    if err := ensureClients(ctx); err != nil { return err }
    name, err := resolveAgentName(ctx)
    instanceID, err := findInstance(ctx)
    result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, statusScript, 30*time.Second)
    // parse result.Stdout...
}
```

**After**:
```go
func statusRun(cmd *cobra.Command, args []string) error {
    ctx := commandContext()
    name, err := resolveAgentName(ctx)
    if err != nil { return err }
    status, err := prov.GetStatus(ctx, name)
    if err != nil { return err }
    printStatus(status) // extracted formatting logic
    return nil
}
```

### 7.3 Full Command Migration Table

| Command File | Before | After |
|-------------|--------|-------|
| `status.go` | `RunCommand` + parse | `prov.GetStatus()` |
| `logs.go` | `RunCommand` + docker logs | `prov.GetLogs()` |
| `refresh.go` | Render template + `RunCommand` | `prov.RefreshAgent()` |
| `connect.go` | `StartTunnel` + token fetch | `prov.Connect()` |
| `secrets.go` | `awsutil.SetSecret/List/Delete` | `prov.SetSecret/ListSecrets/DeleteSecret()` |
| `admin_setup.go` | SSM manifest read + prompt loop | `prov.Setup()` |
| `admin_provision.go` | SSM param + `RunCommand` | `prov.ProvisionAgent()` |
| `admin.go` (list) | `discovery.ListAgents()` | `prov.ListAgents()` |
| `admin_remove.go` | Script + SSM delete + secrets delete | `prov.RemoveAgent()` |
| `admin_cycle.go` | EC2 stop/start | `prov.CycleHost()` |
| `admin_refresh_all.go` | Render template + `RunCommand` | `prov.RefreshAll()` |
| `auth.go` (status) | `GetCallerIdentity` + `ResolveIdentity` | `prov.WhoAmI()` |

### 7.4 Commands Unchanged

- `auth login` — AWS-specific by nature; show "not applicable" for local provider
- `version` — no provider interaction
- `init` — updated to ask for provider selection

---

## 8. Router (Local)

The router runs identically to AWS. Same `router/src/index.js` code, same environment variables, same routing.json format.

**Differences from AWS:**
- No systemd unit — just a Docker container managed by the local provider
- `routing.json` mounted as a volume (hot-reload works via fs.watchFile, same as AWS)
- `SLACK_APP_TOKEN` comes from local secrets file instead of Secrets Manager
- Router connects to agent networks via `docker network connect`

**Router env file** (`~/.conga/config/router.env`):
```
SLACK_APP_TOKEN=xapp-...
SLACK_SIGNING_SECRET=...
```

---

## 9. Config Integrity (Local)

A lightweight approach using `docker exec` on a schedule:

```go
// integrity.go
package local

// StartIntegrityCheck launches a background goroutine that checks
// openclaw.json hashes every 5 minutes while the CLI is running.
// For persistent monitoring, creates a host-side cron job.
func (p *LocalProvider) StartIntegrityCheck() error

// SetupIntegrityCron creates a crontab entry (or launchd plist on macOS):
//   */5 * * * * conga integrity-check >> ~/.conga/logs/integrity.log 2>&1
func (p *LocalProvider) SetupIntegrityCron() error
```

Hash baselines are stored at `~/.conga/config/{name}.sha256` and compared against the live `openclaw.json` in the data directory.

---

## 10. Edge Cases & Error Handling

### 10.1 Docker Not Available
- `Setup()` checks `docker info` first
- Clear error: "Docker is not running. Please install Docker Desktop and start it."

### 10.2 Port Conflicts
- `ProvisionAgent()` checks if the gateway port is already in use (`net.Listen` probe)
- Suggest next available port if conflict detected

### 10.3 Partial Failure During Provisioning
- If container creation fails after network creation: clean up network
- If routing.json regeneration fails after container start: container still works, routing can be manually fixed
- All cleanup is idempotent (removing a non-existent container/network is a no-op)

### 10.4 Container Image Pull Failures
- Retry once with backoff
- Clear error message with manual pull instructions

### 10.5 Secret File Permissions
- On write: `os.WriteFile(path, data, 0400)` + `os.Chmod(path, 0400)` (belt and suspenders)
- On read: warn if permissions are wider than 0400

### 10.6 Concurrent CLI Invocations
- File-based locking on `~/.conga/.lock` for write operations (agent provisioning, secret updates, routing regeneration)
- Read operations (status, logs, list) do not require locks

### 10.7 Provider Mismatch
- If `~/.conga/config.json` says `local` but user passes `--provider aws`: honor the flag
- If config says `aws` but no AWS credentials available: suggest `--provider local` or `aws sso login`

---

## 11. File Changes Summary

### New Files

| File | Lines (est.) | Purpose |
|------|-------------|---------|
| `cli/pkg/provider/provider.go` | ~120 | Interface + shared types |
| `cli/pkg/provider/registry.go` | ~40 | Provider registry |
| `cli/pkg/provider/config.go` | ~60 | Config load/save |
| `cli/pkg/common/config.go` | ~100 | OpenClaw config generation |
| `cli/pkg/common/routing.go` | ~40 | Routing JSON generation |
| `cli/pkg/common/behavior.go` | ~80 | Behavior file composition |
| `cli/pkg/common/ports.go` | ~20 | Port allocation |
| `cli/pkg/common/secrets.go` | ~15 | Secret name conversion |
| `cli/pkg/provider/aws/provider.go` | ~400 | AWS provider implementation |
| `cli/pkg/provider/local/provider.go` | ~150 | Local provider struct + lifecycle |
| `cli/pkg/provider/local/docker.go` | ~250 | Docker CLI wrapper |
| `cli/pkg/provider/local/secrets.go` | ~80 | Local secrets file storage |
| `cli/pkg/provider/local/network.go` | ~60 | Network isolation + egress proxy |
| `cli/pkg/provider/local/integrity.go` | ~50 | Config integrity monitoring |
| `deploy/egress-proxy/Dockerfile` | ~5 | Egress proxy image |
| `deploy/egress-proxy/nginx.conf` | ~30 | Egress proxy config |

### Modified Files

| File | Change |
|------|--------|
| `cli/cmd/root.go` | Replace `clients` with `prov`, add `--provider`/`--data-dir` flags |
| `cli/cmd/status.go` | Use `prov.GetStatus()` |
| `cli/cmd/logs.go` | Use `prov.GetLogs()` |
| `cli/cmd/refresh.go` | Use `prov.RefreshAgent()` |
| `cli/cmd/connect.go` | Use `prov.Connect()` |
| `cli/cmd/secrets.go` | Use `prov.SetSecret/ListSecrets/DeleteSecret()` |
| `cli/cmd/admin_setup.go` | Use `prov.Setup()` |
| `cli/cmd/admin_provision.go` | Use `prov.ProvisionAgent()` |
| `cli/cmd/admin.go` | Use `prov.ListAgents()` |
| `cli/cmd/admin_remove.go` | Use `prov.RemoveAgent()` |
| `cli/cmd/admin_cycle.go` | Use `prov.CycleHost()` |
| `cli/cmd/admin_refresh_all.go` | Use `prov.RefreshAll()` |
| `cli/cmd/auth.go` | Use `prov.WhoAmI()` |
| `cli/go.mod` | No new external dependencies |

### Unchanged Files

- All `terraform/` files
- `router/src/index.js`
- `behavior/` files
- `cli/pkg/aws/` (kept for AWS provider, not modified)
- `cli/pkg/discovery/` (kept for AWS provider, not modified)
- `cli/pkg/tunnel/` (kept for AWS provider, not modified)
- `cli/scripts/` (kept for AWS provider shell templates)

---

## 12. Testing Strategy

### Unit Tests
- `common/config_test.go` — Verify generated openclaw.json matches expected output for user/team agents
- `common/routing_test.go` — Verify routing.json structure
- `common/behavior_test.go` — Verify behavior file composition priority
- `provider/local/docker_test.go` — Verify Docker command construction (mock exec.Command)
- `provider/local/secrets_test.go` — Verify file write/read/delete with correct permissions
- `provider/aws/provider_test.go` — Verify method delegation to existing utilities

### Integration Tests
- `provider/local/integration_test.go` — Full lifecycle test requiring Docker:
  1. Setup → ProvisionAgent → GetStatus → GetLogs → RefreshAgent → RemoveAgent
  2. Network isolation: verify container cannot reach arbitrary external ports
  3. Secrets: set, list, delete, verify env injection
- Guarded by `// +build integration` tag and `-tags integration` flag

### Regression Tests
- Run all existing CLI tests against the AWS provider path
- Verify `--provider aws` produces identical behavior to current code
