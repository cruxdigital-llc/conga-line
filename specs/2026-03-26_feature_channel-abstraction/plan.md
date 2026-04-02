# Plan — Channel Abstraction

## Approach

Extract all Slack-specific logic from core packages into a `channels/` package structure behind a `Channel` interface. The refactor works bottom-up: define the interface, implement Slack behind it, then rewire consumers. Breaking changes are acceptable.

## Phase 1: Channel Interface + Slack Implementation

**New package**: `cli/pkg/channels/channels.go`

Define the `Channel` interface that any messaging platform must implement:

```go
package channels

// ChannelBinding is a platform-specific identifier linking an agent to a channel endpoint.
// For Slack: member ID (user agents) or channel ID (team agents).
type ChannelBinding struct {
    Platform string `json:"platform"`          // "slack", "discord", etc.
    ID       string `json:"id"`                // platform-specific identifier
    Label    string `json:"label,omitempty"`    // human-readable (e.g., "#general")
}

// Secrets holds channel-specific shared credentials.
type Secrets struct {
    Name     string // secret file/key name (e.g., "slack-bot-token")
    EnvVar   string // env var name (e.g., "SLACK_BOT_TOKEN")
    Prompt   string // interactive setup prompt text
    Required bool   // required for this channel to function
}

// Channel defines what a messaging platform integration must provide.
type Channel interface {
    // Name returns the platform identifier (e.g., "slack").
    Name() string

    // ValidateBinding checks if a binding ID is valid for this platform.
    ValidateBinding(agentType string, id string) error

    // SharedSecrets returns the secret definitions this channel needs during setup.
    SharedSecrets() []Secrets

    // HasCredentials checks if the provided secret values are sufficient.
    HasCredentials(secretValues map[string]string) bool

    // GenerateOpenClawConfig returns the channels.{platform} section for openclaw.json.
    GenerateOpenClawConfig(agentType string, binding ChannelBinding, secretValues map[string]string) (map[string]interface{}, error)

    // GeneratePluginConfig returns the plugins.entries.{platform} section.
    GeneratePluginConfig(hasCredentials bool) map[string]interface{}

    // GenerateRoutingEntry returns the routing map entries for this agent.
    // Returns (key, url) pairs to add to routing.json.
    GenerateRoutingEntries(agentType string, binding ChannelBinding, agentName string, port int) map[string]RoutingEntry

    // EnvVars returns environment variables to include in the agent's env file.
    EnvVars(secretValues map[string]string) map[string]string

    // RouterEnvVars returns environment variables for the channel's proxy/router.
    RouterEnvVars(secretValues map[string]string) map[string]string

    // WebhookPath returns the webhook path agents listen on (e.g., "/slack/events").
    WebhookPath() string

    // BehaviorTemplateVars returns template variables for behavior file rendering.
    // e.g., {"SLACK_ID": "U0123456789"} or {"CHANNEL_ID": "some-id"}
    BehaviorTemplateVars(agentType string, binding ChannelBinding) map[string]string
}

// RoutingEntry represents one entry in the routing config.
type RoutingEntry struct {
    Section string // "channels" or "members" (Slack-specific sections become per-channel)
    Key     string // the platform identifier
    URL     string // webhook URL
}
```

**New package**: `cli/pkg/channels/slack/slack.go`

Implements `Channel` for Slack. Moves from:
- `common/validate.go` → `ValidateMemberID`, `ValidateChannelID` become `ValidateBinding()`
- `common/config.go` → Slack section of `GenerateOpenClawConfig()` becomes `GenerateOpenClawConfig()`
- `common/config.go` → `HasSlack()` becomes `HasCredentials()`
- `common/config.go` → Slack env vars in `GenerateEnvFile()` become `EnvVars()`
- `common/routing.go` → Slack routing logic becomes `GenerateRoutingEntries()`

**New file**: `cli/pkg/channels/registry.go`

Channel registry (same pattern as provider registry):
```go
var registered = map[string]Channel{}

func Register(ch Channel)               { registered[ch.Name()] = ch }
func Get(name string) (Channel, bool)   { return registered[name] }
func All() map[string]Channel           { return registered }
```

Slack auto-registers via `init()` in its package.

## Phase 2: AgentConfig Refactor

**File**: `cli/pkg/provider/provider.go`

Replace Slack-specific fields with generic channel bindings:

```go
type AgentConfig struct {
    Name        string           `json:"name"`
    Type        AgentType        `json:"type"`
    Channels    []ChannelBinding `json:"channels,omitempty"` // replaces SlackMemberID/SlackChannel
    GatewayPort int              `json:"gateway_port"`
    IAMIdentity string           `json:"iam_identity,omitempty"`
    Paused      bool             `json:"paused,omitempty"`
}
```

This is a **breaking change** to the agent JSON format. Existing agent JSON files (`~/.conga/agents/*.json`, `/opt/conga/agents/*.json`) will need migration or re-provisioning.

Impact: every file that reads `SlackMemberID` or `SlackChannel` must be updated. The grep shows 48 files reference these fields (most are specs — ~15 are source files).

