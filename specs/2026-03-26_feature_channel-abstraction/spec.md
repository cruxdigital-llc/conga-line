# Specification — Channel Abstraction

## Overview

Extract all Slack-specific logic from the core CLI into a `cli/internal/channels/` package behind a `Channel` interface. Slack becomes the first (and currently only) implementation. No new channel types are added — this is a structural refactor.

---

## 1. Data Models

### 1.1 `ChannelBinding` (new type in `cli/internal/channels/channels.go`)

```go
// ChannelBinding links an agent to a specific endpoint on a messaging platform.
type ChannelBinding struct {
    Platform string `json:"platform"`       // registered channel name: "slack"
    ID       string `json:"id"`             // platform-specific identifier
    Label    string `json:"label,omitempty"` // optional human label (e.g. "#general")
}
```

Serialized in agent JSON files (`~/.conga/agents/*.json`):

```json
{
  "name": "myagent",
  "type": "user",
  "channels": [
    {"platform": "slack", "id": "U0123456789"}
  ],
  "gateway_port": 18789
}
```

**Breaking change**: replaces `"slack_member_id"` and `"slack_channel"` fields. Existing agent JSON files are incompatible — agents must be re-provisioned.

### 1.2 `SecretDef` (new type in `cli/internal/channels/channels.go`)

```go
// SecretDef declares a secret that a channel needs during admin setup.
type SecretDef struct {
    Name     string // file/key name: "slack-bot-token"
    EnvVar   string // env var: "SLACK_BOT_TOKEN"
    Prompt   string // interactive prompt: "Slack bot token (xoxb-...)"
    Required bool   // true = channel cannot function without it
    RouterOnly bool // true = only needed by the router, not agent containers
}
```

### 1.3 `RoutingEntry` (new type in `cli/internal/channels/channels.go`)

```go
// RoutingEntry is one entry for a channel's routing config.
type RoutingEntry struct {
    Section string // routing.json top-level key: "channels", "members"
    Key     string // platform identifier: Slack channel/member ID
    URL     string // webhook URL: "http://conga-name:port/slack/events"
}
```

### 1.4 `AgentConfig` (modified in `cli/internal/provider/provider.go`)

```go
type AgentConfig struct {
    Name        string                   `json:"name"`
    Type        AgentType                `json:"type"`
    Channels    []channels.ChannelBinding `json:"channels,omitempty"`
    GatewayPort int                      `json:"gateway_port"`
    IAMIdentity string                   `json:"iam_identity,omitempty"`
    Paused      bool                     `json:"paused,omitempty"`
}
```

Removed: `SlackMemberID`, `SlackChannel`.

Helper method for callers that need a specific platform binding:

```go
// ChannelBinding returns the first binding for the given platform, or nil.
func (a *AgentConfig) ChannelBinding(platform string) *channels.ChannelBinding {
    for i := range a.Channels {
        if a.Channels[i].Platform == platform {
            return &a.Channels[i]
        }
    }
    return nil
}
```

### 1.5 `SharedSecrets` (modified in `cli/internal/common/config.go`)

Replace Slack-specific fields with a generic value map:

```go
type SharedSecrets struct {
    Values             map[string]string // "slack-bot-token" → "xoxb-...", etc.
    GoogleClientID     string
    GoogleClientSecret string
}
```

The `HasSlack()` method is removed. Channel credential checks use `channels.Get("slack").HasCredentials(secrets.Values)`.

### 1.6 `SetupConfig` (modified in `cli/internal/provider/setup_config.go`)

Replace Slack-named fields with a generic secrets map:

```go
type SetupConfig struct {
    // Connection (remote provider)
    SSHHost    string `json:"ssh_host,omitempty"`
    SSHPort    int    `json:"ssh_port,omitempty"`
    SSHUser    string `json:"ssh_user,omitempty"`
    SSHKeyPath string `json:"ssh_key_path,omitempty"`

    // Config values
    RepoPath string `json:"repo_path,omitempty"`
    Image    string `json:"image,omitempty"`

    // Shared secrets — generic map replaces Slack-specific fields.
    // Keys are secret names: "slack-bot-token", "google-client-id", etc.
    Secrets map[string]string `json:"secrets,omitempty"`

    // InstallDocker skips the Docker install confirmation prompt.
    InstallDocker bool `json:"install_docker,omitempty"`
}
```

