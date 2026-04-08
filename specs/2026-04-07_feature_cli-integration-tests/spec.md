# Specification: CLI Integration Tests

## 1. Overview

Build-tagged integration tests in `internal/cmd/` that exercise the full
CLI against real Docker. Four test functions cover the primary use cases:
agent lifecycle, behavior management, policy validation, and egress
enforcement. Each test verifies actual effects (env vars in containers,
files in workspaces, network traffic blocked/allowed) rather than just
CLI exit codes.

## 2. Test Infrastructure

### 2.1 File: `internal/cmd/integration_helpers_test.go`

Build tag: `//go:build integration`
Package: `cmd` (internal access to `rootCmd`, flag vars, `prov`)

#### `requireDocker(t *testing.T)`

Calls `docker info` via `exec.Command`. If Docker is unavailable,
calls `t.Skip("Docker not available")`. Every test function calls
this first.

#### `resetCLIState()`

Zeros all package-level flag variables and ui globals so cobra doesn't
carry state between `Execute()` calls:

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
    secretValue = ""
    secretForce = false
    adminForce = false
    adminDeleteSecrets = false
    behaviorAsName = ""
}
```

This function lives in the test file (package `cmd`, build-tagged) so it
has access to all the unexported variables. It does NOT need to be in
`root.go` — test files in the same package can access unexported symbols.

#### `runCLI(t *testing.T, args ...string) (stdout, stderr string, err error)`

Resets state, configures cobra output capture, executes:

```go
func runCLI(t *testing.T, args ...string) (string, string, error) {
    t.Helper()
    resetCLIState()
    var outBuf, errBuf bytes.Buffer
    rootCmd.SetOut(&outBuf)
    rootCmd.SetErr(&errBuf)
    rootCmd.SetArgs(args)
    err := rootCmd.Execute()
    return outBuf.String(), errBuf.String(), err
}
```

Note: `fmt.Fprintf(os.Stderr, ...)` calls in the codebase (e.g.
behavior deployment logs, egress warnings) go to the real stderr, not
cobra's `SetErr` buffer. The `stderr` return captures cobra-level
errors only. Tests that need to assert on `os.Stderr` output should
capture it separately if needed, but most assertions use Docker state
(container running, env vars, files) rather than stderr parsing.

#### `mustRunCLI(t *testing.T, args ...string) string`

Calls `runCLI`, fatals on error, returns stdout.

#### `setupTestEnv(t *testing.T) (dataDir, agentName string)`

Creates an isolated test environment:

```go
func setupTestEnv(t *testing.T) (string, string) {
    t.Helper()
    requireDocker(t)

    dataDir := filepath.Join(t.TempDir(), ".conga")
    agentName := fmt.Sprintf("itest-%08x", crc32.ChecksumIEEE([]byte(t.Name())))[:12]

    t.Cleanup(func() {
        // Try graceful teardown first
        runCLI(t, "--provider", "local", "--data-dir", dataDir,
            "admin", "teardown", "--force")
        // Belt-and-suspenders: kill any leaked containers/networks
        cleanupTestContainers(agentName)
    })

    return dataDir, agentName
}
```

The agent name is derived from the test name via CRC32, producing
stable names like `itest-a3f2b1c8`. The `conga-` prefix is added by the
provider, giving container names like `conga-itest-a3f2b1c8`.

#### `repoRoot() string`

Returns the congaline repo root by running `git rev-parse --show-toplevel`.
Used to set `repo_path` in setup config for behavior file tests.

#### Docker assertion helpers

```go
func dockerExec(t *testing.T, container string, cmd ...string) (string, error)
func assertContainerRunning(t *testing.T, agentName string)
func assertContainerNotExists(t *testing.T, agentName string)
func assertEnvVar(t *testing.T, agentName, key, value string)
func assertNoEnvVar(t *testing.T, agentName, key string)
func assertFileContent(t *testing.T, agentName, path, contains string)
func assertFileNotExists(t *testing.T, agentName, path string)
```

`assertContainerRunning` uses a retry loop (5 attempts, 2s apart) to
handle container startup timing. All assertion helpers use `docker exec`
via `exec.Command` (not through the provider, to avoid coupling).

The `agentName` parameter is the logical name (e.g. `itest-a3f2b1c8`);
helpers prepend `conga-` for the Docker container name.

#### `cleanupTestContainers(agentName string)`

Removes containers and networks matching `conga-<agentName>` and
`conga-egress-<agentName>`:

```go
func cleanupTestContainers(agentName string) {
    for _, name := range []string{
        "conga-" + agentName,
        "conga-egress-" + agentName,
    } {
        exec.Command("docker", "rm", "-f", name).Run()
    }
    exec.Command("docker", "network", "rm", "conga-"+agentName).Run()
}
```

Runs in `t.Cleanup()` after the graceful teardown attempt. Errors are
ignored — the containers may already be gone.

### 2.2 Shared test constants

```go
const (
    testImage = "ghcr.io/openclaw/openclaw:2026.3.11"
)
```

## 3. Test Functions

### 3.1 File: `internal/cmd/integration_test.go`

Build tag: `//go:build integration`
Package: `cmd`

