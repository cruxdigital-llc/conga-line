# Requirements: CLI Hardening — Design, Reliability & Test Coverage

## Problem Statement

The `cruxclaw` CLI (~3,500 LOC, 24 Go source files, 13 commands) was shipped as part of the CruxClaw CLI feature (Epic 7). It is functional and deployed, but has zero test coverage, several silent failure bugs, and reliability gaps that will compound as more users onboard. Before handing the CLI to additional team members, we need to fix known bugs, add testability infrastructure, and establish baseline test coverage.

## Who Benefits

- **All CLI users** — bugs fixed, better error messages, more predictable behavior
- **Admins** — cleanup operations report failures instead of silently orphaning AWS resources
- **Future contributors** — testable architecture with interfaces, CI-gated tests prevent regressions

## Requirements

### R1: Fix Silent Failures (Critical)
- R1.1: `json.Marshal` errors in `admin add-user` and `admin add-team` must be checked and surfaced
- R1.2: Cleanup errors in `admin remove-agent` (DeleteParameter, DeleteSecret, RunCommand) must be reported to the user
- R1.3: `DeleteSecret` in `internal/aws/secrets.go` must wrap errors with context like all other functions in that package

### R2: Reliability
- R2.1: Slack member ID validation must enforce the `U` prefix and 11-character length; channel ID validation must enforce the `C` prefix and 11-character length
- R2.2: All commands must respect a global timeout (default 5 minutes) via context deadline
- R2.3: The `pollDevicePairing` goroutine in `connect` must exit cleanly when context is cancelled and log errors when `--verbose` is set

### R3: Testability & Backend-Agnostic Refactoring
- R3.1: AWS service calls must go through interfaces (not concrete SDK client pointers) to enable mock injection
- R3.2: Remote script execution must go through a `HostExecutor` interface that abstracts the transport (SSM for AWS, `exec.Command` for local). This enables a future `--local` mode where the same CLI commands manage a local Docker instance without SSM. Only the `HostExecutor` implementation and `connect`/`cycle-host` need backend-specific logic; all other commands become backend-agnostic.
- R3.3: Global mutable state (`clients`, `resolvedProfile`, etc.) must be encapsulated in a `CLIContext` struct that can be injected into command handlers. The struct must include an `Executor` field of type `HostExecutor`.
- R3.4: UI prompt functions must accept `io.Reader`/`io.Writer` parameters instead of hardcoding `os.Stdin`/`os.Stdout`

### R4: Test Coverage (Target: 50%+ on `internal/`, 30%+ on `cmd/`)
- R4.1: Pure function unit tests for `parseKeyValues`, `splitStats`, `secretNameToEnvVar`, `validateAgentName`, `validateMemberID`, `validateChannelID`, ARN parsing in `identity.go`, readiness phase logic in `status.go`
- R4.2: Mocked AWS client tests for `RunCommand` (polling, timeout, consecutive errors), `SetSecret` (create-or-update), `ListSecrets` (pagination), `ResolveIdentity`, `ListAgents`, `ResolveAgent`
- R4.3: CI pipeline runs `go test ./...` on every push

### R5: Code Organization
- R5.1: Split `admin.go` (549 lines) into focused files: `admin_setup.go`, `admin_provision.go`, `admin_remove.go`, `admin_cycle.go`

### R6: UX Improvements
- R6.1: `secrets set` must show the SCREAMING_SNAKE_CASE env var name before prompting for the value, even when the name is passed as an argument
- R6.2: `admin add-user` success message must include `--agent <name>` in the next-steps commands (new user won't have IAM mapping yet)
- R6.3: `status` command must show human-readable uptime duration (e.g., "up 3h 42m") alongside or instead of raw ISO timestamp

## Out of Scope

- Color/styled output (lipgloss) — deferred to a future UX polish pass
- `--json` output flag — deferred; no scripting consumers yet
- `auth login` executing `aws sso login` directly — current guided instructions are sufficient
- Clipboard auto-copy for gateway token — platform-specific complexity not justified yet
- End-to-end tests against real AWS or LocalStack — deferred to Horizon 2
- Rewriting the Slack router in Go — separate feature

## Success Criteria

1. No silent failures: every AWS API call error is either returned or reported to the user
2. `go test ./...` passes with >50% coverage on `internal/` packages
3. CI pipeline blocks merges on test failure
4. All existing CLI functionality continues to work identically (no behavioral regressions)