`SecretValue(name)` becomes a simple map lookup: `c.Secrets[name]`.

**JSON input compatibility**: The old field names (`slack_bot_token`, etc.) are no longer accepted. Users pass `"secrets": {"slack-bot-token": "xoxb-..."}` instead. This is a breaking change but aligns with the generic model.

---

## 2. Channel Interface

### 2.1 `Channel` (new interface in `cli/internal/channels/channels.go`)

```go
// Channel defines the contract for a messaging platform integration.
type Channel interface {
    // Name returns the platform identifier used in ChannelBinding.Platform.
    Name() string

    // ValidateBinding checks whether id is valid for the given agent type.
    // agentType is "user" or "team".
    ValidateBinding(agentType string, id string) error

    // SharedSecrets returns the secrets this channel needs during admin setup.
    SharedSecrets() []SecretDef

    // HasCredentials returns true if secretValues contains the required secrets.
    HasCredentials(secretValues map[string]string) bool

    // OpenClawChannelConfig returns the channels.{platform} section for openclaw.json.
    OpenClawChannelConfig(agentType string, binding ChannelBinding, secretValues map[string]string) (map[string]interface{}, error)

    // OpenClawPluginConfig returns the plugins.entries.{platform} section.
    OpenClawPluginConfig(enabled bool) map[string]interface{}

    // RoutingEntries returns routing.json entries for this agent+binding.
    RoutingEntries(agentType string, binding ChannelBinding, agentName string, port int) []RoutingEntry

    // AgentEnvVars returns env vars for the agent container's env file.
    AgentEnvVars(secretValues map[string]string) map[string]string

    // RouterEnvVars returns env vars for the channel proxy's env file.
    RouterEnvVars(secretValues map[string]string) map[string]string

    // WebhookPath returns the container endpoint path (e.g., "/slack/events").
    WebhookPath() string

    // BehaviorTemplateVars returns template substitution vars for behavior files.
    BehaviorTemplateVars(agentType string, binding ChannelBinding) map[string]string
}
```

### 2.2 Registry (new file `cli/internal/channels/registry.go`)

```go
package channels

import "fmt"

var registered = map[string]Channel{}

// Register adds a channel implementation. Panics on duplicate.
func Register(ch Channel) {
    name := ch.Name()
    if _, exists := registered[name]; exists {
        panic(fmt.Sprintf("channels: duplicate registration %q", name))
    }
    registered[name] = ch
}

// Get returns the channel for the given platform name.
func Get(name string) (Channel, bool) {
    ch, ok := registered[name]
    return ch, ok
}

// All returns all registered channels.
func All() map[string]Channel {
    return registered
}

// ParseBinding parses "platform:id" into a ChannelBinding.
// If no colon is present, returns an error.
func ParseBinding(s string) (ChannelBinding, error) {
    i := strings.Index(s, ":")
    if i < 0 {
        return ChannelBinding{}, fmt.Errorf("invalid channel binding %q: expected format platform:id (e.g., slack:U0123456789)", s)
    }
    platform := s[:i]
    id := s[i+1:]
    if _, ok := registered[platform]; !ok {
        return ChannelBinding{}, fmt.Errorf("unknown channel platform %q; registered: %v", platform, registeredNames())
    }
    return ChannelBinding{Platform: platform, ID: id}, nil
}

func registeredNames() []string {
    names := make([]string, 0, len(registered))
    for n := range registered {
        names = append(names, n)
    }
    return names
}
```

---

## 3. Slack Implementation

### 3.1 `cli/internal/channels/slack/slack.go`