### 3.2 `TestAgentLifecycle`

Exercises the full user-agent lifecycle in sequential subtests. Each
subtest depends on the previous — if one fails, later ones are skipped.

```go
func TestAgentLifecycle(t *testing.T) {
    dataDir, agentName := setupTestEnv(t)
    baseArgs := []string{"--provider", "local", "--data-dir", dataDir}

    t.Run("setup", func(t *testing.T) { ... })
    t.Run("add-user", func(t *testing.T) { ... })
    // ... etc
}
```

**Subtests:**

| # | Subtest | CLI command | Assertion |
|---|---------|------------|-----------|
| 1 | setup | `admin setup --json '{"image":"<testImage>"}'` | `local-config.json` exists in data dir |
| 2 | add-user | `admin add-user <agent>` | `assertContainerRunning(t, agent)` |
| 3 | list-agents | `admin list-agents --output json` | JSON stdout contains agent name |
| 4 | status | `status --agent <agent> --output json` | JSON contains `"state":"running"` |
| 5 | secrets-set | `secrets set test-key --value dummy123 --agent <agent>` | exit 0 |
| 6 | secrets-list | `secrets list --agent <agent> --output json` | JSON contains `"test-key"` |
| 7 | secrets-not-in-env | (docker exec) | `assertNoEnvVar(t, agent, "TEST_KEY")` — not refreshed yet |
| 8 | refresh | `refresh --agent <agent>` | `assertContainerRunning(t, agent)` |
| 9 | secrets-in-env | (docker exec) | `assertEnvVar(t, agent, "TEST_KEY", "dummy123")` |
| 10 | secrets-delete | `secrets delete test-key --agent <agent> --force` | exit 0 |
| 11 | refresh-after-delete | `refresh --agent <agent>` | exit 0 |
| 12 | secrets-gone-from-env | (docker exec) | `assertNoEnvVar(t, agent, "TEST_KEY")` |
| 13 | logs | `logs --agent <agent> -n 10` | stdout is non-empty |
| 14 | pause | `admin pause <agent>` | container not running (inspect shows exited or not found) |
| 15 | unpause | `admin unpause <agent>` | `assertContainerRunning(t, agent)` |
| 16 | remove-agent | `admin remove-agent <agent> --force --delete-secrets` | `assertContainerNotExists(t, agent)` |
| 17 | teardown | `admin teardown --force` | exit 0, data dir cleaned |

### 3.3 `TestTeamAgentWithBehavior`

Tests per-agent behavior file deployment and manifest reconciliation
using a team agent with the `nvidia-team` overlay files from the repo.

```go
func TestTeamAgentWithBehavior(t *testing.T) {
    dataDir, agentName := setupTestEnv(t)
    // ...
}
```

**Setup**: `admin setup` with `repo_path` pointing to `repoRoot()`.
This copies `behavior/` (including `agents/nvidia-team/`) to the
test data dir. The test agent name differs from `nvidia-team`, so
it will get defaults — we create a test-specific override in the
test.

**Subtests:**

| # | Subtest | Action | Assertion |
|---|---------|--------|-----------|
| 1 | setup | `admin setup --json '{"image":"...","repo_path":"<repoRoot>"}'` | exit 0 |
| 2 | create-agent-behavior | Write test SOUL.md to `<dataDir>/behavior/agents/<agent>/SOUL.md` | file exists |
| 3 | add-team | `admin add-team <agent>` | `assertContainerRunning` |
| 4 | verify-soul-in-container | `docker exec ... cat .../SOUL.md` | `assertFileContent(t, agent, ".../SOUL.md", "test soul content")` |
| 5 | verify-agents-default | `docker exec ... cat .../AGENTS.md` | contains default AGENTS.md content (not agent-specific) |
| 6 | verify-memory-pristine | `docker exec ... cat .../MEMORY.md` | exactly `# Memory\n` |
| 7 | add-agents-md-override | Write custom AGENTS.md to agent behavior dir | file exists |
| 8 | refresh | `refresh --agent <agent>` | exit 0 |
| 9 | verify-agents-md-overridden | `docker exec ... cat .../AGENTS.md` | custom content |
| 10 | remove-agents-md-override | `os.Remove` AGENTS.md from agent dir | file gone |
| 11 | refresh-after-rm | `refresh --agent <agent>` | exit 0 |
| 12 | verify-agents-md-reverted | `docker exec ... cat .../AGENTS.md` | default content (reverted) |
| 13 | verify-memory-still-pristine | `docker exec ... cat .../MEMORY.md` | still `# Memory\n` |
| 14 | teardown | `admin teardown --force` | exit 0 |

