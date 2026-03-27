# Specification: Channel Management CLI

## Overview

Extract channel lifecycle management from `admin setup` into five new provider methods, CLI commands, and MCP tools. After this change, `admin setup` creates a gateway-only environment. Channels are added/removed independently via `conga channels add|remove`. Agent-channel bindings are managed via `conga channels bind|unbind`.

## 1. Provider Interface Extension

### 1.1 New Types (`cli/internal/provider/provider.go`)

```go
// ChannelStatus reports the state of a configured channel platform.
type ChannelStatus struct {
    Platform      string   `json:"platform"`       // "slack"
    Configured    bool     `json:"configured"`      // shared secrets present
    RouterRunning bool     `json:"router_running"`  // router container is running
    BoundAgents   []string `json:"bound_agents"`    // agent names with this platform binding
}
```

### 1.2 New Interface Methods (`cli/internal/provider/provider.go`)

Add to the `Provider` interface under a new **Channel Management** section:

```go
// Channel Management
AddChannel(ctx context.Context, platform string, secrets map[string]string) error
RemoveChannel(ctx context.Context, platform string) error
ListChannels(ctx context.Context) ([]ChannelStatus, error)
BindChannel(ctx context.Context, agentName string, binding channels.ChannelBinding) error
UnbindChannel(ctx context.Context, agentName string, platform string) error
```

This brings the Provider interface from 24 to 29 methods. The new section sits between "Secrets" and "Connectivity".

### 1.3 `hasAnyChannel` Promotion

The duplicated `hasAnyChannel(shared common.SharedSecrets) bool` helper in both `localprovider/provider.go:1503` and `remoteprovider/provider.go:1200` should be promoted to `common/config.go` as `common.HasAnyChannel()`.

## 2. Local Provider Implementation

New file: `cli/internal/provider/localprovider/channels.go`

### 2.1 `AddChannel(ctx, platform, secrets)`

**Preconditions**: Setup must have been run (check `p.configDir()` exists). Platform must be registered in the channel registry.

**Steps**:
1. Validate platform is registered: `ch, ok := channels.Get(platform)`
2. Validate all required secrets present: iterate `ch.SharedSecrets()`, check `def.Required` against provided `secrets` map
3. Write each secret to `p.sharedSecretsDir()/{name}` with mode 0400
4. Build `router.env` from all configured channels:
   ```go
   shared, _ := p.readSharedSecrets()
   var buf strings.Builder
   for _, ch := range channels.All() {
       if ch.HasCredentials(shared.Values) {
           for k, v := range ch.RouterEnvVars(shared.Values) {
               fmt.Fprintf(&buf, "%s=%s\n", k, v)
           }
       }
   }
   os.WriteFile(routerEnvPath, []byte(buf.String()), 0400)
   ```
5. Start (or restart) the router: `p.ensureRouter(ctx, true)`

**Idempotency**: If secrets already exist, overwrite them (allows credential rotation). The router is always restarted to pick up updated credentials. Network connections are established during `BindChannel`, not `AddChannel`.

**Output** (JSON mode):
```json
{"platform": "slack", "status": "configured", "router_started": true}
```

### 2.2 `RemoveChannel(ctx, platform)`

**Steps** (order matters for clean teardown):
1. Validate platform is registered
2. Stop and remove the router container (if running): `removeContainer(ctx, routerContainer)`
3. Strip bindings from all agents:
   ```go
   agents, _ := p.ListAgents(ctx)
   for _, a := range agents {
       if a.ChannelBinding(platform) != nil {
           // Remove the binding
           filtered := filterBindings(a.Channels, platform)
           a.Channels = filtered
           p.saveAgentConfig(&a)
           // Regenerate agent's openclaw.json and .env
           p.regenerateAgentConfig(ctx, a)
       }
   }
   ```
4. Regenerate `routing.json` (will now have no entries for this platform)
5. Delete shared secrets for this platform:
   ```go
   for _, def := range ch.SharedSecrets() {
       os.Remove(filepath.Join(p.sharedSecretsDir(), def.Name))
   }
   ```
6. Remove `router.env`

**Idempotency**: If platform isn't configured, return nil (no-op). If router isn't running, skip step 2.

