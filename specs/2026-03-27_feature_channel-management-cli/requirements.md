# Requirements: Channel Management CLI

## Goal

Decouple channel (Slack) configuration from environment setup so that channels can be added and removed as an independent, post-setup step. This enables a cleaner demo flow: (1) set up a gateway-only environment, (2) provision agents, (3) add Slack as a channel — then demonstrate interactions in both the web UI and Slack.

## Background

Today, `admin setup` lumps Slack credential collection in with environment bootstrapping. The Channel interface (`cli/pkg/channels/`) is already cleanly abstracted — Slack is a proper plugin that declares secrets, validates IDs, and generates config. However, there's no way to add or remove a channel *after* setup without re-running `admin setup` or manually editing secrets/config.

Similarly, the `--channel slack:ID` flag on `admin add-user`/`admin add-team` is the only way to bind an agent to Slack. There's no way to add Slack to an existing gateway-only agent, or remove Slack from an agent that has it.

## Success Criteria

1. **`admin setup` defaults to gateway-only** — no Slack secret prompts during setup. Slack credentials are collected later via `conga channels add slack`.
2. **New CLI commands**:
   - `conga channels add slack` — collects Slack credentials (bot-token, signing-secret, app-token), stores as shared secrets, starts the router
   - `conga channels remove slack` — removes Slack shared secrets, stops the router, strips Slack bindings from all agents, regenerates configs
   - `conga channels list` — shows configured channels and their status (credentials present, router running)
3. **Agent-level channel binding**:
   - `conga channels bind <agent> slack:<id>` — adds Slack binding to an existing agent, regenerates its config and routing
   - `conga channels unbind <agent> slack` — removes Slack binding from an agent, regenerates config and routing
4. **MCP tool wrappers** for all five commands (for LLM-driven demo flow)
5. **`--json` input/output** on all new commands (consistent with CLI JSON Input feature)
6. **Router lifecycle is dynamic** — router starts when first Slack channel is added, stops when Slack is removed. No orphan router in gateway-only environments.
7. **Existing `--channel` flag on `admin add-user`/`admin add-team` continues to work** as a convenience shortcut (equivalent to provision + bind in one step)
8. **All three providers** (local, remote, AWS) support the new commands
9. **Demo flow** works as described: setup → add agents → `channels add slack` → bind agents → interact

## Constraints

- Must not break existing JSON schema for `AgentConfig` — `Channels []ChannelBinding` stays
- Must not break existing `SetupConfig` JSON schema for `--json` automation (secrets can still be passed to setup, they just aren't prompted)
- Router env file (`router.env`) must be regenerated when channels are added/removed
- Agent `.env` files and `openclaw.json` must be regenerated when bindings change
- Per the Channel interface, secrets are shared (not per-agent) — one set of Slack creds for all agents

## Out of Scope

- Adding new channel types (Discord, etc.) — the interface already supports this; this feature just adds the management surface
- Changing the Channel interface itself
- AWS bootstrap script changes (deferred, as with other features)
