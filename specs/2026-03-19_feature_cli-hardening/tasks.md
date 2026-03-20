# Tasks: CLI Hardening

## Phase 1: Fix Silent Failures
- [ ] 1.1 Check `json.Marshal` error in `adminAddUserRun` (admin.go:227)
- [ ] 1.2 Check `json.Marshal` error in `adminAddTeamRun` (admin.go:308)
- [ ] 1.3 Collect and report cleanup errors in `adminRemoveAgentRun` (admin.go:442-457)
- [ ] 1.4 Wrap `DeleteSecret` error in `internal/aws/secrets.go`
- [ ] 1.5 Verify: `go build` compiles, manual smoke test of affected commands

## Phase 2: Validation & UX Fixes
- [ ] 2.1 Tighten `validMemberIDPattern` to `^U[A-Z0-9]{10}$`
- [ ] 2.2 Tighten `validChannelIDPattern` to `^C[A-Z0-9]{10}$`
- [ ] 2.3 Add env var preview in `secrets set` when name is passed as argument
- [ ] 2.4 Add `--agent <name>` to next-steps in `admin add-user` success message
- [ ] 2.5 Add `--timeout` global flag (default 5m), apply to all commands
- [ ] 2.6 Fix `pollDevicePairing` — verbose logging, clean context exit
- [ ] 2.7 Verify: manual test with invalid Slack IDs (old format rejected, new format accepted)

## Phase 3: Testability & Backend-Agnostic Refactoring
- [ ] 3.1 Create `internal/executor/executor.go` with `HostExecutor` interface and `Result` type
- [ ] 3.2 Create `internal/executor/ssm.go` with `SSMExecutor` (wraps `awsutil.RunCommand`)
- [ ] 3.3 Create `internal/aws/interfaces.go` with SSMClient, SecretsManagerClient, EC2Client, STSClient
- [ ] 3.4 Update `Clients` struct to use interface types
- [ ] 3.5 Update all function signatures in `internal/aws/` to accept interfaces
- [ ] 3.6 Update all function signatures in `internal/discovery/` to accept interfaces
- [ ] 3.7 Create `CLIContext` struct in `cmd/context.go` (includes `Executor HostExecutor` field)
- [ ] 3.8 Migrate global variables to `CLIContext` fields
- [ ] 3.9 Migrate command handlers from `awsutil.RunCommand(ctx, clients.SSM, instanceID, ...)` to `cliCtx.Executor.RunScript(ctx, ...)`
- [ ] 3.10 Move `findInstance` into executor initialization (commands no longer need instance IDs)
- [ ] 3.11 Add `ConfirmWith`, `TextPromptWith`, etc. to `internal/ui/prompt.go`
- [ ] 3.12 Verify: `go build` compiles, all existing functionality works

## Phase 4: Unit Tests
- [ ] 4.1 `cmd/status_test.go` — parseKeyValues, splitStats, formatUptime
- [ ] 4.2 `cmd/secrets_test.go` — secretNameToEnvVar
- [ ] 4.3 `cmd/root_test.go` — validateAgentName, validateMemberID, validateChannelID
- [ ] 4.4 `internal/discovery/identity_test.go` — ARN parsing
- [ ] 4.5 `internal/aws/ssm_test.go` — RunCommand (happy, failure, timeout, consecutive errors)
- [ ] 4.6 `internal/aws/secrets_test.go` — SetSecret, ListSecrets, DeleteSecret
- [ ] 4.7 `internal/aws/params_test.go` — GetParameter, PutParameter, ListParametersByPath
- [ ] 4.8 `internal/discovery/agent_test.go` — ListAgents, ResolveAgent, ResolveAgentByIAM
- [ ] 4.9 `internal/ui/prompt_test.go` — Confirm, TextPromptWithDefault
- [ ] 4.10 Verify: `go test ./...` passes, check coverage report

## Phase 5: Code Organization
- [ ] 5.1 Extract `adminSetupRun` → `admin_setup.go`
- [ ] 5.2 Extract provisioning functions → `admin_provision.go`
- [ ] 5.3 Extract `adminRemoveAgentRun` → `admin_remove.go`
- [ ] 5.4 Extract `adminCycleHostRun` → `admin_cycle.go`
- [ ] 5.5 Verify: `go build` compiles, `go test ./...` passes

## Phase 6: Status Uptime Display
- [ ] 6.1 Add `formatUptime` helper to `status.go`
- [ ] 6.2 Update status display to show "up Xh Ym"
- [ ] 6.3 Add `formatUptime` tests to `status_test.go`

## CI Integration
- [ ] 7.1 Create `.github/workflows/ci.yml` with `go test` and coverage
- [ ] 7.2 Verify: CI passes on push
