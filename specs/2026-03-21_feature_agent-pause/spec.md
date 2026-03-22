# Specification: Agent Pause / Unpause

## Overview

Add the ability to temporarily stop an agent without destroying it. A paused agent's container is stopped and removed from Slack event routing, but all configuration, secrets, data, and SSM state are preserved. Unpausing restores the agent to full operation.

## Motivation

Agents sometimes need to be taken offline temporarily — for billing reasons, during maintenance, or when a user is on leave. Today the only options are `remove-agent` (destructive) or `cycle-host` (affects all agents). Pause fills the gap: a lightweight, reversible, per-agent off switch.

## 1. State Model

Paused state is stored in the agent's SSM parameter (`/conga/agents/<name>`) as a `"paused": true` field in the existing JSON config. When `paused` is absent or `false`, the agent is active.

```json
{
  "type": "user",
  "slack_member_id": "U0123456789",
  "gateway_port": 18789,
  "iam_identity": "aaron@example.com",
  "paused": true
}
```

SSM is the source of truth so that paused state survives host cycles.

## 2. CLI Commands

### 2.1 `conga admin pause <name>`

1. Resolve agent from SSM; error if not found; no-op if already paused.
2. Run `pause-agent.sh.tmpl` on the instance via SSM SendCommand:
   - `systemctl stop conga-<name>`
   - Remove the agent's Slack identifier from `routing.json` (user → `members`, team → `channels`). Read agent type from `/opt/conga/config/<name>.type` and Slack ID from `/opt/conga/config/<name>.slack-id`.
   - `docker network disconnect conga-<name> conga-router` (cleanup, non-fatal)
3. Update SSM parameter to set `"paused": true`.
4. Print confirmation with unpause instructions.

### 2.2 `conga admin unpause <name>`

1. Resolve agent from SSM; error if not found; no-op if not paused.
2. Run `unpause-agent.sh.tmpl` on the instance via SSM SendCommand:
   - `systemctl start conga-<name>` (ExecStartPost reconnects the router to the agent's Docker network automatically).
   - Re-add the agent's Slack identifier to `routing.json`.
3. Update SSM parameter to remove `"paused"` (or set to `false`).
4. Print confirmation.

### 2.3 `conga admin list-agents` update

Add a STATUS column showing `active` or `paused` based on the `Paused` field in the SSM config.

## 3. Bootstrap Integration

In `user-data.sh.tftpl`, during the agent discovery loop, read `paused` from the parameter JSON:

```bash
AGENT_PAUSED=$(echo "$PARAM_VALUE" | jq -r '.paused // false')
if [ "$AGENT_PAUSED" = "true" ]; then
  echo "=== Skipping paused agent: $AGENT_NAME ==="
  continue
fi
```

This ensures paused agents are not started on host cycle / reboot.

## 4. Code Changes

| File | Change |
|------|--------|
| `cli/internal/discovery/agent.go` | Add `Paused bool` field to `AgentConfig` struct |
| `cli/cmd/admin_pause.go` | New file: `adminPauseAgentRun`, `adminUnpauseAgentRun`, `putAgentConfig` helper |
| `cli/cmd/admin.go` | Register `pause` and `unpause` subcommands; add STATUS column to `list-agents` |
| `cli/scripts/pause-agent.sh.tmpl` | New: stop systemd, remove from routing.json, disconnect router network |
| `cli/scripts/unpause-agent.sh.tmpl` | New: start systemd, re-add to routing.json |
| `cli/scripts/embed.go` | Embed the two new script templates |
| `terraform/user-data.sh.tftpl` | Skip agents with `paused: true` during bootstrap discovery loop |

## 5. Design Decisions

- **SSM as source of truth** over a file on the instance — survives host cycles and is queryable from the CLI without instance access.
- **`putAgentConfig` helper** reconstructs the SSM JSON from the `AgentConfig` struct, only including `"paused"` when true (omitempty). This avoids polluting active agents' config with `"paused": false`.
- **Idempotent** — pausing an already-paused agent or unpausing an active one prints a message and returns success.
- **No data loss** — pause preserves systemd unit, config files, env file, data directory, Docker network, and secrets. Only the running container is stopped and routing is removed.
- **Router auto-reconnect** — unpause relies on the existing `ExecStartPost` in the systemd unit to reconnect the router to the agent's Docker network, so the unpause script only needs to `systemctl start`.

## 6. Edge Cases

- **Pausing during active conversation**: The container receives SIGTERM with a 30-second grace period (existing `TimeoutStopSec=30`). OpenClaw handles graceful shutdown. Messages sent to Slack during pause will not be delivered to the agent (routing removed).
- **Refresh while paused**: `conga refresh --agent <name>` on a paused agent should either error with "agent is paused" or be allowed (updates secrets/config without starting). TBD — leaning toward erroring.
- **`refresh-all` while some agents paused**: Should skip paused agents and note them in output.
- **`remove-agent` while paused**: Should work normally (superset of pause).

## 7. Dependencies

- Blocked on Zach's PR merge — avoid conflicts with that changeset.
