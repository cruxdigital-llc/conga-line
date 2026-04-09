# Specification: Remote Provider Integration Tests

## 1. Overview

Extend the integration test suite with remote provider tests that exercise
the CLI through SSH+SFTP code paths. An ephemeral SSH container (Alpine +
openssh + docker-cli) with the host's Docker socket mounted acts as the
"remote host". Test scenarios mirror the local provider tests, reusing
the same Docker assertion helpers.

## 2. SSH Test Container

### 2.1 Dockerfile: `internal/cmd/testdata/sshd/Dockerfile`

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache openssh docker-cli bash && \
    ssh-keygen -A && \
    mkdir -p /root/.ssh && chmod 700 /root/.ssh && \
    echo "PermitRootLogin yes" >> /etc/ssh/sshd_config && \
    echo "PubkeyAuthentication yes" >> /etc/ssh/sshd_config && \
    echo "PasswordAuthentication no" >> /etc/ssh/sshd_config
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
```

Key choices:
- **Key-based auth only** — safer than empty passwords even in tests
- **docker-cli** (not docker engine) — uses the mounted socket
- **bash** — needed by some remote provider commands (`set -euo pipefail`)
- Image name: `conga-test-sshd`
- Build cached by Docker layer cache after first run

### 2.2 Container startup

```
docker run -d --name conga-test-sshd \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v <keyDir>/id_test.pub:/root/.ssh/authorized_keys:ro \
  -p 0:22 \
  conga-test-sshd
```

- `-p 0:22` binds to a random host port (extracted via `docker port`)
- Docker socket mounted read-write (remote provider runs `docker run`, etc.)
- Ephemeral public key injected via volume mount

## 3. Test Infrastructure Extensions

### 3.1 New helpers in `integration_helpers_test.go`

```go
const sshContainerName = "conga-test-sshd"
const sshImageName = "conga-test-sshd"

func buildSSHImage(t *testing.T)
```

Runs `docker build -t conga-test-sshd <testdata/sshd>`. Uses a
`sync.Once` so it only builds once per test process, even if multiple
remote tests run.

```go
func generateSSHKey(t *testing.T) (keyDir string)
```

Runs `ssh-keygen -t ed25519 -f <tmpdir>/id_test -N ""`. Returns the
directory containing `id_test` (private) and `id_test.pub` (public).

```go
func startSSHContainer(t *testing.T, keyDir string) (port int)
```

Starts the SSH container, extracts the assigned port via
`docker port conga-test-sshd 22`. Returns the host port number.

```go
func waitForSSH(t *testing.T, port int)
```

Retries `net.DialTimeout("tcp", "127.0.0.1:<port>", 1s)` up to 10 times
with 500ms sleep between attempts. Fatals if SSH doesn't become reachable.

```go
func stopSSHContainer(t *testing.T)
```

`docker rm -f conga-test-sshd`. Called in `t.Cleanup` after agent teardown.

```go
func setupRemoteTestEnv(t *testing.T) (dataDir, agentName string, sshPort int, keyPath string)
```

Orchestrates the full remote test setup:
1. `requireDocker(t)`
2. `buildSSHImage(t)`
3. `keyDir := generateSSHKey(t)`
4. `sshPort := startSSHContainer(t, keyDir)`
5. `waitForSSH(t, sshPort)`
6. Creates temp data dir and `rtest-<hash>` agent name
7. Registers cleanup (LIFO order: teardown → cleanup containers → stop SSH)

Returns `dataDir`, `agentName`, `sshPort`, and `keyPath` (for setup JSON).

### 3.2 Remote base args helper

```go
func remoteBaseArgs(dataDir string) []string {
    return []string{"--provider", "remote", "--data-dir", dataDir}
}
```

## 4. Test Functions

### 4.1 File: `internal/cmd/integration_remote_test.go`

Build tag: `//go:build integration`
Package: `cmd`

### 4.2 `TestRemoteAgentLifecycle`

```go
func TestRemoteAgentLifecycle(t *testing.T) {
    dataDir, agentName, sshPort, keyPath := setupRemoteTestEnv(t)
    base := remoteBaseArgs(dataDir)
    root := repoRoot(t)

    t.Run("setup", func(t *testing.T) {
        cfg := fmt.Sprintf(
            `{"ssh_host":"127.0.0.1","ssh_port":%d,"ssh_user":"root","ssh_key_path":%q,"image":%q,"repo_path":%q}`,
            sshPort, keyPath, testImage, root)
        mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
    })
    // ... mirrors TestAgentLifecycle subtests with `base` using remote args
}
```

**Subtests** (same as local, with `--provider remote`):

