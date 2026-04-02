# Spec — Non-Root Container Enforcement

## Summary

Add explicit `--user 1000:1000` to all agent and router `docker run` commands across all three providers (local, remote, AWS). This makes non-root enforcement independent of upstream image `USER` directives, closing a gap where the router runs as root today and agent containers could silently regress.

## Current State

| Container | Local | Remote | AWS | Actual UID |
|---|---|---|---|---|
| Agent | No `--user` | No `--user` | No `--user` | 1000 (image default, fragile) |
| Router | No `--user` | No `--user` | No `--user` | **0 (root)** |
| Egress proxy | `--user 101:101` | `--user 101:101` | `--user 101:101` | 101 (correct) |

## Target State

| Container | All Providers | Actual UID |
|---|---|---|
| Agent | `--user 1000:1000` | 1000 (explicit) |
| Router | `--user 1000:1000` | 1000 (explicit) |
| Egress proxy | `--user 101:101` (unchanged) | 101 (unchanged) |

## Data Safety

This change does not affect agent data. The `--user` flag controls the process UID inside the container. Data directories are already `chown -R 1000:1000` before container start in all three providers. Volume mounts (`-v /path:/home/node/.openclaw:rw`) are unchanged. No data directory creation, deletion, or ownership changes are introduced.

## Changes

### 1. Agent Container — Local Provider

**File**: `cli/pkg/provider/localprovider/docker.go`
**Function**: `runAgentContainer()` (line 76)
**Change**: Insert `"--user", "1000:1000"` into the args slice after `"--pids-limit", "256"`.

```go
// Before:
"--pids-limit", "256",
"-v", fmt.Sprintf("%s:/home/node/.openclaw:rw", opts.DataDir),

// After:
"--pids-limit", "256",
"--user", "1000:1000",
"-v", fmt.Sprintf("%s:/home/node/.openclaw:rw", opts.DataDir),
```

### 2. Agent Container — Remote Provider

**File**: `cli/pkg/provider/remoteprovider/docker.go`
**Function**: `runAgentContainer()` (line 76)
**Change**: Identical to local provider — insert `"--user", "1000:1000"` after `"--pids-limit", "256"`.

```go
// Before:
"--pids-limit", "256",
"-v", fmt.Sprintf("%s:/home/node/.openclaw:rw", opts.DataDir),

// After:
"--pids-limit", "256",
"--user", "1000:1000",
"-v", fmt.Sprintf("%s:/home/node/.openclaw:rw", opts.DataDir),
```

### 3. Agent Container — AWS Bootstrap

**File**: `terraform/user-data.sh.tftpl`
**Location**: Agent systemd `ExecStart` (line 893)
**Change**: Add `--user 1000:1000` after `--pids-limit 256`.

```bash
# Before:
ExecStart=/usr/bin/docker run --name conga-$AGENT_NAME ... --pids-limit 256 -v /opt/conga/data/...

# After:
ExecStart=/usr/bin/docker run --name conga-$AGENT_NAME ... --pids-limit 256 --user 1000:1000 -v /opt/conga/data/...
```

### 4. Agent Container — AWS add-user Script

**File**: `cli/scripts/add-user.sh.tmpl`
**Location**: ExecStart line (line 191)
**Change**: Add `--user 1000:1000` after `--pids-limit 256`.

### 5. Agent Container — AWS add-team Script

**File**: `cli/scripts/add-team.sh.tmpl`
**Location**: ExecStart line (line 190)
**Change**: Add `--user 1000:1000` after `--pids-limit 256`.

### 6. Agent Container — AWS refresh-user Script

**File**: `cli/scripts/refresh-user.sh.tmpl`
**Location**: sed replacement pattern (line 62)
**Change**: Add `--user 1000:1000` after `--pids-limit 256` in the replacement string.

### 7. Router Container — Local Provider

**File**: `cli/pkg/provider/localprovider/docker.go`
**Function**: `runRouterContainer()` (line 128)
**Change**: Insert `"--user", "1000:1000"` after `"--tmpfs", "/tmp:rw,noexec,nosuid"`.

```go
// Before:
"--tmpfs", "/tmp:rw,noexec,nosuid",
"-v", fmt.Sprintf("%s:/app:ro", opts.RouterDir),

// After:
"--tmpfs", "/tmp:rw,noexec,nosuid",
"--user", "1000:1000",
"-v", fmt.Sprintf("%s:/app:ro", opts.RouterDir),
```

### 8. Router Container — Remote Provider

**File**: `cli/pkg/provider/remoteprovider/docker.go`
**Function**: `runRouterContainer()` (line 124)
**Change**: Identical to local provider.

### 9. Router Container — AWS Bootstrap

**File**: `terraform/user-data.sh.tftpl`
**Location**: Router systemd `ExecStart` (line 219)
**Change**: Add `--user 1000:1000 --tmpfs /tmp:rw,noexec,nosuid` after `--read-only`.

The `--tmpfs` is currently missing from the AWS router (present in local/remote). This is an alignment fix.

```bash
# Before:
ExecStart=/usr/bin/docker run --name conga-router --cap-drop ALL --security-opt no-new-privileges --memory 128m --cpus 0.25 --pids-limit 64 --read-only -v /opt/conga/router:/app:ro ...

# After:
ExecStart=/usr/bin/docker run --name conga-router --cap-drop ALL --security-opt no-new-privileges --memory 128m --cpus 0.25 --pids-limit 64 --read-only --user 1000:1000 --tmpfs /tmp:rw,noexec,nosuid -v /opt/conga/router:/app:ro ...
```

### 10. Security Documentation

**File**: `product-knowledge/standards/security.md`
**Location**: Universal Baseline table, "Non-root container" row (line 33)
**Change**: Update description from "Runs as uid 1000 (`node`)" to "Explicit `--user 1000:1000` on all containers".

## Edge Cases

### macOS Docker Desktop (Local Provider)
The `chown -R 1000:1000` on the data directory is best-effort on macOS (uid 1000 doesn't exist on the host). Docker Desktop handles UID mapping via its VM layer. Adding `--user 1000:1000` to the container is independent of host UID resolution — Docker passes it to the Linux kernel inside the VM. No behavioral change.

### Existing Running Containers
Adding `--user` only affects new containers created after the code change. Existing containers continue with their current user until restarted or refreshed via `conga admin refresh-all`. No disruptive migration.

### Node.js on Non-Root
Node.js does not require root to run. The router binds to no privileged ports (listens on stdin from Slack Socket Mode, writes to stdout). The gateway listens on port 18789 (>1024). No capability needed.

### node:22-alpine uid 1000
The `node:22-alpine` image includes `node` user at uid 1000 in `/etc/passwd`. Running `--user 1000:1000` maps to this existing user. No "unknown user" warnings.

## Verification

1. **Compilation**: `cd cli && go build ./...`
2. **Unit tests**: `cd cli && go test ./...`
3. **Local smoke test**:
   - `conga admin teardown && conga admin setup --provider local`
   - `conga admin add-user --agent testuser --json '{"agent_name":"testuser"}'`
   - `docker inspect --format '{{.Config.User}}' conga-testuser` -> `1000:1000`
   - `docker inspect --format '{{.Config.User}}' conga-router` -> `1000:1000`
   - `conga status --agent testuser` -> running
   - `conga connect --agent testuser` -> gateway accessible
4. **MCP verification**: `conga_get_status` shows agents running and healthy