```go
package slack

import (
    "fmt"
    "regexp"

    "github.com/cruxdigital-llc/conga-line/cli/internal/channels"
)

func init() {
    channels.Register(&Slack{})
}

var (
    memberIDPattern  = regexp.MustCompile(`^U[A-Z0-9]{10}$`)
    channelIDPattern = regexp.MustCompile(`^C[A-Z0-9]{10}$`)
)

type Slack struct{}

func (s *Slack) Name() string { return "slack" }

func (s *Slack) ValidateBinding(agentType, id string) error {
    switch agentType {
    case "user":
        if !memberIDPattern.MatchString(id) {
            return fmt.Errorf("invalid Slack member ID %q: must match U + 10 alphanumeric chars (e.g., U0123456789)", id)
        }
    case "team":
        if !channelIDPattern.MatchString(id) {
            return fmt.Errorf("invalid Slack channel ID %q: must match C + 10 alphanumeric chars (e.g., C0123456789)", id)
        }
    }
    return nil
}

func (s *Slack) SharedSecrets() []channels.SecretDef {
    return []channels.SecretDef{
        {Name: "slack-bot-token", EnvVar: "SLACK_BOT_TOKEN", Prompt: "Slack bot token (xoxb-...)", Required: true},
        {Name: "slack-signing-secret", EnvVar: "SLACK_SIGNING_SECRET", Prompt: "Slack signing secret", Required: true},
        {Name: "slack-app-token", EnvVar: "SLACK_APP_TOKEN", Prompt: "Slack app-level token (xapp-...)", Required: false, RouterOnly: true},
    }
}

func (s *Slack) HasCredentials(sv map[string]string) bool {
    return sv["slack-bot-token"] != "" && sv["slack-signing-secret"] != ""
}

func (s *Slack) OpenClawChannelConfig(agentType string, binding channels.ChannelBinding, sv map[string]string) (map[string]interface{}, error) {
    cfg := map[string]interface{}{
        "mode":              "http",
        "enabled":           true,
        "botToken":          sv["slack-bot-token"],
        "signingSecret":     sv["slack-signing-secret"],
        "webhookPath":       "/slack/events",
        "userTokenReadOnly": true,
        "streaming":         "partial",
        "nativeStreaming":   true,
    }
    switch agentType {
    case "user":
        cfg["groupPolicy"] = "disabled"
        cfg["dmPolicy"] = "allowlist"
        if binding.ID != "" {
            cfg["allowFrom"] = []string{binding.ID}
        }
        cfg["dm"] = map[string]interface{}{"enabled": true}
    case "team":
        cfg["groupPolicy"] = "allowlist"
        cfg["dmPolicy"] = "disabled"
        if binding.ID != "" {
            cfg["channels"] = map[string]interface{}{
                binding.ID: map[string]interface{}{"allow": true, "requireMention": false},
            }
        }
    }
    return cfg, nil
}

func (s *Slack) OpenClawPluginConfig(enabled bool) map[string]interface{} {
    return map[string]interface{}{"enabled": enabled}
}

func (s *Slack) RoutingEntries(agentType string, binding channels.ChannelBinding, agentName string, port int) []channels.RoutingEntry {
    if binding.ID == "" {
        return nil
    }
    url := fmt.Sprintf("http://conga-%s:%d/slack/events", agentName, port)
    switch agentType {
    case "user":
        return []channels.RoutingEntry{{Section: "members", Key: binding.ID, URL: url}}
    case "team":
        return []channels.RoutingEntry{{Section: "channels", Key: binding.ID, URL: url}}
    }
    return nil
}

func (s *Slack) AgentEnvVars(sv map[string]string) map[string]string {
    vars := map[string]string{}
    if v := sv["slack-bot-token"]; v != "" {
        vars["SLACK_BOT_TOKEN"] = v
    }
    if v := sv["slack-signing-secret"]; v != "" {
        vars["SLACK_SIGNING_SECRET"] = v
    }
    return vars
}

func (s *Slack) RouterEnvVars(sv map[string]string) map[string]string {
    vars := map[string]string{}
    if v := sv["slack-app-token"]; v != "" {
        vars["SLACK_APP_TOKEN"] = v
    }
    if v := sv["slack-signing-secret"]; v != "" {
        vars["SLACK_SIGNING_SECRET"] = v
    }
    return vars
}

func (s *Slack) WebhookPath() string { return "/slack/events" }

func (s *Slack) BehaviorTemplateVars(agentType string, binding channels.ChannelBinding) map[string]string {
    return map[string]string{"SLACK_ID": binding.ID}
}
```

