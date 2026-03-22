# Specification: Agent Pause / Unpause

## Overview

Add the ability to temporarily stop an agent without destroying it. A paused agent's container is stopped and removed from Slack event routing, but all configuration, secrets, data, and state are preserved. Unpausing restores the agent to full operation. Works identically on both AWS and local providers.

## Motivation

Agents sometimes need to be taken offline temporarily — for billing reasons, during maintenance, or when a user is on leave. Today the only options are `remove-agent` (destructive) or `cycle-host` (affects all agents). Pause fills the gap: a lightweight, reversible, per-agent off switch.

## 1. State Model

### 1.1 Provider-agnostic: `provider.AgentConfig`

Add a `Paused` field to the shared agent config struct:

```go
type AgentConfig struct {
    Name          string    `json:"name"`
    Type          AgentType `json:"type"`
    SlackMemberID string    `json:"slack_member_id,omitempty"`
    SlackChannel  string    `json:"slack_channel,omitempty"`
    GatewayPort   int       `json:"gateway_port"`
    IAMIdentity   string    `json:"iam_identity,omitempty"`
    Paused        bool      `json:"paused,omitempty"`
}
```

`omitempty` ensures active agents' serialized config is unchanged — `"paused"` only appears when `true`.

### 1.2 AWS provider: SSM Parameter Store

Paused state is stored in the agent's SSM parameter (`/conga/agents/<name>`) as `"paused": true` in the existing JSON config:

```json
{
  "type": "user",
  "slack_member_id": "U0123456789",
  "gateway_port": 18789,
  "iam_identity": "aaron@example.com",
  "paused": true
}
```

SSM is the source of truth — survives host cycles and is queryable from the CLI without instance access.

### 1.3 Local provider: Agent JSON file

Paused state is stored in `~/.conga/agents/<name>.json`:

```json
{
  "type": "user",
  "slack_member_id": "U0123456789",
  "gateway_port": 18790,
  "paused": true
}
```

The file is already the source of truth for local agent config.

### 1.4 AWS discovery package

Add `Paused bool` field to `discovery.AgentConfig` struct:

```go
type AgentConfig struct {
    Name          string
    Type          string `json:"type"`
    SlackMemberID string `json:"slack_member_id,omitempty"`
    SlackChannel  string `json:"slack_channel,omitempty"`
    GatewayPort   int    `json:"gateway_port"`
    IAMIdentity   string `json:"iam_identity,omitempty"`
    Paused        bool   `json:"paused,omitempty"`
}
```

The AWS provider's `convertAgent` helper propagates `Paused` to the provider-level struct.

## 2. Provider Interface

Add two methods to `provider.Provider`:

```go
// PauseAgent stops an agent's container and removes it from routing.
// All configuration, secrets, and data are preserved.
PauseAgent(ctx context.Context, name string) error

// UnpauseAgent restarts a paused agent and restores routing.
UnpauseAgent(ctx context.Context, name string) error
```

## 3. Local Provider Implementation

### 3.1 `PauseAgent`

```go
func (p *LocalProvider) PauseAgent(ctx context.Context, name string) error {
    cfg, err := p.GetAgent(ctx, name)
    if err != nil {
        return err
    }
    if cfg.Paused {
        fmt.Printf("Agent %s is already paused.\n", name)
        return nil
    }

    // 1. Stop container (preserve data)
    cName := containerName(name)
    if containerExists(ctx, cName) {
        stopContainer(ctx, cName)
        removeContainer(ctx, cName)
    }

    // 2. Disconnect router from agent network
    netName := networkName(name)
    if containerExists(ctx, routerContainer) {
        disconnectNetwork(ctx, netName, routerContainer)
    }

    // 3. Update agent config
    cfg.Paused = true
    if err := p.saveAgentConfig(cfg); err != nil {
        return err
    }

    // 4. Regenerate routing (excludes paused agents)
    p.regenerateRouting(ctx)

    return nil
}
```

### 3.2 `UnpauseAgent`

```go
func (p *LocalProvider) UnpauseAgent(ctx context.Context, name string) error {
    cfg, err := p.GetAgent(ctx, name)
    if err != nil {
        return err
    }
    if !cfg.Paused {
        fmt.Printf("Agent %s is not paused.\n", name)
        return nil
    }

    // 1. Update agent config first (so RefreshAgent sees active state)
    cfg.Paused = false
    if err := p.saveAgentConfig(cfg); err != nil {
        return err
    }

    // 2. Refresh agent (regenerates config, starts container, reconnects router)
    if err := p.RefreshAgent(ctx, name); err != nil {
        return err
    }

    // 3. Regenerate routing (includes this agent again)
    p.regenerateRouting(ctx)

    return nil
}
```

