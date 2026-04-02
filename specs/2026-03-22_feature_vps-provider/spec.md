# VPS Provider — Technical Specification

## Overview
Third `provider.Provider` implementation managing OpenClaw agent clusters on any VPS over SSH. Structurally a remote local provider — same container topology, file-based secrets, Docker CLI operations — with SSH as the transport layer.

---

## 1. Package Structure

```
cli/pkg/provider/vpsprovider/
    provider.go      — VPSProvider struct, init/factory, 17 Provider interface methods
    ssh.go           — SSH client wrapper (connect, exec, SFTP, tunnel)
    docker.go        — Remote Docker CLI helpers
    secrets.go       — Remote file-based secret management
    integrity.go     — Remote config integrity monitoring
    setup.go         — Host preparation wizard (Docker install, directory creation)
```

---

## 2. Data Models

### 2.1 Config Changes (`cli/pkg/provider/config.go`)

Add SSH fields to the existing `Config` struct:

```go
type Config struct {
    Provider string `json:"provider"`
    DataDir  string `json:"data_dir,omitempty"`
    Region   string `json:"region,omitempty"`
    Profile  string `json:"profile,omitempty"`
    // VPS-specific
    SSHHost    string `json:"ssh_host,omitempty"`
    SSHPort    int    `json:"ssh_port,omitempty"`
    SSHUser    string `json:"ssh_user,omitempty"`
    SSHKeyPath string `json:"ssh_key_path,omitempty"`
}
```

Persisted in `~/.conga/config.json`. SSHPort defaults to 22, SSHUser defaults to "root".

### 2.2 VPS Local Config (`~/.conga/vps-config.json`)

Parallels `local-config.json` — stores setup wizard values:

```json
{
    "image": "ghcr.io/openclaw/openclaw:2026.3.11",
    "repo_path": "/path/to/conga-line"
}
```

Read via `p.getConfigValue(key)`, written via `p.setConfigValue(key, value)`. Uses local filesystem (not remote).

### 2.3 VPSProvider Struct

```go
type VPSProvider struct {
    ssh       *SSHClient
    dataDir   string   // local ~/.conga/ (for vps-config.json)
    remoteDir string   // /opt/conga/ on the VPS
}
```

### 2.4 SSHClient Struct

```go
type SSHClient struct {
    client *ssh.Client
    host   string
    port   int
    user   string
}
```

### 2.5 SSHTunnel Struct

```go
type SSHTunnel struct {
    listener net.Listener
    done     chan error
    cancel   context.CancelFunc
}

func (t *SSHTunnel) Wait() error   // blocks until tunnel closes
func (t *SSHTunnel) Stop()         // closes the tunnel
```

---

## 3. Remote Directory Layout

All agent data lives at `/opt/conga/` on the VPS (matches AWS provider):

```
/opt/conga/
    agents/{name}.json              — agent config (provider.AgentConfig)
    secrets/shared/                 — shared secrets (mode 0700 dir)
        slack-bot-token             — mode 0400
        slack-signing-secret        — mode 0400
        slack-app-token             — mode 0400
        google-client-id            — mode 0400
        google-client-secret        — mode 0400
    secrets/agents/{name}/          — per-agent secrets (mode 0700 dir)
        {secret-name}               — mode 0400
    config/
        {name}.env                  — env file for Docker (mode 0400)
        {name}.sha256               — config integrity baseline
        routing.json                — Slack router routing table
        router.env                  — router env file (mode 0400)
    data/{name}/                    — per-agent OpenClaw data directory
        openclaw.json               — OpenClaw config
        data/workspace/             — behavior files (SOUL.md, etc.)
        memory/ logs/ agents/ ...   — OpenClaw internal directories
    router/src/                     — router source (index.js)
    behavior/                       — behavior file templates
    egress-proxy/                   — egress proxy Dockerfile + config
    logs/                           — integrity check logs
```

---

## 4. SSH Client (`ssh.go`)

### 4.1 Connection

