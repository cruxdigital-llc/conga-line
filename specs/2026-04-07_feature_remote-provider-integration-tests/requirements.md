# Requirements: Remote Provider Integration Tests

## Problem

The local provider integration tests verify the CLI works end-to-end
against Docker, but the remote provider has entirely different code paths
(SSH exec, SFTP upload/download, remote Docker commands, SSH tunneling).
These paths are untested in integration — only the SSH reconnect logic
has unit-level tests. Bugs like the missing `deployBehavior` call in
`RefreshAgent` (found in the behavior feature) could exist in the remote
provider without being caught.

## Goals

1. **Exercise the same use cases as local tests** through the remote
   provider's SSH+SFTP code paths — setup, provision, secrets, refresh,
   behavior, status, logs, pause/unpause, egress, removal, teardown.

2. **No external infrastructure** — the "remote host" is a Docker
   container running sshd with the host's Docker socket mounted.
   Everything runs on the developer's machine or in CI.

3. **Verify actual effects** — same standard as local tests: secrets in
   container env, behavior files in workspace, egress proxy blocking/
   allowing traffic, MEMORY.md untouched.

4. **Reuse existing test helpers** — extend `integration_helpers_test.go`
   with SSH container setup; test scenarios share assertion helpers with
   local tests.

5. **Runnable in CI** alongside local tests in the same integration job.

## Non-Goals

- Testing SSH reconnect/failover (covered by existing unit tests).
- Testing SSH agent forwarding or password auth.
- Testing the remote provider's `InstallDocker` flow.
- Full parity with every local test edge case — cover the primary paths.

## Constraints

- The SSH container must have Docker CLI installed and the host Docker
  socket mounted at `/var/run/docker.sock`.
- The SSH container uses `NoClientAuth` or ephemeral key auth for
  simplicity — no real credentials in tests.
- Container naming must not conflict with local provider tests if both
  run (different agent name prefixes).
- The remote provider creates `/opt/conga/` on the SSH host (container).
  This directory must be writable.

## Success Criteria

- Remote provider tests pass alongside local provider tests in a single
  `go test -tags integration` run.
- Same assertions as local tests (env vars, files, egress) succeed
  through the SSH path.
- No leaked SSH containers, agent containers, or networks after test run.
- CI job passes on `ubuntu-latest`.
