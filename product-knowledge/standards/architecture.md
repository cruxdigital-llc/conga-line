<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-04-02
To modify: Edit directly. These standards are expected to evolve as the architecture matures.
-->

# Architecture Standards — Conga Line

> These standards govern how the codebase is structured. They apply to all new features
> and should be consulted during spec review. Existing code that predates a standard is
> not required to be retroactively refactored unless it blocks new work.

## Principles

1. **Provider contract is the API boundary** — Every feature that touches agent lifecycle must work across all three providers. The `Provider` interface is the contract. Provider-specific behavior is acceptable only when the interface explicitly allows it (e.g., `Connect()` returns a tunnel on remote/AWS, a localhost URL on local).
2. **Shared logic lives in common or its own package** — Config generation, routing, behavior composition, validation, and policy live outside provider packages. Provider packages contain only transport-specific code (SSH commands, SSM calls, Docker CLI invocations).
3. **Portable artifacts, provider-specific state** — Agent config, behavioral files, secrets naming, and policy are portable across providers. Provider-specific state (SSH config, IAM roles, SSM parameters) stays in the provider. Secrets don't promote — they're set per-environment.
4. **Secure by default, open by policy** — Agents are fully locked down at provisioning time (deny-all egress). The policy file (`conga-policy.yaml`) opens up specific capabilities (allowed domains, routing rules). When absent, enforcement is maximally restrictive. The policy file is optional — agents function without it, but with no outbound network access until a policy is applied.
5. **Channel abstraction over platform coupling** — Messaging platforms (Slack, Telegram, Discord, etc.) are channels, not core identity. Agent identity, provisioning, and security controls must not depend on any specific messaging platform. See the Channel Abstraction section below.

## Agent Data Safety

**Severity: must**

Agent data (memory, workspace, logs, canvas, identity) is the most valuable artifact in the system. It accumulates over time through agent interactions and cannot be regenerated. Every feature, operation, and infrastructure change must preserve agent data integrity.

### Data Locations

| Provider | Agent Data Path | Storage | Protection |
|---|---|---|---|
| AWS | `/opt/conga/data/<name>/` | Dedicated EBS volume (gp3, encrypted) | `prevent_destroy = true` in Terraform |
| Remote | `/opt/conga/data/<name>/` | Remote host disk | Operator-managed backups |
| Local | `~/.conga/data/<name>/` | Local disk | Operator-managed backups |

All three mount into the container at `/home/node/.openclaw:rw`.

### Rules

1. **Never delete, overwrite, or recreate agent data directories** during provisioning, refresh, teardown-and-rebuild, or upgrade operations. Data directories are created once during `ProvisionAgent` and persist through all subsequent operations.

2. **Container lifecycle must not affect data.** Container stop, start, remove, and recreate operations must preserve the volume mount. The `-v /path/to/data:/home/node/.openclaw:rw` mount is the contract — data lives on the host, not in the container.

3. **Refresh operations rebuild config, not data.** `RefreshAgent`, `RefreshAll`, and `CycleHost` may regenerate `openclaw.json`, env files, systemd units, and proxy containers. They must never touch the data directory contents.

4. **Teardown must preserve data by default.** `admin teardown` removes containers, networks, and config but must **not** delete data directories unless the user explicitly opts in. The default is to keep data. Deletion must be supported across all three interfaces:
   - **CLI**: `--delete-data` flag (default `false`)
   - **JSON input**: `"delete_data": true` field in the JSON schema
   - **MCP**: `delete_data` boolean parameter on the `conga_teardown` tool

   When deletion is requested, prompt for confirmation with a list of affected agents and data sizes before proceeding. The `--force` flag may skip the confirmation prompt, but never implies `--delete-data` — deletion must always be explicitly requested.

5. **Spec review must consider data impact.** Every spec that touches agent lifecycle, container operations, volume mounts, or directory structures must include a "Data Safety" section confirming that agent data is preserved. If a spec cannot guarantee data safety, it must be flagged as a blocking concern during persona review.

6. **Test for data persistence across operations.** Integration tests for lifecycle operations (provision → refresh → upgrade → teardown) should verify that data directory contents survive each transition.

