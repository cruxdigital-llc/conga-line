# Requirements: Agent Portability

## Goal

Make Conga Line runtime-agnostic so it can deploy and manage any compatible AI agent runtime — not just OpenClaw. Hermes Agent is the first alternative runtime. All existing Conga capabilities (multi-agent orchestration, security hardening, egress control, channel management, provider portability) remain intact regardless of which runtime is deployed.

## Motivation

Conga Line's value proposition is **infrastructure orchestration and security hardening** for AI agents, not the agent runtime itself. Coupling to OpenClaw limits adoption and creates vendor lock-in. Users should be able to choose their agent runtime (OpenClaw, Hermes, future runtimes) while getting the same deployment, isolation, and governance layer from Conga.

## Non-Goals

- Adding new communication channel support (Telegram, Discord, etc.) at the Conga layer — Conga manages Slack today; that scope is unchanged
- Changing the Provider interface (AWS, local, remote) — this feature adds a **Runtime** dimension orthogonal to providers
- Building a Hermes Docker image — users supply their own image (or we document how to build one)
- Supporting runtimes that don't run as long-lived Docker containers

## Key Design Decisions

1. **Conga remains the routing/control layer**: The external Slack router model stays. All runtimes receive events via HTTP webhook from the Conga router, regardless of the runtime's native channel support. This preserves centralized access control.
2. **Conga generates each runtime's native config**: Just as Conga generates `openclaw.json`, it will generate `config.yaml` for Hermes (and whatever future runtimes need). The Conga data model is the source of truth; runtime-specific config is a derived artifact.
3. **Slack-only channel scope**: The channel abstraction (`pkg/channels/`) already handles Slack. No new channel types are added as part of this feature. If a runtime natively supports more platforms, those are not exposed through Conga's management layer.
4. **Local provider first**: End-to-end verification on the local Docker provider. Remote and AWS providers follow in subsequent phases.

## Functional Requirements

### FR-1: Runtime Interface

A new `Runtime` interface in `pkg/runtime/` that encapsulates all agent-runtime-specific behavior:

- **Config generation**: Produce the runtime's native config file (JSON, YAML, etc.) from Conga's data model (agent config, secrets, channels, gateway settings)
- **Container spec**: Docker image, container user/group, memory limits, environment variables, volume mounts, port mappings
- **Health detection**: Determine container readiness from logs or health endpoints
- **Token management**: Read/write gateway authentication tokens from/to the running container
- **Directory layout**: Define the container-internal paths (config file, data directory, workspace)
- **Webhook path**: The HTTP path where the router delivers Slack events
- **Env file generation**: Produce the `.env` or environment variable set the runtime needs

### FR-2: Runtime Registry

A registry pattern (similar to Provider registry) allowing runtimes to self-register:

- `runtime.Register(name, factory)`
- `runtime.Get(name)` returns a configured Runtime instance
- Built-in runtimes: `openclaw`, `hermes`

### FR-3: OpenClaw Runtime (Extraction)

Extract all existing OpenClaw-specific logic from `pkg/common/` and provider implementations into `pkg/runtime/openclaw/`:

- `GenerateOpenClawConfig()` moves from `pkg/common/config.go`
- `openclaw-defaults.json` moves with it
- Health detection log markers (`[gateway] listening`, `[slack] starting provider`, etc.)
- `NODE_OPTIONS`, heap size, proxy bootstrap injection
- Container path constants (`/home/node/.openclaw/`, port 18789)
- Token extraction JavaScript
- Directory creation logic (data/workspace, memory, logs, etc.)

**Zero behavioral change** — existing OpenClaw deployments must work identically.

### FR-4: Hermes Runtime (New)

Implement `pkg/runtime/hermes/` supporting:

- Config generation: produce `config.yaml` from Conga's agent config + secrets
- Container spec: Python-based image, appropriate user, memory limits, port mapping (8642 for API server, or a webhook port for Slack events)
- Health detection: Hermes-specific log markers or `/health` endpoint on port 8642
- Slack integration via HTTP webhook mode (Hermes receives events from Conga router, not direct Socket Mode)
- Env file generation: `.env` format with API keys, Slack tokens, etc.
- Directory layout: `/opt/data` or equivalent inside container

### FR-5: CLI Integration

- `--runtime openclaw|hermes` flag on `conga admin setup` (persisted in config alongside `--provider`)
- `--runtime` flag on `conga admin add-user` and `conga admin add-team`
- Per-agent runtime stored in agent JSON config (`AgentConfig.Runtime` field)
- Default runtime configurable (default: `openclaw` for backward compatibility)
- `conga status` shows runtime alongside provider info

### FR-6: Provider-Runtime Composition

Providers delegate to the Runtime interface for all runtime-specific operations:

- `ProvisionAgent` calls `runtime.GenerateConfig()`, `runtime.ContainerSpec()`, `runtime.CreateDirectories()`
- `RefreshAgent` calls `runtime.GenerateConfig()` to regenerate config
- `ConnectAgent` calls `runtime.GatewayToken()` to extract the auth token
- Health checks call `runtime.DetectReady()`
- The Provider handles infrastructure (Docker, SSH, AWS); the Runtime handles agent-specific behavior

### FR-7: Manifest Support

The `conga bootstrap` manifest YAML gains an optional `runtime:` field (default: `openclaw`). Per-agent runtime override also supported.

### FR-8: Backward Compatibility

- Existing deployments with no `--runtime` flag default to `openclaw`
- Existing agent JSON files without a `Runtime` field are treated as `openclaw`
- No migration required for current users
- All existing tests continue to pass

## Non-Functional Requirements

### NFR-1: Extensibility

Adding a third runtime should require:
1. A new package under `pkg/runtime/<name>/` implementing the Runtime interface
2. A `Register()` call in `init()`
3. No changes to providers, CLI commands, or core logic

### NFR-2: Test Coverage

- Unit tests for the Runtime interface contract (shared test suite that all runtimes must pass)
- Unit tests for config generation per runtime
- Integration test: local provider + openclaw runtime (existing behavior)
- Integration test: local provider + hermes runtime (new)

### NFR-3: Security Parity

All security hardening applies regardless of runtime:
- Egress proxy / iptables enforcement
- Per-agent network isolation
- Non-root container execution (`--user`)
- File permissions (0400 secrets)
- Config integrity monitoring

## Success Criteria

1. `conga admin setup --provider local --runtime hermes` completes successfully
2. `conga admin add-user --name test --runtime hermes` provisions a Hermes container that starts and becomes healthy
3. `conga status` shows the agent with runtime=hermes, healthy
4. `conga connect --agent test` opens the Hermes web gateway
5. Existing `--runtime openclaw` (or default) behavior is unchanged
6. All existing tests pass
7. At least one provider (local) fully verified end-to-end with both runtimes
