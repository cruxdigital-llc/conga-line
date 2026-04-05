# Specification: Agent Portability

## Overview

This spec defines the **Runtime** interface and supporting changes needed to make Conga Line agent-runtime-agnostic. The Runtime abstraction is orthogonal to the Provider interface: providers handle *where* (AWS, local, remote), runtimes handle *what* (OpenClaw, Hermes, future agents).

## 1. Data Model Changes

### 1.1 AgentConfig (pkg/provider/provider.go)

```go
type AgentConfig struct {
    Name        string                    `json:"name"`
    Type        AgentType                 `json:"type"`
    Runtime     string                    `json:"runtime,omitempty"` // NEW: "openclaw", "hermes" (default: "openclaw")
    Channels    []channels.ChannelBinding `json:"channels,omitempty"`
    GatewayPort int                       `json:"gateway_port"`
    IAMIdentity string                    `json:"iam_identity,omitempty"`
    Paused      bool                      `json:"paused,omitempty"`
}
```

**Backward compatibility**: Existing agent JSON files without `Runtime` field unmarshal to `""`, which all code treats as `"openclaw"`.

### 1.2 Config (pkg/provider/config.go)

```go
type Config struct {
    Provider   ProviderName `json:"provider"`
    Runtime    string       `json:"runtime,omitempty"` // NEW: default runtime for new agents
    DataDir    string       `json:"data_dir,omitempty"`
    Region     string       `json:"region,omitempty"`
    Profile    string       `json:"profile,omitempty"`
    SSHHost    string       `json:"ssh_host,omitempty"`
    SSHPort    int          `json:"ssh_port,omitempty"`
    SSHUser    string       `json:"ssh_user,omitempty"`
    SSHKeyPath string       `json:"ssh_key_path,omitempty"`
}
```

### 1.3 SetupConfig (pkg/provider/setup_config.go)

```go
type SetupConfig struct {
    // ... existing fields ...
    Runtime string `json:"runtime,omitempty"` // NEW
}
```

### 1.4 Manifest (pkg/manifest/manifest.go)

```go
type Manifest struct {
    APIVersion string            `yaml:"apiVersion"`
    Kind       string            `yaml:"kind"`
    Provider   string            `yaml:"provider,omitempty"`
    Runtime    string            `yaml:"runtime,omitempty"` // NEW: default runtime
    Setup      *ManifestSetup    `yaml:"setup,omitempty"`
    Agents     []ManifestAgent   `yaml:"agents,omitempty"`
    Channels   []ManifestChannel `yaml:"channels,omitempty"`
    Policy     *ManifestPolicy   `yaml:"policy,omitempty"`
}

type ManifestAgent struct {
    Name     string            `yaml:"name"`
    Type     string            `yaml:"type"`
    Runtime  string            `yaml:"runtime,omitempty"` // NEW: per-agent override
    Channels []ManifestBinding `yaml:"channels,omitempty"`
    Secrets  map[string]string `yaml:"secrets,omitempty"`
}
```

### 1.5 RuntimeName Constants

```go
// pkg/runtime/runtime.go
type RuntimeName string

const (
    RuntimeOpenClaw RuntimeName = "openclaw"
    RuntimeHermes   RuntimeName = "hermes"
)
```

### 1.6 Helper: Resolve Runtime

```go
// ResolveRuntime returns the effective runtime name for an agent.
// Falls back to the global default, then to "openclaw".
func ResolveRuntime(agentRuntime, globalDefault string) RuntimeName {
    if agentRuntime != "" {
        return RuntimeName(agentRuntime)
    }
    if globalDefault != "" {
        return RuntimeName(globalDefault)
    }
    return RuntimeOpenClaw
}
```

## 2. Runtime Interface (pkg/runtime/runtime.go)

```go
package runtime

import (
    "github.com/cruxdigital-llc/conga-line/pkg/channels"
    "github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// Runtime encapsulates all agent-runtime-specific behavior.
// Each implementation lives in its own sub-package (openclaw/, hermes/).
type Runtime interface {
    // Name returns the runtime identifier.
    Name() RuntimeName

    // --- Config Generation ---

    // GenerateConfig produces the runtime's native config file bytes.
    // For OpenClaw: openclaw.json. For Hermes: config.yaml.
    GenerateConfig(params ConfigParams) ([]byte, error)

    // ConfigFileName returns the config file name written to the data directory.
    ConfigFileName() string

    // GenerateEnvFile produces the .env file content for the agent container.
    GenerateEnvFile(params EnvParams) []byte

    // --- Container Specification ---

    // ContainerSpec returns Docker container parameters.
    ContainerSpec(agent provider.AgentConfig) ContainerSpec

    // DefaultImage returns the default Docker image for this runtime.
    DefaultImage() string

    // --- Directory Layout ---

    // CreateDirectories creates the runtime-specific directory structure
    // inside the agent's host-side data directory.
    CreateDirectories(dataDir string) error

    // ContainerConfigPath returns the path inside the container where the
    // config file is expected. Used for volume mount destination.
    // e.g., "/home/node/.openclaw" for OpenClaw, "/opt/data" for Hermes.
    ContainerDataPath() string

    // --- Health Detection ---

    // DetectReady parses container log output and returns the readiness phase.
    DetectReady(logOutput string, hasSlack bool) ReadyPhase

    // --- Gateway Token ---

    // ReadGatewayToken extracts the gateway auth token from the config file on disk.
    // configData is the raw bytes of the runtime's config file.
    ReadGatewayToken(configData []byte) string

    // GatewayTokenDockerExec returns a docker exec command to extract the
    // gateway token from inside a running container.
    // Returns nil if the runtime doesn't support in-container extraction.
    GatewayTokenDockerExec() []string

    // --- Channel Integration ---

    // ChannelConfig produces the runtime-native channel configuration for
    // embedding in the runtime's config file. Called per-channel-binding.
    // Returns nil if this runtime doesn't embed channel config in its config file.
    ChannelConfig(agentType string, binding channels.ChannelBinding, secretValues map[string]string) (map[string]any, error)

    // PluginConfig produces runtime-native plugin/adapter enable/disable config.
    // Returns nil if this runtime doesn't have a plugin system.
    PluginConfig(platform string, enabled bool) map[string]any

    // WebhookPath returns the HTTP path where the router should deliver
    // channel events to this runtime's container.
    // e.g., "/slack/events" for OpenClaw, "/webhooks/slack" for Hermes.
    WebhookPath(platform string) string

    // --- Egress Proxy ---

    // SupportsNodeProxy returns true if this runtime is Node.js-based and
    // needs the proxy-bootstrap.js --require injection.
    SupportsNodeProxy() bool

    // ProxyEnvVars returns additional env vars needed for egress proxy support.
    // Most runtimes just need HTTP_PROXY/HTTPS_PROXY (handled by provider).
    // This is for runtime-specific additions.
    ProxyEnvVars(proxyHost string) map[string]string
}
```

