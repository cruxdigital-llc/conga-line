# Specification — CLI JSON Input

## Overview

Add `--json` and `--output` persistent flags to the root command that make every `conga` CLI command fully drivable by LLMs and automation. `--json '<data>'` provides structured input (replacing interactive prompts) and implies `--output json`. `--output json` switches all command output to machine-readable JSON on stdout.

---

## 1. Flag Design

### 1.1 Root-Level Persistent Flags

Add to `cli/cmd/root.go`:

```go
var (
    flagJSON   string // --json '{"key":"val"}' or --json @file.json
    flagOutput string // --output text|json (default: "text")
)
```

Registration in `init()`:

```go
rootCmd.PersistentFlags().StringVar(&flagJSON, "json", "", `JSON input (inline or @file.json); implies --output json`)
rootCmd.PersistentFlags().StringVar(&flagOutput, "output", "text", `Output format: text, json`)
```

### 1.2 Flag Semantics

| Invocation | Input Mode | Output Mode |
|---|---|---|
| `conga status` | interactive | text |
| `conga status --output json` | interactive | json |
| `conga admin setup --json '{...}'` | json | json |
| `conga admin setup --json @setup.json` | json (from file) | json |

- `--json ''` (empty string) is ignored (no input override, no output switch).
- `--json '{}'` activates JSON mode with an empty input object. Prompts that require input will error.
- `--json` with `--output text` is an error — JSON input always implies JSON output.

### 1.3 Initialization in PersistentPreRunE

After provider init in `root.go`:

```go
// Initialize JSON mode
if flagJSON != "" {
    if err := ui.SetJSONMode(flagJSON); err != nil {
        // In JSON mode, even init errors must be JSON
        ui.EmitError(err)
        os.Exit(1)
    }
}
if flagOutput == "json" {
    ui.OutputJSON = true
}
```

---

## 2. UI Package — JSON Mode (`cli/internal/ui/json_mode.go`)

### 2.1 Package-Level State

```go
package ui

import (
    "encoding/json"
    "fmt"
    "os"
    "strings"
)

var (
    // JSONInputActive is true when --json was provided with data.
    JSONInputActive bool

    // OutputJSON is true when output should be JSON (--output json or implied by --json).
    OutputJSON bool

    // jsonData holds the parsed input object.
    jsonData map[string]interface{}
)
```

### 2.2 SetJSONMode

```go
func SetJSONMode(input string) error {
    input = strings.TrimSpace(input)
    if input == "" {
        return nil
    }

    var raw []byte
    if strings.HasPrefix(input, "@") {
        // File reference: --json @setup.json
        var err error
        raw, err = os.ReadFile(input[1:])
        if err != nil {
            return fmt.Errorf("reading JSON file %s: %w", input[1:], err)
        }
    } else {
        raw = []byte(input)
    }

    jsonData = make(map[string]interface{})
    if err := json.Unmarshal(raw, &jsonData); err != nil {
        return fmt.Errorf("invalid JSON input: %w", err)
    }

    JSONInputActive = true
    OutputJSON = true
    return nil
}
```

### 2.3 Typed Getters

```go
// GetString returns a string value from JSON input. Returns ("", false) if not present.
func GetString(key string) (string, bool) {
    if jsonData == nil {
        return "", false
    }
    v, ok := jsonData[key]
    if !ok {
        return "", false
    }
    s, ok := v.(string)
    if !ok {
        return "", false
    }
    return s, true
}

// GetInt returns an int value from JSON input.
func GetInt(key string) (int, bool) {
    if jsonData == nil {
        return 0, false
    }
    v, ok := jsonData[key]
    if !ok {
        return 0, false
    }
    // JSON numbers decode as float64
    f, ok := v.(float64)
    if !ok {
        return 0, false
    }
    return int(f), true
}

// GetBool returns a bool value from JSON input.
func GetBool(key string) (bool, bool) {
    if jsonData == nil {
        return false, false
    }
    v, ok := jsonData[key]
    if !ok {
        return false, false
    }
    b, ok := v.(bool)
    if !ok {
        return false, false
    }
    return b, true
}

// MustGetString returns a string from JSON input or an error if missing.
func MustGetString(key string) (string, error) {
    s, ok := GetString(key)
    if !ok {
        return "", fmt.Errorf("missing required JSON field: %q", key)
    }
    return s, nil
}
```

