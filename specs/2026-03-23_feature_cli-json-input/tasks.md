# Tasks — CLI JSON Input

## Phase 1: Infrastructure
- [x] 1.1 Create `cli/internal/ui/json_mode.go` — state vars, `SetJSONMode`, typed getters, `*J` prompt variants
- [x] 1.2 Create `cli/internal/ui/json_output.go` — `EmitJSON`, `EmitError`, `Info`, `Infoln`
- [x] 1.3 Modify `cli/cmd/root.go` — add `--json` + `--output` flags, init in `PersistentPreRunE`, JSON error in `Execute()`
- [x] 1.4 Modify `cli/internal/ui/spinner.go` — no-op when `OutputJSON` is true
- [x] 1.5 Modify `cli/internal/ui/table.go` — no-op when `OutputJSON` is true
- [x] 1.6 Create `cli/internal/ui/json_mode_test.go` — unit tests for JSON mode (25 tests, all pass)
- [x] 1.7 Verify: `go build` and `go test ./cli/...` pass

## Phase 2: Tier 1 Commands (output only)
- [x] 2.1 `version.go` — JSON output
- [x] 2.2 `auth.go` — JSON output for `login` and `status`
- [x] 2.3 `status.go` — JSON output with `StatusResult` struct
- [x] 2.4 `logs.go` — JSON output with lines array
- [x] 2.5 `admin.go` — JSON output for `list-agents`
- [x] 2.6 `admin_pause.go` — JSON output for `pause` and `unpause`
- [x] 2.7 Verify: `go build` passes

## Phase 3: Tier 2 Commands (simple input + output)
- [x] 3.1 `secrets.go` — JSON input for `set`, JSON output for `set`/`list`/`delete`, auto-confirm for `delete`
- [x] 3.2 `admin_remove.go` — auto-confirm, JSON output
- [x] 3.3 `admin_cycle.go` — auto-confirm, JSON output
- [x] 3.4 `admin_refresh_all.go` — auto-confirm, JSON output
- [x] 3.5 `admin_teardown.go` — auto-confirm, JSON output
- [x] 3.6 `refresh.go` — JSON output
- [x] 3.7 `connect.go` — JSON output + early exit in JSON mode
- [x] 3.8 Verify: `go build` passes

## Phase 4: Tier 3 Commands (complex input)
- [x] 4.1 `admin_provision.go` — JSON input for `add-user`/`add-team`, JSON output
- [x] 4.2 `admin_setup.go` — JSON input → `SetupConfig`, mutual exclusion with `--config`, JSON output
- [x] 4.3 Verify: `go build` passes

## Phase 5: Schema Discovery
- [x] 5.1 Create `cli/cmd/json_schema.go` — `json-schema` command + schema registry for all 20 commands
- [x] 5.2 Verify: `go build` passes

## Phase 6: Final Verification
- [x] 6.1 `go test ./cli/...` — all tests pass (all packages OK)
- [x] 6.2 `go vet ./cli/...` — no issues