### 3.2 `cli/internal/channels/slack/slack_test.go`

Table-driven tests covering:

| Test | Validates |
|------|-----------|
| `TestValidateBinding_User` | Valid/invalid member IDs |
| `TestValidateBinding_Team` | Valid/invalid channel IDs |
| `TestHasCredentials` | All combos: both present, one missing, both missing |
| `TestOpenClawChannelConfig_User` | User config has `dmPolicy: allowlist`, `allowFrom`, `dm.enabled` |
| `TestOpenClawChannelConfig_Team` | Team config has `groupPolicy: allowlist`, `channels` map |
| `TestOpenClawChannelConfig_NoID` | Binding with empty ID still produces valid config |
| `TestRoutingEntries_User` | Returns `members` entry |
| `TestRoutingEntries_Team` | Returns `channels` entry |
| `TestRoutingEntries_NoID` | Returns nil (gateway-only) |
| `TestAgentEnvVars` | Returns correct env var names and values |
| `TestRouterEnvVars` | Returns app token + signing secret |
| `TestBehaviorTemplateVars` | Returns `SLACK_ID` key |
| `TestSharedSecrets` | Returns 3 secret defs with correct names and required flags |

---

## 4. Modified Functions

### 4.1 `common.GenerateOpenClawConfig()` (`config.go`)

**Before**: Hardcodes Slack plugin and channel section.

**After**: Iterates agent's channel bindings, delegating to each channel implementation:

```go
func GenerateOpenClawConfig(agent provider.AgentConfig, secrets SharedSecrets, gatewayToken string) ([]byte, error) {
    var config map[string]interface{}
    if err := json.Unmarshal(openclawDefaults, &config); err != nil {
        return nil, fmt.Errorf("failed to parse openclaw-defaults.json: %w", err)
    }

    config["gateway"] = buildGatewayConfig(agent.GatewayPort, gatewayToken)

    channelsCfg := map[string]interface{}{}
    pluginsCfg := map[string]interface{}{}

    for _, binding := range agent.Channels {
        ch, ok := channels.Get(binding.Platform)
        if !ok {
            continue
        }
        hasCreds := ch.HasCredentials(secrets.Values)
        pluginsCfg[binding.Platform] = ch.OpenClawPluginConfig(hasCreds)
        if hasCreds {
            section, err := ch.OpenClawChannelConfig(string(agent.Type), binding, secrets.Values)
            if err != nil {
                return nil, fmt.Errorf("channel %s config: %w", binding.Platform, err)
            }
            channelsCfg[binding.Platform] = section
        }
    }

    if len(channelsCfg) > 0 {
        config["channels"] = channelsCfg
    }
    if len(pluginsCfg) > 0 {
        config["plugins"] = map[string]interface{}{"entries": pluginsCfg}
    }

    return json.MarshalIndent(config, "", "  ")
}
```

**Signature change**: `SharedSecrets` no longer has `SlackBotToken` etc. — uses `Values` map.

### 4.2 `common.GenerateEnvFile()` (`config.go`)

**Before**: Hardcodes `SLACK_BOT_TOKEN` and `SLACK_SIGNING_SECRET`.

**After**: Iterates channel bindings for env vars:

```go
func GenerateEnvFile(agent provider.AgentConfig, secrets SharedSecrets, perAgent map[string]string) []byte {
    var buf []byte
    appendEnv := func(key, val string) {
        if val != "" {
            buf = append(buf, []byte(fmt.Sprintf("%s=%s\n", key, val))...)
        }
    }

    // Channel-provided env vars (deduplicated — shared secrets go once)
    seen := map[string]bool{}
    for _, binding := range agent.Channels {
        ch, ok := channels.Get(binding.Platform)
        if !ok { continue }
        for k, v := range ch.AgentEnvVars(secrets.Values) {
            if !seen[k] {
                appendEnv(k, v)
                seen[k] = true
            }
        }
    }

    // Non-channel shared secrets
    appendEnv("GOOGLE_CLIENT_ID", secrets.GoogleClientID)
    appendEnv("GOOGLE_CLIENT_SECRET", secrets.GoogleClientSecret)
    appendEnv("NODE_OPTIONS", "--max-old-space-size=1536")

    for name, value := range perAgent {
        appendEnv(SecretNameToEnvVar(name), value)
    }
    return buf
}
```