### 2.1 Supporting Types

```go
// ConfigParams holds all inputs needed to generate a runtime config file.
type ConfigParams struct {
    Agent        provider.AgentConfig
    Secrets      common.SharedSecrets
    GatewayToken string
}

// EnvParams holds all inputs needed to generate an env file.
type EnvParams struct {
    Agent    provider.AgentConfig
    Secrets  common.SharedSecrets
    PerAgent map[string]string // per-agent secret name→value
}

// ContainerSpec defines Docker container parameters.
type ContainerSpec struct {
    ContainerPort  int               // Port inside the container
    User           string            // "--user" value, e.g. "1000:1000"
    Memory         string            // "--memory" value, e.g. "2g"
    CPUs           string            // "--cpus" value, e.g. "0.75"
    PIDsLimit      string            // "--pids-limit" value
    EnvVars        map[string]string // Runtime-specific env vars (NODE_OPTIONS, etc.)
    Entrypoint     []string          // Override entrypoint (nil = use image default)
    Tmpfs          []string          // tmpfs mounts if needed
}

// ReadyPhase describes the container's readiness state.
type ReadyPhase struct {
    Phase   string // "starting", "gateway_up", "loading", "ready", "error"
    Message string // Human-readable description
    IsReady bool   // true when the agent is fully operational
    HasError bool  // true when errors detected in logs
}
```

## 3. Runtime Registry (pkg/runtime/registry.go)

Mirrors the Provider registry pattern exactly:

```go
package runtime

import (
    "fmt"
    "sort"
)

type Factory func() Runtime

var registry = map[RuntimeName]Factory{}

func Register(name RuntimeName, factory Factory) {
    if _, exists := registry[name]; exists {
        panic(fmt.Sprintf("runtime: duplicate registration %q", name))
    }
    registry[name] = factory
}

func Get(name RuntimeName) (Runtime, error) {
    factory, ok := registry[name]
    if !ok {
        return nil, fmt.Errorf("unknown runtime %q (available: %v)", name, Names())
    }
    return factory(), nil
}

func Names() []string {
    names := make([]string, 0, len(registry))
    for name := range registry {
        names = append(names, string(name))
    }
    sort.Strings(names)
    return names
}
```

## 4. OpenClaw Runtime (pkg/runtime/openclaw/)

Pure extraction — every method implemented by moving existing code.

### 4.1 Config Generation (config.go)

Moves from `pkg/common/config.go`:
- `GenerateOpenClawConfig()` → `openclaw.GenerateConfig()`
- `buildGatewayConfig()` → `openclaw.buildGatewayConfig()` (unexported)
- `openclaw-defaults.json` embed → stays with this package

```go
func (r *OpenClawRuntime) GenerateConfig(params runtime.ConfigParams) ([]byte, error) {
    // Exact same logic as current GenerateOpenClawConfig(),
    // but calls r.ChannelConfig() instead of ch.OpenClawChannelConfig()
}

func (r *OpenClawRuntime) ConfigFileName() string { return "openclaw.json" }
```

### 4.2 Env File (env.go)

Moves from `pkg/common/config.go`:
- `GenerateEnvFile()` → `openclaw.GenerateEnvFile()`

Key: `NODE_OPTIONS=--max-old-space-size=1536` is OpenClaw-specific and moves here.

### 4.3 Container Spec (container.go)

```go
func (r *OpenClawRuntime) ContainerSpec(agent provider.AgentConfig, image string) runtime.ContainerSpec {
    return runtime.ContainerSpec{
        ContainerPort: 18789,
        User:          "1000:1000",
        Memory:        "2g",
        CPUs:          "0.75",
        PIDsLimit:     "256",
        EnvVars:       map[string]string{"NODE_OPTIONS": "--max-old-space-size=1536"},
    }
}

func (r *OpenClawRuntime) DefaultImage() string {
    return "ghcr.io/openclaw/openclaw:latest"
}

func (r *OpenClawRuntime) ContainerDataPath() string {
    return "/home/node/.openclaw"
}

func (r *OpenClawRuntime) SupportsNodeProxy() bool { return true }
```

