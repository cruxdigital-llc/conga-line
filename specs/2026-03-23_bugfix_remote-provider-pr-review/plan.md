# Fix Plan — Remote Provider PR #15 Review Findings

## Context

PR #15 adds `remoteprovider` (SSH-based deployment to any Linux host). Code review found 16 issues. This plan addresses all of them in a single pass, grouped by file.

## Fixes by File

### 1. `ssh.go`
| # | Finding | Fix |
|---|---------|-----|
| 3 | `InsecureIgnoreHostKey` silent fallback | Add `fmt.Fprintf(os.Stderr, "WARNING: ...")` before returning insecure callback |
| 6 | Stale `// Package vpsprovider` doc | Change to `// Package remoteprovider` |
| 14 | `isSafeArg` allows `=` with no comment | Add comment: `// '=' needed for env vars like NODE_OPTIONS=--max-old-space-size=1536` |

### 2. `config.go`
| # | Finding | Fix |
|---|---------|-----|
| 7 | `// VPS-specific` comment | Change to `// Remote provider (SSH)`, update inline comments |

### 3. `provider.go`
| # | Finding | Fix |
|---|---------|-----|
| 1 | `filepath.Join` for remote paths | Replace with `path.Join` for all remote paths (add `"path"` import) |
| 8 | `RefreshAgent` swallows errors silently | Add comment explaining why (matches localprovider pattern) |
| 13 | No `Close()` on `RemoteProvider` | Add `Close()` method that calls `p.ssh.Close()`, add TODO for CLI framework integration |

### 4. `secrets.go`
| # | Finding | Fix |
|---|---------|-----|
| 1 | `filepath.Join` for remote paths | Replace with `path.Join` for all remote paths |

### 5. `integrity.go`
| # | Finding | Fix |
|---|---------|-----|
| 1 | `filepath.Join` for remote paths | Replace with `path.Join` for all remote paths |
| 4 | Shell injection in log append | Replace `echo >> ` with stdin-piped `cat >>` (like `uploadViaShell`) |
| 9 | `joinLines` reimplements `strings.Join` | Delete function, use `strings.Join(logLines, "\n")` |
| 10 | Double `fmt.Sprintf` wrapping | Simplify to `strings.Join(logLines, "\n") + "\n"` |

### 6. `setup.go`
| # | Finding | Fix |
|---|---------|-----|
| 1 | `filepath.Join` for remote paths | Replace with `path.Join` for all remote paths |
| 2 | `SSHKeyPath` not persisted | Add `SSHKeyPath: keyPath` to saved `provider.Config` struct; capture from prompt scope |
| 5 | Docker install without confirmation | Add `ui.Confirm("Docker not found. Install it on the remote host?")` gate |

### 7. `docker.go`
No changes needed — all arguments are container names and Docker flags, no remote filesystem paths.

### 8. New: `provider_test.go`
| # | Finding | Fix |
|---|---------|-----|
| 11 | Missing test coverage | Add tests for `detectReadyPhase` (6 cases) and `readExistingGatewayToken` (4 cases) |

## Deferred Items

| # | Finding | Reason |
|---|---------|--------|
| 12 | SFTP client caching | Adds API complexity + thread safety concerns; add TODO comment in `ssh.go` |
| 15 | Branch name `feature/vps-ssh-support` | Can't rename after PR is open; note in PR comment |
| 16 | `CycleHost` parallelization | Optimization, not a bug; out of scope for this fix |

## `filepath.Join` → `path.Join` Audit

Remote paths (change to `path.Join`):
- `p.remoteDir` and all sub-paths (`remoteAgentsDir`, `remoteConfigDir`, `remoteDataSubDir`, etc.)
- `p.sharedSecretsDir()`, `p.agentSecretsDir()`
- All `filepath.Join` calls in `integrity.go` (all paths are remote)
- Secret paths in `secrets.go`

Local paths (keep `filepath.Join`):
- `p.dataDir` references (local `~/.conga/`)
- `repoPath` and `behaviorDir` in `setup.go` / `provider.go`
- `os.Stat` targets
- `detectRepoRoot()` in `setup.go`
- `keyFileSigner()` and `hostKeyVerifier()` in `ssh.go`

## Verification

```bash
cd cli
go build ./...          # compiles
go vet ./...            # no warnings
go test ./...           # all tests pass (existing 29 + new ~10)
```

Then grep to confirm no stale references:
```bash
grep -rn 'filepath\.Join' cli/pkg/provider/remoteprovider/ | grep -v '_test.go'
# Remaining hits should only be local-path operations (dataDir, repoPath, os.Stat targets)

grep -rn -i 'vps' cli/pkg/provider/remoteprovider/
# Should return zero hits
```

## Implementation Divergences

1. **`posixpath` alias instead of `path`** — `ssh.go` has a function parameter named `path`, so `import posixpath "path"` was used everywhere for consistency.
2. **`readExistingGatewayToken` tests deferred** — requires SSHClient mock; only `detectReadyPhase` tested (7 cases, pure function).
3. **No `AppendFile` helper** — integrity log append uses direct SSH session with stdin pipe instead, avoiding new API surface.
4. **`filepath.Dir` → `posixpath.Dir`** — additional fix not in original plan; `Upload()`, `uploadViaShell()`, and `SetSecret()` also used `filepath.Dir` on remote paths.