### 4.3 `common.GenerateRoutingJSON()` (`routing.go`)

**Before**: Hardcodes Slack member/channel routing.

**After**: Delegates to channel implementations:

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
        for _, binding := range a.Channels {
            ch, ok := channels.Get(binding.Platform)
            if !ok { continue }
            for _, entry := range ch.RoutingEntries(string(a.Type), binding, a.Name, a.GatewayPort) {
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

**Note**: `RoutingConfig` struct and JSON shape are unchanged — the Slack router still reads this format. Future channels may require a per-platform routing structure, but that's a concern for when they're added.

### 4.4 `common.ComposeBehaviorFiles()` (`behavior.go`)

**Before**: Hardcodes `{{SLACK_ID}}` template substitution.

**After**: Gathers template vars from all channel bindings:

```go
// In the USER.md template rendering section:
content := string(data)
content = strings.ReplaceAll(content, "{{AGENT_NAME}}", agent.Name)

for _, binding := range agent.Channels {
    ch, ok := channels.Get(binding.Platform)
    if !ok { continue }
    for k, v := range ch.BehaviorTemplateVars(string(agent.Type), binding) {
        content = strings.ReplaceAll(content, "{{"+k+"}}", v)
    }
}
```

The Slack implementation returns `{"SLACK_ID": binding.ID}`, so existing `{{SLACK_ID}}` templates continue to work.

### 4.5 `common/validate.go`

**Remove**: `ValidateMemberID()`, `ValidateChannelID()`, and their regex patterns.
**Keep**: `ValidateAgentName()` (not channel-specific).

### 4.6 `provider.SetupConfig.SecretValue()` (`setup_config.go`)

**Before**: Switch statement mapping secret names to struct fields.

**After**: Simple map lookup:

```go
func (c *SetupConfig) SecretValue(name string) string {
    if c == nil || c.Secrets == nil {
        return ""
    }
    return c.Secrets[name]
}
```

---

## 5. CLI Command Changes

### 5.1 `admin add-user` / `admin add-team` (`cmd/admin.go` + `cmd/admin_provision.go`)

**New flag**: `--channel` (format: `platform:id`, e.g., `slack:U0123456789`)

```
conga admin add-user <name> [--channel slack:U0123456789]
conga admin add-team <name> [--channel slack:C0123456789]
```

No `--channel` = gateway-only mode (unchanged behavior).

**Command definitions** (`admin.go`):

```go
addUserCmd := &cobra.Command{
    Use:   "add-user <name>",
    Short: "Provision a new user agent",
    Args:  cobra.ExactArgs(1),
    RunE:  adminAddUserRun,
}
addUserCmd.Flags().StringVar(&adminChannel, "channel", "", "Channel binding (platform:id, e.g., slack:U0123456789)")
```

Removed: positional `[slack_member_id]` / `[slack_channel]` args.

**Handler** (`admin_provision.go`):

```go
func adminAddUserRun(cmd *cobra.Command, args []string) error {
    // ... agent name, port, IAM ...

    var bindings []channels.ChannelBinding
    chStr := adminChannel
    if chStr == "" {
        if s, ok := ui.GetString("channel"); ok {
            chStr = s
        }
    }
    if chStr != "" {
        binding, err := channels.ParseBinding(chStr)
        if err != nil {
            return err
        }
        ch, _ := channels.Get(binding.Platform)
        if err := ch.ValidateBinding("user", binding.ID); err != nil {
            return err
        }
        bindings = append(bindings, binding)
    }

    cfg := provider.AgentConfig{
        Name:        agentName,
        Type:        provider.AgentTypeUser,
        Channels:    bindings,
        GatewayPort: gatewayPort,
        IAMIdentity: iamIdentity,
    }
    // ...
}
```

### 5.2 `admin list-agents` (`cmd/admin.go`)

**Before**: IDENTIFIER column shows `SlackMemberID` or `SlackChannel`.

**After**: CHANNEL column shows `platform:id` or `(gateway-only)`:

```go
headers := []string{"NAME", "TYPE", "STATUS", "CHANNEL", "GATEWAY PORT"}
for _, a := range agents {
    channel := "(gateway-only)"
    if len(a.Channels) > 0 {
        channel = a.Channels[0].Platform + ":" + a.Channels[0].ID
    }
    // ...
}
```

### 5.3 `json-schema` (`cmd/json_schema.go`)

Update schemas:

| Command | Old Fields | New Fields |
|---------|-----------|------------|
| `admin.add-user` | `slack_member_id` | `channel` (string, format: `platform:id`) |
| `admin.add-team` | `slack_channel` | `channel` (string, format: `platform:id`) |
| `admin.list-agents` | `slack_member_id`, `slack_channel` | `channels` (array of `{platform, id}` objects) |
| `admin.setup` | `slack_bot_token`, `slack_signing_secret`, `slack_app_token` | `secrets` (object, keys are secret names) |

### 5.4 `cmd/root.go`

Remove: `validateMemberID()`, `validateChannelID()` wrapper functions.
Keep: `validateAgentName()` (delegates to `common.ValidateAgentName()`).

---

## 6. MCP Tool Changes

### 6.1 `conga_provision_agent` (`mcpserver/tools_lifecycle.go`)

**Before**: Separate `slack_member_id` and `slack_channel` params.

**After**: Single `channel` param:

```go
"channel": map[string]any{
    "type":        "string",
    "description": "Channel binding (format: platform:id, e.g., slack:U0123456789). Omit for gateway-only mode.",
},
```

Handler parses via `channels.ParseBinding()`, validates via `ch.ValidateBinding()`, constructs `AgentConfig.Channels`.

---

## 7. Provider Changes

### 7.1 `localprovider/provider.go`

**`readSharedSecrets()`**: Returns `common.SharedSecrets` with `Values` map instead of Slack-named fields:

```go
func (p *LocalProvider) readSharedSecrets() (common.SharedSecrets, error) {
    secrets := common.SharedSecrets{Values: make(map[string]string)}

    // Read all known channel secrets
    for _, ch := range channels.All() {
        for _, def := range ch.SharedSecrets() {
            data, err := os.ReadFile(filepath.Join(p.sharedSecretsDir(), def.Name))
            if err == nil {
                secrets.Values[def.Name] = strings.TrimSpace(string(data))
            }
        }
    }

    // Non-channel secrets
    if data, err := os.ReadFile(filepath.Join(p.sharedSecretsDir(), "google-client-id")); err == nil {
        secrets.GoogleClientID = strings.TrimSpace(string(data))
    }
    if data, err := os.ReadFile(filepath.Join(p.sharedSecretsDir(), "google-client-secret")); err == nil {
        secrets.GoogleClientSecret = strings.TrimSpace(string(data))
    }
    return secrets, nil
}
```

**`Setup()`**: Channel-driven secret prompts:

```go
// For each registered channel, prompt for its secrets
for _, ch := range channels.All() {
    for _, def := range ch.SharedSecrets() {
        value := cfg.SecretValue(def.Name) // from SetupConfig
        if value == "" && !ui.JSONInputActive {
            var err error
            value, err = ui.TextPrompt(def.Prompt)
            if err != nil { return err }
        }
        if value != "" {
            p.writeSharedSecret(def.Name, value)
        }
    }
}
```

**`Setup()` router section**: Check if any registered channel has credentials:

```go
shared, _ := p.readSharedSecrets()
hasAnyChannel := false
for _, ch := range channels.All() {
    if ch.HasCredentials(shared.Values) {
        hasAnyChannel = true
        break
    }
}
if hasAnyChannel {
    // Write router env from Slack channel (router is Slack-specific)
    slackCh, _ := channels.Get("slack")
    routerEnvMap := slackCh.RouterEnvVars(shared.Values)
    // ... write router.env ...
}
```

**`ensureRouter()`**: Unchanged in logic — the router is Slack-specific. Only the credential check changes from `shared.HasSlack()` to `channels.Get("slack").HasCredentials(shared.Values)`.

### 7.2 `remoteprovider/secrets.go`

**`readSharedSecrets()`**: Same pattern as local — reads files by iterating channel secret defs:

```go
func (p *RemoteProvider) readSharedSecrets() (common.SharedSecrets, error) {
    dir := p.sharedSecretsDir()
    secrets := common.SharedSecrets{Values: make(map[string]string)}

    for _, ch := range channels.All() {
        for _, def := range ch.SharedSecrets() {
            data, err := p.ssh.Download(posixpath.Join(dir, def.Name))
            if err == nil {
                secrets.Values[def.Name] = string(data)
            }
        }
    }

    // Non-channel secrets
    if data, err := p.ssh.Download(posixpath.Join(dir, "google-client-id")); err == nil {
        secrets.GoogleClientID = string(data)
    }
    if data, err := p.ssh.Download(posixpath.Join(dir, "google-client-secret")); err == nil {
        secrets.GoogleClientSecret = string(data)
    }
    return secrets, nil
}
```

### 7.3 `remoteprovider/setup.go`

Same channel-driven prompt pattern as local provider.

### 7.4 `awsprovider/provider.go`

Minimal change — the AWS provider reads secret names from the SSM setup manifest. The secret names (`slack-bot-token`, etc.) don't change. `readSharedSecrets()` populates `SharedSecrets.Values` from the manifest-defined names.

---

## 8. Import Graph

The `channels` package must not import `provider` or `common` to avoid circular dependencies:

```
cmd/ ──────────► channels/        ◄──── channels/slack/
  │                 │                        │
  │                 │ (types only)           │ (registers via init())
  ▼                 ▼                        │
common/ ───────► channels/ (interface)       │
  │                                          │
  ▼                                          │
provider/ ─────► channels/ (ChannelBinding)  │
```

- `channels/channels.go`: defines types + interface (no imports from this repo)
- `channels/registry.go`: imports only `channels` types
- `channels/slack/`: imports `channels` types, registers via `init()`
- `common/`: imports `channels` for `Get()`, `ChannelBinding`
- `provider/`: imports `channels` for `ChannelBinding` type
- `cmd/`: imports `channels` for `ParseBinding()`, `Get()`

The Slack `init()` registration means any binary that imports `channels/slack` (directly or transitively) gets Slack registered. This import must be added to `main.go` or `cmd/root.go`:

```go
import _ "github.com/cruxdigital-llc/conga-line/cli/internal/channels/slack"
```

---

## 9. Edge Cases

| Scenario | Behavior |
|----------|----------|
| Agent with zero channel bindings | Gateway-only mode. No `channels` section in openclaw.json. No routing entries. No channel env vars. This is unchanged behavior. |
| Agent with binding for unregistered channel | `channels.Get()` returns `false`. Binding is silently skipped during config/routing generation. Agent JSON is preserved (forward compatibility). |
| Existing agent JSON with old `slack_member_id` field | `json.Unmarshal` into new `AgentConfig` silently ignores unknown fields. Agent loads with empty `Channels` slice — effectively gateway-only. Provider `ListAgents()` and `GetAgent()` will show agents without channel bindings until re-provisioned. |
| `--channel` with unknown platform | `channels.ParseBinding()` returns error listing registered platforms. |
| `--channel` with invalid ID | `ch.ValidateBinding()` returns platform-specific error message. |
| Multiple `--channel` flags | Not supported in v1. Single `--channel` flag per provisioning command. Future: repeatable flag or JSON array input. |
| Channel credentials present but agent has no binding | Channel env vars still included in env file (shared secrets). OpenClaw config omits `channels` section (no binding → no channel config). Router still runs (available for other agents). |

---

## 10. Migration

### Existing Agent JSON Files

Old format:
```json
{"name":"myagent","type":"user","slack_member_id":"U0123456789","gateway_port":18789}
```

New format:
```json
{"name":"myagent","type":"user","channels":[{"platform":"slack","id":"U0123456789"}],"gateway_port":18789}
```

**Strategy**: No automatic migration. Go's `json.Unmarshal` ignores unknown fields, so old JSON files load without error but with empty `Channels`. Agents appear as gateway-only until re-provisioned via `conga admin remove-agent <name> && conga admin add-user <name> --channel slack:ID`.

The `admin list-agents` output will show `(gateway-only)` for un-migrated agents, making it clear they need re-provisioning.

### AWS Bootstrap Scripts

Out of scope. The `add-user.sh.tmpl` and `add-team.sh.tmpl` scripts write agent JSON with the old `slack_member_id`/`slack_channel` format. These must be updated in a follow-up before the AWS provider is functional with the new schema.

**Impact**: AWS provider is broken after this refactor until bootstrap scripts are updated. This is acceptable per requirements (breaking changes OK, AWS scripts deferred).

### SetupConfig JSON Input

Old: `{"slack_bot_token": "xoxb-...", "slack_signing_secret": "..."}`
New: `{"secrets": {"slack-bot-token": "xoxb-...", "slack-signing-secret": "..."}}`

No backward compat shim — breaking change.

---

## 11. Files Summary

### New Files (5)

| File | Lines (est.) | Purpose |
|------|-------------|---------|
| `cli/internal/channels/channels.go` | ~60 | Interface, types (`ChannelBinding`, `SecretDef`, `RoutingEntry`) |
| `cli/internal/channels/registry.go` | ~50 | Registry + `ParseBinding()` |
| `cli/internal/channels/slack/slack.go` | ~130 | Slack `Channel` implementation |
| `cli/internal/channels/slack/slack_test.go` | ~200 | 13 test cases |
| `cli/internal/channels/registry_test.go` | ~40 | Registry + ParseBinding tests |

### Modified Files (~15)

| File | Change Summary |
|------|---------------|
| `cli/internal/provider/provider.go` | `AgentConfig`: remove `SlackMemberID`/`SlackChannel`, add `Channels []ChannelBinding`, add `ChannelBinding()` helper |
| `cli/internal/provider/setup_config.go` | Replace Slack fields with `Secrets map[string]string`, simplify `SecretValue()` |
| `cli/internal/common/config.go` | Remove `SharedSecrets.Slack*` fields, add `Values` map; remove `HasSlack()`; refactor `GenerateOpenClawConfig()` and `GenerateEnvFile()` to delegate to channels |
| `cli/internal/common/routing.go` | Refactor `GenerateRoutingJSON()` to delegate to channels |
| `cli/internal/common/behavior.go` | Replace `{{SLACK_ID}}` hardcoding with channel-provided template vars |
| `cli/internal/common/validate.go` | Remove `ValidateMemberID()`, `ValidateChannelID()` (moved to Slack channel) |
| `cli/cmd/admin.go` | Update `add-user`/`add-team` command defs (remove positional arg, add `--channel`); update `list-agents` display |
| `cli/cmd/admin_provision.go` | Rewrite to use `channels.ParseBinding()` + `ch.ValidateBinding()` |
| `cli/cmd/root.go` | Remove `validateMemberID()`/`validateChannelID()` wrappers; add Slack import |
| `cli/cmd/json_schema.go` | Update schemas for add-user, add-team, list-agents, setup |
| `cli/internal/mcpserver/tools_lifecycle.go` | Replace `slack_member_id`/`slack_channel` with `channel` param |
| `cli/internal/provider/localprovider/provider.go` | `readSharedSecrets()` → generic map; `Setup()` → channel-driven prompts; `HasSlack()` → `ch.HasCredentials()` |
| `cli/internal/provider/remoteprovider/provider.go` | Same changes as local |
| `cli/internal/provider/remoteprovider/setup.go` | Channel-driven setup prompts |
| `cli/internal/provider/remoteprovider/secrets.go` | `readSharedSecrets()` → generic map |

### Modified Test Files (~5)

| File | Change Summary |
|------|---------------|
| `cli/internal/common/routing_test.go` | Update `AgentConfig` literals to use `Channels` field |
| `cli/internal/common/validate_test.go` | Remove Slack validation tests (moved to `slack/slack_test.go`) |
| `cli/cmd/root_test.go` | Remove/update validation wrapper tests |
| `cli/internal/mcpserver/server_test.go` | Update provision tool param from `slack_member_id` → `channel` |
| `cli/internal/provider/awsprovider/provider_test.go` | Update SharedSecrets construction |