### 4.4 Directory Layout (dirs.go)

```go
func (r *OpenClawRuntime) CreateDirectories(dataDir string) error {
    for _, sub := range []string{
        "data/workspace", "memory", "logs", "agents",
        "canvas", "cron", "devices", "identity", "media",
    } {
        if err := os.MkdirAll(filepath.Join(dataDir, sub), 0755); err != nil {
            return err
        }
    }
    // Create empty MEMORY.md so OpenClaw doesn't error on first read
    memoryPath := filepath.Join(dataDir, "data", "workspace", "MEMORY.md")
    if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
        return os.WriteFile(memoryPath, []byte("# Memory\n"), 0644)
    }
    return nil
}
```

### 4.5 Health Detection (health.go)

```go
func (r *OpenClawRuntime) DetectReady(logOutput string, hasSlack bool) runtime.ReadyPhase {
    phase := runtime.ReadyPhase{Phase: "starting", Message: "Container starting"}

    if strings.Contains(logOutput, "[gateway] listening") {
        phase = runtime.ReadyPhase{Phase: "gateway_up", Message: "Gateway up, waiting for plugins"}
    }
    if hasSlack {
        if strings.Contains(logOutput, "[slack]") && strings.Contains(logOutput, "starting provider") {
            phase = runtime.ReadyPhase{Phase: "loading", Message: "Slack plugin loading"}
        }
        if strings.Contains(logOutput, "[slack] http mode listening") {
            phase = runtime.ReadyPhase{Phase: "loading", Message: "Slack endpoint ready, resolving channels"}
        }
        if strings.Contains(logOutput, "[slack] channels resolved") {
            phase = runtime.ReadyPhase{Phase: "ready", Message: "Ready", IsReady: true}
        }
    } else {
        // Gateway-only mode (no Slack) — gateway listening = ready
        if strings.Contains(logOutput, "[gateway] listening") {
            phase = runtime.ReadyPhase{Phase: "ready", Message: "Ready (gateway only)", IsReady: true}
        }
    }

    lower := strings.ToLower(logOutput)
    if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") {
        phase.HasError = true
        phase.Message += " (errors in logs — check `conga logs`)"
    }

    return phase
}
```

### 4.6 Gateway Token (token.go)

```go
func (r *OpenClawRuntime) ReadGatewayToken(configData []byte) string {
    var config map[string]interface{}
    if err := json.Unmarshal(configData, &config); err != nil {
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

func (r *OpenClawRuntime) GatewayTokenDockerExec() []string {
    return []string{
        "node", "-e",
        `try{const c=require('/home/node/.openclaw/openclaw.json');` +
            `console.log(c.gateway?.token||c.gateway?.auth?.token||'')}catch(e){console.log('')}`,
    }
}
```

### 4.7 Channel Integration

```go
func (r *OpenClawRuntime) ChannelConfig(agentType string, binding channels.ChannelBinding, secretValues map[string]string) (map[string]any, error) {
    ch, ok := channels.Get(binding.Platform)
    if !ok {
        return nil, fmt.Errorf("unknown channel %q", binding.Platform)
    }
    // Delegate to the existing OpenClawChannelConfig method on the channel
    return ch.OpenClawChannelConfig(agentType, binding, secretValues)
}

func (r *OpenClawRuntime) PluginConfig(platform string, enabled bool) map[string]any {
    ch, ok := channels.Get(platform)
    if !ok {
        return nil
    }
    return ch.OpenClawPluginConfig(enabled)
}

func (r *OpenClawRuntime) WebhookPath(platform string) string {
    ch, ok := channels.Get(platform)
    if !ok {
        return ""
    }
    return ch.WebhookPath()
}
```

**Note**: `OpenClawChannelConfig()` and `OpenClawPluginConfig()` remain on the Channel interface for now. They are only called from the OpenClaw runtime. In a future cleanup, they could be removed from the interface and accessed via type assertion, but this is not required for this feature.

## 5. Hermes Runtime (pkg/runtime/hermes/)

### 5.1 Config Generation (config.go)

Generates `config.yaml` in Hermes's expected YAML structure.

```go
func (r *HermesRuntime) GenerateConfig(params runtime.ConfigParams) ([]byte, error) {
    cfg := map[string]any{
        "model": map[string]any{
            "provider": "anthropic",
        },
        "platforms": map[string]any{
            "api_server": map[string]any{
                "enabled": true,
                "host":    "0.0.0.0",
                "port":    8642,
            },
        },
    }

    // Gateway auth token
    if params.GatewayToken != "" {
        apiServer := cfg["platforms"].(map[string]any)["api_server"].(map[string]any)
        apiServer["key"] = params.GatewayToken
        // CORS: allow localhost access via tunnel
        origins := []string{
            fmt.Sprintf("http://localhost:%d", 8642),
            fmt.Sprintf("http://localhost:%d", params.Agent.GatewayPort),
        }
        apiServer["cors_origins"] = strings.Join(origins, ",")
    }

    // Slack via webhook adapter (if configured)
    for _, binding := range params.Agent.Channels {
        if binding.Platform == "slack" {
            webhookCfg := cfg["platforms"].(map[string]any)
            webhookCfg["webhook"] = map[string]any{
                "enabled": true,
                "host":    "0.0.0.0",
                "port":    8644,
            }
        }
    }

    return yaml.Marshal(cfg)
}

func (r *HermesRuntime) ConfigFileName() string { return "config.yaml" }
```

