# High-Level Plan: Modular Deployment

## Architecture Overview

The refactoring introduces a **Provider** abstraction between the CLI commands and the infrastructure layer. Today, commands directly call AWS service interfaces (`SSMClient`, `SecretsManagerClient`, `EC2Client`) and use SSM `RunCommand` to execute scripts on the EC2 host. The new architecture replaces this with a high-level `Provider` interface that encapsulates all infrastructure operations.

```
┌─────────────────────────────────────────────────────┐
│                   CLI Commands                       │
│  (status, logs, refresh, secrets, admin, connect)    │
└──────────────────────┬──────────────────────────────┘
                       │ Provider interface
          ┌────────────┴────────────┐
          ▼                         ▼
┌──────────────────┐     ┌──────────────────┐
│   AWS Provider   │     │  Local Provider  │
│                  │     │                  │
│ SSM discovery    │     │ File discovery   │
│ Secrets Manager  │     │ Encrypted files  │
│ SSM RunCommand   │     │ Docker API/CLI   │
│ EC2 lifecycle    │     │ Direct container │
│ SSM tunnel       │     │ Localhost ports  │
└──────────────────┘     └──────────────────┘
          │                         │
          ▼                         ▼
┌──────────────────┐     ┌──────────────────┐
│  AWS (EC2 host   │     │  Local Docker    │
│  in hardened VPC)│     │  engine          │
└──────────────────┘     └──────────────────┘
```

Shared logic — config generation (`openclaw.json`), routing table generation (`routing.json`), behavior file composition, and Slack validation — lives in a new `common` package used by both providers.

## Provider Interface Design

```go
// cli/pkg/provider/provider.go
type Provider interface {
    // Identity & Discovery
    WhoAmI(ctx context.Context) (*Identity, error)
    ListAgents(ctx context.Context) ([]AgentConfig, error)
    GetAgent(ctx context.Context, name string) (*AgentConfig, error)
    ResolveAgentByIdentity(ctx context.Context) (*AgentConfig, error)

    // Agent Lifecycle
    ProvisionAgent(ctx context.Context, cfg AgentConfig) error
    RemoveAgent(ctx context.Context, name string, deleteSecrets bool) error

    // Container Operations
    GetStatus(ctx context.Context, agentName string) (*AgentStatus, error)
    GetLogs(ctx context.Context, agentName string, lines int) (string, error)
    RefreshAgent(ctx context.Context, agentName string) error
    RefreshAll(ctx context.Context) error

    // Secrets
    SetSecret(ctx context.Context, agentName, secretName, value string) error
    ListSecrets(ctx context.Context, agentName string) ([]SecretEntry, error)
    DeleteSecret(ctx context.Context, agentName, secretName string) error

    // Connectivity
    Connect(ctx context.Context, agentName string, localPort int) error

    // Environment Management
    Setup(ctx context.Context, manifest SetupManifest) error
    CycleHost(ctx context.Context) error
}
```

Shared types (`AgentConfig`, `AgentStatus`, `SecretEntry`, `Identity`, `SetupManifest`) move to the `provider` package so both implementations and all commands share them.

## Phases

### Phase 1: Extract Common Logic
**Goal**: Pull infrastructure-agnostic logic out of the AWS-specific code path into shared packages.

**Work**:
1. Create `cli/pkg/common/` package:
   - `config.go` — `GenerateOpenClawConfig(agent AgentConfig, sharedSecrets SharedSecrets) []byte` — the openclaw.json generation logic currently embedded in `user-data.sh.tftpl`
   - `routing.go` — `GenerateRoutingJSON(agents []AgentConfig) []byte` — routing table generation currently in the bootstrap script
   - `behavior.go` — `ComposeBehaviorFiles(agentType string, overrides map[string][]byte) map[string][]byte` — behavior file assembly logic
   - `ports.go` — `NextAvailablePort(agents []AgentConfig) int` — gateway port allocation (currently in `admin_provision.go`)
   - `validate.go` — Slack ID validators (already exist, just re-export)

2. Create `cli/pkg/provider/` package:
   - `provider.go` — Interface definition + shared types
   - `registry.go` — Provider registry (`Register(name, factory)`, `Get(name) Provider`)