### 3.3 Helper: `saveAgentConfig`

Extract the existing write logic from `ProvisionAgent` into a reusable helper:

```go
func (p *LocalProvider) saveAgentConfig(cfg *provider.AgentConfig) error {
    if err := os.MkdirAll(p.agentsDir(), 0700); err != nil {
        return err
    }
    agentJSON, err := json.MarshalIndent(cfg, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(filepath.Join(p.agentsDir(), cfg.Name+".json"), agentJSON, 0600)
}
```

## 4. AWS Provider Implementation

### 4.1 `PauseAgent`

```go
func (p *AWSProvider) PauseAgent(ctx context.Context, name string) error {
    agent, err := discovery.ResolveAgent(ctx, p.clients.SSM, name)
    if err != nil {
        return err
    }
    if agent.Paused {
        fmt.Printf("Agent %s is already paused.\n", name)
        return nil
    }

    // 1. Run pause script on instance
    instanceID, err := p.findInstance(ctx)
    if err != nil {
        return err
    }

    tmpl, err := template.New("pause").Parse(scripts.PauseAgentScript)
    if err != nil {
        return fmt.Errorf("failed to parse pause template: %w", err)
    }

    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, struct{ AgentName string }{name}); err != nil {
        return fmt.Errorf("failed to render pause script: %w", err)
    }

    spin := ui.NewSpinner(fmt.Sprintf("Pausing agent %s...", name))
    result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, buf.String(), 60*time.Second)
    spin.Stop()
    if err != nil {
        return err
    }
    if result.Status != "Success" {
        fmt.Fprintf(os.Stderr, "Output:\n%s\n%s\n", result.Stdout, result.Stderr)
        return fmt.Errorf("pause failed on instance")
    }

    // 2. Update SSM parameter
    if err := p.setAgentPaused(ctx, name, agent, true); err != nil {
        return fmt.Errorf("container stopped but failed to update SSM: %w", err)
    }

    return nil
}
```

### 4.2 `UnpauseAgent`

```go
func (p *AWSProvider) UnpauseAgent(ctx context.Context, name string) error {
    agent, err := discovery.ResolveAgent(ctx, p.clients.SSM, name)
    if err != nil {
        return err
    }
    if !agent.Paused {
        fmt.Printf("Agent %s is not paused.\n", name)
        return nil
    }

    // 1. Update SSM parameter first (so bootstrap sees active state)
    if err := p.setAgentPaused(ctx, name, agent, false); err != nil {
        return err
    }

    // 2. Run unpause script on instance
    instanceID, err := p.findInstance(ctx)
    if err != nil {
        return err
    }

    tmpl, err := template.New("unpause").Parse(scripts.UnpauseAgentScript)
    if err != nil {
        return fmt.Errorf("failed to parse unpause template: %w", err)
    }

    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, struct{ AgentName string }{name}); err != nil {
        return fmt.Errorf("failed to render unpause script: %w", err)
    }

    spin := ui.NewSpinner(fmt.Sprintf("Unpausing agent %s...", name))
    result, err := awsutil.RunCommand(ctx, p.clients.SSM, instanceID, buf.String(), 60*time.Second)
    spin.Stop()
    if err != nil {
        return err
    }
    if result.Status != "Success" {
        fmt.Fprintf(os.Stderr, "Output:\n%s\n%s\n", result.Stdout, result.Stderr)
        return fmt.Errorf("unpause failed on instance")
    }

    return nil
}
```

### 4.3 Helper: `setAgentPaused`

```go
func (p *AWSProvider) setAgentPaused(ctx context.Context, name string, agent *discovery.AgentConfig, paused bool) error {
    // Reconstruct SSM parameter JSON from agent config
    data := map[string]interface{}{
        "type":         agent.Type,
        "gateway_port": agent.GatewayPort,
    }
    if agent.SlackMemberID != "" {
        data["slack_member_id"] = agent.SlackMemberID
    }
    if agent.SlackChannel != "" {
        data["slack_channel"] = agent.SlackChannel
    }
    if agent.IAMIdentity != "" {
        data["iam_identity"] = agent.IAMIdentity
    }
    if paused {
        data["paused"] = true
    }
    // When unpausing, omit "paused" entirely (cleaner than "paused": false)

    jsonBytes, err := json.Marshal(data)
    if err != nil {
        return err
    }
    return awsutil.PutParameter(ctx, p.clients.SSM, fmt.Sprintf("/conga/agents/%s", name), string(jsonBytes))
}
```