**Dependency**: `gopkg.in/yaml.v3` (already in go.mod for the policy package).

### 5.2 Env File (env.go)

```go
func (r *HermesRuntime) GenerateEnvFile(params runtime.EnvParams) []byte {
    var buf []byte
    appendEnv := func(key, val string) {
        if val != "" {
            buf = append(buf, []byte(fmt.Sprintf("%s=%s\n", key, val))...)
        }
    }

    // Channel env vars (SLACK_BOT_TOKEN, SLACK_SIGNING_SECRET)
    seen := map[string]bool{}
    for _, binding := range params.Agent.Channels {
        ch, ok := channels.Get(binding.Platform)
        if !ok {
            continue
        }
        for k, v := range ch.AgentEnvVars(params.Secrets.Values) {
            if !seen[k] {
                appendEnv(k, v)
                seen[k] = true
            }
        }
    }

    // Per-agent secrets (ANTHROPIC_API_KEY, etc.)
    for name, value := range params.PerAgent {
        appendEnv(common.SecretNameToEnvVar(name), value)
    }

    return buf
}
```

**Key difference from OpenClaw**: No `NODE_OPTIONS`, no `GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET` (those are OpenClaw-specific OAuth features).

### 5.3 Container Spec (container.go)

```go
func (r *HermesRuntime) ContainerSpec(agent provider.AgentConfig, image string) runtime.ContainerSpec {
    return runtime.ContainerSpec{
        ContainerPort: 8642,
        User:          "1000:1000",
        Memory:        "2g",
        CPUs:          "0.75",
        PIDsLimit:     "256",
        EnvVars:       map[string]string{},
        // No entrypoint override — use image default
    }
}

func (r *HermesRuntime) DefaultImage() string {
    return "" // No pre-built image; user must supply via setup
}

func (r *HermesRuntime) ContainerDataPath() string {
    return "/opt/data"
}

func (r *HermesRuntime) SupportsNodeProxy() bool { return false }

func (r *HermesRuntime) ProxyEnvVars(proxyHost string) map[string]string {
    // Python requests library respects HTTP_PROXY/HTTPS_PROXY natively.
    // No additional runtime-specific env vars needed.
    return nil
}
```

### 5.4 Directory Layout (dirs.go)

```go
func (r *HermesRuntime) CreateDirectories(dataDir string) error {
    for _, sub := range []string{"workspace", "memory", "skills", "logs"} {
        if err := os.MkdirAll(filepath.Join(dataDir, sub), 0755); err != nil {
            return err
        }
    }
    return nil
}
```

### 5.5 Health Detection (health.go)

```go
func (r *HermesRuntime) DetectReady(logOutput string, hasSlack bool) runtime.ReadyPhase {
    phase := runtime.ReadyPhase{Phase: "starting", Message: "Container starting"}

    if strings.Contains(logOutput, "API server listening on") {
        phase = runtime.ReadyPhase{Phase: "gateway_up", Message: "API server up"}
    }
    if strings.Contains(logOutput, "Gateway running with") {
        phase = runtime.ReadyPhase{Phase: "ready", Message: "Ready", IsReady: true}
    }

    lower := strings.ToLower(logOutput)
    if strings.Contains(lower, "error") || strings.Contains(lower, "traceback") {
        phase.HasError = true
        phase.Message += " (errors in logs — check `conga logs`)"
    }

    return phase
}
```

### 5.6 Gateway Token (token.go)

```go
func (r *HermesRuntime) ReadGatewayToken(configData []byte) string {
    var config map[string]any
    if err := yaml.Unmarshal(configData, &config); err != nil {
        return ""
    }
    if platforms, ok := config["platforms"].(map[string]any); ok {
        if apiServer, ok := platforms["api_server"].(map[string]any); ok {
            if key, ok := apiServer["key"].(string); ok {
                return key
            }
        }
    }
    return ""
}

func (r *HermesRuntime) GatewayTokenDockerExec() []string {
    return []string{
        "python3", "-c",
        `import yaml; c=yaml.safe_load(open('/opt/data/config.yaml')); print(c.get('platforms',{}).get('api_server',{}).get('key',''))`,
    }
}
```

### 5.7 Channel Integration

```go
func (r *HermesRuntime) ChannelConfig(agentType string, binding channels.ChannelBinding, secretValues map[string]string) (map[string]any, error) {
    // Hermes doesn't embed channel config in config.yaml the way OpenClaw does.
    // Channel configuration is done via env vars and the webhook adapter.
    return nil, nil
}

func (r *HermesRuntime) PluginConfig(platform string, enabled bool) map[string]any {
    // Hermes doesn't have OpenClaw's plugin system.
    return nil
}

func (r *HermesRuntime) WebhookPath(platform string) string {
    switch platform {
    case "slack":
        return "/webhooks/slack"
    default:
        return "/webhooks/" + platform
    }
}
```

### 5.8 Slack Event Delivery — The Router Translation Problem

**Problem**: Hermes's Slack adapter only supports Socket Mode. It has a generic WebhookAdapter at `/webhooks/{route_name}` but this expects a different payload format than what the Conga router sends (raw Slack events with HMAC signing headers).

