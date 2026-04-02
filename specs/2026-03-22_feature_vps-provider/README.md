# Remote Provider — Trace Log

**Feature**: `remote-provider` (originally `vps-provider`, renamed to cover all SSH-accessible hosts)
**Created**: 2026-03-22
**Phase**: Verified Complete — End-to-End on Raspberry Pi

## Session Log

### 2026-03-22 — Plan Feature

**Active Personas**: Architect, Product Manager, QA
**Active Capabilities**: Playwright (browser testing), Context7 (library docs)

#### Decisions
- SSH-only provider (no VPS API integrations) — user provisions their own host
- Docker auto-install during setup (detect OS: apt/dnf/yum/pacman)
- File-based secrets on remote host (mode 0400), same model as local provider
- SSH tunnel for gateway access — no inbound ports beyond SSH (22)
- New dependencies: `golang.org/x/crypto` for SSH client + `github.com/pkg/sftp` for file transfer
- Remote directory at `/opt/conga/` (matches AWS provider path)
- SSH key auth only (no password auth)

#### Files Created
- [requirements.md](requirements.md) — Feature requirements and user stories
- [plan.md](plan.md) — High-level implementation plan

### 2026-03-22 — Spec Feature

#### Files Created
- [spec.md](spec.md) — Detailed technical specification (12 sections)

#### Persona Review
- **Architect**: APPROVE
- **Product Manager**: APPROVE
- **QA**: APPROVE with notes

#### Standards Gate: PASS (0 violations, 2 accepted warnings)

### 2026-03-22 — Implement Feature

#### Files Created
- `cli/pkg/provider/remoteprovider/ssh.go` — SSH client (SSHConnect, Run, Upload, Download, UploadDir, ForwardPort, shelljoin)
- `cli/pkg/provider/remoteprovider/docker.go` — Remote Docker CLI helpers (21 functions mirroring localprovider)
- `cli/pkg/provider/remoteprovider/provider.go` — RemoteProvider struct + all 17 Provider interface methods
- `cli/pkg/provider/remoteprovider/secrets.go` — Remote file-based secret management (6 methods)
- `cli/pkg/provider/remoteprovider/integrity.go` — Remote config integrity monitoring (3 methods)
- `cli/pkg/provider/remoteprovider/setup.go` — Setup wizard with Docker auto-install
- `cli/pkg/provider/remoteprovider/ssh_test.go` — 29 unit tests for shell quoting

#### Files Modified
- `cli/pkg/provider/config.go` — Added SSHHost, SSHPort, SSHUser, SSHKeyPath fields
- `cli/cmd/root.go` — Added remoteprovider import, updated provider flag help and Long description
- `cli/go.mod` / `cli/go.sum` — Added golang.org/x/crypto, github.com/pkg/sftp, github.com/kr/fs
- `README.md` — Added Remote provider documentation, quick start, and architecture

#### Implementation Notes
- Remote provider follows local provider structure — 6 source files mirroring the 4 local files
- SSH transport added as thin wrapper; all Docker/config/routing logic reuses `common.*` package
- SFTP with shell fallback for environments where SFTP subsystem is unavailable
- SSH tunnel for `Connect()` returns non-nil `Waiter` (like AWS SSM tunnel), works with existing `connect` command
- Setup wizard handles Docker auto-install for apt/dnf/yum/pacman
- Originally named `vpsprovider`, renamed to `remoteprovider` to accurately cover VPS, bare metal, and any SSH host

### 2026-03-23 — Bugs Found During Raspberry Pi Testing

Three bugs found and fixed before integration testing:

1. **First-time setup chicken-and-egg** (critical): `NewRemoteProvider` required SSH host in config, but first-time users have no config. Fixed to allow provider creation without SSH connection; `Setup()` prompts for SSH details and connects interactively.

2. **SSH auth method ordering** (critical): Go's SSH library treats a rejected key as a definitive auth failure instead of trying the next method. The SSH agent's RSA key was offered first and rejected by Debian 13 (which disables `ssh-rsa`), causing the entire auth to fail before trying the ed25519 file key. Fixed by collecting all signers into a single `ssh.PublicKeys()` call so all keys are offered in one attempt.

