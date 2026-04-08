# Implementation Tasks: CLI Integration Tests

## Phase 1: Test Infrastructure

- [x] **T1: Create `internal/cmd/integration_helpers_test.go`**
  - `requireDocker`, `resetCLIState`, `runCLI`, `mustRunCLI`
  - `setupTestEnv`, `repoRoot`, `cleanupTestContainers`
  - Docker assertion helpers: `dockerExec`, `assertContainerRunning`,
    `assertContainerNotExists`, `assertEnvVar`, `assertNoEnvVar`,
    `assertFileContent`, `assertFileNotExists`
  - `makeHTTPRequest` helper for egress tests

## Phase 2: Test Functions

- [x] **T2: `TestAgentLifecycle`** — 17 subtests covering setup through teardown
- [x] **T3: `TestTeamAgentWithBehavior`** — 14 subtests covering behavior deployment
- [x] **T4: `TestPolicyValidate`** — 6 subtests covering policy validation
- [x] **T5: `TestEgressPolicyEnforcement`** — 10 subtests covering egress proxy modes

## Phase 3: CI Integration

- [x] **T6: Update `.github/workflows/ci.yml`** — add integration job

## Phase 4: Verification

- [x] **T7: Run integration tests locally** — all pass
- [x] **T8: Verify `go test ./...` excludes integration tests**
- [x] **T9: Verify `go build ./...` still clean**