**Solution — Router adaptation layer**: The Conga router already normalizes Slack events before forwarding. We add a thin translation in the router that, for Hermes targets, wraps the Slack event into the WebhookAdapter's expected format:

```javascript
// router/src/index.js — enhanced forwardEvent()
async function forwardEvent(url, payload, headers) {
    // Existing: forward raw Slack event with signing headers
    // For Hermes webhook endpoints (/webhooks/*): wrap payload
    if (url.includes('/webhooks/')) {
        // Hermes WebhookAdapter expects: { event_type, payload, ... }
        // with HMAC in X-Webhook-Signature header
        const wrappedPayload = {
            event_type: 'slack_event',
            payload: payload,
            timestamp: headers['x-slack-request-timestamp'],
        };
        return fetch(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-Webhook-Signature': computeHMAC(wrappedPayload, signingSecret),
            },
            body: JSON.stringify(wrappedPayload),
        });
    }
    // Default: forward as-is (OpenClaw path, unchanged)
    return fetch(url, { method: 'POST', headers, body: JSON.stringify(payload) });
}
```

**Alternative (simpler, Phase 1)**: For the initial local provider implementation, Hermes agents run in **gateway-only mode** (web UI via API server, no Slack). This validates the entire Runtime abstraction end-to-end without requiring router changes. Slack integration is added in a follow-up after the WebhookAdapter compatibility is verified.

**Recommendation**: Ship gateway-only in Phase 1 of Hermes support. Slack integration is Phase 2 once we can test the webhook payload format.

## 6. Channel Interface Refactoring

### 6.1 Current State

The `Channel` interface has two OpenClaw-specific methods:
- `OpenClawChannelConfig()` — returns `channels.{platform}` section of `openclaw.json`
- `OpenClawPluginConfig()` — returns `plugins.entries.{platform}` section

### 6.2 Approach: Runtime Delegates to Channel

Rather than adding `HermesChannelConfig()` methods to the Channel interface (which would grow linearly with runtimes), each **Runtime** is responsible for calling whatever Channel methods it needs:

- The OpenClaw runtime calls `ch.OpenClawChannelConfig()` and `ch.OpenClawPluginConfig()`
- The Hermes runtime doesn't call these methods (it uses env vars + webhook adapter)
- Future runtimes call whatever they need

The `OpenClawChannelConfig()` and `OpenClawPluginConfig()` methods stay on the Channel interface for now. They're only called by the OpenClaw runtime. A future cleanup could move them to a separate interface, but that's not needed for this feature.

### 6.3 Routing Changes

`GenerateRoutingJSON()` currently calls `ch.WebhookPath()` to construct webhook URLs. This returns `/slack/events` unconditionally.

**Change**: Routing needs to use the Runtime's webhook path, not the Channel's, because different runtimes receive events at different paths:

```go
// pkg/common/routing.go — updated
func GenerateRoutingJSON(agents []provider.AgentConfig, runtimeResolver func(string) runtime.Runtime) ([]byte, error) {
    cfg := routingConfig{
        Channels: map[string]string{},
        Members:  map[string]string{},
    }

    for _, agent := range agents {
        if agent.Paused {
            continue
        }
        rt := runtimeResolver(agent.Runtime)
        for _, binding := range agent.Channels {
            ch, ok := channels.Get(binding.Platform)
            if !ok {
                continue
            }
            webhookPath := rt.WebhookPath(binding.Platform)
            port := agent.GatewayPort
            entries := ch.RoutingEntries(string(agent.Type), binding, agent.Name, port)
            for _, e := range entries {
                // Override the URL's path with the runtime's webhook path
                url := fmt.Sprintf("http://conga-%s:%d%s", agent.Name, port, webhookPath)
                switch e.Section {
                case "channels":
                    cfg.Channels[e.Key] = url
                case "members":
                    cfg.Members[e.Key] = url
                }
            }
        }
    }

    return json.MarshalIndent(cfg, "", "  ")
}
```

**Alternative (simpler)**: Have `RoutingEntries()` accept the webhook path as a parameter instead of hardcoding it. This avoids changing the function signature of `GenerateRoutingJSON()`:

```go
// Channel interface gains a parameter:
RoutingEntries(agentType string, binding ChannelBinding, agentName string, port int, webhookPath string) []RoutingEntry
```

**Recommendation**: The parameter approach is simpler and less disruptive. The channel constructs the full URL using the provided webhook path.

## 7. Provider Integration Pattern

### 7.1 Runtime Resolution in Providers

Each provider gains a helper to resolve the runtime for an agent:

```go
// Shared helper (could be in pkg/runtime/ or a provider utility)
func runtimeForAgent(agent provider.AgentConfig, globalDefault string) (runtime.Runtime, error) {
    name := runtime.ResolveRuntime(agent.Runtime, globalDefault)
    return runtime.Get(name)
}
```

### 7.2 Local Provider Changes (pkg/provider/localprovider/)

**ProvisionAgent** — before and after:

```
BEFORE:
1. Read secrets
2. common.GenerateOpenClawConfig() → openclaw.json bytes
3. Create /data/workspace, /memory, /logs, ... (hardcoded)
4. Write openclaw.json to data dir
5. common.GenerateEnvFile() → .env bytes (includes NODE_OPTIONS)
6. Deploy behavior files
7. Read image (default: ghcr.io/openclaw/openclaw:latest)
8. Start egress proxy
9. runAgentContainer() with hardcoded opts (port 18789, user 1000:1000, etc.)
10. Apply iptables rules

AFTER:
1. Read secrets
2. Resolve runtime for agent
3. rt.GenerateConfig() → config bytes
4. rt.CreateDirectories(dataDir)
5. Write rt.ConfigFileName() to data dir
6. rt.GenerateEnvFile() → .env bytes
7. Deploy behavior files
8. Read image (default: rt.DefaultImage())
9. Start egress proxy
10. spec := rt.ContainerSpec() → parameterized container opts
11. runAgentContainer() with spec (port, user, memory, env from spec)
12. Apply iptables rules
```

**runAgentContainer** — parameterized:

```go
func runAgentContainer(ctx context.Context, opts agentContainerOpts, spec runtime.ContainerSpec) error {
    args := []string{
        "run", "-d",
        "--name", opts.Name,
        "--network", opts.Network,
        "--env-file", opts.EnvFile,
        "--cap-drop", "ALL",
        "--security-opt", "no-new-privileges",
        "--memory", spec.Memory,
        "--cpus", spec.CPUs,
        "--pids-limit", spec.PIDsLimit,
        "--user", spec.User,
        "-v", fmt.Sprintf("%s:%s:rw", opts.DataDir, opts.ContainerDataPath),
    }

    // Port mapping: host port → container port
    args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d", opts.GatewayPort, spec.ContainerPort))

    // Egress proxy (if active)
    if opts.EgressProxyName != "" {
        args = append(args, "-e", fmt.Sprintf("HTTPS_PROXY=http://%s:3128", opts.EgressProxyName))
        args = append(args, "-e", fmt.Sprintf("HTTP_PROXY=http://%s:3128", opts.EgressProxyName))
        args = append(args, "-e", "NO_PROXY=localhost,127.0.0.1")

        // Node.js proxy bootstrap (only for Node-based runtimes)
        if opts.ProxyBootstrapPath != "" && spec.SupportsNodeProxy {
            args = append(args, "-v", fmt.Sprintf("%s:/opt/proxy-bootstrap.js:ro", opts.ProxyBootstrapPath))
            spec.EnvVars["NODE_OPTIONS"] += " --require /opt/proxy-bootstrap.js"
        }
    }

    // Runtime-specific env vars
    for k, v := range spec.EnvVars {
        args = append(args, "-e", k+"="+v)
    }

    // Entrypoint override (if specified)
    if len(spec.Entrypoint) > 0 {
        args = append(args, "--entrypoint", spec.Entrypoint[0])
        args = append(args, opts.Image)
        args = append(args, spec.Entrypoint[1:]...)
    } else {
        args = append(args, opts.Image)
    }

    _, err := dockerRun(ctx, args...)
    return err
}
```

**GetStatus** — delegates health detection:

```go
// In GetStatus():
rt, _ := runtimeForAgent(*cfg, p.getConfigValue("runtime"))
hasSlack := cfg.ChannelBinding("slack") != nil
readyPhase := rt.DetectReady(logs, hasSlack)
status.ReadyPhase = readyPhase.Phase
```

**Connect** — delegates token extraction:

```go
// In Connect():
rt, _ := runtimeForAgent(*cfg, p.getConfigValue("runtime"))
configPath := filepath.Join(p.dataSubDir(agentName), rt.ConfigFileName())
if data, err := os.ReadFile(configPath); err == nil {
    token = rt.ReadGatewayToken(data)
}
// Fallback: docker exec
if token == "" && containerExists(ctx, cName) {
    if execCmd := rt.GatewayTokenDockerExec(); execCmd != nil {
        args := append([]string{"exec", cName}, execCmd...)
        output, err := dockerRun(ctx, args...)
        if err == nil {
            token = strings.TrimSpace(output)
        }
    }
}
```

### 7.3 Remote Provider

Same pattern as local. SSH commands parameterized by `ContainerSpec` and `ConfigFileName()`. The remote provider's `docker.go` mirrors the local provider's changes.

### 7.4 AWS Provider

**Shell templates** (`scripts/add-user.sh.tmpl`, `scripts/add-team.sh.tmpl`):

Two approaches, in order of preference:

**Option A (recommended): CLI-side config generation, SSM upload**
- The CLI generates the config file locally using `rt.GenerateConfig()`
- Uploads it to the EC2 host via SSM `PutParameter` or `SendCommand` with base64-encoded content
- Shell template writes it to disk and starts the container with parameters from the CLI

**Option B: Runtime-aware templates**
- Templates gain conditional blocks (`{{if eq .Runtime "hermes"}}`)
- Each runtime section generates the appropriate config structure
- This is brittle and duplicates Go logic in shell

Option A is preferred because it keeps config generation in Go (single source of truth) and makes templates simpler.

## 8. CLI Changes

### 8.1 New Flag: --runtime

```
conga admin setup --provider local --runtime hermes
conga admin add-user --name alice --runtime hermes
conga admin add-team --name leadership --runtime hermes
```

Flag is defined in the root command and propagated to subcommands. Persisted in `~/.conga/config.json` (or provider-specific config) alongside the provider choice.

### 8.2 Status Output

```
$ conga status --agent alice
Agent:    alice
Runtime:  hermes
Provider: local
State:    running
Phase:    ready
Uptime:   2h 15m
Memory:   485 MiB / 2 GiB
CPU:      0.12%
PIDs:     24
```