### 2.4 Modified Prompt Functions

**`TextPrompt`** — add JSON key parameter:

```go
// TextPromptJ is the JSON-aware version. In JSON mode, reads from JSON input.
// In text mode, falls through to interactive prompt. Key is the JSON field name.
func TextPromptJ(key, label string) (string, error) {
    if JSONInputActive {
        return MustGetString(key)
    }
    return TextPrompt(label)
}

// TextPromptWithDefaultJ is the JSON-aware version with a default value.
func TextPromptWithDefaultJ(key, label, defaultVal string) (string, error) {
    if JSONInputActive {
        if s, ok := GetString(key); ok {
            return s, nil
        }
        return defaultVal, nil // Use default when not in JSON input
    }
    return TextPromptWithDefault(label, defaultVal)
}

// SecretPromptJ is the JSON-aware version of SecretPrompt.
func SecretPromptJ(key, label string) (string, error) {
    if JSONInputActive {
        return MustGetString(key)
    }
    return SecretPrompt(label)
}
```

**`Confirm`** — auto-accept in JSON mode:

```go
// ConfirmJ returns true in JSON mode (like --force). In text mode, prompts.
func ConfirmJ(prompt string) bool {
    if JSONInputActive {
        return true
    }
    return Confirm(prompt)
}
```

**Existing functions (`TextPrompt`, `SecretPrompt`, `Confirm`) remain unchanged** for backward compatibility. Commands are updated to call the `*J` variants where JSON input is needed.

### 2.5 Modified Spinner

```go
func NewSpinner(msg string) *Spinner {
    if OutputJSON {
        // Return a no-op spinner — no terminal output in JSON mode
        s := &Spinner{msg: msg, done: make(chan struct{})}
        s.wg.Add(1)
        go func() {
            defer s.wg.Done()
            <-s.done
        }()
        return s
    }
    // ... existing implementation
}
```

### 2.6 Modified PrintTable

```go
func PrintTable(headers []string, rows [][]string) {
    if OutputJSON {
        return // Caller handles JSON output separately
    }
    // ... existing implementation
}
```

---

## 3. JSON Output (`cli/internal/ui/json_output.go`)

### 3.1 Output Helpers

```go
package ui

import (
    "encoding/json"
    "fmt"
    "io"
    "os"
)

// EmitJSON writes a JSON-serializable value to stdout with a trailing newline.
func EmitJSON(v interface{}) {
    data, err := json.MarshalIndent(v, "", "  ")
    if err != nil {
        EmitError(fmt.Errorf("failed to marshal JSON output: %w", err))
        return
    }
    os.Stdout.Write(data)
    os.Stdout.Write([]byte("\n"))
}

// EmitError writes {"error": "message"} to stdout.
func EmitError(err error) {
    data, _ := json.Marshal(map[string]string{"error": err.Error()})
    os.Stdout.Write(data)
    os.Stdout.Write([]byte("\n"))
}

// Info writes human-readable text to stderr in JSON mode, stdout in text mode.
func Info(format string, args ...interface{}) {
    if OutputJSON {
        fmt.Fprintf(os.Stderr, format, args...)
    } else {
        fmt.Printf(format, args...)
    }
}

// Infoln writes a human-readable line. Stderr in JSON mode, stdout in text mode.
func Infoln(msg string) {
    if OutputJSON {
        fmt.Fprintln(os.Stderr, msg)
    } else {
        fmt.Println(msg)
    }
}
```

### 3.2 Error Handling in Root

Modify `Execute()` in `root.go` to catch errors and emit JSON:

```go
func Execute() {
    if err := rootCmd.Execute(); err != nil {
        if ui.OutputJSON {
            ui.EmitError(err)
        }
        os.Exit(1)
    }
}
```

Cobra's default error printing goes to stderr, which is fine — the JSON error object goes to stdout.

---

## 4. Per-Command Output Structs & JSON Wiring

Each command gets a conditional branch: if `ui.OutputJSON`, build a struct and call `ui.EmitJSON()`; otherwise, run existing text output.

### 4.1 `version`

```go
// version.go
if ui.OutputJSON {
    ui.EmitJSON(map[string]string{
        "version": Version,
        "commit":  Commit,
        "date":    Date,
    })
    return
}
```

### 4.2 `auth status`

```go
type AuthStatusResult struct {
    Identity  string `json:"identity"`
    AccountID string `json:"account_id,omitempty"`
    Provider  string `json:"provider"`
    Agent     string `json:"agent,omitempty"`
}
```

### 4.3 `auth login`

```go
type AuthLoginResult struct {
    Provider string `json:"provider"`
    Command  string `json:"command,omitempty"` // The SSO login command to run
    Message  string `json:"message"`
}
```

### 4.4 `status`

```go
type StatusResult struct {
    Agent        string `json:"agent"`
    Container    string `json:"container"`              // "running", "stopped", "not found"
    Service      string `json:"service"`
    Readiness    string `json:"readiness,omitempty"`
    Paused       bool   `json:"paused,omitempty"`
    StartedAt    string `json:"started_at,omitempty"`
    Uptime       string `json:"uptime,omitempty"`
    RestartCount int    `json:"restart_count,omitempty"`
    CPU          string `json:"cpu,omitempty"`
    Memory       string `json:"memory,omitempty"`
    PIDs         int    `json:"pids,omitempty"`
}
```

### 4.5 `logs`

```go
type LogsResult struct {
    Agent string   `json:"agent"`
    Lines []string `json:"lines"`
}
```

Split on `\n`, omit trailing empty line.

### 4.6 `secrets list`

```go
type SecretListEntry struct {
    Name        string `json:"name"`
    EnvVar      string `json:"env_var"`
    LastChanged string `json:"last_changed,omitempty"`
}
// Output: []SecretListEntry
```

### 4.7 `secrets set`

JSON input fields: `name` (string), `value` (string).

```go
type SecretSetResult struct {
    Secret string `json:"secret"`
    EnvVar string `json:"env_var"`
    Status string `json:"status"` // "saved"
}
```

Command change: if `JSONInputActive`, read `name` and `value` from JSON input instead of prompts. The `--value` flag still takes precedence.

### 4.8 `secrets delete`

```go
type SecretDeleteResult struct {
    Secret string `json:"secret"`
    Status string `json:"status"` // "deleted"
}
```

`ConfirmJ()` auto-accepts in JSON mode.

### 4.9 `admin list-agents`

Output: `[]provider.AgentConfig` — already has JSON tags. Emit directly.

### 4.10 `admin add-user`

JSON input fields: `slack_member_id` (string, optional), `gateway_port` (int, optional), `iam_identity` (string, optional).

Positional arg `name` is still required as an arg (not from JSON). `slack_member_id` can come from either arg[1] or JSON.

```go
type ProvisionResult struct {
    Agent       string `json:"agent"`
    Type        string `json:"type"`
    GatewayPort int    `json:"gateway_port"`
    Status      string `json:"status"` // "provisioned"
}
```

### 4.11 `admin add-team`

JSON input fields: `slack_channel` (string, optional), `gateway_port` (int, optional).

Same output struct as `admin add-user` (`ProvisionResult`).

### 4.12 `admin setup`

JSON input: The `--json` value is parsed as a `SetupConfig` struct (same schema as existing `--config`).

When `--json` is provided for `admin setup`, convert the raw JSON input to a `SetupConfig` and pass it to `prov.Setup(ctx, cfg)`. This replaces all prompts.

```go
type SetupResult struct {
    Provider string `json:"provider"`
    Status   string `json:"status"` // "configured"
}
```