```go
func Connect(host string, port int, user, keyPath string) (*SSHClient, error)
```

**Key resolution order:**
1. Explicit `keyPath` parameter (from `Config.SSHKeyPath`)
2. SSH agent via `SSH_AUTH_SOCK` environment variable
3. `~/.ssh/id_ed25519`
4. `~/.ssh/id_rsa`

**Host key verification:** Uses `knownhosts.New()` from `golang.org/x/crypto/ssh/knownhosts` against `~/.ssh/known_hosts`. If host is unknown, connection fails with a clear error message including the fingerprint. During `Setup()`, TOFU flow prompts user to accept and appends to `known_hosts`.

**SSH config:** The provider does NOT parse `~/.ssh/config`. Connection parameters come from `provider.Config` fields only. This avoids complexity and keeps behavior predictable.

### 4.2 Command Execution

```go
func (c *SSHClient) Run(ctx context.Context, cmd string) (string, error)
func (c *SSHClient) RunWithStderr(ctx context.Context, cmd string) (stdout, stderr string, err error)
```

Creates a new `ssh.Session` per call (SSH sessions are lightweight). Context deadline enforced via goroutine + session.Signal(ssh.SIGKILL) on cancellation. Returns combined stdout on success, wraps stderr in error on failure.

### 4.3 File Transfer (SFTP)

```go
func (c *SSHClient) Upload(path string, content []byte, perm os.FileMode) error
func (c *SSHClient) Download(path string) ([]byte, error)
func (c *SSHClient) UploadDir(localDir, remotePath string) error
func (c *SSHClient) MkdirAll(path string, perm os.FileMode) error
```

**Upload:** Uses SFTP subsystem (`github.com/pkg/sftp`). For mode 0400 files (secrets), uses atomic write:
1. Write to temp file in same directory
2. `Chmod(0400)` on temp file
3. `Rename` temp → final path

If SFTP is unavailable (some minimal VPS images), falls back to `Run("cat > path && chmod perm path")` via heredoc.

**UploadDir:** Walks local directory, creates remote directories via `MkdirAll`, uploads files. Skips `node_modules/` (same as `localprovider.copyDir`).

**Note:** `golang.org/x/crypto/ssh` does not include SFTP. We add `github.com/pkg/sftp` as a second dependency, or implement file transfer via `ssh.Session` stdin piping (`cat > file`). Decision: use `github.com/pkg/sftp` — it's the standard Go SFTP library, well-maintained, and avoids fragile shell-based file transfer.

### 4.4 Port Forwarding

```go
func (c *SSHClient) ForwardPort(ctx context.Context, localPort, remotePort int) (*SSHTunnel, error)
```

Opens a local TCP listener on `127.0.0.1:localPort`. For each incoming connection, dials `localhost:remotePort` on the remote host via `c.client.Dial("tcp", fmt.Sprintf("localhost:%d", remotePort))` and bidirectionally copies data. Returns `SSHTunnel` with `Wait()` and `Stop()` methods matching the existing tunnel pattern from `cli/pkg/tunnel/tunnel.go`.

---

## 5. Docker Helpers (`docker.go`)

### 5.1 Core Execution

```go
func (p *VPSProvider) dockerRun(ctx context.Context, args ...string) (string, error) {
    cmd := "docker " + shelljoin(args...)
    return p.ssh.Run(ctx, cmd)
}
```

### 5.2 Shell Quoting

```go
func shelljoin(args ...string) string
```

Quotes each argument for POSIX shell: wraps in single quotes, escapes internal single quotes as `'\''`. Agent names are validated by `common.ValidateAgentName()` (alphanumeric + hyphens only) so injection risk is minimal, but quoting is applied defensively.

### 5.3 Function Mapping

Every function from `localprovider/docker.go` gets a VPS counterpart with identical signatures and behavior, replacing `exec.CommandContext("docker", args...)` with `p.dockerRun(ctx, args...)`:

| Function | Local impl | VPS impl |
|---|---|---|
| `dockerRun(ctx, args...)` | `exec.CommandContext("docker", args...)` | `p.ssh.Run(ctx, "docker " + shelljoin(args...))` |
| `dockerCheck(ctx)` | local docker info | remote docker info via SSH |
| `containerName(name)` | `"conga-" + name` | same (pure function) |
| `networkName(name)` | `"conga-" + name` | same (pure function) |
| `createNetwork(ctx, name)` | local docker network create | remote via SSH |
| `removeNetwork(ctx, name)` | local docker network rm | remote via SSH |
| `connectNetwork(ctx, net, container)` | local docker network connect | remote via SSH |
| `disconnectNetwork(ctx, net, container)` | local docker network disconnect | remote via SSH |
| `runAgentContainer(ctx, opts)` | local docker run | remote via SSH |
| `runRouterContainer(ctx, opts)` | local docker run | remote via SSH |
| `stopContainer(ctx, name)` | local docker stop | remote via SSH |
| `removeContainer(ctx, name)` | local docker rm -f | remote via SSH |
| `restartContainer(ctx, name)` | local docker restart | remote via SSH |
| `containerLogs(ctx, name, lines)` | local docker logs | remote via SSH |
| `inspectState(ctx, name)` | local docker inspect | remote via SSH |
| `containerStats(ctx, name)` | local docker stats | remote via SSH |
| `containerExists(ctx, name)` | local docker inspect | remote via SSH |
| `networkExists(ctx, name)` | local docker network inspect | remote via SSH |
| `pullImage(ctx, image)` | local docker pull | remote via SSH |
| `buildImage(ctx, dir, tag)` | local docker build | remote via SSH |
| `imageExists(ctx, image)` | local docker image inspect | remote via SSH |

### 5.4 Container Opts

Reuse same structs (`agentContainerOpts`, `routerContainerOpts`) but paths reference `/opt/conga/` instead of `~/.conga/`:
- `EnvFile`: `/opt/conga/config/{name}.env`
- `DataDir`: `/opt/conga/data/{name}`
- `RouterDir`: `/opt/conga/router`
- `RoutingJSON`: `/opt/conga/config/routing.json`

---

## 6. Provider Methods — Detailed Implementation

### 6.1 Identity & Discovery

**`Name()`** → `"vps"`

**`WhoAmI(ctx)`** — Runs `whoami` on remote host via SSH. Returns `Identity{Name: "{user}@{host}"}`.

**`ListAgents(ctx)`** — Lists `/opt/conga/agents/*.json` via SSH (`ls`), downloads and parses each file. Sorts by name. Returns empty slice if directory doesn't exist.

**`GetAgent(ctx, name)`** — Downloads `/opt/conga/agents/{name}.json` via SFTP. Returns descriptive error if not found.

**`ResolveAgentByIdentity(ctx)`** — Same as local: auto-resolve to single agent if exactly one exists. No IAM-style identity mapping.

### 6.2 Agent Lifecycle

**`ProvisionAgent(ctx, cfg)`** — Same 10-step sequence as local provider:
1. Upload agent config JSON to `/opt/conga/agents/{name}.json` via SFTP
2. Download shared + per-agent secrets from remote → call `common.GenerateOpenClawConfig()` + `common.GenerateEnvFile()` locally → upload results to remote
3. Create OpenClaw data directory structure on remote: `data/{name}/{data/workspace,memory,logs,...}`
4. Upload `MEMORY.md` stub if not exists
5. Upload behavior files (compose locally via `common.ComposeBehaviorFiles()` using local behavior dir, upload results)
6. Create Docker network on remote
7. Connect egress proxy to network
8. Start agent container on remote
9. Regenerate routing.json (generate locally, upload)
10. Ensure router if Slack configured; connect router to network
11. Save config hash baseline on remote

**`RemoveAgent(ctx, name, deleteSecrets)`** — Same as local but all ops via SSH. Delete remote files via SFTP or `rm`.