### 8.3 Interface Parity (JSON + MCP)

Per the Interface Parity architecture standard, `--runtime` must also be supported in:

- **JSON input**: `"runtime": "hermes"` field in `conga json-schema admin-setup`, `conga json-schema admin-add-user`, `conga json-schema admin-add-team`
- **MCP tools**: `runtime` string parameter on `conga_admin_setup`, `conga_admin_add_user`, `conga_admin_add_team` tools

Both use the same validation and default behavior as the CLI flag.

### 8.4 Validation

- `--runtime` must be a registered runtime name (error otherwise)
- When `--runtime hermes` and no `--image` specified, require user to provide an image during setup (Hermes has no pre-built GHCR image)
- When adding agents to an existing environment, warn if the runtime differs from other agents (informational, not blocking)

## 9. Edge Cases & Error Handling

### 9.1 Mixed Runtimes

Agents with different runtimes can coexist in the same environment. Each agent's container uses its runtime's spec. The router handles mixed webhook paths via routing.json (each entry has the full URL including the runtime-specific path).

### 9.2 Runtime Mismatch on Refresh

`RefreshAgent` re-reads the agent's `Runtime` field and resolves the runtime. If someone manually edits the agent JSON to change the runtime, `RefreshAgent` will regenerate config for the new runtime and restart the container with the new spec. The old config file (e.g., `openclaw.json`) is left on disk but ignored.

### 9.3 Missing Image

If `rt.DefaultImage()` returns `""` (Hermes) and no image is configured, `ProvisionAgent` returns a clear error: `"no Docker image configured for runtime %q — set via 'conga admin setup' or --image flag"`.

### 9.4 Health Detection Timeout

`DetectReady` is called on log snapshots. If the container never reaches "ready" phase, `GetStatus` reports the current phase (e.g., "starting") indefinitely. This is unchanged behavior — we don't add timeout logic here.

### 9.5 Gateway Token Not Found

If `ReadGatewayToken()` returns `""` and `GatewayTokenDockerExec()` also returns `""`, `Connect` prints a note and returns a URL without a token. This is existing behavior, unchanged.

### 9.6 Unsupported Channel

If an agent has a Slack channel binding but the runtime's `WebhookPath("slack")` returns `""`, routing skips that binding and logs a warning: `"runtime %q does not support channel %q — skipping routing for agent %q"`.

### 9.7 Egress Proxy on Non-Node Runtimes

When `rt.SupportsNodeProxy()` returns false, the provider skips the proxy-bootstrap.js mount and `--require` injection. HTTP_PROXY/HTTPS_PROXY env vars are still set (most HTTP libraries respect them). Python's `requests` library honors these natively.

### 9.8 Behavior Files

Behavior file deployment (`deployBehavior()`) writes to the workspace directory. The workspace path is runtime-specific:
- OpenClaw: `{dataDir}/data/workspace/`
- Hermes: `{dataDir}/workspace/`

The provider asks the runtime for the workspace subdirectory path. Add a method or derive from `CreateDirectories` conventions. For simplicity, add:

```go
// WorkspacePath returns the relative path (within dataDir) to the agent's workspace.
WorkspacePath() string
```

OpenClaw: `"data/workspace"`, Hermes: `"workspace"`.

## 10. Import Cycle Resolution

**Problem**: `SharedSecrets` is defined in `pkg/common/config.go`. The Runtime interface in `pkg/runtime/` references it as a parameter type. The backward-compat wrappers in `pkg/common/` import `pkg/runtime/`. This creates a cycle: `common` → `runtime` → `common`.

**Solution**: Move `SharedSecrets` and the related helper types to `pkg/provider/` (where `AgentConfig` already lives). This is a data-only type with no logic dependencies.

```go
// pkg/provider/provider.go — add:
type SharedSecrets struct {
    Values             map[string]string
    GoogleClientID     string
    GoogleClientSecret string
}
```

The `ConfigParams` and `EnvParams` types in `pkg/runtime/` then reference `provider.SharedSecrets` instead of `common.SharedSecrets`. The backward-compat wrappers in `pkg/common/` can import `pkg/runtime/` without cycles because `pkg/runtime/` only imports `pkg/provider/` (not `pkg/common/`).

**Migration**: `pkg/common/` keeps a type alias during transition: `type SharedSecrets = provider.SharedSecrets`.

## 11. pkg/common/ Backward Compatibility

During the transition, `pkg/common/` retains thin wrappers so that callers not yet migrated to the Runtime interface continue working:

```go
// GenerateOpenClawConfig is a backward-compatible wrapper.
// Deprecated: Use runtime.Get("openclaw").GenerateConfig() instead.
func GenerateOpenClawConfig(agent provider.AgentConfig, secrets SharedSecrets, gatewayToken string) ([]byte, error) {
    rt, err := runtime.Get(runtime.RuntimeOpenClaw)
    if err != nil {
        return nil, err
    }
    return rt.GenerateConfig(runtime.ConfigParams{
        Agent:        agent,
        Secrets:      secrets,
        GatewayToken: gatewayToken,
    })
}
```

These wrappers are removed once all providers are migrated.

## 12. Testing Strategy

### 12.1 Runtime Contract Tests (pkg/runtime/runtime_test.go)

A shared test suite that **all** Runtime implementations must pass:

```go
func testRuntimeContract(t *testing.T, rt runtime.Runtime) {
    t.Run("Name is non-empty", func(t *testing.T) { ... })
    t.Run("ConfigFileName is non-empty", func(t *testing.T) { ... })
    t.Run("GenerateConfig returns valid bytes", func(t *testing.T) { ... })
    t.Run("GenerateEnvFile returns bytes", func(t *testing.T) { ... })
    t.Run("ContainerSpec has valid port", func(t *testing.T) { ... })
    t.Run("ContainerSpec has valid user", func(t *testing.T) { ... })
    t.Run("CreateDirectories creates expected structure", func(t *testing.T) { ... })
    t.Run("DetectReady returns valid phase", func(t *testing.T) { ... })
    t.Run("ReadGatewayToken round-trips with GenerateConfig", func(t *testing.T) { ... })
    t.Run("WebhookPath returns valid path for slack", func(t *testing.T) { ... })
}

func TestOpenClawContract(t *testing.T) {
    testRuntimeContract(t, openclaw.New())
}

func TestHermesContract(t *testing.T) {
    testRuntimeContract(t, hermes.New())
}
```

### 12.2 OpenClaw-Specific Tests

- Config generation output matches expected openclaw.json structure
- Env file includes NODE_OPTIONS
- Health detection recognizes all known log markers
- Token extraction parses both `gateway.token` and `gateway.auth.token`
- Directory layout matches AWS bootstrap expectations

### 12.3 Hermes-Specific Tests

- Config generation produces valid YAML
- Env file excludes NODE_OPTIONS
- Health detection recognizes Hermes log markers
- Token extraction parses `platforms.api_server.key`
- Config includes API server on port 8642

### 12.4 Routing Tests

- Mixed-runtime routing.json: an environment with one OpenClaw agent and one Hermes agent produces routing.json with `/slack/events` paths for the OpenClaw agent and `/webhooks/slack` paths for the Hermes agent
- Single-runtime routing.json: backward compatible with current output

### 12.5 Integration Tests

- Local provider + OpenClaw: existing behavior unchanged (regression)
- Local provider + Hermes: full lifecycle (provision → status → connect → teardown)
- Mixed agents: one OpenClaw + one Hermes on same local provider

## 13. File Impact Summary

### New Files

| File | Purpose |
|------|---------|
| `pkg/runtime/runtime.go` | Runtime interface, types, helpers |
| `pkg/runtime/registry.go` | Register/Get/Names registry |
| `pkg/runtime/runtime_test.go` | Contract test suite |
| `pkg/runtime/openclaw/runtime.go` | OpenClaw implementation + init() registration |
| `pkg/runtime/openclaw/config.go` | Config generation (moved from pkg/common/) |
| `pkg/runtime/openclaw/env.go` | Env file generation (moved from pkg/common/) |
| `pkg/runtime/openclaw/health.go` | Health detection (moved from localprovider/) |
| `pkg/runtime/openclaw/token.go` | Gateway token extraction (moved from localprovider/) |
| `pkg/runtime/openclaw/dirs.go` | Directory layout (moved from localprovider/) |
| `pkg/runtime/openclaw/container.go` | ContainerSpec (extracted from localprovider/) |
| `pkg/runtime/openclaw/openclaw-defaults.json` | Embedded defaults (moved from pkg/common/) |
| `pkg/runtime/hermes/runtime.go` | Hermes implementation + init() registration |
| `pkg/runtime/hermes/config.go` | YAML config generation |
| `pkg/runtime/hermes/env.go` | Env file generation |
| `pkg/runtime/hermes/health.go` | Health detection |
| `pkg/runtime/hermes/token.go` | Gateway token extraction |
| `pkg/runtime/hermes/dirs.go` | Directory layout |
| `pkg/runtime/hermes/container.go` | ContainerSpec |

### Modified Files

| File | Change |
|------|--------|
| `pkg/provider/provider.go` | Add `Runtime` field to `AgentConfig` |
| `pkg/provider/config.go` | Add `Runtime` field to `Config` |
| `pkg/provider/setup_config.go` | Add `Runtime` field to `SetupConfig` |
| `pkg/manifest/manifest.go` | Add `Runtime` fields to `Manifest` and `ManifestAgent` |
| `pkg/common/config.go` | Replace implementations with backward-compat wrappers |
| `pkg/common/routing.go` | Accept webhook path parameter |
| `pkg/common/ports.go` | Keep `NextAvailablePort` (runtime-agnostic) |
| `pkg/provider/localprovider/provider.go` | Delegate to Runtime interface |
| `pkg/provider/localprovider/docker.go` | Parameterize `runAgentContainer` |
| `pkg/provider/remoteprovider/provider.go` | Delegate to Runtime interface |
| `pkg/provider/remoteprovider/docker.go` | Parameterize container creation |
| `pkg/channels/slack/slack.go` | Update `RoutingEntries` to accept webhookPath param |
| `internal/cmd/root.go` | Add `--runtime` flag |
| `internal/cmd/admin.go` | Pass runtime to provider operations |

### Unchanged Files

| File | Why unchanged |
|------|---------------|
| `pkg/channels/channels.go` | Interface gains optional `webhookPath` param, OpenClaw methods stay |
| `pkg/policy/` | Egress policy is runtime-agnostic |
| `pkg/provider/iptables/` | iptables rules are runtime-agnostic |
| `router/src/index.js` | Unchanged in Phase 1 (gateway-only for Hermes) |
| `terraform/` | AWS changes deferred to Phase 6 |