### 2.3 `ListChannels(ctx)`

**Steps**:
1. Read shared secrets: `shared, _ := p.readSharedSecrets()`
2. Check router status: `containerExists(ctx, routerContainer) && isRunning`
3. For each registered channel, build `ChannelStatus`:
   ```go
   var result []ChannelStatus
   agents, _ := p.ListAgents(ctx)
   for _, ch := range channels.All() {
       status := ChannelStatus{Platform: ch.Name()}
       status.Configured = ch.HasCredentials(shared.Values)
       status.RouterRunning = routerRunning && status.Configured
       for _, a := range agents {
           if a.ChannelBinding(ch.Name()) != nil {
               status.BoundAgents = append(status.BoundAgents, a.Name)
           }
       }
       result = append(result, status)
   }
   ```

### 2.4 `BindChannel(ctx, agentName, binding)`

**Preconditions**:
- Agent must exist
- Channel platform must be configured (secrets present) — error: `"slack is not configured; run 'conga channels add slack' first"`
- Agent must not already have a binding for this platform — error: `"agent already has a slack binding; unbind first"`
- Binding must be valid: `ch.ValidateBinding(agent.Type, binding.ID)`

**Steps**:
1. Load agent config: `a, err := p.GetAgent(ctx, agentName)`
2. Validate preconditions above
3. Append binding: `a.Channels = append(a.Channels, binding)`
4. Save agent config: `p.saveAgentConfig(&a)`
5. Regenerate agent's `openclaw.json` and `.env` file (call `p.regenerateAgentConfig(ctx, a)`)
6. Regenerate `routing.json`: `p.regenerateRouting(ctx)`
7. Ensure router is connected to this agent's network:
   ```go
   if containerExists(ctx, routerContainer) {
       connectNetwork(ctx, networkName(agentName), routerContainer)
   }
   ```
8. Restart the agent container to pick up new config: `p.RefreshAgent(ctx, agentName)`

### 2.5 `UnbindChannel(ctx, agentName, platform)`

**Preconditions**:
- Agent must exist
- Agent must have a binding for this platform — if not, return nil (no-op with message)

**Steps**:
1. Load agent config
2. Filter out the binding: `a.Channels = filterBindings(a.Channels, platform)`
3. Save agent config
4. Regenerate agent's `openclaw.json` and `.env`
5. Regenerate `routing.json`
6. Restart agent container: `p.RefreshAgent(ctx, agentName)`

### 2.6 Helper: `regenerateAgentConfig`

New private method to regenerate an agent's config files without restarting the container (restart is caller's responsibility):

```go
func (p *LocalProvider) regenerateAgentConfig(ctx context.Context, cfg provider.AgentConfig) error {
    shared, err := p.readSharedSecrets()
    if err != nil { return err }
    perAgent, err := p.readAgentSecrets(cfg.Name)
    if err != nil { return err }

    openClawJSON, err := common.GenerateOpenClawConfig(cfg, shared, "")
    if err != nil { return err }
    dataDir := p.dataSubDir(cfg.Name)
    if err := os.WriteFile(filepath.Join(dataDir, "openclaw.json"), openClawJSON, 0644); err != nil {
        return err
    }

    envContent := common.GenerateEnvFile(cfg, shared, perAgent)
    envPath := filepath.Join(p.configDir(), cfg.Name+".env")
    if err := os.WriteFile(envPath, envContent, 0400); err != nil {
        return err
    }

    // Re-chown data dir for container user
    exec.CommandContext(ctx, "chown", "-R", "1000:1000", dataDir).Run()
    return nil
}
```

### 2.7 Helper: `filterBindings`

```go
func filterBindings(bindings []channels.ChannelBinding, platform string) []channels.ChannelBinding {
    var result []channels.ChannelBinding
    for _, b := range bindings {
        if b.Platform != platform {
            result = append(result, b)
        }
    }
    return result
}
```

## 3. Remote Provider Implementation

New file: `cli/internal/provider/remoteprovider/channels.go`

Same logic as local provider, with SSH/SFTP transport:

| Operation | Local | Remote |
|-----------|-------|--------|
| Write secret | `os.WriteFile(path, value, 0400)` | `p.ssh.Upload(path, value, 0400)` |
| Remove secret | `os.Remove(path)` | `p.ssh.Exec("rm -f " + path)` |
| Write router.env | `os.WriteFile(routerEnvPath, ...)` | `p.ssh.Upload(routerEnvPath, ...)` |
| Start router | `runRouterContainer(ctx, ...)` | `p.docker.Run(ctx, ...)` |
| Stop router | `removeContainer(ctx, ...)` | `p.docker.Remove(ctx, ...)` |
| Save agent config | `os.WriteFile(path, json)` | `p.ssh.Upload(path, json)` |
| Regenerate config | `os.WriteFile(path, ...)` | `p.ssh.Upload(path, ...)` |

The method signatures and logic are identical. Only the I/O layer changes.

## 4. AWS Provider Stubs

File: `cli/internal/provider/awsprovider/provider.go` (add to existing)

All five methods return `fmt.Errorf("channel management not yet implemented for AWS provider")`. Consistent with other deferred AWS features.

## 5. Setup Flow Simplification

### 5.1 Local Provider (`cli/internal/provider/localprovider/provider.go`)

**Remove from Setup()** (lines ~848-918 in current code):
- The `channels.All()` secret collection loop (the `secretItems` list that includes channel secrets)
- The router startup block (lines ~982-1003)

**Keep in Setup()**:
- Directory creation (unchanged)
- Repo path and image configuration (unchanged)
- Google OAuth credentials (google-client-id, google-client-secret) — these are not channel secrets
- Router source/behavior/egress-proxy file copying (unchanged — files are needed when channels are added later)
- Image pulling (unchanged)
- Empty routing.json creation (unchanged)
- Egress proxy startup (unchanged — egress is not channel-dependent)

**Backwards compatibility**: If `SetupConfig.Secrets` contains channel secrets (e.g., `"slack-bot-token"`), auto-invoke `AddChannel` at the end of Setup:

```go
// After normal setup completes...
// Auto-configure channels if secrets were provided in SetupConfig
for _, ch := range channels.All() {
    channelSecrets := map[string]string{}
    hasRequired := true
    for _, def := range ch.SharedSecrets() {
        val := cfg.SecretValue(def.Name)
        if val != "" {
            channelSecrets[def.Name] = val
        } else if def.Required {
            hasRequired = false
            break
        }
    }
    if hasRequired && len(channelSecrets) > 0 {
        if err := p.AddChannel(ctx, ch.Name(), channelSecrets); err != nil {
            return fmt.Errorf("auto-configure %s channel: %w", ch.Name(), err)
        }
    }
}
```

This preserves the existing `conga_setup` MCP tool behavior when called with Slack secrets in the JSON body.

### 5.2 Remote Provider (`cli/internal/provider/remoteprovider/setup.go`)

Same changes: remove channel secret prompts and router startup. Keep Google OAuth, router source upload, image pulls. Add `AddChannel` auto-invoke for backwards compatibility.

### 5.3 Updated Next-Steps Message

Change the post-setup output from:
```
Next steps:
  conga admin add-user <name> [--channel slack:U0123456789]
```
to:
```
Next steps:
  conga channels add slack        # optional: add Slack integration
  conga admin add-user <name>     # provision an agent
  conga channels bind <name> slack:<id>  # optional: bind agent to Slack
```

## 6. CLI Commands

New file: `cli/cmd/channels.go`

### 6.1 Command Registration

```go
var channelsCmd = &cobra.Command{
    Use:   "channels",
    Short: "Manage messaging channel integrations",
}

func init() {
    rootCmd.AddCommand(channelsCmd)
    channelsCmd.AddCommand(channelsAddCmd)
    channelsCmd.AddCommand(channelsRemoveCmd)
    channelsCmd.AddCommand(channelsListCmd)
    channelsCmd.AddCommand(channelsBindCmd)
    channelsCmd.AddCommand(channelsUnbindCmd)
}
```

### 6.2 `conga channels add <platform>`

```
Usage: conga channels add <platform>
Args:  platform (e.g., "slack")
Flags: --json (inline JSON or @file.json)
```

