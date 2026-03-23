# VPS Provider — High-Level Plan

## Approach
The VPS provider is a "remote local provider" — same container topology (per-agent Docker containers, optional Slack router, egress proxy, per-agent bridge networks) with all operations executing on a remote host via SSH instead of local `exec.CommandContext`. This mirrors how the AWS provider wraps SSM around shell scripts, but using SSH as the transport.

## Architecture

### Provider Comparison
| Concern | AWS | VPS | Local |
|---|---|---|---|
| Remote execution | SSM RunCommand | SSH (`golang.org/x/crypto/ssh`) | Local `exec.Command` |
| Agent discovery | SSM Parameter Store | JSON files on remote disk | JSON files on local disk |
| Secrets | Secrets Manager | Files mode 0400 on remote | Files mode 0400 on local |
| Gateway access | SSM port forwarding | SSH tunnel | Direct localhost |
| Container mgmt | Docker CLI via SSM scripts | Docker CLI via SSH | Docker CLI locally |

### Container Topology on VPS (identical to local)
```
VPS Host (/opt/conga/)
├── conga-router          (Socket Mode → HTTP fan-out, optional)
├── conga-egress-proxy    (egress control)
├── conga-myagent           (network: conga-myagent)
├── conga-leadership      (network: conga-leadership)
└── ...
```

## Package Structure
```
cli/internal/provider/vpsprovider/
    provider.go      — VPSProvider struct, init/factory, 17 Provider methods
    ssh.go           — SSH client: connect, exec, SFTP upload/download, tunnel
    docker.go        — Remote Docker CLI helpers (mirror of localprovider/docker.go)
    secrets.go       — Remote file-based secrets via SFTP
    integrity.go     — Remote config integrity checks via SSH
    setup.go         — Host preparation: Docker auto-install, directory creation, wizard
```

## Implementation Phases

### Phase 1: SSH Foundation (`ssh.go`)
SSH client wrapping `golang.org/x/crypto/ssh` with:
- `Connect()` — key resolution: explicit path → SSH agent → ~/.ssh/id_ed25519 → ~/.ssh/id_rsa
- `Run()` / `RunWithStderr()` — remote command execution with context deadlines
- `Upload()` / `Download()` — SFTP file transfer (atomic write for secrets)
- `UploadDir()` — recursive directory upload
- `MkdirAll()` — remote directory creation
- `ForwardPort()` — SSH local port forwarding for `conga connect`

### Phase 2: Docker Helpers (`docker.go`)
Mirror `localprovider/docker.go` replacing `exec.CommandContext("docker", ...)` with `p.ssh.Run(ctx, "docker " + shelljoin(...))`. Includes `shelljoin()` for safe argument quoting.

### Phase 3: Core Provider (`provider.go`)
`VPSProvider` struct with `ssh *SSHClient`, `dataDir string`, `remoteDir string`. All 17 interface methods ported from local provider with SSH transport. Config generation still uses `common.GenerateOpenClawConfig()` etc. locally, then uploads results.

### Phase 4: Secrets + Integrity (`secrets.go`, `integrity.go`)
File-based secrets at `/opt/conga/secrets/` via SFTP. SHA256 integrity checking of remote `openclaw.json`.

### Phase 5: Setup Wizard (`setup.go`)
1. Prompt SSH details (host, port, user, key path)
2. Test connection, TOFU host key verification
3. Auto-install Docker (detect apt/dnf/yum/pacman)
4. Create remote directory tree
5. Prompt for image, repo path, shared secrets
6. Upload router/behavior/egress-proxy files
7. Pull images, build egress proxy, start infrastructure
8. Save config

### Phase 6: Config + Wiring
- Add SSH fields to `provider.Config` struct
- Add vpsprovider import to `root.go`
- Add `golang.org/x/crypto` to `go.mod`

### Phase 7: Documentation
- User-facing setup guide
- CLAUDE.md VPS section
- PROJECT_STATUS.md update

## Files to Modify
| File | Change |
|---|---|
| `cli/internal/provider/vpsprovider/*` | **New** — 6 files |
| `cli/internal/provider/config.go` | Add SSHHost, SSHPort, SSHUser, SSHKeyPath fields |
| `cli/cmd/root.go` | Add vpsprovider import, update provider flag help |
| `cli/go.mod` / `cli/go.sum` | Add golang.org/x/crypto |

## Files to Reuse (no changes needed)
- `cli/internal/provider/provider.go` — Provider interface
- `cli/internal/provider/registry.go` — Register/Get pattern
- `cli/internal/provider/localprovider/*` — Primary reference to port
- `cli/internal/common/*` — Config generation, routing, behavior, validation, ports

## Security
- No inbound ports beyond SSH — gateway via SSH tunnel only
- SSH key auth only, host key verification via known_hosts
- Secrets: mode 0400, atomic write, same risk profile as local
- Container hardening: cap-drop ALL, no-new-privileges, mem/cpu/pid limits
- Shell injection prevention: `shelljoin()` + `ValidateAgentName()`

## Verification
1. Spin up Hetzner CAX11 (~$4/mo, Ubuntu 24.04)
2. Full lifecycle: setup → add-user → add-team → status → logs → secrets → connect → pause → unpause → teardown
3. Gateway-only mode (no Slack)
4. Slack mode (with router)
5. Browser test: gateway UI loads with auth token via SSH tunnel
