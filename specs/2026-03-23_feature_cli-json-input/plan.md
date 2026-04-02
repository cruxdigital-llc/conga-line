# Plan — CLI JSON Input

## Approach

Add a `--json` persistent flag to the root command that activates two behaviors:
1. **Input**: The flag value is a JSON string (or `@file.json`) whose fields replace interactive prompts
2. **Output**: All command output is emitted as structured JSON to stdout; human text goes to stderr

This is implemented as a thin layer between the command handlers and the existing UI/provider code — no provider changes needed.

## Phase 1: JSON Mode Infrastructure (root + ui package)

**Files**: `cli/cmd/root.go`, `cli/pkg/ui/json_mode.go`

1. Add `--json` persistent flag to root command (string, optional value)
   - When present with a value: JSON input mode + JSON output mode
   - When present as `--output json`: output-only mode (no input override)
   - Store parsed JSON as `map[string]interface{}` accessible to commands
2. Add `--output` persistent flag (`text` default, `json` option)
   - `--json '...'` implies `--output json` automatically
3. Create `ui/json_mode.go`:
   - `var JSONMode bool` — global flag checked by all ui functions
   - `var JSONInput map[string]interface{}` — parsed input data
   - `SetJSONMode(input string) error` — parse and activate
   - `GetJSONString(key string) (string, bool)` — read string from input
   - `GetJSONInt(key string) (int, bool)` — read int from input
   - `GetJSONBool(key string) (bool, bool)` — read bool from input
4. Modify existing `ui` functions for JSON mode:
   - `TextPrompt()` → if JSONMode, look up key from JSONInput; error if not found
   - `SecretPrompt()` → same behavior (no special terminal handling in JSON mode)
   - `Confirm()` → auto-return true in JSON mode (like `--force`)
   - `Spinner` → no-op in JSON mode (or write to stderr)
   - `PrintTable()` → no-op in JSON mode (output handled by command)

**Key design decision**: Rather than modifying every `TextPrompt("label")` call, we add a key-mapping system. Each prompt call gets an optional key parameter that maps to the JSON input field. This keeps changes minimal.

## Phase 2: JSON Output Envelope

**Files**: `cli/pkg/ui/json_output.go`

1. Create output helpers:
   - `type Result struct` — wraps any command output
   - `EmitJSON(v interface{})` — marshal + print to stdout
   - `EmitError(err error)` — `{"error":"msg"}` to stdout
   - `Fprintf(w io.Writer, ...)` — in JSON mode, routes to stderr; in text mode, routes to stdout
2. Define per-command output structs (can be in each command file or a shared `types.go`):
   - `StatusResult` — mirrors `AgentStatus` with JSON tags
   - `SecretsListResult` — array of secret entries
   - `ProvisionResult` — agent name, port, status
   - `SetupResult` — provider, status
   - `AgentListResult` — array of agent configs
   - Simple commands: `{"status":"ok"}` or `{"status":"deleted"}`

## Phase 3: Command-by-Command Wiring

Wire each command to use JSON input and produce JSON output. Ordered by complexity (simplest first):

### Tier 1 — Output only (no interactive input)
These commands have no prompts; they just need JSON output formatting.

| Command | Output Struct | Notes |
|---------|--------------|-------|
| `version` | `{"version":"...","commit":"...","date":"..."}` | |
| `status` | Mirrors `AgentStatus` | Already has structured data |
| `logs` | `{"agent":"...","lines":[...]}` | Array of log lines |
| `auth status` | `{"name":"...","agent":"...","account_id":"..."}` | Mirrors `Identity` |
| `secrets list` | `[{"name":"...","env_var":"...","last_changed":"..."}]` | Already structured |
| `admin list-agents` | `[AgentConfig]` | Already JSON-serializable |
| `admin pause` | `{"agent":"...","status":"paused"}` | |
| `admin unpause` | `{"agent":"...","status":"unpaused"}` | |

### Tier 2 — Input + Output (simple: flags/args replace prompts)
These have minor interactive elements that can be replaced by flags or JSON fields.

| Command | JSON Input Fields | Notes |
|---------|------------------|-------|
| `secrets set` | `{"name":"...","value":"..."}` | Already has `--value` flag; just need name from JSON |
| `secrets delete` | `{}` (name is positional) | Auto-confirm in JSON mode |
| `admin remove-agent` | `{}` (name is positional) | Auto-confirm |
| `admin cycle-host` | `{}` | Auto-confirm |
| `admin refresh-all` | `{}` | Auto-confirm |
| `admin teardown` | `{}` | Auto-confirm |
| `refresh` | `{}` | No prompts |
| `connect` | `{}` | Output: `{"url":"...","port":...}` |

### Tier 3 — Input + Output (complex: multiple prompts)
These have multi-field interactive wizards.

| Command | JSON Input Fields | Notes |
|---------|------------------|-------|
| `admin add-user` | `{"slack_member_id":"...","gateway_port":...,"iam_identity":"..."}` | Args still work for name + member_id |
| `admin add-team` | `{"slack_channel":"...","gateway_port":...}` | Args still work for name + channel |
| `admin setup` | Reuse existing `SetupConfig` schema | Subsumes `--config` flag |

## Phase 4: Schema Discovery

**Files**: `cli/cmd/json_schema.go`

1. Add `conga --json-schema <command>` (or `conga json-schema <command>`) that prints the JSON input/output schema for any command
   - Input: which fields the command accepts via `--json`
   - Output: what the JSON response looks like
   - Format: JSON Schema (draft-07) or a simpler custom format
2. Add `conga json-schema --all` to dump all command schemas at once
3. This enables LLMs to self-discover the API surface without documentation

## Phase 5: Tests

**Files**: `cli/cmd/*_test.go`, `cli/pkg/ui/json_mode_test.go`

1. `json_mode_test.go`:
   - Parse valid JSON, empty JSON, malformed JSON
   - GetJSONString/Int/Bool with present and missing keys
   - Type mismatch handling
2. Per-command tests:
   - Each Tier 1 command: verify JSON output matches expected struct
   - Each Tier 2 command: verify JSON input skips prompts, verify output
   - Each Tier 3 command: verify complex JSON input fills all fields
   - Error cases: missing required fields, invalid values, unknown fields
3. Integration pattern: commands that use `ui.Confirm()` auto-accept in JSON mode

## Implementation Order

```
Phase 1 → Phase 2 → Phase 3 (Tier 1 → Tier 2 → Tier 3) → Phase 4 → Phase 5
```

Phase 5 (tests) runs in parallel with each phase — write tests alongside implementation.

## Risk Assessment

| Risk | Mitigation |
|------|-----------|
| JSON mode accidentally suppresses real errors | All errors go to both stdout (as JSON) and exit code |
| Breaking existing `--config` flag | `--config` preserved, `--json` on setup delegates to same `ParseSetupConfig` |
| UI functions called without JSON key mapping | Default: error with "missing JSON field for prompt: {label}" |
| Large JSON output for `logs` command | Cap at `--lines` (existing), return array not single string |
| `connect` command is long-running (waits for Ctrl+C) | In JSON mode, emit connection info and exit (no blocking) |

## Estimated Scope

- **New files**: 3 (`json_mode.go`, `json_output.go`, `json_schema.go`)
- **Modified files**: ~15 (root.go + every command file + ui/prompt.go + ui/spinner.go + ui/table.go)
- **No provider changes**: All changes are in `cmd/` and `ui/` layers
- **No new dependencies**: stdlib `encoding/json` only