## 5. SSM RunCommand Scripts (AWS only)

### 5.1 `cli/scripts/pause-agent.sh.tmpl`

```bash
set -euo pipefail

AGENT_NAME="{{.AgentName}}"
CONTAINER="conga-$AGENT_NAME"
NETWORK="conga-$AGENT_NAME"

echo "Stopping $CONTAINER..."
systemctl stop "conga-$AGENT_NAME" 2>/dev/null || true

# Read agent type and Slack ID for routing update
AGENT_TYPE=$(cat "/opt/conga/config/$AGENT_NAME.type" 2>/dev/null || echo "user")
SLACK_ID=$(cat "/opt/conga/config/$AGENT_NAME.slack-id" 2>/dev/null || echo "")

# Remove from routing.json
ROUTING="/opt/conga/config/routing.json"
if [ -f "$ROUTING" ] && [ -n "$SLACK_ID" ]; then
  if [ "$AGENT_TYPE" = "team" ]; then
    jq --arg id "$SLACK_ID" 'del(.channels[$id])' "$ROUTING" > "$ROUTING.tmp" && mv "$ROUTING.tmp" "$ROUTING"
  else
    jq --arg id "$SLACK_ID" 'del(.members[$id])' "$ROUTING" > "$ROUTING.tmp" && mv "$ROUTING.tmp" "$ROUTING"
  fi
  echo "Removed $SLACK_ID from routing.json"
fi

# Disconnect router from agent network (non-fatal)
docker network disconnect "$NETWORK" conga-router 2>/dev/null || true

echo "Agent $AGENT_NAME paused"
```

### 5.2 `cli/scripts/unpause-agent.sh.tmpl`

```bash
set -euo pipefail

AGENT_NAME="{{.AgentName}}"
CONTAINER="conga-$AGENT_NAME"
NETWORK="conga-$AGENT_NAME"

echo "Starting $CONTAINER..."
systemctl start "conga-$AGENT_NAME"
# ExecStartPost in the systemd unit reconnects the router to the agent's Docker network automatically

# Read agent type and Slack ID for routing update
AGENT_TYPE=$(cat "/opt/conga/config/$AGENT_NAME.type" 2>/dev/null || echo "user")
SLACK_ID=$(cat "/opt/conga/config/$AGENT_NAME.slack-id" 2>/dev/null || echo "")

# Re-add to routing.json
ROUTING="/opt/conga/config/routing.json"
if [ -f "$ROUTING" ] && [ -n "$SLACK_ID" ]; then
  URL="http://conga-$AGENT_NAME:18789/slack/events"
  if [ "$AGENT_TYPE" = "team" ]; then
    jq --arg id "$SLACK_ID" --arg url "$URL" '.channels[$id] = $url' "$ROUTING" > "$ROUTING.tmp" && mv "$ROUTING.tmp" "$ROUTING"
  else
    jq --arg id "$SLACK_ID" --arg url "$URL" '.members[$id] = $url' "$ROUTING" > "$ROUTING.tmp" && mv "$ROUTING.tmp" "$ROUTING"
  fi
  echo "Added $SLACK_ID back to routing.json"
fi

echo "Agent $AGENT_NAME unpaused"
```

### 5.3 Embed in `cli/scripts/embed.go`

```go
//go:embed pause-agent.sh.tmpl
var PauseAgentScript string

//go:embed unpause-agent.sh.tmpl
var UnpauseAgentScript string
```

## 6. CLI Commands

### 6.1 `conga admin pause <name>`

File: `cli/cmd/admin_pause.go`

```go
func adminPauseRun(cmd *cobra.Command, args []string) error {
    ctx, cancel := commandContext()
    defer cancel()

    name := args[0]
    if err := prov.PauseAgent(ctx, name); err != nil {
        return err
    }

    fmt.Printf("Agent %s paused.\n", name)
    fmt.Printf("To resume: conga admin unpause %s\n", name)
    return nil
}

func adminUnpauseRun(cmd *cobra.Command, args []string) error {
    ctx, cancel := commandContext()
    defer cancel()

    name := args[0]
    if err := prov.UnpauseAgent(ctx, name); err != nil {
        return err
    }

    fmt.Printf("Agent %s unpaused and running.\n", name)
    return nil
}
```

