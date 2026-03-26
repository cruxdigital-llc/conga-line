<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-25
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
4. **No enforcement without policy** — The policy file (`conga-policy.yaml`) is optional. When absent, all behavior is unchanged. Features must never require the policy file to function. This prevents breaking existing deployments.
5. **Channel abstraction over platform coupling** — Messaging platforms (Slack, Telegram, Discord, etc.) are channels, not core identity. Agent identity, provisioning, and security controls must not depend on any specific messaging platform. See the Channel Abstraction section below.

## CLI Conventions

All new CLI commands must follow these patterns:

| Pattern | Implementation | Reference |
|---|---|---|
| Command structure | Cobra `*cobra.Command` with `RunE` handler, registered via `init()` | `cli/cmd/secrets.go` |
| Timeout | `commandContext()` for global timeout via `--timeout` flag | `cli/cmd/root.go:117` |
| Agent resolution | `resolveAgentName(ctx)` — uses `--agent` flag or identity-based resolution | `cli/cmd/root.go:131` |
| JSON output | Check `ui.OutputJSON`, emit via `ui.EmitJSON()` | `cli/internal/ui/json_output.go` |
| Human output | `ui.PrintTable(headers, rows)` for tabular data | `cli/internal/ui/table.go` |
| JSON input | `ui.JSONInputActive` + `ui.MustGetString()` for non-interactive mode | `cli/internal/ui/json_input.go` |
| Error handling | Return `fmt.Errorf("context: %w", err)` — never swallow errors silently | `specs/2026-03-19_feature_cli-hardening/` |

## Config Format Boundary

| Format | Use | Library | Rationale |
|---|---|---|---|
| **JSON** | Machine-generated config: agent config, openclaw.json, routing.json, setup config, provider config | `encoding/json` | Programmatic read/write, no ambiguity |
| **YAML** | Operator-authored policy: `conga-policy.yaml` | `gopkg.in/yaml.v3` | More readable for hand-authored domain lists and nested config |

New config files should default to JSON unless they are primarily hand-authored by operators. The policy file is the only YAML file — this is intentional, not accidental.

## Package Boundaries

| Package | Owns | Does NOT Own |
|---|---|---|
| `cli/internal/provider/` | Provider interface, registry, shared types (`AgentConfig`, `AgentStatus`) | Provider-specific implementation |
| `cli/internal/provider/{name}provider/` | Transport-specific code for one provider | Shared logic, cross-provider behavior |
| `cli/internal/common/` | Config generation, routing, behavior composition, validation | Policy, provider interface, CLI commands |
| `cli/internal/policy/` | Policy schema, parsing, validation, enforcement reporting | Enforcement logic (that's in providers) |
| `cli/cmd/` | CLI commands, flag parsing, user interaction | Business logic (delegate to providers/packages) |

New packages are preferred over growing existing ones when the domain is distinct. `policy/` was created rather than adding to `common/` because policy is a separate concern with its own lifecycle.

## Channel Abstraction

### Current State

Slack is deeply coupled into the codebase:
- `AgentConfig` has `SlackMemberID` and `SlackChannel` fields
- `GenerateOpenClawConfig()` hardcodes Slack plugin config and `/slack/events` webhook path
- Router (`router/src/index.js`) is 100% Slack-specific (Socket Mode, Slack event parsing, Slack signature verification)
- Routing config assumes Slack's two-tier model (channel IDs → URLs, member IDs → URLs)
- ID validation is Slack-format-specific (`^U[A-Z0-9]{10}$`, `^C[A-Z0-9]{10}$`)
- Shared secrets are Slack-named (`SlackBotToken`, `SlackSigningSecret`, `SlackAppToken`)

However, gateway-only mode already proves agents don't require Slack — the `HasSlack()` check allows agents to run with only the web UI.

### Standard: New Code Must Not Deepen Slack Coupling

**Severity: should**

New features must not introduce additional Slack-specific logic outside of the existing Slack integration points. Specifically:

1. **Agent identity must not depend on Slack IDs.** Agent name is the primary key. Slack IDs are optional channel bindings, not identity. New features that need to identify agents must use agent name, not `SlackMemberID` or `SlackChannel`.

2. **Policy must remain channel-agnostic.** The `conga-policy.yaml` schema must not contain Slack-specific fields. Egress rules, routing, and posture declarations apply regardless of messaging platform.

3. **New CLI commands must not assume Slack.** Commands that operate on agents (status, logs, secrets, connect, pause, refresh) must work without Slack configured. Commands that configure messaging should accept platform as a parameter.

4. **Security controls must not depend on Slack constructs.** Channel allowlists are a security boundary, but the enforcement mechanism (which channels an agent responds to) should be expressible for any platform, not just Slack channel IDs.

### Package Structure: `channels/`

Channel integrations live in a dedicated root-level `channels/` directory, one subdirectory per platform:

```
channels/
  slack/       — Slack Socket Mode router, event parsing, signature verification
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

### Future Direction: Multi-Channel Support

When adding a second messaging platform (e.g., Telegram), the architecture should evolve toward:

- **Platform bindings on AgentConfig** — Replace `SlackMemberID`/`SlackChannel` with a `Channels` map (e.g., `map[string]ChannelBinding` where key is platform name). This is a breaking change to the agent config schema and should be its own spec.
- **Protocol-aware fan-out** — Each `channels/{platform}/` package manages its own connection and normalizes events for delivery to agent containers via platform-specific webhook paths (`/slack/events`, `/telegram/events`).
- **Platform-specific validation** — `ValidateMemberID()` moves from `common/` into `channels/slack/`. Each platform owns its own ID validation.
- **Plugin config generation** — `GenerateOpenClawConfig()` conditionally enables Slack, Telegram, etc. based on which platform bindings exist on the agent.

This refactor is not required now. The standard exists to prevent new code from making the eventual refactor harder.

## Testing Conventions

| Convention | Rationale |
|---|---|
| Unit tests in `_test.go` alongside source | Standard Go convention |
| Table-driven tests for validation functions | Pattern established in `common/validate_test.go`, `policy/policy_test.go` |
| `t.Helper()` on test helper functions | Clean stack traces on failure |
| `t.TempDir()` for temp files | Auto-cleanup, no manual teardown |
| No mocks for pure functions | Test real behavior; mocks only for external services (AWS SDK, SSH) |
| Test file loading with `os.IsNotExist` path | Every `Load()` function must handle missing files gracefully |