Implementation: In `adminSetupRun`, if `ui.JSONInputActive`:
1. Re-serialize `ui.jsonData` to JSON bytes
2. Unmarshal into `provider.SetupConfig`
3. Pass to `prov.Setup(ctx, &cfg)`
4. Emit `SetupResult`

This means `--json` on `admin setup` accepts the same schema as `--config`.

### 4.13 `admin remove-agent`

```go
type RemoveResult struct {
    Agent  string `json:"agent"`
    Status string `json:"status"` // "removed"
}
```

### 4.14 `admin cycle-host`

```go
// Output: {"status": "ok"}
```

### 4.15 `admin refresh-all`

```go
type RefreshAllResult struct {
    Count  int    `json:"agents_refreshed"`
    Status string `json:"status"` // "ok"
}
```

### 4.16 `admin teardown`

```go
// Output: {"status": "ok"}
```

### 4.17 `admin pause`

```go
type PauseResult struct {
    Agent  string `json:"agent"`
    Status string `json:"status"` // "paused"
}
```

### 4.18 `admin unpause`

```go
type UnpauseResult struct {
    Agent  string `json:"agent"`
    Status string `json:"status"` // "unpaused"
}
```

### 4.19 `refresh`

```go
type RefreshResult struct {
    Agent  string `json:"agent"`
    Status string `json:"status"` // "refreshed"
}
```

### 4.20 `connect`

```go
type ConnectResult struct {
    Agent string `json:"agent"`
    URL   string `json:"url"`
    Port  int    `json:"port"`
    Token string `json:"token,omitempty"`
}
```

**Behavior change in JSON mode**: Emit the connection info and **exit immediately** (do not block waiting for Ctrl+C). The caller can hold the connection open by other means or issue another `connect` when needed. For providers with a `Waiter` (AWS SSM tunnel), the tunnel is established, info emitted, and the process exits — the tunnel terminates.

---

## 5. Schema Discovery (`cli/cmd/json_schema.go`)

### 5.1 Command

```
conga json-schema [command-path]    # e.g., conga json-schema admin.setup
conga json-schema --all             # dump all schemas
```

### 5.2 Schema Format

Lightweight JSON, not full JSON Schema (draft-07). Keeps things simple and LLM-friendly:

```json
{
  "command": "admin setup",
  "input": {
    "fields": {
      "ssh_host": {"type": "string", "required": false, "description": "SSH host for remote provider"},
      "ssh_port": {"type": "integer", "required": false, "description": "SSH port"},
      "image": {"type": "string", "required": false, "description": "Docker image to deploy"},
      "slack_bot_token": {"type": "string", "required": false, "description": "Slack bot token (xoxb-...)"},
      ...
    }
  },
  "output": {
    "fields": {
      "provider": {"type": "string"},
      "status": {"type": "string", "enum": ["configured"]}
    }
  }
}
```

### 5.3 Implementation

Each command registers its schema via a map in a shared `schemas.go` file:

```go
var commandSchemas = map[string]CommandSchema{
    "version":           { /* ... */ },
    "status":            { /* ... */ },
    "admin.setup":       { /* ... */ },
    "admin.add-user":    { /* ... */ },
    // ...
}
```

The `json-schema` command looks up the key and emits it. `--all` dumps the full map.

---

## 6. Edge Cases & Error Handling

### 6.1 Malformed JSON Input

```
$ conga status --json '{bad'
{"error":"invalid JSON input: unexpected end of JSON input"}
```
Exit code 1. No text output to stdout.

### 6.2 Missing Required Field

```
$ conga secrets set --json '{}' --agent myagent
{"error":"missing required JSON field: \"name\""}
```

### 6.3 Unknown Fields

Silently ignored. This enables forward compatibility — an LLM sending new fields to an older CLI version won't break.

### 6.4 Type Mismatch

```
$ conga admin add-user myagent --json '{"gateway_port":"not_a_number"}'
```
`GetInt("gateway_port")` returns `(0, false)`. The command falls through to auto-assignment. No error unless the field is required.