**`PauseAgent(ctx, name)`** — Download agent config, set `Paused: true`, upload, stop container, disconnect router, regenerate routing.

**`UnpauseAgent(ctx, name)`** — Download agent config, set `Paused: false`, upload, call `RefreshAgent()`, regenerate routing.

### 6.3 Container Operations

**`GetStatus(ctx, agentName)`** — `docker inspect` + `docker stats` via SSH. Parse same JSON/pipe-delimited format as local. `detectReadyPhase()` reused (pure function, operates on log string).

**`GetLogs(ctx, agentName, lines)`** — `docker logs --tail N --timestamps` via SSH.

**`ContainerExec(ctx, agentName, command)`** — `docker exec {container} {args}` via SSH.

**`RefreshAgent(ctx, agentName)`** — Same flow as local:
1. Download agent config from remote
2. Check integrity (download hash from remote, compute hash of remote config)
3. Preserve or regenerate gateway token
4. Download secrets → generate config locally → upload
5. Stop/remove/recreate container
6. Reconnect egress proxy and router
7. Save new baseline hash

**`RefreshAll(ctx)`** — Iterate agents, refresh each non-paused one. Same spinner UX.

### 6.4 Secrets

**`SetSecret(ctx, agentName, secretName, value)`** — Upload to `/opt/conga/secrets/agents/{name}/{secret}` via SFTP with mode 0400, atomic write.

**`ListSecrets(ctx, agentName)`** — List files in remote secrets dir. Parse modification times from `stat` output or SFTP `Stat()`.

**`DeleteSecret(ctx, agentName, secretName)`** — Remove file via SFTP or `rm`.

### 6.5 Connectivity

**`Connect(ctx, agentName, localPort)`** —
1. Download agent config to get `GatewayPort`
2. Read gateway token from remote `openclaw.json` (same JSON parsing as local)
3. Fallback: `docker exec` on remote to read token from running container
4. Start SSH tunnel: `p.ssh.ForwardPort(ctx, localPort, cfg.GatewayPort)`
5. Return `ConnectInfo` with non-nil `Waiter` channel (tunnel must stay alive)

**Key difference from local:** `Waiter` is non-nil because the SSH tunnel must remain open. The `connect` command already handles this correctly — it blocks on `Waiter` until Ctrl+C.

### 6.6 Environment Management

**`Setup(ctx)`** — See Section 7 (Setup Wizard).

**`CycleHost(ctx)`** — Same as local: stop all containers, restart infrastructure (egress proxy, router), refresh all agents. All via SSH.

**`Teardown(ctx)`** —
1. Remove all agents via `removeAgentDocker()` pattern
2. Cleanup conga-* containers/networks by prefix (same as local `cleanupDockerByPrefix`)
3. Remove `/opt/conga/` on remote: `rm -rf /opt/conga`
4. Clear local VPS config from `~/.conga/config.json`

---

## 7. Setup Wizard (`setup.go`)

### 7.1 Flow

```
conga admin setup --provider vps
```

1. **Prompt SSH details**
   - Host (required): IP or hostname
   - Port (default 22)
   - User (default "root")
   - SSH key path (default: auto-detect)

2. **Test SSH connection**
   - Attempt `Connect()`
   - If host key unknown: display fingerprint, ask user to confirm (TOFU)
   - On confirmation: append to `~/.ssh/known_hosts`
   - Run `whoami` to verify access

3. **Check/install Docker**
   ```bash
   if command -v docker &>/dev/null; then
       docker info --format '{{.ServerVersion}}'
   else
       # Detect package manager
       if command -v apt-get &>/dev/null; then
           apt-get update && apt-get install -y docker.io
       elif command -v dnf &>/dev/null; then
           dnf install -y docker
       elif command -v yum &>/dev/null; then
           yum install -y docker
       elif command -v pacman &>/dev/null; then
           pacman -S --noconfirm docker
       else
           exit 1  # "Unsupported OS — install Docker manually"
       fi
       systemctl enable docker && systemctl start docker
   fi
   ```

