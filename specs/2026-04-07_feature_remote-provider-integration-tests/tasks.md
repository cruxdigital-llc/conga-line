# Implementation Tasks: Remote Provider Integration Tests

## Phase 1: SSH Container

- [ ] **T1: Create `internal/cmd/testdata/sshd/Dockerfile`**

## Phase 2: Test Helpers

- [ ] **T2: Extend `internal/cmd/integration_helpers_test.go`**
  - `buildSSHImage`, `generateSSHKey`, `startSSHContainer`, `waitForSSH`, `stopSSHContainer`
  - `setupRemoteTestEnv`, `remoteBaseArgs`

## Phase 3: Test Functions

- [ ] **T3: `TestRemoteAgentLifecycle`** — 17 subtests
- [ ] **T4: `TestRemoteTeamAgentWithBehavior`** — 14 subtests
- [ ] **T5: `TestRemoteEgressPolicyEnforcement`** — 10 subtests

## Phase 4: Verification

- [ ] **T6: Run all integration tests** — local + remote pass together
- [ ] **T7: Verify `go test ./...` still excludes integration tests**
- [ ] **T8: Verify no leaked containers after test run**