**Validation**: Existing CLI tests pass. No behavioral change.

### Phase 2: AWS Provider Refactor
**Goal**: Wrap existing AWS code behind the Provider interface with zero behavioral changes.

**Work**:
1. Create `cli/pkg/provider/aws/` package:
   - `provider.go` — `AWSProvider` struct implementing `Provider` interface
   - Wraps existing `awsutil.Clients`, `discovery.*`, `tunnel.*` code
   - `WhoAmI` → calls `discovery.ResolveIdentity()`
   - `ListAgents` → calls `discovery.ListAgents()`
   - `GetStatus` / `GetLogs` / `RefreshAgent` → calls `awsutil.RunCommand()` with existing script templates
   - `SetSecret` / `ListSecrets` / `DeleteSecret` → calls `awsutil.SetSecret()` etc.
   - `Connect` → calls `tunnel.StartTunnel()`
   - `Setup` → reads manifest from SSM, prompts for missing values (existing `admin setup` logic)
   - `ProvisionAgent` → creates SSM param + runs provision script (existing `admin add-user/add-team` logic)
   - `CycleHost` → EC2 stop/start (existing `admin cycle-host` logic)

2. Refactor all `cli/cmd/*.go` files to use `Provider` instead of direct AWS calls:
   - Replace `clients.SSM` / `clients.EC2` / `clients.SecretsManager` with `provider.Method()`
   - `root.go` initializes provider based on config/flag

3. Register AWS provider: `provider.Register("aws", NewAWSProvider)`

**Validation**: All existing CLI tests pass. Manual verification of all commands against live AWS environment.

### Phase 3: Local Provider — Core
**Goal**: Implement the local Docker provider for container lifecycle and discovery.

**Work**:
1. Create `cli/pkg/provider/local/` package:
   - `provider.go` — `LocalProvider` struct implementing `Provider`
   - `config.go` — Local config management (`~/.conga/config.json`, `~/.conga/agents/`, `~/.conga/secrets/`)
   - `docker.go` — Docker operations (create network, run container, stop, remove, logs, inspect)
   - `secrets.go` — Local secrets storage (encrypted with `age` or OS keychain)
   - `network.go` — Network isolation (Docker network create with egress restrictions)

2. Local directory structure:
   ```
   ~/.conga/
   ├── config.json              # Provider selection, shared config
   ├── agents/
   │   ├── myagent.json           # Per-agent config (equivalent to SSM param)
   │   └── leadership.json
   ├── secrets/
   │   ├── shared/              # Shared secrets (Slack tokens, etc.)
   │   │   ├── slack-bot-token
   │   │   └── slack-signing-secret
   │   └── agents/
   │       ├── myagent/           # Per-agent secrets
   │       │   └── anthropic-api-key
   │       └── leadership/
   ├── data/                    # Container data volumes
   │   ├── myagent/
   │   │   └── openclaw.json
   │   └── leadership/
   ├── config/                  # Generated env files + routing.json
   │   ├── myagent.env
   │   ├── leadership.env
   │   └── routing.json
   ├── router/                  # Router source (copied from repo)
   │   ├── package.json
   │   └── src/index.js
   └── behavior/                # Behavior files (copied from repo)
       ├── base/
       ├── user/
       └── team/
   ```

3. Docker operations (via Docker CLI, not Docker SDK — keeps dependency light):
   - `docker network create conga-<agent> --internal` (no external access by default)
   - `docker run` with: `--cap-drop ALL`, `--no-new-privileges`, `--memory 2g`, `--network conga-<agent>`, env file mount, config volume mount
   - Router container connects to all agent networks

4. Implement `Provider` methods:
   - `WhoAmI` → returns local username
   - `ListAgents` / `GetAgent` → reads from `~/.conga/agents/*.json`
   - `ProvisionAgent` → creates agent JSON, generates config, creates Docker network + container
   - `GetStatus` → `docker inspect` + `docker stats`
   - `GetLogs` → `docker logs`
   - `RefreshAgent` → `docker restart` + re-inject env
   - `Connect` → direct localhost (no tunnel needed, port already bound)
   - `Setup` → create directory structure, prompt for secrets, pull image, setup router
   - `CycleHost` → `docker restart` all containers

