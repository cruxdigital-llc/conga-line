# Plan: Agent Pause / Unpause

## Approach

Pause/unpause is a provider-level operation. Each provider stores paused state in its canonical agent config store (SSM for AWS, JSON file for local) and performs the container stop/start and routing update through provider-specific mechanisms. The CLI commands delegate entirely to the provider.

## Architecture

```
conga admin pause <name>
  │
  ├─ prov.PauseAgent(ctx, name)
  │    ├─ AWS: SSM SendCommand (stop systemd, update routing.json, disconnect router)
  │    │       then update SSM parameter {"paused": true}
  │    └─ Local: docker stop, regenerate routing.json (excludes paused), save agent JSON
  │
  └─ Print confirmation

conga admin unpause <name>
  │
  ├─ prov.UnpauseAgent(ctx, name)
  │    ├─ AWS: update SSM parameter (remove "paused"), SSM SendCommand (start systemd, update routing.json)
  │    └─ Local: update agent JSON, docker start via RefreshAgent, regenerate routing.json
  │
  └─ Print confirmation
```

## Key Design Decisions

- **Paused state in the canonical store**: SSM parameter (AWS) or `~/.conga/agents/<name>.json` (local). Survives host cycles and is queryable without instance access.
- **`Paused` field on `provider.AgentConfig`**: `json:"paused,omitempty"` — only serialized when true, no pollution of active agent configs.
- **Routing excludes paused agents**: `GenerateRoutingJSON` filters out paused agents. Both providers call this during pause/unpause.
- **Idempotent**: Pausing a paused agent or unpausing an active one is a no-op with a message.
- **No data loss**: Only the running container is stopped and routing is removed. Everything else is preserved.

## Implementation Steps

### 1. Add `Paused` field to `provider.AgentConfig`
- `Paused bool json:"paused,omitempty"` — omitempty keeps active agents clean

### 2. Add `PauseAgent` / `UnpauseAgent` to `Provider` interface
- Two new methods in `provider.go`

### 3. Implement in local provider
- `PauseAgent`: stop container, set `Paused: true` in agent JSON, regenerate routing
- `UnpauseAgent`: set `Paused: false`, refresh agent (starts container), regenerate routing

### 4. Implement in AWS provider
- `PauseAgent`: SSM SendCommand (stop systemd + update routing.json + disconnect router), update SSM parameter
- `UnpauseAgent`: update SSM parameter, SSM SendCommand (start systemd + update routing.json)
- New embedded scripts: `pause-agent.sh.tmpl`, `unpause-agent.sh.tmpl`

### 5. Update routing generation
- `GenerateRoutingJSON` skips agents where `Paused == true`

### 6. Update `RefreshAll` and `CycleHost` to skip paused agents
- Both providers skip paused agents, noting them in output

### 7. Update bootstrap (AWS)
- `user-data.sh.tftpl` skips agents with `"paused": true` in the discovery loop

### 8. CLI commands
- `conga admin pause <name>` — calls `prov.PauseAgent()`
- `conga admin unpause <name>` — calls `prov.UnpauseAgent()`
- `conga admin list-agents` — add STATUS column

### 9. Update `GetStatus` for paused agents
- When agent is paused and container not found, show "paused" instead of generic "not found"

## Files to Modify/Create

| File | Action |
|------|--------|
| `cli/pkg/provider/provider.go` | Modify — add `Paused` to AgentConfig, add PauseAgent/UnpauseAgent to interface |
| `cli/pkg/provider/localprovider/provider.go` | Modify — implement PauseAgent, UnpauseAgent; update RefreshAll/CycleHost |
| `cli/pkg/provider/awsprovider/provider.go` | Modify — implement PauseAgent, UnpauseAgent; update RefreshAll |
| `cli/pkg/common/routing.go` | Modify — filter paused agents in GenerateRoutingJSON |
| `cli/pkg/discovery/agent.go` | Modify — add Paused field to AgentConfig struct |
| `cli/cmd/admin.go` | Modify — register pause/unpause commands, add STATUS column to list-agents |
| `cli/cmd/admin_pause.go` | Create — pause/unpause command handlers |
| `cli/scripts/pause-agent.sh.tmpl` | Create — AWS: stop systemd, update routing, disconnect router |
| `cli/scripts/unpause-agent.sh.tmpl` | Create — AWS: start systemd, update routing |
| `cli/scripts/embed.go` | Modify — embed new script templates |
| `terraform/user-data.sh.tftpl` | Modify — skip paused agents in discovery loop |