**Interactive mode**: Prompts for each secret defined by `ch.SharedSecrets()`:
```
[secret] slack-bot-token — Slack bot token (xoxb-...) (required)
  Enter slack-bot-token: ****
[secret] slack-signing-secret — Slack signing secret (required)
  Enter slack-signing-secret: ****
[secret] slack-app-token — Slack app-level token (xapp-...) (optional)
  Enter slack-app-token: ****
```

**JSON input**:
```json
{
  "platform": "slack",
  "secrets": {
    "slack-bot-token": "xoxb-...",
    "slack-signing-secret": "abc123",
    "slack-app-token": "xapp-..."
  }
}
```

**JSON output**:
```json
{"platform": "slack", "status": "configured", "router_started": true}
```

### 6.3 `conga channels remove <platform>`

```
Usage: conga channels remove <platform>
Args:  platform (e.g., "slack")
Flags: --json, --force (skip confirmation)
```

**Interactive mode**: Confirmation prompt listing affected agents:
```
This will:
  - Stop the Slack router
  - Remove Slack bindings from agents: aaron, leadership
  - Delete Slack credentials

Continue? [y/N]
```

**JSON input**: `{"platform": "slack"}` (no confirmation in JSON mode)

### 6.4 `conga channels list`

```
Usage: conga channels list
Flags: --output json
```

**Human output**:
```
PLATFORM  STATUS      ROUTER  BOUND AGENTS
slack     configured  running aaron, leadership
```

**JSON output**:
```json
[
  {
    "platform": "slack",
    "configured": true,
    "router_running": true,
    "bound_agents": ["aaron", "leadership"]
  }
]
```

### 6.5 `conga channels bind <agent> <platform:id>`

```
Usage: conga channels bind <agent> <platform:id>
Args:  agent name, channel binding (e.g., "slack:U0123456789")
Flags: --json
```

**JSON input**: `{"agent_name": "aaron", "channel": "slack:U0123456789"}`

**Validation errors**:
- `"slack is not configured; run 'conga channels add slack' first"` — channel not added
- `"agent 'aaron' already has a slack binding; use 'channels unbind' first"` — duplicate
- `"invalid slack user ID: must match U + 10 alphanumeric characters"` — bad ID format
- `"user agents require a Slack member ID (U...), not a channel ID (C...)"` — type mismatch

### 6.6 `conga channels unbind <agent> <platform>`

```
Usage: conga channels unbind <agent> <platform>
Args:  agent name, platform name (e.g., "slack")
Flags: --json, --force (skip confirmation)
```

**Interactive mode**: Confirmation that agent will switch to gateway-only for this platform.

### 6.7 `--channel` Flag Compatibility

The existing `--channel slack:U...` flag on `admin add-user`/`admin add-team` continues to work. Internally, `ProvisionAgent` already handles channel bindings. No change needed — the flag creates an agent with the binding pre-populated.

The difference: with the old flow, `admin setup` collected Slack secrets. With the new flow, `channels add slack` must be run first. If `--channel` is used but the channel isn't configured, `ProvisionAgent` will still work (it checks `HasCredentials` and omits the channel config section) but the agent won't receive Slack events until `channels add slack` is run and the agent is refreshed.

## 7. MCP Tools

New file: `cli/internal/mcpserver/tools_channels.go`

### 7.1 Tool Definitions

| Tool | Description | Parameters | Annotations |
|------|-------------|-----------|-------------|
| `conga_channels_add` | Add a messaging channel integration | `platform` (required), `slack_bot_token`, `slack_signing_secret`, `slack_app_token` | IdempotentHint: true |
| `conga_channels_remove` | Remove a messaging channel integration | `platform` (required) | DestructiveHint: true |
| `conga_channels_list` | List configured channels and their status | (none) | ReadOnlyHint: true |
| `conga_channels_bind` | Bind an agent to a channel | `agent_name` (required), `channel` (required, "platform:id") | — |
| `conga_channels_unbind` | Remove a channel binding from an agent | `agent_name` (required), `platform` (required) | DestructiveHint: true |

### 7.2 `conga_channels_add` Parameters

For the initial implementation, Slack-specific secret parameter names are used directly (matching the MCP tool pattern where parameters are explicit, not generic maps):

