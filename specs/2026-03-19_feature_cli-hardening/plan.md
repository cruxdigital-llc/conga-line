# Plan: CLI Hardening — Design, Reliability & Test Coverage

## Approach

Six phases, ordered by risk reduction then enablement. Bug fixes first (they're small, high-value, and unblock confidence). Then testability refactoring (enables everything after). Then tests. Then organization and UX polish last.

## Phase 1: Fix Silent Failures
**Files:** `cli/cmd/admin.go`, `cli/internal/aws/secrets.go`

1. Check `json.Marshal` error at lines 227 and 308 of `admin.go`
2. Collect and report cleanup errors in `adminRemoveAgentRun` (lines 443-457)
3. Wrap `DeleteSecret` error in `secrets.go` line 91-97

## Phase 2: Tighten Validation & Small UX Fixes
**Files:** `cli/cmd/root.go`, `cli/cmd/secrets.go`, `cli/cmd/admin.go`, `cli/cmd/status.go`

1. Update `validIDPattern` to `^U[A-Z0-9]{10}$` and `validChannelPattern` to `^C[A-Z0-9]{10}$`
2. Add env var preview line in `secretsSetRun` when name is passed as argument
3. Add `--agent <name>` to next-steps in `adminAddUserRun` success message
4. Add `--timeout` global flag (default 5m), wrap `context.Background()` calls
5. Fix `pollDevicePairing` to log errors when verbose, exit cleanly on context cancellation

## Phase 3: Testability Refactoring
**Files:** `cli/internal/aws/*.go`, `cli/cmd/root.go`, `cli/internal/ui/prompt.go`

1. Define interfaces for each AWS service in a new file `cli/internal/aws/interfaces.go`:
   - `SSMClient`, `SecretsManagerClient`, `EC2Client`, `STSClient`
   - Each interface declares only the methods actually used
   - Update all function signatures to accept interfaces instead of concrete types
2. Create `CLIContext` struct in `cli/cmd/context.go`:
   - Holds `Clients`, `Profile`, `Region`, `Agent`, `Verbose` fields
   - Replace global variables with a single `cliCtx` variable
   - Command handlers receive context through closure or Cobra's `SetContext`
3. Update `ui.Confirm`, `ui.TextPrompt`, `ui.SecretPrompt`, `ui.TextPromptWithDefault` to accept `io.Reader` and `io.Writer` parameters
   - Add convenience wrappers that use `os.Stdin`/`os.Stdout` to avoid breaking all call sites at once

## Phase 4: Unit Tests
**Files:** New `*_test.go` files

### Phase 4a: Pure Function Tests (no mocks needed)
- `cli/cmd/status_test.go` — `parseKeyValues`, `splitStats`, readiness phase logic
- `cli/cmd/secrets_test.go` — `secretNameToEnvVar`
- `cli/cmd/root_test.go` — `validateAgentName`, `validateMemberID`, `validateChannelID`
- `cli/internal/discovery/identity_test.go` — ARN parsing, session name extraction

### Phase 4b: Mocked AWS Tests
- `cli/internal/aws/ssm_test.go` — `RunCommand` polling, timeout, consecutive error limit
- `cli/internal/aws/secrets_test.go` — `SetSecret` create-or-update, `ListSecrets` pagination, `DeleteSecret` error wrapping
- `cli/internal/aws/params_test.go` — `GetParameter`, `PutParameter`, `ListParametersByPath`
- `cli/internal/discovery/agent_test.go` — `ListAgents` JSON parsing, `ResolveAgent`, `ResolveAgentByIAM`
- `cli/internal/discovery/identity_test.go` — full `ResolveIdentity` flow with mocked STS + SSM

### Phase 4c: UI Tests
- `cli/internal/ui/prompt_test.go` — `Confirm` (y/n/empty/EOF), `TextPromptWithDefault` (empty input returns default)

## Phase 5: Code Organization
**Files:** `cli/cmd/admin.go` → split

1. Extract `adminSetupRun` → `cli/cmd/admin_setup.go`
2. Extract `adminAddUserRun`, `adminAddTeamRun`, `resolveGatewayPort`, `validateAgentName` → `cli/cmd/admin_provision.go`
3. Extract `adminRemoveAgentRun` → `cli/cmd/admin_remove.go`
4. Extract `adminCycleHostRun` → `cli/cmd/admin_cycle.go`
5. Keep `adminCmd` definition, flag variables, and `init()` registration in `cli/cmd/admin.go`

## Phase 6: Status Duration Display
**Files:** `cli/cmd/status.go`

1. Parse `CONTAINER_STARTED` ISO timestamp
2. Compute duration from now
3. Display as `"running (up 3h 42m)"` or `"running (up 2d 5h)"`

## CI Integration

Add `go test ./...` to `.github/workflows/release.yml` (or create a separate `ci.yml` for push/PR triggers).

## Verification

1. `go test ./... -v` — all tests pass
2. `go test ./... -coverprofile=coverage.out` — check coverage meets targets
3. Manual smoke test: `conga status`, `conga secrets list`, `conga admin list-agents` — all work as before
4. Manual test: `conga admin add-user test-agent UINVALIDX` — rejected with tighter validation message
5. Build: `go build -o conga ./cli` — compiles cleanly
