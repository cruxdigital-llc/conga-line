# Plan: Agent Portability

## Approach

Introduce a **Runtime** interface orthogonal to the existing **Provider** interface. Providers handle *where* agents run (AWS, local Docker, remote SSH). Runtimes handle *what* agent runs (OpenClaw, Hermes, future runtimes). The composition is `Provider × Runtime` — any provider works with any runtime.

```
                 ┌─────────────┐
                 │   Conga CLI  │
                 └──────┬───────┘
                        │
            ┌───────────┼───────────┐
            ▼           ▼           ▼
       ┌─────────┐ ┌─────────┐ ┌─────────┐
       │  Local   │ │ Remote  │ │   AWS   │    ← Provider (where)
       │ Provider │ │ Provider│ │ Provider│
       └────┬─────┘ └────┬────┘ └────┬────┘
            │             │           │
            └──────┬──────┘───────────┘
                   │
            ┌──────┴───────┐
            ▼              ▼
       ┌──────────┐  ┌──────────┐
       │ OpenClaw  │  │  Hermes  │              ← Runtime (what)
       │  Runtime  │  │  Runtime │
       └──────────┘  └──────────┘
```

## Phases

### Phase 1: Runtime Interface & Registry (~pkg/runtime/)

**Goal**: Define the abstraction and registry, mirroring the Provider pattern.

**New files**:
- `pkg/runtime/runtime.go` — Runtime interface
- `pkg/runtime/registry.go` — Register/Get/Names functions

**Runtime interface** (methods):

```go
type Runtime interface {
    // Name returns the runtime identifier ("openclaw", "hermes").
    Name() string

    // GenerateConfig produces the runtime's native config file bytes
    // (openclaw.json for OpenClaw, config.yaml for Hermes).
    GenerateConfig(agent provider.AgentConfig, secrets common.SharedSecrets, gatewayToken string) ([]byte, error)

    // ConfigFileName returns the config file name ("openclaw.json", "config.yaml").
    ConfigFileName() string

    // GenerateEnvFile produces the .env file content for this runtime.
    GenerateEnvFile(agent provider.AgentConfig, secrets common.SharedSecrets, perAgent map[string]string) []byte

    // ContainerSpec returns Docker container parameters for this runtime.
    ContainerSpec(agent provider.AgentConfig, image string) ContainerSpec

    // CreateDirectories creates the runtime-specific directory structure
    // inside the agent's data directory.
    CreateDirectories(dataDir string) error

    // DetectReady parses container log output and returns the readiness phase.
    DetectReady(logOutput string) ReadyPhase

    // ExtractGatewayToken returns a command or script that extracts the
    // gateway auth token from inside the running container.
    ExtractGatewayToken() TokenExtractor

    // WebhookPath returns the HTTP path for router event delivery
    // (e.g. "/slack/events" for OpenClaw).
    WebhookPath(platform string) string

    // DefaultImage returns the default Docker image for this runtime.
    DefaultImage() string

    // ChannelConfig produces the runtime-native channel configuration section
    // for a given channel binding.
    ChannelConfig(agentType string, binding channels.ChannelBinding, secrets map[string]string) (map[string]any, error)
}
```

**Supporting types**:

```go
type ContainerSpec struct {
    ContainerPort int           // Port inside the container (18789 for OpenClaw, 8642 for Hermes)
    User          string        // "1000:1000"
    Memory        string        // "2g"
    CPUs          string        // "0.75"
    PIDsLimit     string        // "256"
    VolumeMounts  []VolumeMount // [{HostPath, ContainerPath, ReadOnly}]
    EnvVars       map[string]string // Runtime-specific env vars (NODE_OPTIONS, etc.)
    Entrypoint    []string      // Override entrypoint if needed
}

type ReadyPhase struct {
    Phase   string // "starting", "gateway up", "loading", "ready", "error"
    Message string // Human-readable description
}

type TokenExtractor struct {
    // For OpenClaw: JavaScript executed via `docker exec node -e "..."`
    // For Hermes: Python or shell command, or read from a known file
    Command []string // Command to run inside container
    Parse   func(stdout string) string // Extract token from output
}
```

### Phase 2: Extract OpenClaw Runtime (~pkg/runtime/openclaw/)

**Goal**: Move all OpenClaw-specific logic out of `pkg/common/` and provider code into a self-contained runtime package. **Zero behavioral change.**

**What moves**:

| From | To | What |
|------|----|------|
| `pkg/common/config.go` → `GenerateOpenClawConfig()` | `pkg/runtime/openclaw/config.go` | Config generation |
| `pkg/common/openclaw-defaults.json` | `pkg/runtime/openclaw/` | Embedded defaults |
| `pkg/common/config.go` → `buildGatewayConfig()` | `pkg/runtime/openclaw/config.go` | Gateway config |
| `pkg/common/config.go` → `GenerateEnvFile()` | `pkg/runtime/openclaw/env.go` | Env file (NODE_OPTIONS, heap size) |
| `pkg/common/ports.go` → `BaseGatewayPort` | `pkg/runtime/openclaw/` | Port constant (18789) |
| Providers → directory creation logic | `pkg/runtime/openclaw/dirs.go` | /home/node/.openclaw subdirs, MEMORY.md |
| Providers → `detectReadyPhase()` | `pkg/runtime/openclaw/health.go` | Log marker parsing |
| Providers → gateway token JavaScript | `pkg/runtime/openclaw/token.go` | Node.js extraction script |
| `pkg/channels/slack/` → `OpenClawChannelConfig()` | Stays, but called via Runtime interface | Slack config (already channel-scoped) |
| Providers → default image `ghcr.io/openclaw/openclaw:latest` | `pkg/runtime/openclaw/` | Default image constant |
| Providers → container opts (user, memory, NODE_OPTIONS) | `pkg/runtime/openclaw/container.go` | ContainerSpec |

**What stays in `pkg/common/`**:
- `SharedSecrets` struct (runtime-agnostic)
- `HasAnyChannel()`, `BuildChannelStatuses()`, `BuildRouterEnvContent()` (channel-layer, not runtime-specific)
- Routing JSON generation (runtime calls `WebhookPath()`, routing uses it)
- Validation, secrets helpers
- `NextAvailablePort()` — moves to `pkg/runtime/` as a shared utility since different runtimes start from different base ports

**Backward compatibility shims in `pkg/common/`**:
- `GenerateOpenClawConfig()` becomes a thin wrapper: calls `openclaw.Runtime.GenerateConfig()`
- `GenerateEnvFile()` becomes a thin wrapper: calls `openclaw.Runtime.GenerateEnvFile()`
- `GenerateAgentFiles()` becomes runtime-aware: takes a `Runtime` parameter

These shims allow incremental migration — providers can be updated one at a time.

### Phase 3: Wire Providers to Runtime Interface

**Goal**: Providers call the Runtime instead of hardcoded OpenClaw logic.

**Changes per provider**:

**Local provider** (`pkg/provider/localprovider/`):
- `ProvisionAgent()`: Replace direct `common.GenerateOpenClawConfig()` calls with `runtime.GenerateConfig()`. Replace hardcoded directory creation with `runtime.CreateDirectories()`. Replace hardcoded `runAgentContainer()` opts with `runtime.ContainerSpec()`.
- `RefreshAgent()`: Use `runtime.GenerateConfig()` and `runtime.ConfigFileName()`.
- `GetStatus()` → `detectReadyPhase()`: Delegate to `runtime.DetectReady()`.
- `Connect()`: Use `runtime.ExtractGatewayToken()` instead of inline JavaScript.
- `runAgentContainer()`: Accept `ContainerSpec` instead of hardcoded opts. Volume mount, port, user, env vars all come from the spec.

**Remote provider** (`pkg/provider/remoteprovider/`):
- Same pattern as local. SSH commands parameterized by `ContainerSpec` and `runtime.ConfigFileName()`.

**AWS provider** (`pkg/provider/awsprovider/`):
- Config generation in SSM scripts uses embedded shell templates. These need the runtime's config structure. Approach: generate config on the CLI side, upload via SSM (already partially done for some operations).

**Routing** (`pkg/common/routing.go`):
- `GenerateRoutingJSON()` needs the runtime's webhook path. Currently hardcoded in `pkg/channels/slack/slack.go` as `/slack/events`. The Runtime interface provides `WebhookPath("slack")` so different runtimes can use different paths.

### Phase 4: Hermes Runtime (~pkg/runtime/hermes/)

**Goal**: Implement the Hermes runtime.

**New files**:
- `pkg/runtime/hermes/runtime.go` — implements Runtime interface
- `pkg/runtime/hermes/config.go` — generates `config.yaml`
- `pkg/runtime/hermes/env.go` — generates `.env`
- `pkg/runtime/hermes/health.go` — health detection (log markers or `/health` endpoint)
- `pkg/runtime/hermes/token.go` — gateway token extraction
- `pkg/runtime/hermes/container.go` — ContainerSpec
- `pkg/runtime/hermes/dirs.go` — directory layout

**Key implementation details**:

- **Config**: Generate YAML (`config.yaml`) with Hermes's expected structure. Gateway section points to Hermes's API server. Slack configured in HTTP webhook mode (events received from Conga router, not direct Socket Mode).
- **Container**: Python-based image, port 8642 (API server), user 1000:1000, memory 2g. Volume mount to `/opt/data`.
- **Health**: Parse logs for Hermes startup markers, or poll `http://localhost:8642/health`.
- **Webhook**: Hermes has a webhook endpoint for receiving Slack events — determine the path from Hermes docs/source.
- **Env file**: Standard dotenv with `ANTHROPIC_API_KEY`, `SLACK_BOT_TOKEN`, etc.
- **No NODE_OPTIONS**: Hermes is Python, not Node.js. No heap size tuning needed.
- **Egress proxy**: HTTP_PROXY/HTTPS_PROXY still work (Python requests library respects these). No Node.js-specific proxy bootstrap needed.

**Open questions** (resolve during spec/implementation):
1. Does Hermes support HTTP webhook mode for Slack (receiving events from an external router), or only direct Socket Mode? If only Socket Mode, we may need to contribute webhook support upstream or use a different approach.
2. What's the exact health check mechanism? Log markers or HTTP endpoint?
3. Does Hermes have a gateway auth token mechanism, or do we need to add one?

### Phase 5: CLI & Data Model Changes

**Goal**: Add `--runtime` flag and persist runtime choice.

**Changes**:

- `AgentConfig` gains `Runtime string` field (default: `"openclaw"`)
- `SetupConfig` gains `Runtime string` field
- `--runtime openclaw|hermes` flag added to:
  - `conga admin setup` (persisted to config alongside provider)
  - `conga admin add-user` / `conga admin add-team` (persisted per-agent)
- `conga status` displays runtime in output
- `conga bootstrap` manifest gains `runtime:` field
- Config file (`~/.conga/config.json` / `~/.conga/local-config.json` / `~/.conga/remote-config.json`) gains `runtime` key

**Backward compatibility**: Missing `Runtime` field defaults to `"openclaw"`.

### Phase 6: Remote & AWS Provider Integration

**Goal**: Extend the runtime abstraction to remote and AWS providers.

**Remote provider**: Same pattern as local — SSH commands parameterized by Runtime.

**AWS provider**: 
- Shell template scripts (`add-user.sh.tmpl`, `add-team.sh.tmpl`) need runtime-aware config generation. Approach: have the CLI generate the config file locally, then upload it via SSM/S3, rather than generating it in shell.
- systemd unit templates parameterized by `ContainerSpec`.
- Bootstrap script (`user-data.sh.tftpl`) gains runtime awareness.

### Phase 7: Testing & Verification

**Goal**: Comprehensive test coverage.

- **Runtime contract tests**: Shared test suite that any Runtime implementation must pass (generates valid config, creates expected directories, returns valid ContainerSpec, etc.)
- **OpenClaw runtime tests**: Existing tests refactored to test via Runtime interface. All must pass unchanged.
- **Hermes runtime tests**: Config generation, env file, directory layout, health detection.
- **Integration test**: Local provider + Hermes runtime end-to-end (provision, status, connect, teardown).
- **Regression**: All existing tests pass with no changes (OpenClaw path unchanged).

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Hermes doesn't support HTTP webhook Slack mode | Blocks router integration | Research during Phase 4; fallback: contribute upstream or use API server as webhook target |
| Large refactor surface in Phase 2-3 | Regression risk | Phase 2 is pure extraction with zero behavioral change; backward compat shims allow incremental migration |
| AWS shell templates are hard to parameterize | Delays Phase 6 | Phase 6 is explicitly after local verification; can ship local+remote first |
| Runtime interface too broad or too narrow | Rework in later phases | Start with OpenClaw extraction (we know exactly what it needs), then validate with Hermes |

## Implementation Order

1. **Phase 1** → Interface & registry (foundation)
2. **Phase 2** → Extract OpenClaw (biggest phase, zero behavior change, de-risks everything)
3. **Phase 3** → Wire local provider first (validates the abstraction)
4. **Phase 5** → CLI changes (needed before Hermes can be selected)
5. **Phase 4** → Hermes runtime (the payoff)
6. **Phase 7** → Testing (continuous, but formal verification here)
7. **Phase 6** → Remote & AWS (extend to all providers)

Phases 1-3 can be shipped as a standalone PR (pure refactor, no new features, all tests pass). Phases 4-5 are the "add Hermes" PR. Phase 6 extends to all providers. Phase 7 is continuous.
