# Requirements — Channel Abstraction

## Goal

Extract Slack-specific logic from the core CLI into a `channels/` package structure, introducing a `Channel` interface that Slack implements. This creates a clean abstraction boundary so future channel types (Discord, Telegram, Teams, etc.) can be added without modifying core agent lifecycle code.

**This is a refactor, not a new capability.** No new channel types are implemented. The only behavioral change is structural: Slack code moves behind an interface.

## Success Criteria

1. A `Channel` interface exists in `cli/internal/channels/` that defines the contract for any messaging platform integration
2. Slack is the sole implementation, in `cli/internal/channels/slack/`
3. Core packages (`common/`, `provider/`, `cmd/`) reference the `Channel` interface, not Slack-specific types or logic
4. `AgentConfig` uses a channel-agnostic binding model (replaces `SlackMemberID`/`SlackChannel`)
5. All existing functionality works identically — gateway-only mode, Slack mode, pause/unpause, routing, policy, MCP tools
6. Breaking changes to CLI flags, config schemas, and internal APIs are acceptable
7. The Node.js Slack router (`router/src/index.js`) is left as-is — it becomes the Slack-specific proxy, referenced by the Slack channel package
8. Existing tests continue to pass (updated as needed for renamed types/fields)

## Scope

### In Scope

- **Channel interface** — defines what a channel implementation must provide: config generation, routing, validation, secrets, setup prompts, OpenClaw config section
- **Slack implementation** — moves existing Slack logic from `common/config.go`, `common/routing.go`, `common/validate.go`, `provider/provider.go`, `cmd/admin_provision.go` into `channels/slack/`
- **AgentConfig refactor** — replace `SlackMemberID`/`SlackChannel` with a generic `Channels map[string]ChannelBinding` (per architecture standards)
- **Config generation refactor** — `GenerateOpenClawConfig()` delegates channel-specific sections to the channel implementation
- **Routing generation refactor** — `GenerateRoutingJSON()` delegates to channel implementations for their routing entries
- **Validation refactor** — Slack ID validation (`^U[A-Z0-9]{10}$`, `^C[A-Z0-9]{10}$`) moves to `channels/slack/`
- **Secrets refactor** — Slack-named secrets (`SlackBotToken`, `SlackSigningSecret`, `SlackAppToken`) owned by the Slack channel package
- **Setup flow refactor** — Slack setup prompts owned by the Slack channel package
- **CLI command updates** — provisioning commands accept channel type + channel-specific identifiers
- **Behavior template update** — `{{SLACK_ID}}` template variable generalized or delegated to channel
- **MCP tool updates** — `conga_provision_agent` parameters updated for channel-agnostic bindings
- **Test updates** — existing tests updated for new types/paths; new tests for channel interface contract

### Out of Scope

- Implementing any channel type other than Slack
- Rewriting the Node.js router in Go
- Changes to the router's internal logic or protocol
- Changes to `conga-policy.yaml` schema (already channel-agnostic per architecture standards)
- Changes to Terraform/AWS infrastructure
- Changes to bootstrap scripts (`user-data.sh.tftpl`, `add-user.sh.tmpl`, `add-team.sh.tmpl`) — these are AWS-provider-specific and can be updated in a follow-up

## Non-Functional Requirements

- No new dependencies (pure Go interfaces + existing `encoding/json`)
- Channel implementations must be self-contained — all platform-specific logic in one package
- The interface must support the connection proxy pattern (one proxy per platform, fan-out to agents)
- Channel packages must be testable in isolation (no provider dependencies)

## Constraints

- OpenClaw's `openclaw.json` format is the authority — the `channels.{platform}` key structure must match what OpenClaw expects
- OpenClaw currently only supports Slack as a channel, but the interface should not preclude platforms OpenClaw may add (Discord, Telegram, Teams, Matrix, WhatsApp, Signal, iMessage, webchat)
- Gateway (web UI) is NOT a channel — it's always available and configured separately
- Agent type (user vs team) is a Conga Line concept that maps differently per channel (Slack: DM vs channel; others TBD)