```go
InputSchema: mcp.ToolInputSchema{
    Type: "object",
    Properties: map[string]any{
        "platform":             map[string]any{"type": "string", "description": "Channel platform (e.g., 'slack')"},
        "slack_bot_token":      map[string]any{"type": "string", "description": "Slack bot token (xoxb-...)"},
        "slack_signing_secret": map[string]any{"type": "string", "description": "Slack signing secret"},
        "slack_app_token":      map[string]any{"type": "string", "description": "Slack app-level token (xapp-..., optional)"},
    },
    Required: []string{"platform", "slack_bot_token", "slack_signing_secret"},
},
```

**Design note**: When a second channel is added (e.g., Telegram), the tool parameters would be extended with `telegram_bot_token`, etc. The alternative — a generic `secrets` map — is less ergonomic for LLMs because the parameter names don't describe what values are needed.

**Handler**: Maps parameter names to secret names (`slack_bot_token` → `"slack-bot-token"`) and calls `prov.AddChannel(ctx, platform, secrets)`.

### 7.3 Registration (`cli/internal/mcpserver/tools.go`)

Add a new section after "Policy":

```go
// Channel Management
s.addTool(s.toolChannelsAdd()),
s.addTool(s.toolChannelsRemove()),
s.addTool(s.toolChannelsList()),
s.addTool(s.toolChannelsBind()),
s.addTool(s.toolChannelsUnbind()),
```

## 8. Demo Flow Update

### 8.1 New Step Order (`DEMO.md`)

| Step | Old Flow | New Flow |
|------|----------|----------|
| 1 | `conga_setup` with SSH + Slack tokens | `conga_setup` with SSH only (gateway-only) |
| 2 | `conga_provision_agent` × 2 with `--channel` | `conga_provision_agent` × 2 (no channel flag) |
| 3 | `conga_set_secret` API keys | `conga_channels_add slack` (collect Slack creds) |
| 4 | `conga_policy_set_egress` | `conga_channels_bind aaron slack:U...` |
| 5 | `conga_policy_deploy` | `conga_channels_bind leadership slack:C...` |
| 6 | `conga_get_status` | `conga_set_secret` API keys |
| 7 | Connect + seed memories | `conga_policy_set_egress` + `conga_policy_deploy` |
| 8 | Egress demo | Connect + seed memories + egress demo |
| 9 | Slack isolation demo | Slack isolation demo |

### 8.2 Key Demo Narrative Change

The new flow demonstrates progressive enhancement:
1. Start with a working web UI (gateway-only)
2. Show agents are functional without Slack
3. Add Slack as a capability layer
4. Show the same agents now also respond in Slack
5. Prove isolation across both interfaces

This better showcases the modular architecture and makes the demo resilient to Slack configuration issues — if Slack setup fails, the gateway demo still works.

## 9. Edge Cases & Error Handling

| Scenario | Behavior |
|----------|----------|
| `channels add slack` when already configured | Idempotent: updates secrets, restarts router. No error. |
| `channels remove slack` when not configured | No-op. Print "slack is not configured." Return nil. |
| `channels remove slack` when agents have bindings | Auto-strips bindings first (step 3 in RemoveChannel), then removes credentials. |
| `channels bind` when channel not configured | Error: `"slack is not configured; run 'conga channels add slack' first"` |
| `channels bind` when agent already bound | Error: `"agent 'aaron' already has a slack binding; use 'channels unbind' first"` |
| `channels bind` with wrong ID type | Error from `ch.ValidateBinding()`: `"user agents require a Slack member ID (U...), not a channel ID (C...)"` |
| `channels unbind` when agent has no binding | No-op. Print "agent 'aaron' has no slack binding." Return nil. |
| `channels unbind` on paused agent | Works. Updates config on disk. Changes take effect when agent is unpaused. |
| `channels add` when setup hasn't been run | Error: `"conga is not set up; run 'conga admin setup' first"` (check for config dir) |
| Router fails to start after `channels add` | Error propagated. Secrets are still saved (can retry by re-running `channels add`). |
| Agent refresh fails after `bind` | Error propagated. Config files are updated. Container will pick up changes on next manual refresh. |

## 10. File Inventory

### New Files (5)

