# Requirements: Agent Pause / Unpause

## Goal

Enable temporarily stopping an agent without destroying it. A paused agent's container is stopped and removed from Slack event routing, but all configuration, secrets, data, and state are preserved. Unpausing restores the agent to full operation.

## Background

Agents sometimes need to be taken offline temporarily — for billing reasons, during maintenance, or when a user is on leave. Today the only options are `remove-agent` (destructive) or `cycle-host` (affects all agents). Pause fills the gap: a lightweight, reversible, per-agent off switch.

## Success Criteria

1. `conga admin pause <name>` stops the agent container and removes it from Slack routing
2. `conga admin unpause <name>` restarts the agent and restores Slack routing
3. Paused state persists across host reboots / cycle-host (AWS: SSM parameter; local: agent JSON file)
4. Paused agents are skipped during bootstrap (AWS), `RefreshAll`, and `CycleHost`
5. `conga admin list-agents` shows a STATUS column distinguishing `active` from `paused`
6. Both operations are idempotent — pausing a paused agent or unpausing an active one is a no-op
7. No data loss — pause preserves config, secrets, data directory, Docker network, and all other agent state
8. Works identically on both AWS and local providers

## Constraints

- Must work through the existing Provider interface (new methods added)
- AWS provider uses SSM SendCommand for on-instance operations
- Local provider uses direct Docker CLI calls
- Routing regeneration must exclude paused agents on both providers
- Must not break existing `remove-agent`, `refresh`, or `status` commands