### 6.5 Empty JSON Object on Commands That Need Input

```
$ conga secrets set --json '{}'
{"error":"missing required JSON field: \"name\""}
```

### 6.6 `--json` With `--output text`

```
$ conga status --json '{}' --output text
{"error":"--json implies --output json; cannot use --output text with --json"}
```

### 6.7 File Not Found for `@file.json`

```
$ conga admin setup --json @missing.json
{"error":"reading JSON file missing.json: open missing.json: no such file or directory"}
```

### 6.8 Mixing Positional Args + JSON

Positional args and flags take precedence over JSON input. Example:

```
$ conga admin add-user myagent U99999 --json '{"slack_member_id":"U11111"}'
```

Agent name is `myagent` (positional), Slack member ID is `U99999` (positional takes precedence over JSON's `U11111`).

### 6.9 `connect` in JSON Mode

Emits `ConnectResult` and exits. Does **not** block. The LLM/agent gets the URL and port immediately.

### 6.10 Commands That Output "Cancelled"

In JSON mode, confirmation is auto-accepted (like `--force`). The `"Cancelled."` path is never reached. If an explicit `--force=false` is set alongside `--json`, `--json` wins (JSON mode always auto-confirms).

---

## 7. File Inventory

### New Files

| File | Purpose |
|---|---|
| `cli/internal/ui/json_mode.go` | JSON mode state, getters, modified prompt variants |
| `cli/internal/ui/json_output.go` | `EmitJSON`, `EmitError`, `Info`, `Infoln` |
| `cli/cmd/json_schema.go` | `json-schema` command + schema registry |
| `cli/internal/ui/json_mode_test.go` | Unit tests for JSON mode parsing and getters |
| `cli/cmd/json_schema_test.go` | Tests for schema command |

### Modified Files

| File | Change |
|---|---|
| `cli/cmd/root.go` | Add `--json`, `--output` flags; init JSON mode in `PersistentPreRunE`; JSON error in `Execute()` |
| `cli/internal/ui/spinner.go` | No-op when `OutputJSON` is true |
| `cli/internal/ui/table.go` | No-op when `OutputJSON` is true |
| `cli/cmd/version.go` | Add JSON output branch |
| `cli/cmd/auth.go` | Add JSON output for `login` and `status` |
| `cli/cmd/status.go` | Add JSON output branch |
| `cli/cmd/logs.go` | Add JSON output branch (split lines) |
| `cli/cmd/secrets.go` | JSON input for `set`; JSON output for `set`, `list`, `delete`; `ConfirmJ` for `delete` |
| `cli/cmd/connect.go` | JSON output + early exit in JSON mode |
| `cli/cmd/refresh.go` | Add JSON output branch |
| `cli/cmd/admin.go` | JSON output for `list-agents` |
| `cli/cmd/admin_setup.go` | JSON input → `SetupConfig`; JSON output |
| `cli/cmd/admin_provision.go` | JSON input for `add-user`/`add-team`; JSON output |
| `cli/cmd/admin_remove.go` | `ConfirmJ`; JSON output |
| `cli/cmd/admin_cycle.go` | `ConfirmJ`; JSON output |
| `cli/cmd/admin_refresh_all.go` | `ConfirmJ`; JSON output |
| `cli/cmd/admin_teardown.go` | `ConfirmJ`; JSON output |

### Provider Files Modified (discovered during implementation)

| File | Change |
|---|---|
| `cli/internal/provider/provider.go` | `Setup(ctx)` → `Setup(ctx, *SetupConfig)` to accept non-interactive config |
| `cli/internal/provider/setup_config.go` | New file: `SetupConfig` struct + `ParseSetupConfig()` |
| `cli/internal/provider/awsprovider/provider.go` | Updated `Setup` signature |
| `cli/internal/provider/localprovider/provider.go` | Updated `Setup` to use `SetupConfig` values when non-nil |
| `cli/internal/provider/remoteprovider/setup.go` | Updated `Setup` to use `SetupConfig` values when non-nil |

### Files NOT Modified

| File | Reason |
|---|---|
| `cli/internal/common/*` | No changes to shared logic |
| `cli/internal/ui/prompt.go` | Original functions preserved; new `*J` variants in `json_mode.go` |

---

## 8. Implementation Phases

### Phase 1: Infrastructure (~2 new files, 3 modified)
1. Create `cli/internal/ui/json_mode.go` — state, `SetJSONMode`, getters, `*J` prompt variants
2. Create `cli/internal/ui/json_output.go` — `EmitJSON`, `EmitError`, `Info`, `Infoln`
3. Modify `cli/cmd/root.go` — add flags, init in `PersistentPreRunE`, JSON error in `Execute()`
4. Modify `cli/internal/ui/spinner.go` — no-op in JSON mode
5. Modify `cli/internal/ui/table.go` — no-op in JSON mode
6. Create `cli/internal/ui/json_mode_test.go` — unit tests

### Phase 2: Tier 1 Commands — Output Only (~6 files modified)
- `version.go`, `auth.go`, `status.go`, `logs.go`, `admin.go` (list-agents), pause/unpause in admin files

### Phase 3: Tier 2 Commands — Simple Input + Output (~6 files modified)
- `secrets.go`, `admin_remove.go`, `admin_cycle.go`, `admin_refresh_all.go`, `admin_teardown.go`, `refresh.go`, `connect.go`

### Phase 4: Tier 3 Commands — Complex Input (~2 files modified)
- `admin_provision.go`, `admin_setup.go`

### Phase 5: Schema Discovery (~1 new file)
- `cli/cmd/json_schema.go` + tests

---

## 9. Test Plan

### Unit Tests (`cli/internal/ui/json_mode_test.go`)

| Test | What it verifies |
|---|---|
| `TestSetJSONMode_InlineJSON` | Parses `{"key":"val"}` correctly |
| `TestSetJSONMode_FileRef` | Reads `@file.json` and parses |
| `TestSetJSONMode_MalformedJSON` | Returns error for `{bad` |
| `TestSetJSONMode_EmptyString` | No-op, no error |
| `TestGetString_Present` | Returns `("val", true)` |
| `TestGetString_Missing` | Returns `("", false)` |
| `TestGetString_WrongType` | Returns `("", false)` for non-string |
| `TestGetInt_Float64Decoding` | JSON `42` decodes as `float64`, returns `(42, true)` |
| `TestGetBool` | Standard true/false |
| `TestMustGetString_Missing` | Returns error with field name |
| `TestTextPromptJ_JSONMode` | Reads from JSON data, no stdin |
| `TestTextPromptJ_TextMode` | Falls through to regular prompt |
| `TestConfirmJ_JSONMode` | Returns true without prompting |
| `TestEmitJSON` | Outputs valid JSON to stdout |
| `TestEmitError` | Outputs `{"error":"msg"}` to stdout |

### Command Integration Tests

For each command, verify:
1. `--output json` produces valid JSON matching the expected struct
2. `--json '{...}'` skips prompts and produces correct output
3. Error cases produce `{"error":"..."}` with non-zero exit

### Regression Tests

- All existing tests continue to pass (no JSON flags = unchanged behavior)
- Commands without `--json` or `--output` produce identical text output

---

## 10. Security Considerations

- **No secret leakage**: `--json` input may contain secrets (e.g., `slack_bot_token`). The JSON input string is held in process memory only, never logged or written to disk. Same security posture as existing `--config` and `--value` flags.
- **No new attack surface**: JSON parsing uses stdlib `encoding/json`. No deserialization of arbitrary types.
- **Auto-confirm is safe**: `--json` implies `--force` for confirmations. This is the expected behavior for automation — the caller has already decided to proceed. Same risk as `--force` flag which already exists.
- **File read via `@`**: Limited to reading a single file path. No path traversal beyond what the OS allows for the running user. Same pattern as `curl --data @file`.
