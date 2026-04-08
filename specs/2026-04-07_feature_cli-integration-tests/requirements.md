# Requirements: CLI Integration Tests

## Problem

The congaline CLI has 14 top-level commands and 26 provider interface methods
but zero integration tests. All existing tests are unit-level with mocks.
During the per-agent behavior feature, manual smoke tests caught real bugs
that unit tests missed (RefreshAgent not calling deployBehavior, template
substitution mismatch). There is no automated way to verify the CLI works
end-to-end against a real Docker environment.

## Goals

1. **Exercise all primary CLI use cases** against a real local Docker
   environment — setup, provision, secrets, refresh, behavior, status,
   logs, pause/unpause, policy, egress enforcement, removal, teardown.

2. **Verify actual effects, not just exit codes.** Tests must assert that:
   - Secrets set via CLI appear as environment variables inside the container
   - Secrets removed via CLI disappear from the container after refresh
   - Behavior files deployed via CLI are readable inside the container
   - Behavior files removed via CLI are deleted from the container after refresh
   - MEMORY.md is never modified by behavior deployment
   - Egress proxy blocks traffic when no policy is set (deny-all default)
   - Egress proxy allows traffic in validate mode
   - Egress proxy allows/blocks traffic per domain in enforce mode

3. **Runnable locally** with `go test -tags integration` — skip gracefully
   if Docker is unavailable.

4. **Runnable in CI** as a GitHub Actions job after unit tests pass.

5. **Isolated from user state** — never read or write `~/.conga/`, never
   conflict with running agents, clean up all Docker resources on
   completion (even on failure).

## Non-Goals

- Testing AWS or remote (SSH) providers — local Docker only.
- Testing Slack/Telegram channel integration — no external services.
- Testing OpenClaw agent readiness — container "running" is sufficient.
- Testing the MCP server — covered by existing mock-based tests.
- Performance benchmarking.

## Success Criteria

- `go test -tags integration ./internal/cmd/ -v -timeout 10m` passes
  on a machine with Docker running.
- `go test ./...` does NOT run integration tests (build tag gate).
- CI job passes on `ubuntu-latest` with Docker pre-installed.
- No `conga-itest-*` containers or networks remain after test run.
- At least 4 test functions covering: agent lifecycle, behavior management,
  policy validation, and egress enforcement.
