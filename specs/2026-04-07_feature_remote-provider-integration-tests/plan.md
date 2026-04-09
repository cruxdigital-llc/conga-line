# Plan: Remote Provider Integration Tests

## Approach

Run an ephemeral SSH container (Alpine + openssh + docker-cli) with the
host's Docker socket mounted. The remote provider SSHs into it and
executes Docker commands that control the same daemon the local tests use.
Test scenarios mirror the local provider tests with `--provider remote`.

## SSH Container Design

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache openssh docker-cli bash && \
    ssh-keygen -A && \
    echo "PermitRootLogin yes" >> /etc/ssh/sshd_config && \
    echo "PermitEmptyPasswords yes" >> /etc/ssh/sshd_config && \
    passwd -d root
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
```

- Alpine is tiny (~7MB base), fast to build
- `docker-cli` only (no daemon) — uses the mounted socket
- Root login with no password for test simplicity
- Alternatively: inject an ephemeral SSH public key via volume mount

The Dockerfile lives at `internal/cmd/testdata/sshd/Dockerfile`.
Built once per test run via `docker build`.

## File Layout

```
internal/cmd/
  integration_test.go               # existing local tests
  integration_remote_test.go         # NEW: remote provider tests
  integration_helpers_test.go        # extended with SSH container helpers
  testdata/
    sshd/
      Dockerfile                     # Alpine + openssh + docker-cli
```

## SSH Container Lifecycle

```go
func setupRemoteTestEnv(t *testing.T) (dataDir, agentName string, sshPort int) {
    requireDocker(t)
    
    // Build SSH image (cached after first run)
    buildSSHImage(t)
    
    // Generate ephemeral SSH key pair
    keyDir := t.TempDir()
    generateSSHKey(t, keyDir)
    
    // Start SSH container with Docker socket + authorized_keys
    sshPort = startSSHContainer(t, keyDir)
    
    // Wait for sshd to be ready
    waitForSSH(t, sshPort)
    
    // Create data dir and agent name
    dataDir = filepath.Join(t.TempDir(), ".conga")
    agentName = "rtest-" + hash(t.Name())
    
    t.Cleanup(func() {
        runCLI(t, "--provider", "remote", "--data-dir", dataDir,
            "admin", "teardown", "--force")
        cleanupTestContainers(agentName)
        stopSSHContainer(t)
    })
    
    return dataDir, agentName, sshPort
}
```

Key helpers:
- `buildSSHImage(t)` — `docker build -t conga-test-sshd testdata/sshd/`
- `generateSSHKey(t, dir)` — `ssh-keygen -t ed25519 -f <dir>/id_test -N ""`
- `startSSHContainer(t, keyDir)` — runs container with `-v /var/run/docker.sock`,
  `-v <keyDir>/id_test.pub:/root/.ssh/authorized_keys`, `-p 0:22` (random port)
- `waitForSSH(t, port)` — retry TCP connect to `localhost:<port>` until ready

## Setup Config for Remote Provider

The `admin setup --provider remote` needs SSH connection details passed
via `--json`:

```json
{
  "ssh_host": "127.0.0.1",
  "ssh_port": <dynamic>,
  "ssh_user": "root",
  "ssh_key_path": "<tempdir>/id_test",
  "image": "ghcr.io/openclaw/openclaw:2026.3.11",
  "repo_path": "<repo_root>"
}
```

## Test Functions

### `TestRemoteAgentLifecycle`

Mirrors `TestAgentLifecycle` with `--provider remote`:

1. Start SSH container
2. `admin setup --provider remote --json '{"ssh_host":"127.0.0.1",...}'`
3. `admin add-user <agent>` → assert container running
4. `secrets set` → verify not in env → `refresh` → verify in env
5. `secrets delete` → `refresh` → verify gone
6. `logs`, `pause`, `unpause`
7. `admin remove-agent` → `admin teardown`

### `TestRemoteTeamAgentWithBehavior`

Mirrors `TestTeamAgentWithBehavior`:

1. Start SSH container
2. `admin setup` with `repo_path`
3. Create agent-specific SOUL.md in data dir
4. `admin add-team` → verify SOUL.md in container, MEMORY.md pristine
5. Override AGENTS.md → `refresh` → verify overridden
6. Remove override → `refresh` → verify reverted to default
7. Teardown

### `TestRemoteEgressPolicyEnforcement`

Mirrors `TestEgressPolicyEnforcement`:

1. Start SSH container
2. Setup + provision with no policy → verify blocked
3. Validate mode → verify allowed
4. Enforce mode → verify allowed/blocked per domain

## Docker Assertions

The existing `assertContainerRunning`, `assertEnvVar`, `assertFileContent`
helpers use `docker exec` directly (not through the provider). This still
works for remote tests because the agent containers run on the same Docker
daemon — they're visible to both the SSH container and the host.

## Agent Name Prefix

- Local tests: `itest-<hash>`
- Remote tests: `rtest-<hash>`

This prevents conflicts if both test suites run in the same `go test` invocation.

## CI Integration

The existing integration CI job already has Docker. Extend it:

```yaml
- name: Integration tests
  run: go test -tags integration ./internal/cmd/ -v -timeout 10m -count=1
```

No changes needed — the remote tests build the SSH image inline. The only
addition: the SSH image build adds ~5s to the test run.

## Risks

- **Docker socket permissions**: The SSH container needs access to
  `/var/run/docker.sock`. On Linux (CI), the socket is typically
  owned by `root:docker`. The SSH container runs as root, so no issue.
  On macOS (Docker Desktop), the socket is accessed through the VM — also
  works as root.
- **Port conflicts**: SSH container uses `-p 0:22` for random port
  allocation. No conflict risk.
- **Cleanup ordering**: The SSH container must be stopped AFTER agent
  containers are torn down (teardown runs Docker commands through SSH).
  The `t.Cleanup` ordering handles this (LIFO).
- **Remote setup installs Docker**: The setup flow checks if Docker is
  installed. Since our SSH container has `docker-cli`, the check passes
  and it skips installation.
