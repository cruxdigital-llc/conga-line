# GLaDOS Trace — Non-Root Container Enforcement

**Feature**: Explicit `--user 1000:1000` on all agent and router containers
**Created**: 2026-03-29
**Active Personas**: Architect, QA
**Active Capabilities**: Conga MCP tools (live verification)

## Session Log

### 2026-03-29 — Plan Phase
- **Codebase exploration**: Analyzed container privilege model across all 3 providers (local, remote, AWS)
- **Finding**: Agent containers rely on image `USER` directive (fragile). Router container runs as root (`node:22-alpine` default). Egress proxy already correct (`--user 101:101`).
- **Bonus finding**: AWS router systemd unit missing `--tmpfs /tmp:rw,noexec,nosuid` that local/remote already have.
- **Decision**: Add `--user 1000:1000` to agent + router containers. Leave systemd `User=` out of scope (iptables needs root).
- **Plan created**: `plan.md`

### 2026-03-29 — Spec Phase
- Session resumed for detailed specification.
- **Spec created**: `spec.md` — 10 changes across 7 files, all three providers covered.
- **Persona review**:
  - **Architect**: Approved. Pattern consistent with egress proxy. No new dependencies. Data safety confirmed.
  - **QA**: Approved. Edge cases covered (macOS, existing containers, non-root Node.js, alpine uid). Note: verify sed replacement in refresh-user.sh.tmpl renders correctly.
- **Standards gate**: 8 checks, 0 violations, 0 warnings. All pass.

### 2026-03-29 — Implementation Phase
- **Phase 1**: Agent containers — added `--user 1000:1000` to 6 files (local, remote, AWS bootstrap, add-user, add-team, refresh-user)
- **Phase 2**: Router containers — added `--user 1000:1000` to 3 files (local, remote, AWS bootstrap + missing `--tmpfs` alignment)
- **Phase 3**: Updated security.md non-root container row
- **Phase 4**: Compilation success, all 17 test packages pass

**Files modified**:
- `cli/internal/provider/localprovider/docker.go` — `--user 1000:1000` on agent + router
- `cli/internal/provider/remoteprovider/docker.go` — `--user 1000:1000` on agent + router
- `terraform/user-data.sh.tftpl` — `--user 1000:1000` on agent + router; added missing `--tmpfs` to router
- `cli/scripts/add-user.sh.tmpl` — `--user 1000:1000` on agent ExecStart
- `cli/scripts/add-team.sh.tmpl` — `--user 1000:1000` on agent ExecStart
- `cli/scripts/refresh-user.sh.tmpl` — `--user 1000:1000` in sed replacement
- `product-knowledge/standards/security.md` — updated non-root description

### 2026-03-29 — Verification Phase
- **Test suite**: 17 packages pass, 0 failures
- **Linting**: `go vet ./...` clean
- **Persona verification**:
  - **Architect**: Approved. Diff is minimal, symmetric across providers, matches spec exactly.
  - **QA**: Approved. sed replacement verified correct. No issues found.
- **Standards gate (post-implementation)**: 8/8 pass, 0 violations, 0 warnings
- **Spec retrospection**: Implementation matches spec exactly. No divergences.
- **Test synchronization**: No new public methods. Existing script template tests pass. No gaps.
- **Status**: Complete

## Standards Gate Report (Post-Implementation)
| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Security — Non-root container | all | must | ✅ PASSES |
| Security — Cap-drop ALL | all | must | ✅ PASSES |
| Security — Secrets via env vars | all | must | ✅ PASSES |
| Architecture — Provider contract | all | must | ✅ PASSES |
| Architecture — Agent Data Safety | all | must | ✅ PASSES |
| Architecture — Interface Parity | all | must | ✅ PASSES |
| Architecture — Secure by default | all | must | ✅ PASSES |
| Egress Controls | all | should | ✅ PASSES |

## Standards Gate Report (Pre-Implementation)
| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Security — Non-root container | all | must | ✅ PASSES |
| Security — Cap-drop ALL | all | must | ✅ PASSES |
| Security — Secrets via env vars | all | must | ✅ PASSES |
| Architecture — Provider contract | all | must | ✅ PASSES |
| Architecture — Agent Data Safety | all | must | ✅ PASSES |
| Architecture — Interface Parity | all | must | ✅ PASSES |
| Architecture — Secure by default | all | must | ✅ PASSES |
| Egress Controls | all | should | ✅ PASSES |
