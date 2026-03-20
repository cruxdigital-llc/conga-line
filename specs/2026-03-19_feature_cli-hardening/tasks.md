# Tasks: CLI Hardening

## Phase 1: Fix Silent Failures
- [x] 1.1 Check `json.Marshal` error in `adminAddUserRun` (admin.go:227)
- [x] 1.2 Check `json.Marshal` error in `adminAddTeamRun` (admin.go:308)
- [x] 1.3 Collect and report cleanup errors in `adminRemoveAgentRun` (admin.go:442-457)
- [x] 1.4 Wrap `DeleteSecret` error in `internal/aws/secrets.go`
- [x] 1.5 Verify: `go build` compiles

## Phase 2: Validation & UX Fixes
- [x] 2.1 Tighten `validMemberIDPattern` to `^U[A-Z0-9]{10}$`
- [x] 2.2 Tighten `validChannelIDPattern` to `^C[A-Z0-9]{10}$`
- [x] 2.3 Add env var preview in `secrets set` when name is passed as argument
- [x] 2.4 Add `--agent <name>` to next-steps in `admin add-user` success message
- [x] 2.5 Add `--timeout` global flag (default 5m), apply to all commands
- [x] 2.6 Fix `pollDevicePairing` — verbose logging, clean context exit
- [x] 2.7 Verify: `go build` compiles

## Phase 3: Testability & Backend-Agnostic Refactoring
- [x] 3.1 Create `internal/executor/executor.go` with `HostExecutor` interface and `Result` type
- [x] 3.2 Create `internal/executor/ssm.go` with `SSMExecutor` (wraps `awsutil.RunCommand`)
- [x] 3.3 Create `internal/aws/interfaces.go` with SSMClient, SecretsManagerClient, EC2Client, STSClient
- [x] 3.4 Update `Clients` struct to use interface types
- [x] 3.5 Update all function signatures in `internal/aws/` to accept interfaces
- [x] 3.6 Update all function signatures in `internal/discovery/` to accept interfaces
- [ ] 3.7 Create `CLIContext` struct in `cmd/context.go` — deferred (interfaces enable testing without full migration)
- [ ] 3.8 Migrate global variables to `CLIContext` fields — deferred
- [ ] 3.9 Migrate command handlers to `cliCtx.Executor.RunScript` — deferred
- [ ] 3.10 Move `findInstance` into executor initialization — deferred
- [x] 3.11 Add `ConfirmWith`, `TextPromptWith`, etc. to `internal/ui/prompt.go`
- [x] 3.12 Verify: `go build` compiles, `go vet` passes

## Phase 4: Unit Tests
- [x] 4.1 `cmd/status_test.go` — parseKeyValues, splitStats
- [x] 4.2 `cmd/secrets_test.go` — secretNameToEnvVar
- [x] 4.3 `cmd/root_test.go` — validateAgentName, validateMemberID, validateChannelID
- [x] 4.4 `internal/discovery/identity_test.go` — ARN parsing
- [x] 4.5 `internal/aws/ssm_test.go` — RunCommand (happy, failure, timeout, consecutive errors, send failure)
- [x] 4.6 `internal/aws/secrets_test.go` — SetSecret (update/create/both-fail), ListSecrets (single/multi/empty), DeleteSecret (error wrapping)
- [ ] 4.7 `internal/aws/params_test.go` — deferred (lower priority, same pattern as secrets)
- [ ] 4.8 `internal/discovery/agent_test.go` — deferred (needs mock SSM for GetParametersByPath)
- [x] 4.9 `internal/ui/prompt_test.go` — Confirm, TextPromptWith, TextPromptWithDefaultFrom
- [x] 4.10 All tests pass. Coverage: aws=26.9%, ui=28.2%, cmd=10.1%

## Phase 5: Code Organization
- [x] 5.1 Extract `adminSetupRun` → `admin_setup.go`
- [x] 5.2 Extract provisioning functions → `admin_provision.go`
- [x] 5.3 Extract `adminRemoveAgentRun` → `admin_remove.go`
- [x] 5.4 Extract `adminCycleHostRun` → `admin_cycle.go`
- [x] 5.5 Verify: `go build` compiles, `go test ./...` passes

## Phase 6: Status Uptime Display
- [x] 6.1 Add `formatUptime` helper to `status.go`
- [x] 6.2 Update status display to show "up Xh Ym"
- [x] 6.3 Add `formatUptime` tests to `status_test.go`

## CI Integration
- [x] 7.1 Add `go test` and coverage steps to `.github/workflows/ci.yml`
- [ ] 7.2 Verify: CI passes on push