3. **Non-root directory creation** (important): `/opt/conga/` creation failed when SSH user wasn't root. Fixed with sudo detection and clear error when neither root nor sudo is available.

### 2026-03-23 — Verify Feature

#### Automated Verification
- `go test ./... -count=1` — all packages pass (no cached results)
- `go vet ./...` — zero warnings
- `go build -o /tmp/conga .` — binary builds and runs, shows "remote" in provider list

#### Persona Verification (Post-Implementation)
- **Architect**: APPROVE — structurally consistent with existing providers, minimal dependencies, no API changes
- **Product Manager**: APPROVE — all 14 user stories implementable, scope maintained, documentation complete
- **QA**: APPROVE — 29 unit tests (better than sibling localprovider at 0), integration tests completed on real hardware

#### Standards Gate (Post-Implementation): PASS
| Standard | Verdict |
|---|---|
| Zero trust the AI agent | ✅ PASSES |
| Immutable configuration | ⚠️ WARNING (accepted, same as local) |
| Least privilege everywhere | ✅ PASSES |
| Defense in depth | ✅ PASSES |
| Secrets never touch disk | ⚠️ WARNING (accepted, same as local) |
| Detect what you can't prevent | ✅ PASSES |
| Isolated Docker networks | ✅ PASSES |
| Container resource limits | ✅ PASSES |
| Drop all capabilities | ✅ PASSES |
| Shell injection prevention | ✅ PASSES |

### 2026-03-23 — Integration Test: Raspberry Pi (Bare Metal)

**Test host**: Raspberry Pi, Debian 13 (trixie), ARM64 (aarch64), 905MB RAM, Docker 29.2.1

#### Full Lifecycle Results

| Command | Result | Notes |
|---|---|---|
| `auth status` | ✅ PASS | `pi@pi`, provider: remote |
| `admin list-agents` (empty) | ✅ PASS | "No agents found." |
| `status --agent test` (no container) | ✅ PASS | "Container: not found" |
| `secrets set test-key --agent test` | ✅ PASS | File written to `/opt/conga/secrets/agents/test/test-key`, mode 0400, atomic write |
| `secrets list --agent test` | ✅ PASS | Shows name, env var mapping, last changed timestamp |
| `secrets delete test-key --agent test` | ✅ PASS | File removed from remote host |
| `admin add-user test` | ✅ PASS | Network created, config uploaded via SFTP, container started, gateway listening |
| `admin list-agents` | ✅ PASS | Shows `test` agent, type: user, status: active, port: 18789 |
| `status --agent test` | ✅ PASS | Running, gateway up, CPU/memory stats |
| `logs --agent test` | ✅ PASS | Gateway startup logs retrieved: token generated, canvas mounted, heartbeat started |
| `connect --agent test` | ✅ PASS | SSH tunnel opened, HTTP 200 from gateway, auth token in URL |
| `admin pause test` | ✅ PASS | Container stopped, status shows "paused" |
| `admin list-agents` (paused) | ✅ PASS | Shows `test` agent with status: paused |
| `admin unpause test` | ✅ PASS | Container restarted, integrity check ran (detected OpenClaw's self-modification, generated fresh token) |
| `admin teardown` | ✅ PASS | All containers removed, `/opt/conga` deleted, no Docker artifacts remaining |

#### Key Observations
- OpenClaw runs on 905MB RAM Pi — container started and gateway became healthy despite being well under the 2GB recommended minimum
- Image pull took ~15 minutes on Pi's bandwidth (~1.3GB image); first `add-user` attempt timed out at default 5m timeout while pulling inline. Succeeded after image was cached locally.
- Config integrity check correctly detected OpenClaw's auto-generated gateway token write-back to `openclaw.json` on first boot — expected behavior, fresh token generated on unpause
- `docker stats` reports `0B / 0B` for memory on this Pi's Docker version — cosmetic issue with stats format, container runs fine
- Gateway-only mode (no Slack) works end-to-end: container starts, gateway listens, SSH tunnel provides access, browser gets HTTP 200