## Phase 3: Rewire `common/` Package

### `config.go` — `GenerateOpenClawConfig()`

Replace the hardcoded Slack section with a loop over registered channels:

```go
func GenerateOpenClawConfig(agent AgentConfig, secretValues map[string]string, gatewayToken string) ([]byte, error) {
    // ... load defaults, overlay gateway ...

    channelsConfig := map[string]interface{}{}
    pluginsConfig := map[string]interface{}{}

    for _, binding := range agent.Channels {
        ch, ok := channels.Get(binding.Platform)
        if !ok { continue }
        if ch.HasCredentials(secretValues) {
            chCfg, err := ch.GenerateOpenClawConfig(string(agent.Type), binding, secretValues)
            if err != nil { return nil, err }
            channelsConfig[binding.Platform] = chCfg
        }
        pluginsConfig[binding.Platform] = ch.GeneratePluginConfig(ch.HasCredentials(secretValues))
    }

    if len(channelsConfig) > 0 {
        config["channels"] = channelsConfig
    }
    config["plugins"] = map[string]interface{}{"entries": pluginsConfig}
    // ...
}
```

### `config.go` — `SharedSecrets` struct

Replace Slack-named fields with a generic map:

```go
type SharedSecrets struct {
    Values         map[string]string // keyed by secret name: "slack-bot-token" → value
    GoogleClientID     string
    GoogleClientSecret string
}
```

Or simpler: just use `map[string]string` everywhere, since each channel declares what secrets it needs via `SharedSecrets()`.

### `config.go` — `GenerateEnvFile()`

Replace hardcoded `SLACK_BOT_TOKEN` / `SLACK_SIGNING_SECRET` with channel-provided env vars:

```go
func GenerateEnvFile(agent AgentConfig, secretValues map[string]string, perAgent map[string]string) []byte {
    // ... per-channel env vars ...
    for _, binding := range agent.Channels {
        ch, ok := channels.Get(binding.Platform)
        if !ok { continue }
        for k, v := range ch.EnvVars(secretValues) {
            appendEnv(k, v)
        }
    }
    // Google, NODE_OPTIONS, per-agent secrets ...
}
```

### `routing.go` — `GenerateRoutingJSON()`

Replace hardcoded Slack routing with channel-delegated entries:

```go
func GenerateRoutingJSON(agents []AgentConfig) ([]byte, error) {
    cfg := RoutingConfig{
        Channels: make(map[string]string),
        Members:  make(map[string]string),
    }
    for _, a := range agents {
        if a.Paused { continue }
        for _, binding := range a.Channels {
            ch, ok := channels.Get(binding.Platform)
            if !ok { continue }
            for _, entry := range ch.GenerateRoutingEntries(string(a.Type), binding, a.Name, a.GatewayPort) {
                switch entry.Section {
                case "channels":
                    cfg.Channels[entry.Key] = entry.URL
                case "members":
                    cfg.Members[entry.Key] = entry.URL
                }
            }
        }
    }
    return json.MarshalIndent(cfg, "", "  ")
}
```

Note: `RoutingConfig` stays Slack-shaped for now since the router is Slack-specific. When a second channel is added, routing.json may need a per-platform structure.

### `behavior.go` — `ComposeBehaviorFiles()`

Replace `{{SLACK_ID}}` hardcoding with channel-provided template vars:

```go
// Gather template vars from all channel bindings
for _, binding := range agent.Channels {
    ch, ok := channels.Get(binding.Platform)
    if !ok { continue }
    for k, v := range ch.BehaviorTemplateVars(string(agent.Type), binding) {
        content = strings.ReplaceAll(content, "{{"+k+"}}", v)
    }
}
```

### `validate.go`

Remove `ValidateMemberID()` and `ValidateChannelID()` — these move to `channels/slack/`.

## Phase 4: Rewire CLI Commands

### `cmd/admin_provision.go`

Replace `slack_member_id` / `slack_channel` args with a channel-aware flow:

```
conga admin add-user <name> [--channel slack:U0123456789]
conga admin add-team <name> [--channel slack:C0123456789]
```

Or keep positional args for backward compat:
```
conga admin add-user <name> [identifier]  # interpreted by registered channels
```

The simpler approach: keep `add-user` / `add-team`, accept a `--channel` flag of format `platform:id`. When platform is omitted, default to `slack` for backward compat. Gateway-only mode when no `--channel` given.

### `cmd/admin.go` — `adminListAgentsRun()`

Update IDENTIFIER column to show channel bindings instead of `SlackMemberID`/`SlackChannel`.

### `cmd/json_schema.go`

Update schemas for `admin.add-user`, `admin.add-team`, `admin.list-agents` to use `channel` instead of `slack_member_id`/`slack_channel`.

### `cmd/root.go`

Remove `validateMemberID()` / `validateChannelID()` wrappers — validation now goes through the channel interface.

## Phase 5: Rewire MCP Tools

### `mcpserver/tools_lifecycle.go`

Replace `slack_member_id` / `slack_channel` params with a `channel` param (format: `platform:id`):