| File | Lines (est.) | Purpose |
|------|-------------|---------|
| `cli/internal/provider/localprovider/channels.go` | ~200 | Local provider: AddChannel, RemoveChannel, ListChannels, BindChannel, UnbindChannel + helpers |
| `cli/internal/provider/remoteprovider/channels.go` | ~220 | Remote provider: same methods, SSH/SFTP transport |
| `cli/cmd/channels.go` | ~250 | CLI commands: channels add/remove/list/bind/unbind |
| `cli/internal/mcpserver/tools_channels.go` | ~250 | MCP tool handlers for all 5 channel management tools |
| `cli/internal/mcpserver/tools_channels_test.go` | ~150 | MCP tool handler tests |

### Modified Files (7)

| File | Change |
|------|--------|
| `cli/internal/provider/provider.go` | Add ChannelStatus type + 5 interface methods |
| `cli/internal/provider/localprovider/provider.go` | Remove channel secret prompts + router startup from Setup(). Add AddChannel auto-invoke for SetupConfig compat. Remove `hasAnyChannel` (promoted). |
| `cli/internal/provider/remoteprovider/setup.go` | Same Setup simplification as local. |
| `cli/internal/provider/remoteprovider/provider.go` | Remove `hasAnyChannel` (promoted). |
| `cli/internal/provider/awsprovider/provider.go` | Add 5 stub methods. |
| `cli/internal/common/config.go` | Add `HasAnyChannel()` promoted from providers. |
| `cli/internal/mcpserver/tools.go` | Register 5 new channel management tools. |

### Updated Files (1)

| File | Change |
|------|--------|
| `DEMO.md` | Reorder demo flow: gateway-first, then channels add, then bind. |

## 11. Testing

### Unit Tests for Provider Methods (~15 tests)

Focus on local provider (remote uses same logic over SSH):

| Test | Validates |
|------|-----------|
| `TestAddChannel_WritesSecrets` | Secrets written to correct paths with mode 0400 |
| `TestAddChannel_BuildsRouterEnv` | router.env contains correct env vars |
| `TestAddChannel_IdempotentUpdate` | Second call overwrites secrets, no error |
| `TestAddChannel_MissingRequiredSecret` | Error when required secret omitted |
| `TestAddChannel_UnknownPlatform` | Error for unregistered platform |
| `TestRemoveChannel_StripsBindings` | All agent configs updated, bindings removed |
| `TestRemoveChannel_DeletesSecrets` | Secret files removed |
| `TestRemoveChannel_Noop` | No error when platform not configured |
| `TestListChannels_ShowsBoundAgents` | Correct agent names in BoundAgents |
| `TestListChannels_EmptyWhenNoChannels` | Returns entry with Configured=false |
| `TestBindChannel_AddsBinding` | Agent config updated with new binding |
| `TestBindChannel_ChannelNotConfigured` | Error when secrets missing |
| `TestBindChannel_DuplicateBinding` | Error when agent already bound |
| `TestBindChannel_ValidationError` | Error for invalid ID format |
| `TestUnbindChannel_RemovesBinding` | Agent config updated, binding removed |
| `TestUnbindChannel_Noop` | No error when no binding exists |

### MCP Tool Handler Tests (~10 tests)

Following pattern in existing `tools_policy_test.go`:

| Test | Validates |
|------|-----------|
| `TestToolChannelsAdd_Success` | Correct secrets passed to AddChannel |
| `TestToolChannelsAdd_MissingRequired` | Error for missing required parameter |
| `TestToolChannelsRemove_Success` | Calls RemoveChannel with correct platform |
| `TestToolChannelsList_Success` | Returns JSON array of ChannelStatus |
| `TestToolChannelsBind_Success` | Parses binding, calls BindChannel |
| `TestToolChannelsBind_InvalidBinding` | Error for malformed "platform:id" |
| `TestToolChannelsUnbind_Success` | Calls UnbindChannel with correct args |

### Setup Backwards Compatibility Tests (~3 tests)

| Test | Validates |
|------|-----------|
| `TestSetup_NoChannelPrompts` | Interactive setup doesn't prompt for Slack secrets |
| `TestSetup_AutoChannelFromConfig` | SetupConfig with Slack secrets auto-invokes AddChannel |
| `TestSetup_GatewayOnlyDefault` | Setup completes without router when no secrets |