## CLI Conventions

All new CLI commands must follow these patterns:

| Pattern | Implementation | Reference |
|---|---|---|
| Command structure | Cobra `*cobra.Command` with `RunE` handler, registered via `init()` | `internal/cmd/secrets.go` |
| Timeout | `commandContext()` for global timeout via `--timeout` flag | `internal/cmd/root.go` |
| Agent resolution | `resolveAgentName(ctx)` — uses `--agent` flag or identity-based resolution | `internal/cmd/root.go` |
| JSON output | Check `ui.OutputJSON`, emit via `ui.EmitJSON()` | `pkg/ui/json_output.go` |
| Human output | `ui.PrintTable(headers, rows)` for tabular data | `pkg/ui/table.go` |
| JSON input | `ui.JSONInputActive` + `ui.MustGetString()` for non-interactive mode | `pkg/ui/json_mode.go` |
| Error handling | Return `fmt.Errorf("context: %w", err)` — never swallow errors silently | `specs/2026-03-19_feature_cli-hardening/` |

## Interface Parity

**Severity: must**

Every CLI command must be fully operable through all three interfaces. A feature that only works interactively is incomplete.

| Interface | Purpose | Implementation |
|---|---|---|
| **CLI (human)** | Interactive flags, prompts, formatted output | Cobra flags + `ui.Confirm()` / `ui.Prompt()` |
| **JSON I/O (agent)** | Non-interactive automation via `--json` + `--output json` | `ui.JSONInputActive` + `ui.EmitJSON()` + `conga json-schema` |
| **MCP (tool use)** | LLM-driven operations via MCP tool calls | `mcpserver/tools*.go` tool registration with typed parameters |

### Rules

1. **Every new flag must have a JSON input field and an MCP parameter.** If a CLI command accepts `--delete-data`, the JSON schema must include `"delete_data"` and the MCP tool must accept a `delete_data` boolean parameter.

2. **Every new command must have an MCP tool.** CLI commands are registered in `internal/cmd/`, JSON schemas in `internal/cmd/json_schema.go`, and MCP tools in `internal/mcpserver/tools*.go`. All three must be updated together.

3. **Behavior must be identical across interfaces.** A `--force` flag in CLI, `"force": true` in JSON, and no confirmation prompt in MCP must all produce the same result. MCP tools inherently skip interactive prompts (the LLM is the user), but destructive operations should use `DestructiveHint: true` in tool annotations.

4. **Default values must be consistent.** If `--delete-data` defaults to `false` in CLI, it must also default to `false` in JSON input and MCP. Never infer different defaults per interface.

## Config Format Boundary

| Format | Use | Library | Rationale |
|---|---|---|---|
| **JSON** | Machine-generated config: agent config, openclaw.json, routing.json, setup config, provider config | `encoding/json` | Programmatic read/write, no ambiguity |
| **YAML** | Operator-authored policy: `conga-policy.yaml` | `gopkg.in/yaml.v3` | More readable for hand-authored domain lists and nested config |

New config files should default to JSON unless they are primarily hand-authored by operators. The policy file is the only YAML file — this is intentional, not accidental.

## Module Structure

**Severity: must**

The Go module is `github.com/cruxdigital-llc/conga-line` with `go.mod` at the repo root. The codebase is split into two top-level Go directories with distinct visibility rules:

| Directory | Visibility | Purpose |
|---|---|---|
| `pkg/` | **Public** — importable by external modules | Core library: provider interface, policy engine, channels, common utilities |
| `internal/` | **Private** — only the `conga` binary can import | Interface layers: CLI commands (`internal/cmd/`), MCP server (`internal/mcpserver/`) |

**The rule:** If an external consumer (like `terraform-provider-conga`) needs to import it, it belongs in `pkg/`. If it's specific to a particular interface (CLI flags, MCP tool registration), it belongs in `internal/`.

The binary entry point is at `cmd/conga/main.go` — it imports `internal/cmd` and calls `cmd.Execute()`.

### Why this matters