### 3.4 `TestPolicyValidate`

Validates the `conga-policy.yaml` parser without Docker containers.

| # | Subtest | Action | Assertion |
|---|---------|--------|-----------|
| 1 | setup | `admin setup --json '{"image":"..."}'` | exit 0 |
| 2 | write-valid-policy | Write valid YAML to `<dataDir>/conga-policy.yaml` | file exists |
| 3 | validate | `policy validate` | exit 0, stdout contains "valid" or no error |
| 4 | write-invalid-policy | Overwrite with invalid YAML (missing `apiVersion`) | |
| 5 | validate-fails | `policy validate` | exit non-zero, error mentions `apiVersion` |
| 6 | teardown | `admin teardown --force` | exit 0 |

### 3.5 `TestEgressPolicyEnforcement`

Verifies that the egress proxy controls outbound traffic from inside the
container. Uses `node -e` (available in the OpenClaw image) to make HTTP
requests through the proxy.

**HTTP request helper:**

```go
func makeHTTPRequest(t *testing.T, agentName, url string) (statusCode int, err error) {
    // node script that makes an HTTPS CONNECT through the proxy
    script := fmt.Sprintf(`
        const https = require('https');
        const req = https.get('%s', {timeout: 5000}, (res) => {
            process.stdout.write(String(res.statusCode));
        });
        req.on('error', (e) => {
            process.stderr.write(e.message);
            process.exit(1);
        });
    `, url)
    stdout, err := dockerExec(t, "conga-"+agentName, "node", "-e", script)
    if err != nil {
        return 0, err
    }
    code, _ := strconv.Atoi(strings.TrimSpace(stdout))
    return code, nil
}
```

**Subtests:**

| # | Subtest | Setup | Request | Expected |
|---|---------|-------|---------|----------|
| 1 | setup | `admin setup` + `admin add-user` (no policy file) | | |
| 2 | no-policy-blocks | (default deny-all proxy) | `https://api.anthropic.com` | Error (connection refused or 403) |
| 3 | write-validate-policy | Write `mode: validate, allowed_domains: [api.anthropic.com]` | | |
| 4 | refresh-validate | `refresh --agent <agent>` | | |
| 5 | validate-allows | | `https://api.anthropic.com` | Succeeds (any HTTP status — got through proxy) |
| 6 | write-enforce-policy | Write `mode: enforce, allowed_domains: [api.anthropic.com]` | | |
| 7 | refresh-enforce | `refresh --agent <agent>` | | |
| 8 | enforce-allowed | | `https://api.anthropic.com` | Succeeds |
| 9 | enforce-blocked | | `https://example.com` | Error (403 from proxy) |
| 10 | teardown | `admin teardown --force` | | |

**Policy YAML format:**

```yaml
apiVersion: conga.dev/v1alpha1
egress:
  mode: validate  # or enforce
  allowed_domains:
    - api.anthropic.com
```

## 4. Files to Modify

### 4.1 `.github/workflows/ci.yml`

Add new job after the existing `go` job:

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

No Docker-in-Docker needed — `ubuntu-latest` has Docker pre-installed.

## 5. Edge Cases

| Scenario | Handling |
|----------|----------|
| Docker not running | `requireDocker` → `t.Skip` |
| Image not pulled | First `admin setup` pulls it (slow but works). CI pre-pulls. |
| Container startup timing | `assertContainerRunning` retries 5x with 2s sleep |
| Port conflict with user's agents | Agent names are `itest-*`, ports auto-assigned starting at 18789. Low risk — user would need agents on the exact same ports. |
| Test failure mid-lifecycle | `t.Cleanup` runs teardown + direct `docker rm -f` |
| Concurrent test runs | Not supported — sequential by design. CI uses `-count=1`. |
| Egress request timeout | `node` script uses 5s timeout. If proxy is slow, test fails clearly. |
| Egress proxy not built | `admin setup` builds it from `deploy/egress-proxy/`. CI pre-builds. |
| macOS iptables warnings | Expected and ignored — proxy-level enforcement still works |

## 6. File Manifest

| File | Action | Description |
|------|--------|-------------|
| `internal/cmd/integration_test.go` | Create | 4 test functions (~400 lines) |
| `internal/cmd/integration_helpers_test.go` | Create | Test infrastructure (~200 lines) |
| `.github/workflows/ci.yml` | Modify | Add `integration` job |

## 7. Handoff

After implementation, run:
```
go test -tags integration ./internal/cmd/ -v -timeout 10m -count=1
```
Then verify `go test ./...` does NOT include integration tests.