### 6.2 Registration in `cli/cmd/admin.go`

```go
pauseCmd := &cobra.Command{
    Use:   "pause <name>",
    Short: "Temporarily stop an agent (preserves all data)",
    Args:  cobra.ExactArgs(1),
    RunE:  adminPauseRun,
}

unpauseCmd := &cobra.Command{
    Use:   "unpause <name>",
    Short: "Resume a paused agent",
    Args:  cobra.ExactArgs(1),
    RunE:  adminUnpauseRun,
}
```

Add to `adminCmd.AddCommand(...)`.

### 6.3 `list-agents` STATUS column

Update `adminListAgentsRun` to include a STATUS column:

```go
headers := []string{"NAME", "TYPE", "STATUS", "IDENTIFIER", "GATEWAY PORT"}
var rows [][]string
for _, a := range agents {
    identifier := a.SlackMemberID
    if a.Type == "team" {
        identifier = a.SlackChannel
    }
    status := "active"
    if a.Paused {
        status = "paused"
    }
    rows = append(rows, []string{a.Name, string(a.Type), status, identifier, strconv.Itoa(a.GatewayPort)})
}
```

## 7. Routing Update

### 7.1 `GenerateRoutingJSON` — filter paused agents

```go
func GenerateRoutingJSON(agents []provider.AgentConfig) ([]byte, error) {
    cfg := RoutingConfig{
        Channels: make(map[string]string),
        Members:  make(map[string]string),
    }

    for _, a := range agents {
        if a.Paused {
            continue
        }
        url := fmt.Sprintf("http://conga-%s:18789/slack/events", a.Name)
        switch a.Type {
        case provider.AgentTypeUser:
            if a.SlackMemberID != "" {
                cfg.Members[a.SlackMemberID] = url
            }
        case provider.AgentTypeTeam:
            if a.SlackChannel != "" {
                cfg.Channels[a.SlackChannel] = url
            }
        }
    }

    return json.MarshalIndent(cfg, "", "  ")
}
```

This ensures `regenerateRouting` on the local provider automatically excludes paused agents.

## 8. Skip Paused Agents in Bulk Operations

### 8.1 Local provider: `RefreshAll`

```go
func (p *LocalProvider) RefreshAll(ctx context.Context) error {
    agents, err := p.ListAgents(ctx)
    if err != nil {
        return err
    }

    spin := ui.NewSpinner("Refreshing all agents...")
    for _, a := range agents {
        if a.Paused {
            spin.Stop()
            fmt.Printf("Skipping paused agent: %s\n", a.Name)
            spin = ui.NewSpinner("Refreshing all agents...")
            continue
        }
        if err := p.RefreshAgent(ctx, a.Name); err != nil {
            spin.Stop()
            return fmt.Errorf("failed to refresh %s: %w", a.Name, err)
        }
    }
    spin.Stop()
    return nil
}
```

### 8.2 Local provider: `CycleHost`

Skip paused agents in the restart loop:

```go
// Restart agents
for _, a := range agents {
    if a.Paused {
        fmt.Printf("Skipping paused agent: %s\n", a.Name)
        continue
    }
    if err := p.RefreshAgent(ctx, a.Name); err != nil {
        fmt.Fprintf(os.Stderr, "Warning: failed to restart %s: %v\n", a.Name, err)
    }
}
```

### 8.3 AWS provider: `RefreshAll` (refresh-all.sh.tmpl)

The `RefreshAll` template already receives the agent list. Filter in Go before passing to the template:

```go
var activeAgents []discovery.AgentConfig
for _, a := range agents {
    if a.Paused {
        fmt.Printf("Skipping paused agent: %s\n", a.Name)
        continue
    }
    activeAgents = append(activeAgents, a)
}
```

### 8.4 Bootstrap: `terraform/user-data.sh.tftpl`

In the agent discovery loop, skip paused agents:

```bash
AGENT_PAUSED=$(echo "$PARAM_VALUE" | jq -r '.paused // false')
if [ "$AGENT_PAUSED" = "true" ]; then
  echo "=== Skipping paused agent: $AGENT_NAME ==="
  continue
fi
```

## 9. Interaction with Other Commands

### 9.1 `refresh --agent <name>` on a paused agent