5. Register: `provider.Register("local", NewLocalProvider)`

**Validation**: Unit tests for config generation, Docker command construction. Integration test with local Docker.

### Phase 4: Local Provider — Network Isolation
**Goal**: Enforce the same egress restrictions locally that the AWS security group provides.

**Work**:
1. **Per-agent Docker networks**: `--internal` flag prevents direct external access
2. **Egress proxy container**: A lightweight container (e.g., squid or nginx stream proxy) that:
   - Runs on a shared `conga-egress` network
   - Allows only HTTPS (443) and DNS (53) outbound
   - Each agent network connects to the egress network through this proxy
   - Agent containers use the proxy as their gateway
3. **Alternative (simpler)**: Use Docker's built-in `--dns` and iptables rules:
   - Set `--dns 8.8.8.8` (or configured DNS)
   - Use `iptables` / `nftables` rules on the Docker bridge to restrict egress to ports 443 and 53 only
   - This is platform-dependent (Linux vs macOS) and may require elevated privileges

**Recommendation**: Start with Docker `--internal` networks + an egress proxy container. It's more portable (works on macOS Docker Desktop) and doesn't require host-level iptables access.

**Validation**: Verify agent containers cannot reach arbitrary ports. Verify HTTPS and DNS work.

### Phase 5: Local Provider — Config Integrity & Router
**Goal**: Port the config integrity monitoring and router setup to local Docker.

**Work**:
1. **Router**: Same Node.js code, running as `conga-router` container:
   - Mount `router/` source code into container
   - Mount `routing.json` for hot-reload
   - Connect to all agent networks
   - SLACK_APP_TOKEN from local secrets

2. **Config integrity**: Lightweight container or host-side cron:
   - SHA256 check of `openclaw.json` every 5 minutes
   - Log mismatches to `~/.conga/logs/integrity.log`
   - Simpler than systemd timers — just `docker exec` or a sidecar

3. **Behavior file deployment**:
   - Copy from `behavior/` directory in the repo (or a configured path) into agent data directories
   - `conga admin refresh-all` re-syncs behavior files

**Validation**: Router connects to Slack, routes events correctly. Config integrity catches tampering.

### Phase 6: CLI Integration & Provider Selection
**Goal**: Wire provider selection into the CLI UX.

**Work**:
1. **Provider flag**: `--provider aws|local` on root command
2. **Persistent config**: After `conga admin setup --provider local`, store in `~/.conga/config.json`
3. **Auto-detection**: If `~/.conga/config.json` exists and has a provider, use it. If AWS credentials are available and no local config, default to `aws`.
4. **Init command update**: `conga init` asks for provider selection
5. **Help text**: Update all command help to mention provider-agnostic behavior

**Validation**: End-to-end test of full lifecycle with local provider. AWS provider regression test.

## Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Refactoring breaks existing AWS commands | High | Phase 2 is pure refactor — run all existing tests + manual verification before proceeding |
| Docker network egress restrictions differ between Linux/macOS | Medium | Use egress proxy pattern instead of host iptables |
| Local secrets encryption adds complexity | Medium | Start with file-mode 0400 (same as AWS env files on disk) + optional age encryption |
| Docker CLI dependency vs Docker SDK | Low | Docker CLI is universally available; SDK would add a heavy Go dependency |
| Router hot-reload behavior differs locally | Low | Same code, same mechanism — just different mount paths |

## Dependency Order

```
Phase 1 (common) → Phase 2 (AWS refactor) → Phase 3 (local core) → Phase 4 (network) → Phase 5 (integrity/router) → Phase 6 (CLI UX)
```

Phases 4 and 5 can run in parallel after Phase 3.

## Estimated Scope

- **Phase 1**: ~5 new files, ~300 lines extracted/moved
- **Phase 2**: ~14 files modified (all cmd/*.go + new provider/aws/), ~600 lines
- **Phase 3**: ~6 new files, ~800 lines
- **Phase 4**: ~2 new files + Docker config, ~200 lines
- **Phase 5**: ~2 new files, ~150 lines
- **Phase 6**: ~3 files modified, ~100 lines

**Total**: ~2,150 lines of new/modified Go code + Docker configurations