```json
{
  "channel": {
    "type": "string",
    "description": "Channel binding (format: platform:id, e.g., slack:U0123456789)"
  }
}
```

## Phase 6: Rewire Provider Implementations

### `localprovider/provider.go`

- `Setup()`: Query registered channels for their secrets, prompt accordingly
- `ProvisionAgent()`: Use channel interface for config/env generation (already via common/)
- `ensureRouter()`: Slack-specific router startup stays (router is Slack-specific by design)
- Router env file generation: uses `channels.Get("slack").RouterEnvVars()`

### `remoteprovider/provider.go` + `remoteprovider/setup.go`

Same pattern as local — channel-driven setup prompts and env generation.

### `remoteprovider/secrets.go`

`readSharedSecrets()` currently returns `SharedSecrets` with Slack fields. Switch to returning `map[string]string` keyed by secret name.

### `awsprovider/provider.go`

Minimal changes — AWS provider reads secrets from Secrets Manager by name. The secret names (`slack-bot-token`, etc.) are already stored in the SSM setup manifest. The channel package just needs to declare which secret names it uses.

### Bootstrap scripts (`add-user.sh.tmpl`, `add-team.sh.tmpl`, `user-data.sh.tftpl`)

**Out of scope** for this refactor. These are AWS-specific shell scripts that template agent JSON. They'll need a follow-up to read the new `channels` field format instead of `slack_member_id`/`slack_channel`.

## Phase 7: Update Tests

- `common/routing_test.go` — update for new `AgentConfig.Channels` field
- `common/validate_test.go` — move Slack validation tests to `channels/slack/`
- `cmd/root_test.go` — update validation wrapper tests
- `mcpserver/server_test.go` — update provision tool tests
- `awsprovider/provider_test.go` — update for new secret reading
- New: `channels/slack/slack_test.go` — test all Slack channel interface methods
- New: `channels/registry_test.go` — test channel registration

## Files Changed (Estimated)

### New Files (4)
| File | Purpose |
|------|---------|
| `cli/pkg/channels/channels.go` | Channel interface + types |
| `cli/pkg/channels/registry.go` | Channel registry |
| `cli/pkg/channels/slack/slack.go` | Slack implementation |
| `cli/pkg/channels/slack/slack_test.go` | Slack tests |

### Modified Files (~15)
| File | Change |
|------|--------|
| `cli/pkg/provider/provider.go` | `AgentConfig` channels field |
| `cli/pkg/provider/setup_config.go` | Generalize Slack secret fields |
| `cli/pkg/common/config.go` | Delegate to channel interface |
| `cli/pkg/common/routing.go` | Delegate to channel interface |
| `cli/pkg/common/behavior.go` | Delegate template vars to channel |
| `cli/pkg/common/validate.go` | Remove Slack validation (moved) |
| `cli/cmd/admin.go` | Update list-agents display |
| `cli/cmd/admin_provision.go` | Channel-aware provisioning |
| `cli/cmd/root.go` | Remove Slack validation wrappers |
| `cli/cmd/json_schema.go` | Update schemas |
| `cli/pkg/mcpserver/tools_lifecycle.go` | Channel-aware provision tool |
| `cli/pkg/provider/localprovider/provider.go` | Channel-driven setup/routing |
| `cli/pkg/provider/remoteprovider/provider.go` | Channel-driven setup/routing |
| `cli/pkg/provider/remoteprovider/setup.go` | Channel-driven setup prompts |
| `cli/pkg/provider/remoteprovider/secrets.go` | Generic secret map |

### Test Files Modified (~5)
| File | Change |
|------|--------|
| `cli/pkg/common/routing_test.go` | New AgentConfig format |
| `cli/cmd/root_test.go` | Remove Slack validation tests |
| `cli/pkg/mcpserver/server_test.go` | New provision params |
| `cli/pkg/provider/awsprovider/provider_test.go` | New secret format |
| `cli/pkg/common/validate_test.go` | Remove Slack tests (moved) |

## Persona Review Checkpoints

### Architect
- Does the `Channel` interface cover all platform integration points?
- Is the `ChannelBinding` model on `AgentConfig` sufficient for multi-channel agents?
- Does the routing model (still Slack-shaped) need generalization now or can it wait?

### QA
- What happens when an agent has zero channel bindings? (Gateway-only — must still work)
- What happens when an agent has a binding for an unregistered channel? (Skip with warning, or error?)
- Are existing agent JSON files handled? (Breaking change — need migration story or error message)

### Product Manager
- Is the CLI UX for `--channel slack:U0123456789` clear enough?
- Should the current `add-user <name> [slack_member_id]` positional arg be preserved as a shorthand?
- Does this refactor provide value without a second channel implementation?

## Risk Assessment

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Existing agent JSON files break | Certain | Provide migration path or clear error message pointing to re-provision |
| AWS bootstrap scripts incompatible | Certain | Document as known limitation; scripts need separate update |
| Interface too narrow for future channels | Medium | Designed from architecture standards + OpenClaw's actual channel config model |
| Interface too broad (over-engineered) | Low | Only includes what Slack already needs — no speculative methods |