4. **Create remote directory tree**
   ```
   mkdir -p /opt/conga/{agents,secrets/{shared,agents},config,data,router/src,behavior,egress-proxy,logs}
   chmod 700 /opt/conga/secrets /opt/conga/secrets/shared /opt/conga/secrets/agents
   ```

5. **Prompt for Docker image** (same flow as local setup)

6. **Prompt for repo path** (local, for uploading files to remote)

7. **Prompt for shared secrets** (same prompts as local — all optional for gateway-only)
   - Upload each secret to `/opt/conga/secrets/shared/{name}` via SFTP (mode 0400)

8. **Upload source files**
   - `UploadDir(repo/router, /opt/conga/router)` — router source
   - `UploadDir(repo/behavior, /opt/conga/behavior)` — behavior files
   - `UploadDir(repo/deploy/egress-proxy, /opt/conga/egress-proxy)` — egress proxy

9. **Pull Docker images on remote**
   - `docker pull {image}` via SSH
   - `docker pull node:22-alpine` via SSH

10. **Build egress proxy on remote**
    - `docker build -t conga-egress-proxy /opt/conga/egress-proxy` via SSH

11. **Create initial routing.json**
    - Upload `{"channels":{},"members":{}}` to `/opt/conga/config/routing.json`

12. **Start infrastructure containers**
    - Start egress proxy (same `ensureEgressProxy` logic)
    - If Slack configured: write router.env, start router (same `ensureRouter` logic)

13. **Save config**
    - `~/.conga/config.json`: `{provider: "vps", ssh_host, ssh_port, ssh_user, ssh_key_path}`
    - `~/.conga/vps-config.json`: `{image, repo_path}`

14. **Print next steps**
    ```
    VPS deployment ready! Next steps:
      conga admin add-user <name> [slack_member_id]
      conga admin add-team <name> [slack_channel]
    ```

### 7.2 TOFU (Trust On First Use) Flow

When the VPS host key is not in `~/.ssh/known_hosts`:

```
The authenticity of host '203.0.113.42 (203.0.113.42)' can't be established.
ED25519 key fingerprint is SHA256:abc123...
Are you sure you want to continue connecting? [y/N]
```

On "y": append `{host} {key-type} {base64-key}` to `~/.ssh/known_hosts`. On "n": abort setup.

---

## 8. Wiring Changes

### 8.1 `cli/cmd/root.go`

Add blank import:
```go
_ "github.com/cruxdigital-llc/conga-line/cli/pkg/provider/vpsprovider"
```

Update `--provider` flag help text:
```go
rootCmd.PersistentFlags().StringVar(&flagProvider, "provider", "", "Deployment provider: aws, local, vps (default: local)")
```

Update `Long` description to mention VPS.

### 8.2 `cli/go.mod`

```
require golang.org/x/crypto v0.32.0  // SSH, known_hosts, agent
require github.com/pkg/sftp v1.13.7  // SFTP file transfer
```

---

## 9. Edge Cases & Error Handling

### 9.1 SSH Failures

| Scenario | Behavior |
|---|---|
| Connection refused | `"failed to connect to VPS {user}@{host}:{port}: connection refused"` |
| Auth failure (wrong key) | `"SSH authentication failed. Check key path and ensure public key is in ~/.ssh/authorized_keys on the VPS"` |
| Host key mismatch | `"WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! ... Remove the old key from ~/.ssh/known_hosts line N"` |
| Connection drops mid-command | Error propagates: `"ssh session failed (connection may be broken): {err}"`. User reruns. No auto-reconnect. |
| Timeout | Context deadline enforced. Session killed via SIGKILL. `"operation timed out after {duration}"` |

### 9.2 Docker Failures