Error with a clear message:

```go
func (p *LocalProvider) RefreshAgent(ctx context.Context, agentName string) error {
    cfg, err := p.GetAgent(ctx, agentName)
    if err != nil {
        return err
    }
    if cfg.Paused {
        return fmt.Errorf("agent %s is paused. Use `conga admin unpause %s` first", agentName, agentName)
    }
    // ... existing logic
}
```

Same guard in the AWS provider's `RefreshAgent`.

### 9.2 `remove-agent` while paused

Works normally — remove is a superset of pause. No special handling needed since `RemoveAgent` already stops the container and cleans up routing.

### 9.3 `status` on a paused agent

The `GetStatus` method returns container state "not found" when the container isn't running. To distinguish paused from truly missing, check the agent config:

**Local provider** — after `GetStatus` sees "not found", the CLI command can check `cfg.Paused` and annotate the output. No change to `GetStatus` itself needed.

**Alternatively**, the `list-agents` STATUS column already shows paused state, which is sufficient.

### 9.4 `connect` on a paused agent

Error with a clear message:

```go
if cfg.Paused {
    return nil, fmt.Errorf("agent %s is paused. Use `conga admin unpause %s` first", agentName, agentName)
}
```

## 10. Design Decisions

- **Provider interface methods** rather than CLI-level orchestration — each provider knows its own stop/start mechanics, and the CLI stays thin.
- **`omitempty` on Paused** — active agents' serialized config is unchanged. No migration needed for existing agents.
- **Unpause uses `RefreshAgent`** (local provider) — gets the full config regeneration and container startup flow for free, including behavior file deployment and router reconnection.
- **AWS unpause relies on `ExecStartPost`** — the existing systemd unit's `ExecStartPost` reconnects the router to the agent's Docker network automatically, so the unpause script only needs `systemctl start`.
- **Routing regeneration is the canonical exclusion mechanism** (local) — `GenerateRoutingJSON` already rebuilds from scratch, so adding a `Paused` filter is trivial and correct.
- **AWS routing uses jq manipulation** — the on-instance scripts edit `routing.json` directly because regenerating from SSM would require AWS CLI calls and is slower.

## 11. Edge Cases

| Scenario | Behavior |
|----------|----------|
| Pause during active conversation | Container receives SIGTERM with 30s grace period (existing `TimeoutStopSec=30` on AWS, Docker default on local). OpenClaw handles graceful shutdown. |
| Pause when container already stopped | Idempotent — `docker stop` on a stopped container is a no-op. SSM `systemctl stop` succeeds. |
| Unpause when container already running | Idempotent — prints "not paused" and returns. |
| `refresh-all` with some paused | Skips paused agents, notes them in output. |
| `cycle-host` with some paused | Skips paused agents in restart loop. |
| `remove-agent` while paused | Works normally (superset of pause). |
| Host reboot (AWS) | Bootstrap skips paused agents via `jq` check. |
| Slack messages to paused agent | Not routed (removed from routing.json). Messages are lost — not queued. |

## 12. File Manifest

| File | Action | Description |
|------|--------|-------------|
| `cli/internal/provider/provider.go` | Modify | Add `Paused` to AgentConfig; add PauseAgent/UnpauseAgent to Provider interface |
| `cli/internal/provider/localprovider/provider.go` | Modify | Implement PauseAgent, UnpauseAgent, saveAgentConfig; update RefreshAll, CycleHost, RefreshAgent guard |
| `cli/internal/provider/awsprovider/provider.go` | Modify | Implement PauseAgent, UnpauseAgent, setAgentPaused; update RefreshAll, RefreshAgent guard |
| `cli/internal/common/routing.go` | Modify | Filter paused agents in GenerateRoutingJSON |
| `cli/internal/discovery/agent.go` | Modify | Add `Paused bool` field to AgentConfig |
| `cli/cmd/admin.go` | Modify | Register pause/unpause commands; add STATUS column to list-agents |
| `cli/cmd/admin_pause.go` | Create | Pause/unpause command handlers |
| `cli/scripts/pause-agent.sh.tmpl` | Create | AWS: stop systemd, remove from routing, disconnect router |
| `cli/scripts/unpause-agent.sh.tmpl` | Create | AWS: start systemd, re-add to routing |
| `cli/scripts/embed.go` | Modify | Embed new script templates |
| `terraform/user-data.sh.tftpl` | Modify | Skip agents with `paused: true` in discovery loop |