The provider interface, policy engine, and channel system are shared by three consumers: the CLI, the MCP server, and the Terraform provider. Putting shared logic in `internal/` or coupling it to a specific interface breaks external consumers. Putting interface-specific code in `pkg/` pollutes the public API.

## Package Boundaries

| Package | Owns | Does NOT Own |
|---|---|---|
| `pkg/channels/` | Channel interface, registry, shared types (`ChannelBinding`, `SecretDef`, `RoutingEntry`) | Channel-specific implementation |
| `pkg/channels/{name}/` | Platform-specific implementation for one channel (validation, config, routing, secrets) | Cross-channel logic |
| `pkg/provider/` | Provider interface, registry, shared types (`AgentConfig`, `AgentStatus`) | Provider-specific implementation |
| `pkg/provider/{name}provider/` | Transport-specific code for one provider | Shared logic, cross-provider behavior |
| `pkg/common/` | Config generation, routing, behavior composition, validation | Policy, provider interface, CLI commands |
| `pkg/policy/` | Policy schema, parsing, validation, enforcement reporting | Enforcement logic (that's in providers) |
| `internal/cmd/` | CLI commands, flag parsing, user interaction | Business logic (delegate to providers/packages) |
| `internal/mcpserver/` | MCP tool registration, tool handlers | Business logic (delegate to providers/packages) |

New packages are preferred over growing existing ones when the domain is distinct. `policy/` was created rather than adding to `common/` because policy is a separate concern with its own lifecycle.

## Channel Abstraction

### Current State

Slack is decoupled from the core via the `Channel` interface (`pkg/channels/`):
- `AgentConfig` has `Channels []channels.ChannelBinding` — platform-agnostic bindings
- `GenerateOpenClawConfig()` delegates to `ch.OpenClawChannelConfig()` per binding
- `GenerateRoutingJSON()` delegates to `ch.RoutingEntries()` per binding
- `GenerateEnvFile()` delegates to `ch.AgentEnvVars()` per binding
- Validation delegated to `ch.ValidateBinding()` — each channel owns its ID format
- Secrets declared via `ch.SharedSecrets()` — each channel owns its secret names
- `SharedSecrets.Values map[string]string` keyed by secret name, not Slack-specific fields
- Router (`router/src/index.js`) remains Slack-specific (Socket Mode, event parsing)
- Gateway-only mode works with zero channel bindings (no `channels` section in openclaw.json)

### Standard: New Code Must Not Deepen Slack Coupling

**Severity: should**

New features must not introduce additional Slack-specific logic outside of the existing Slack integration points. Specifically:

1. **Agent identity must not depend on channel IDs.** Agent name is the primary key. Channel bindings are optional. New features that need to identify agents must use agent name, not platform-specific IDs.

2. **Policy must remain channel-agnostic.** The `conga-policy.yaml` schema must not contain Slack-specific fields. Egress rules, routing, and posture declarations apply regardless of messaging platform.

3. **New CLI commands must not assume Slack.** Commands that operate on agents (status, logs, secrets, connect, pause, refresh) must work without Slack configured. Commands that configure messaging should accept platform as a parameter.

4. **Security controls must not depend on Slack constructs.** Channel allowlists are a security boundary, but the enforcement mechanism (which channels an agent responds to) should be expressible for any platform, not just Slack channel IDs.

### Package Structure: `pkg/channels/`

Channel integrations live in `pkg/channels/`, one subdirectory per platform:

```
pkg/channels/
  channels.go  — Channel interface + shared types (ChannelBinding, SecretDef, RoutingEntry)
  registry.go  — Register/Get/All + ParseBinding("platform:id")
  slack/       — Slack Channel implementation (validation, config, routing, secrets)
  telegram/    — (future) Telegram bot integration
  discord/     — (future) Discord bot integration
```

Each channel package owns:
- **Connection proxy** — a single persistent connection to the platform, shared across all agents. See the Connection Proxy Pattern below.
- **Event normalization** — translate platform events into a common internal format for fan-out
- **Routing logic** — map platform-specific identifiers (channel IDs, user IDs) to agent webhook URLs
- **Validation** — platform-specific ID format validation (Slack `^U[A-Z0-9]{10}$`, Telegram numeric, etc.)
- **Secrets** — platform-specific token names and setup prompts
- **OpenClaw config generation** — produce the `channels.{platform}` section of `openclaw.json`

The existing `router/src/index.js` (Node.js Slack router) is the current `channels/slack/` implementation. When the router is rewritten in Go (see ROADMAP.md backlog), it should land in `channels/slack/` rather than being generalized prematurely.

"Channels" is OpenClaw's own term for messaging platform integrations. The `channels` section of `openclaw.json` is the top-level key containing per-platform config (Slack, Telegram, Discord, WhatsApp, Signal, iMessage, MS Teams, Matrix, webchat). Our `channels/` directory maps directly: `channels/slack/` generates the `channels.slack` config, `channels/telegram/` would generate `channels.telegram`, etc.

### Connection Proxy Pattern

Many messaging platforms use a single persistent connection (WebSocket, long-poll) per app/bot token. When multiple agents share that connection, the channel package must act as a **connection proxy**: one upstream connection, fan-out to many downstream agent containers.

This is the architecture the Slack router already implements:

```
Platform API
    │
    │  single persistent connection (Socket Mode WebSocket)
    ▼
┌──────────────────┐
│  Channel Proxy   │  one per platform, holds the connection
│  (channels/slack)│  routes events by channel/user ID → agent
└──────┬───┬───┬───┘
       │   │   │     HTTP webhook fan-out
       ▼   ▼   ▼
    Agent  Agent  Agent
    (conga-a) (conga-b) (conga-c)
```

**Why this matters:** Without the proxy, each agent would need its own platform connection. For Slack Socket Mode, multiple connections to the same app cause ~50% event loss (Slack load-balances across connections). For Telegram, each bot token supports only one `getUpdates` long-poll or one webhook URL. The proxy is not optional — it's architecturally required for multi-agent deployments on most platforms.

**One proxy per platform.** A single Slack proxy serves all agents on the host. A single Telegram proxy (when added) would do the same. Each platform runs as its own process/container — a Slack WebSocket disconnect should not affect Telegram polling. This gives independent restart, independent logging, and simpler failure isolation.

**Each channel proxy must:**
1. Hold a single persistent connection to the platform using one set of shared credentials
2. Route incoming events to the correct agent container based on platform-specific identifiers (channel ID, user ID, group ID)
3. Sign forwarded requests so agent containers can verify the source (Slack: HMAC-SHA256 signature + timestamp headers)
4. Deduplicate events (Slack: 30-second TTL by event ID; other platforms may have their own retry semantics)
5. Filter platform noise (bot echoes, duplicate event types, system messages) before fan-out
6. Reconnect automatically on connection loss without losing events

**Per-agent containers use HTTP webhook mode** (`mode: "http"` in `openclaw.json`) — they never connect to the platform directly. The proxy forwards events with signed HTTP requests to each container's webhook endpoint (`http://conga-{name}:{port}/{platform}/events`).

### Adding a New Channel

To add a second messaging platform (e.g., Telegram):

1. Create `pkg/channels/telegram/telegram.go` implementing the `Channel` interface
2. Register via `init()` — `channels.Register(&Telegram{})`
3. Add `_ "...channels/telegram"` import to `internal/cmd/root.go`
4. Add a connection proxy container (if the platform needs one) analogous to the Slack router
5. No changes needed to `common/` or `provider/` — the interface handles everything

The `Channel` interface covers: validation, secrets, OpenClaw config generation, plugin config, routing entries, agent/router env vars, webhook paths, and behavior template vars. All delegated per-platform.

## Testing Conventions

| Convention | Rationale |
|---|---|
| Unit tests in `_test.go` alongside source | Standard Go convention |
| Table-driven tests for validation functions | Pattern established in `common/validate_test.go`, `policy/policy_test.go` |
| `t.Helper()` on test helper functions | Clean stack traces on failure |
| `t.TempDir()` for temp files | Auto-cleanup, no manual teardown |
| No mocks for pure functions | Test real behavior; mocks only for external services (AWS SDK, SSH) |
| Test file loading with `os.IsNotExist` path | Every `Load()` function must handle missing files gracefully |