| # | Subtest | Assertion |
|---|---------|-----------|
| 1 | setup | `remote-config.json` exists in data dir |
| 2 | add-user | `assertContainerRunning(t, agentName)` |
| 3 | list-agents | JSON contains agent name |
| 4 | status | container state "running" |
| 5 | secrets-set | exit 0 |
| 6 | secrets-list | contains "test-key" |
| 7 | secrets-not-in-env | `assertNoEnvVar` (not refreshed) |
| 8 | refresh | container running |
| 9 | secrets-in-env | `assertEnvVar(t, agentName, "TEST_KEY", "dummy123")` |
| 10 | secrets-delete | removed from list |
| 11 | refresh-after-delete | exit 0 |
| 12 | secrets-gone | `assertNoEnvVar` |
| 13 | logs | non-empty output |
| 14 | pause | container stopped |
| 15 | unpause | container running |
| 16 | remove-agent | container not exists |
| 17 | teardown | exit 0 |

### 4.3 `TestRemoteTeamAgentWithBehavior`

Same structure as `TestTeamAgentWithBehavior` but with remote setup.
The behavior files are pushed to the remote host via SFTP during
`admin setup` (which copies the `behavior/` tree).

Agent-specific behavior files are written to the **local** data dir's
`behavior/agents/<agent>/` — the remote provider reads from the local
`repo_path` and pushes via SFTP.

| # | Subtest | Assertion |
|---|---------|-----------|
| 1 | setup | exit 0 |
| 2 | create-agent-behavior | write SOUL.md to local behavior dir |
| 3 | add-team | container running |
| 4 | verify-soul | `assertFileContent` — agent-specific SOUL.md |
| 5 | verify-agents-default | default AGENTS.md content |
| 6 | verify-memory-pristine | `# Memory\n` |
| 7 | add-agents-override | write custom AGENTS.md |
| 8 | refresh | exit 0 |
| 9 | verify-overridden | custom AGENTS.md content |
| 10 | remove-override | delete AGENTS.md from behavior dir |
| 11 | refresh-after-rm | exit 0 |
| 12 | verify-reverted | default AGENTS.md content |
| 13 | verify-memory-still-pristine | `# Memory\n` |
| 14 | teardown | exit 0 |

### 4.4 `TestRemoteEgressPolicyEnforcement`

Same structure as `TestEgressPolicyEnforcement` but with remote setup.
Policy file is written to the local data dir; the remote provider
reads it and deploys the egress proxy via SSH.

| # | Subtest | Assertion |
|---|---------|-----------|
| 1 | setup + add-user | container running, no policy |
| 2 | no-policy-blocks | HTTP request fails |
| 3 | write-validate-policy | write policy YAML |
| 4 | refresh-validate | exit 0 |
| 5 | validate-allows | HTTP request succeeds |
| 6 | write-enforce-policy | update policy YAML |
| 7 | refresh-enforce | exit 0 |
| 8 | enforce-allowed | `api.anthropic.com` succeeds |
| 9 | enforce-blocked | `example.com` fails |
| 10 | teardown | exit 0 |

## 5. Cleanup Strategy

Cleanup runs in LIFO order via `t.Cleanup`:

1. `admin teardown --force` (remote provider tears down agents via SSH)
2. `cleanupTestContainers(agentName)` (direct Docker cleanup for stragglers)
3. `stopSSHContainer(t)` (remove the SSH container itself)

The SSH container is stopped LAST because steps 1-2 may use it for
SSH-based Docker commands. `t.Cleanup` callbacks execute in reverse
registration order (LIFO), so we register SSH stop first, then agent
cleanup, then teardown — making teardown execute first.

Actually, `t.Cleanup` is LIFO, so we register in this order:
1. Register `stopSSHContainer` (will run last)
2. Register `cleanupTestContainers` (will run second)
3. Register `admin teardown` (will run first)

## 6. Edge Cases

| Scenario | Handling |
|----------|----------|
| Docker socket not at `/var/run/docker.sock` | macOS symlinks it; Linux has it natively. If missing, SSH container fails to run Docker → test fails clearly |
| SSH container fails to start | `startSSHContainer` fatals after timeout |
| SSH container has no Docker access | `admin setup` detects missing Docker on remote → test fails at setup |
| Remote provider writes to `/opt/conga/` | SSH container runs as root, directory is writable |
| Agent containers visible from host | Yes — same Docker daemon. Assertion helpers work unchanged |
| Port conflict with local tests | Different agent prefix (`rtest-` vs `itest-`), auto-assigned ports |
| SSH key permissions | `ssh-keygen` creates 0600 by default; SSH client requires this |
| Alpine missing `sudo` | Remote provider uses `sudo` for non-root users; we run as root, so no sudo needed |

## 7. CI Integration

No changes to `.github/workflows/ci.yml` — the existing integration job
runs `go test -tags integration ./internal/cmd/` which picks up the new
remote test files automatically. The SSH image build adds ~5s.

Total CI time estimate: ~90s (local) + ~90s (remote) + ~5s (SSH build) ≈ 3 min.

## 8. File Manifest

| File | Action | Description |
|------|--------|-------------|
| `internal/cmd/testdata/sshd/Dockerfile` | Create | Alpine + openssh + docker-cli |
| `internal/cmd/integration_remote_test.go` | Create | 3 remote test functions |
| `internal/cmd/integration_helpers_test.go` | Modify | Add SSH container helpers |
