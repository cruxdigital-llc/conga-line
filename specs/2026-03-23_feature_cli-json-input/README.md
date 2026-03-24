# Trace Log — CLI JSON Input

**Feature**: CLI JSON Input
**Created**: 2026-03-23
**Status**: Implemented

## Session Log

### 2026-03-23 — Planning Session

- **Started**: Plan Feature workflow
- **Goal**: Enable JSON objects to be passed into CLI commands for scripting — allow users to provide structured input as JSON via stdin or flags instead of interactive prompts, enabling automation and scripting of conga CLI commands.
- **Primary use case**: LLMs and autonomous agents driving the CLI as a tool

#### Active Capabilities
- **File I/O**: Read/write Go source files
- **Shell**: Run `go build`, `go test`, `go vet` for validation
- No browser/UI, database, or external PM tools active

#### Active Personas
- **Architect**: Architecture fit, pattern consistency, no unnecessary dependencies
- **QA**: Edge cases, unhappy paths, test coverage for JSON parsing and validation

#### Decisions
- All commands will support JSON input and output (not a subset)
- `--json '{...}'` flag for input (persistent root flag), `--output json` for output mode
- `--json` implies `--output json` automatically
- No new dependencies — stdlib `encoding/json` only
- Confirmations auto-accepted in JSON mode
- Human text suppressed/routed to stderr in JSON mode
- Schema discovery via `conga json-schema <command>` for LLM self-service
- `connect` command in JSON mode emits connection info and exits (no blocking)
- Existing `--config` flag on `admin setup` preserved for backward compatibility

#### Files Created
- `specs/2026-03-23_feature_cli-json-input/requirements.md`
- `specs/2026-03-23_feature_cli-json-input/plan.md`
- `specs/2026-03-23_feature_cli-json-input/spec.md`

### 2026-03-23 — Spec Session

- **Spec created**: Detailed technical specification covering all 20 commands
- **Approach**: `*J` variant functions in `ui` package (e.g., `TextPromptJ`, `ConfirmJ`) that check `JSONInputActive` and fall through to interactive prompts when not in JSON mode
- **New files**: 5 (`json_mode.go`, `json_output.go`, `json_schema.go`, + 2 test files)
- **Modified files**: 17 (`root.go` + all command files + `spinner.go` + `table.go`)

#### Persona Review Results
- **Architect**: ✅ Approved. Clean, minimal, consistent. No new dependencies. No provider changes. Package-level state is acceptable for single-threaded CLI.
- **QA**: ✅ Approved with one addition. Edge cases well-covered. Added: `--json` and `--config` mutual exclusion on `admin setup`.

#### Standards Gate Report (Pre-Implementation)
| Standard | Verdict |
|---|---|
| Zero trust the AI agent | ✅ PASSES |
| Immutable configuration | ✅ PASSES |
| Least privilege | ✅ PASSES |
| Secrets never touch disk | ✅ PASSES |
| Network security | ✅ PASSES |
| Container isolation | ✅ PASSES |
| `@file.json` file read | ⚠️ WARNING — accepted (same risk as existing `--config` flag) |

**Gate**: PASS (0 violations, 1 accepted warning)

### 2026-03-23 — Implementation Session

All 6 phases completed. 24/24 tasks done.

#### New Files Created
- `cli/internal/ui/json_mode.go` — JSON mode state, `SetJSONMode`, typed getters (`GetString`, `GetInt`, `GetBool`), `*J` prompt variants (`TextPromptJ`, `SecretPromptJ`, `ConfirmJ`)
- `cli/internal/ui/json_output.go` — `EmitJSON`, `EmitError`, `Info`, `Infoln` (stderr routing in JSON mode)
- `cli/internal/ui/json_mode_test.go` — 25 unit tests for JSON mode
- `cli/cmd/json_schema.go` — `json-schema` command with schema registry for all 20 commands

#### Modified Files
- `cli/cmd/root.go` — `--json` and `--output` persistent flags, JSON mode init in `PersistentPreRunE`, JSON error in `Execute()`
- `cli/internal/ui/spinner.go` — no-op spinner in JSON mode
- `cli/internal/ui/table.go` — no-op PrintTable in JSON mode
- `cli/cmd/version.go` — JSON output
- `cli/cmd/auth.go` — JSON output for `login` and `status`
- `cli/cmd/status.go` — JSON output with full status struct
- `cli/cmd/logs.go` — JSON output with lines array
- `cli/cmd/secrets.go` — JSON input for `set`, JSON output for `set`/`list`/`delete`, auto-confirm for `delete`
- `cli/cmd/refresh.go` — JSON output
- `cli/cmd/connect.go` — JSON output + early exit in JSON mode
- `cli/cmd/admin.go` — JSON output for `list-agents` (emits `[]AgentConfig`)
- `cli/cmd/admin_setup.go` — JSON input → `SetupConfig`, `--json`/`--config` mutual exclusion, JSON output
- `cli/cmd/admin_provision.go` — JSON input for `add-user`/`add-team` (slack IDs, gateway port, IAM identity), JSON output
- `cli/cmd/admin_remove.go` — auto-confirm in JSON mode, JSON output
- `cli/cmd/admin_cycle.go` — auto-confirm in JSON mode, JSON output
- `cli/cmd/admin_refresh_all.go` — auto-confirm in JSON mode, JSON output
- `cli/cmd/admin_teardown.go` — auto-confirm in JSON mode, JSON output
- `cli/cmd/admin_pause.go` — JSON output for pause and unpause

#### Verification
- `go build ./...` — clean
- `go test ./...` — all packages pass
- `go vet ./...` — clean
- Zero new dependencies

### 2026-03-23 — Verification Session

#### Automated Verification
- `go test ./... -count=1` — all 8 test packages pass (no cache)
- `go vet ./...` — clean

#### Persona Verification
- **Architect**: ✅ Approved. No new dependencies. Pattern consistent (early JSON return, text fallthrough). Provider interface change (`Setup` signature) is minimal and correctly propagated. JSON mode init before provider init ensures JSON error formatting for all failures.
- **QA**: ✅ Approved. 25 unit tests covering all public functions, edge cases (malformed JSON, missing keys, wrong types, nil data, file not found), and `*J` variant behavior. All spec edge cases implemented.

#### Standards Gate (Post-Implementation)
| Standard | Verdict |
|---|---|
| Zero trust the AI agent | ✅ PASSES |
| Immutable configuration | ✅ PASSES |
| Least privilege | ✅ PASSES |
| Secrets never touch disk | ✅ PASSES |
| Container isolation | ✅ PASSES |
| `@file.json` file read | ⚠️ WARNING — pre-accepted |

**Gate**: PASS

#### Spec Retrospection
- Provider interface divergence documented: `Setup(ctx)` → `Setup(ctx, *SetupConfig)` was necessary for non-interactive setup. `spec.md` updated to reflect this.
- Confirmation pattern divergence: commands use `!ui.JSONInputActive` guard instead of `ConfirmJ()` — equivalent behavior, cleaner integration with existing `--force` flag.

#### Test Synchronization
- No stale references found
- All public methods have corresponding tests
- Sibling comparison with `prompt_test.go` — no gaps
- Full regression suite passes
