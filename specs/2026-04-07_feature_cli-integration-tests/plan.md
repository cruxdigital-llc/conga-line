# Plan: CLI Integration Tests

## Approach

Build-tagged test files in `internal/cmd/` that drive cobra programmatically
against real Docker. Tests use `--data-dir` with `t.TempDir()` for isolation
and unique agent names (`itest-<hash>`) to avoid conflicts.

## File Layout

```
internal/cmd/
  integration_test.go          # //go:build integration — 4 test functions
  integration_helpers_test.go  # //go:build integration — runCLI, cleanup, assertions

.github/workflows/ci.yml      # New "integration" job after unit tests
```

Tests live in `internal/cmd/` (package `cmd`) for direct access to `rootCmd`
and flag variables. Build tag prevents compilation during `go test ./...`.

## Files to Create

### `internal/cmd/integration_helpers_test.go`

Test infrastructure:

- `requireDocker(t)` — skip if Docker not available
- `setupTestEnv(t) (dataDir, agentName)` — create temp data dir, register cleanup
- `runCLI(t, args...) (stdout, stderr, err)` — reset cobra state, execute, capture output
- `mustRunCLI(t, args...) string` — fatalf on error
- `resetCLIState()` — zero flag variables and ui globals between invocations
- `dockerExec(t, container, cmd...) string` — run command inside container
- `assertContainerRunning(t, name)` / `assertContainerStopped(t, name)`
- `assertEnvVar(t, container, key, value)` / `assertNoEnvVar(t, container, key)`
- `assertFileInContainer(t, container, path, contains)` / `assertNoFileInContainer(t, container, path)`
- `cleanupTestContainers(prefix)` — belt-and-suspenders Docker cleanup

### `internal/cmd/integration_test.go`

Four test functions:

**TestAgentLifecycle** (~2-3 min) — full user-agent lifecycle:
1. `admin setup` → verify config created
2. `admin add-user` → assert container running
3. `admin list-agents` → assert agent in list
4. `status` → assert container state "running"
5. `secrets set test-key --value dummy123` → assert exit 0
6. `secrets list` → assert "test-key" in list
7. Verify `TEST_KEY` NOT in container env (not yet refreshed)
8. `refresh` → assert container running
9. Verify `TEST_KEY=dummy123` IN container env
10. `secrets delete test-key` → assert removed from list
11. `refresh` → verify `TEST_KEY` gone from container env
12. `logs -n 5` → assert non-empty output
13. `admin pause` → assert container stopped
14. `admin unpause` → assert container running
15. `admin remove-agent --force --delete-secrets` → assert no container
16. `admin teardown --force` → clean exit

**TestTeamAgentWithBehavior** (~1-2 min) — behavior file lifecycle:
1. `admin setup` (with `repo_path` to exercise behavior copying)
2. `admin add-team` → assert behavior log shows agent-specific files
3. `agent list` → shows SOUL.md, AGENTS.md
4. Verify SOUL.md in container is agent-specific content
5. Verify MEMORY.md in container is pristine
6. `agent add <testfile> --as TEST.md` → file in source + deployed
7. `refresh` → verify TEST.md readable inside container
8. `agent rm TEST.md` → file removed from source
9. `refresh` → verify TEST.md deleted from container (manifest reconciliation)
10. `admin teardown --force`

**TestPolicyValidate** (~5 sec, no containers) — policy file validation:
1. `admin setup`
2. Write valid `conga-policy.yaml` to data dir
3. `policy validate` → assert valid
4. `admin teardown --force`

**TestEgressPolicyEnforcement** (~2-3 min) — real network traffic:
1. `admin setup` + `admin add-user`
2. **no-policy (deny-all)**: `docker exec` → HTTP request to `api.anthropic.com`
   → assert blocked (connection refused or 403)
3. Write policy: `mode: validate`, `allowed_domains: [api.anthropic.com]`
4. `refresh` → HTTP request → assert succeeds (gets through proxy)
5. Write policy: `mode: enforce`, `allowed_domains: [api.anthropic.com]`
6. `refresh` → request to `api.anthropic.com` → assert succeeds
7. Request to `example.com` (not in allowlist) → assert blocked (403)
8. `admin teardown --force`

## Files to Modify

### `internal/cmd/root.go`

Add `resetCLIState()`:

```go
func resetCLIState() {
    flagProvider = ""
    flagDataDir = ""
    flagAgent = ""
    flagJSON = ""
    flagOutput = "text"
    flagVerbose = false
    flagRuntime = ""
    flagRegion = ""
    flagProfile = ""
    flagTimeout = 5 * time.Minute
    prov = nil
    ui.OutputJSON = false
    ui.JSONInputActive = false
}
```

### `.github/workflows/ci.yml`

Add `integration` job:

```yaml
  integration:
    name: Integration Tests
    runs-on: ubuntu-latest
    needs: go
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Pull test image
        run: docker pull ghcr.io/openclaw/openclaw:2026.3.11
      - name: Build egress proxy image
        run: docker build -t conga-egress-proxy deploy/egress-proxy/
      - name: Integration tests
        run: go test -tags integration ./internal/cmd/ -v -timeout 10m -count=1
```

## Isolation Strategy

- **Data dir**: `t.TempDir()` per test function — auto-cleaned
- **Agent names**: `itest-<crc32(test-name)>` → containers `conga-itest-*`
- **Cleanup**: `t.Cleanup()` runs `admin teardown --force`, then
  `docker rm -f` / `docker network rm` for any `conga-itest-*` leftovers
- **Docker gate**: `docker info` at test start → `t.Skip` if unavailable
- **Port allocation**: let the provider auto-assign (starts at 18789)

## Key Design Decisions

1. **Cobra programmatic** — faster than shelling out, captures output, no build step
2. **Build tag `integration`** — clean separation from unit tests
3. **Sequential subtests** — operations are ordered; parallel would create Docker conflicts
4. **Verify effects via `docker exec`** — secrets in env, files in workspace, egress behavior
5. **No readiness wait** — "running" is sufficient; OpenClaw takes 30-60s to fully start
6. **Skip iptables assertions** — macOS doesn't support them; proxy-level tests are cross-platform

## Risks

- **Cobra state leakage**: `rootCmd` carries flag state between `Execute()` calls.
  Mitigated by `resetCLIState()` before each invocation.
- **Container startup timing**: `docker exec` may fail if container isn't ready.
  Mitigated by retry loops (3 attempts, 2s apart) in assertion helpers.
- **Port conflicts**: Two concurrent test runs could collide on gateway ports.
  Low risk — CI runs single-threaded, local runs are manual.
- **Image pull time**: First run pulls ~1.5GB OpenClaw image. CI pre-pulls.
  Locally, developers must have the image or wait for the pull.
