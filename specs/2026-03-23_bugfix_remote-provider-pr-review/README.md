# Trace: Remote Provider PR Review Fixes

**Bug/Issue**: PR #15 code review identified 16 findings across security, correctness, and code quality
**Created**: 2026-03-23
**Phase**: CLOSED
**PR**: https://github.com/cruxdigital-llc/conga-line/pull/15

## Session Log

### 2026-03-23 — Bug Identification (PR Review)

Critical review of PR #15 ("Add remote provider for SSH-based agent deployment") identified 16 issues:

#### Critical
1. **`filepath.Join` for remote paths** — uses OS-local separators; breaks Windows/macOS clients constructing Linux remote paths
2. **`SSHKeyPath` not persisted during setup** — custom key path lost after setup, subsequent CLI invocations fail

#### Security
3. **`InsecureIgnoreHostKey` with no warning** — silent MITM vulnerability when `~/.ssh/known_hosts` missing
4. **Shell injection surface in `RunIntegrityCheck`** — log append via `echo >> ` instead of SFTP
5. **Docker install runs without confirmation** — `apt-get install -y` on someone else's machine with no prompt

#### Correctness
6. **Stale package doc** — `// Package vpsprovider` in `ssh.go` after rename
7. **Stale "VPS-specific" comment** in `config.go`
8. **Error handling asymmetry in `RefreshAgent`** — swallows errors that `ProvisionAgent` checks
9. **Redundant `joinLines` function** — reimplements `strings.Join`
10. **Double `fmt.Sprintf` wrapping** in integrity log

#### Test Coverage
11. **Only shell quoting tested** — `detectReadyPhase`, `readExistingGatewayToken` have zero coverage

#### Design
12. **SFTP client created per-operation** — performance concern during setup (dozens of handshakes)
13. **No SSH connection cleanup** — `Close()` never called
14. **`isSafeArg` allows `=` with no comment** — allowlist reasoning undocumented

#### Nits
15. **Branch name still says "vps"** — `feature/vps-ssh-support`
16. **`CycleHost` sequential operations** — could parallelize

### 2026-03-23 — Root Cause Analysis

**Why did these happen?**
1. Remote provider was developed and tested on macOS (where `filepath.Join` produces `/`). Would only surface with Windows clients.
2. Setup wizard captures `keyPath` in a local variable but doesn't thread it into the persisted config struct.
3. `InsecureIgnoreHostKey` was a pragmatic choice for initial development — no TOFU implementation yet.
4. Integrity logging was an afterthought added late in the implementation.
5. Docker install was designed for self-owned hosts (Raspberry Pi) — didn't consider managed hosts.

**5 Whys summary**: The provider was built and tested in a single-platform, single-user context. Review caught the cross-platform and multi-user gaps.

### 2026-03-23 — Fix Strategy (Architect Persona)

Strategy chosen: **plan.md** — fix all 16 findings in a single commit, grouped by file.

**Risk assessment:**
- `path.Join` migration: Low risk — mechanical replacement, verified by grep
- Host key warning: Low risk — additive, no behavior change
- SSHKeyPath persistence: Low risk — one-line addition to existing struct
- Docker install prompt: Low risk — adds `ui.Confirm` gate
- Test additions: Zero risk — new test file only

**Band-aid vs real fix:**
- Items 1, 2, 5, 8 are real fixes
- Item 3 (host key warning) is a band-aid — real fix is TOFU, deferred
- Item 12 (SFTP caching) deferred — TODO comment only

### 2026-03-23 — Implement Fix

All 13 fixes applied. 3 items deferred (SFTP caching, branch rename, CycleHost parallelization).

#### Files Modified
| File | Changes |
|---|---|
| `ssh.go` | Package doc fix, host key warning, `posixpath` import for remote paths in Upload/UploadDir, `isSafeArg` comment, SFTP cache TODO |
| `config.go` | Updated "VPS-specific" comment to "Remote provider (SSH)", inline comments |
| `provider.go` | 20+ `filepath.Join` → `posixpath.Join` for remote paths, `RefreshAgent` error comment, `Close()` method, `posixpath.Base` for remote filename extraction |
| `secrets.go` | All `filepath.Join` → `posixpath.Join` (all paths are remote), `filepath.Dir` → `posixpath.Dir` |
| `integrity.go` | All `filepath.Join` → `posixpath.Join`, replaced `echo >>` with stdin-piped `cat >>`, removed `joinLines`, simplified double `Sprintf` |
| `setup.go` | 4 remote-path `filepath.Join` → `posixpath.Join`, Docker install confirmation prompt, `SSHKeyPath` persisted to config |
| `provider_test.go` | **New** — 7 tests for `detectReadyPhase` |

#### Verification
- `go build ./...` — PASS
- `go vet ./...` — PASS (zero warnings)
- `go test ./internal/provider/remoteprovider/ -count=1` — 36/36 PASS (29 existing + 7 new)
- `grep -rn 'filepath\.' remoteprovider/*.go` — remaining hits are all local paths (dataDir, repoPath, SSH key paths, WalkDir)
- `grep -rni 'vps' remoteprovider/*.go` — only 1 hit: comment listing "VPS instances" as a supported host type (accurate)

### 2026-03-23 — Verify Fix

#### Regression Testing (QA Persona)
- `go test ./... -count=1` — all 8 test packages pass, zero regressions
- Side-effect check: changes isolated to `remoteprovider/` + comment-only change in `config.go`

#### Code Review (Architect Persona)
- Security standards audit: PASS (shell injection fix strengthens; host key warning adds detection)
- No new technical debt beyond one documented TODO (SFTP caching)
- `posixpath` alias pattern is clean and consistent

#### Test Synchronization
- No stale references in tests (only `"testing"` imported)
- No fakes used — pure function tests
- `Close()` is only new public method, one-liner — mock test deferred
- Sibling comparison: localprovider has 0 tests; remoteprovider has 36 (exceeds sibling)
- Full test suite: PASS
- `go vet`: PASS (zero warnings)

#### Spec Divergences (4 documented in plan.md)
1. `posixpath` alias instead of bare `path` (parameter name collision)
2. `readExistingGatewayToken` tests deferred (needs SSH mock)
3. Direct session stdin pipe instead of `AppendFile` helper
4. `filepath.Dir` → `posixpath.Dir` fixes beyond original plan scope

**Status**: CLOSED