| Scenario | Behavior |
|---|---|
| Docker not installed (during non-setup commands) | `"Docker is not available on the VPS. Run 'conga admin setup --provider vps' to install it."` |
| Image pull fails (registry auth, network) | Warning + instructions to pull manually. Same as local. |
| Container won't start | `"failed to start container: {docker stderr}"`. User checks logs. |
| Port conflict on VPS | Docker error surfaces: `"port is already allocated"`. User changes gateway_port. |

### 9.3 File System

| Scenario | Behavior |
|---|---|
| Permission denied on /opt/conga | `"permission denied: ensure SSH user has write access to /opt/conga"` |
| Disk full on VPS | Docker/write errors surface. User responsibility to monitor. |
| Agent config not found | Same error as local: `"agent {name} not found. Use 'conga admin add-user' ..."` |

### 9.4 Concurrency

Multiple CLI invocations against same VPS: each creates its own SSH connection. Docker commands are atomic. `routing.json` regeneration is idempotent (reads all agents, writes complete file). No locking needed.

### 9.5 Setup Idempotency

`conga admin setup --provider vps` is idempotent:
- Docker install: skipped if already present
- Directory creation: `mkdir -p` is idempotent
- Secret prompts: show "(set)" / "(not set)" status, ask before overwriting
- Image pull: always re-pulls (gets latest tag if tag is mutable)
- Infrastructure containers: `ensure*` functions check if running before recreating

---

## 10. Security Analysis

### 10.1 Transport Security
- All communication over SSH (encrypted, authenticated)
- No passwords — SSH key auth only
- Host key verification via known_hosts (TOFU during setup)

### 10.2 Secret Storage
- Same model as local provider: files with mode 0400
- Atomic write prevents momentary exposure
- No encryption at rest beyond what the VPS disk provides
- Disk encryption is user's responsibility (documented in setup guide)

### 10.3 Network Exposure
- No inbound ports opened beyond SSH (22)
- Gateway accessible only via SSH tunnel — never exposed to internet
- Container ports bound to 127.0.0.1 on VPS — not reachable from outside

### 10.4 Container Hardening
Identical to local provider:
- `--cap-drop ALL`
- `--security-opt no-new-privileges`
- `--memory 2g --cpus 0.75 --pids-limit 256`
- Port binding: `127.0.0.1:{port}:{port}`
- Per-agent Docker networks

### 10.5 Shell Injection Prevention
- All Docker CLI arguments pass through `shelljoin()` (POSIX shell quoting)
- Agent names validated by `common.ValidateAgentName()` (alphanumeric + hyphens)
- No user-controlled values interpolated into shell commands without quoting

---

## 11. Dependencies

| Package | Version | Purpose |
|---|---|---|
| `golang.org/x/crypto` | latest | SSH client, known_hosts verification, SSH agent |
| `github.com/pkg/sftp` | latest | SFTP file transfer over SSH |

Both are well-maintained, widely-used Go packages with no transitive dependencies of concern.

---

## 12. Files Summary

### New Files (6)
| File | Lines (est.) | Description |
|---|---|---|
| `vpsprovider/provider.go` | ~800 | Provider struct, factory, 17 interface methods |
| `vpsprovider/ssh.go` | ~250 | SSH client wrapper |
| `vpsprovider/docker.go` | ~200 | Remote Docker CLI helpers |
| `vpsprovider/secrets.go` | ~120 | Remote secret management |
| `vpsprovider/integrity.go` | ~60 | Remote config integrity |
| `vpsprovider/setup.go` | ~300 | Setup wizard |

### Modified Files (3)
| File | Change |
|---|---|
| `cli/pkg/provider/config.go` | Add 4 SSH fields to Config struct |
| `cli/cmd/root.go` | Add vpsprovider import, update flag help |
| `cli/go.mod` | Add golang.org/x/crypto, github.com/pkg/sftp |

### Reused (no changes)
- `cli/pkg/provider/provider.go` — Provider interface
- `cli/pkg/provider/registry.go` — Registration pattern
- `cli/pkg/common/*` — All config generation, routing, behavior, validation
