# Plan: Network-Level Egress Enforcement

## Architecture

### Core Mechanism

Docker's `--internal` flag on bridge networks removes the default gateway and adds firewall rules that "drop all traffic to or from other networks." However, per the Docker docs, "the host may communicate with any container IP directly." This gives us:

- **Agent container**: trapped on the internal network. Cannot initiate connections to any external IP.
- **Egress proxy**: dual-homed (internal + external network). Receives proxy requests from the agent, forwards allowed ones to the internet.
- **Gateway access**: forwarded from localhost to the container IP via socat (Linux) or a forwarder container (macOS).

```
Linux Host (Remote / AWS)
├── socat: 127.0.0.1:18789 → agent-ip:18789
│
├── conga-{agent} network [--internal]
│   ├── conga-{agent}          (agent — no outbound route)
│   ├── conga-egress-{agent}   (proxy — also on conga-egress-ext)
│   └── conga-router           (also on default bridge)
│
├── conga-egress-ext network [standard bridge]
│   └── conga-egress-{agent}   (proxy's internet-facing interface)
│
└── default bridge
    └── conga-router            (Slack Socket Mode access)
```

```
macOS Host (Local Provider)
│
├── conga-{agent} network [--internal]
│   ├── conga-{agent}          (agent — no outbound route)
│   ├── conga-egress-{agent}   (proxy — also on conga-egress-ext)
│   ├── conga-router           (also on default bridge)
│   └── conga-fwd-{agent}      (forwarder — also on bridge with -p)
│
├── conga-egress-ext network [standard bridge]
│   └── conga-egress-{agent}   (proxy's internet-facing interface)
│
└── default bridge
    ├── conga-router
    └── conga-fwd-{agent}       (-p 127.0.0.1:port:port)
```

### Why This Solves the DNS Issue

The previous attempt failed because the egress proxy was only on the internal network — it couldn't resolve external hostnames or connect to external IPs. By dual-homing the proxy on both the internal agent network and the external `conga-egress-ext` network, tinyproxy has full DNS resolution and external connectivity on its second interface.

### When Enforcement Applies

- **Egress enforced** (domains defined in policy): `--internal` network, proxy, socat/forwarder
- **No policy or no domains**: Standard bridge network, no proxy, no forwarder — same as today
- **HTTPS_PROXY env vars**: Still set when enforcing (defense in depth — guides well-behaved apps to the proxy immediately, avoiding "network unreachable" errors)

---

## Implementation Phases

### Phase 1: Remote Provider (demo-critical)

#### 1a. `createNetwork` supports `--internal`

**File**: `cli/pkg/provider/remoteprovider/docker.go`

Add `internal bool` parameter:
```go
func (p *RemoteProvider) createNetwork(ctx context.Context, name string, internal bool) error {
    args := "docker network create " + shellQuote(name) + " --driver bridge"
    if internal {
        args += " --internal"
    }
    _, err := p.ssh.Run(ctx, args)
    return err
}
```

Update all call sites in `provider.go` (`ProvisionAgent`, `RefreshAgent`, `startAgentEgressProxy`).

#### 1b. Shared external network for proxies

**File**: `cli/pkg/provider/remoteprovider/provider.go`

Add constant and ensure it exists:
```go
const egressExtNetwork = "conga-egress-ext"
```

Create in `Setup()` and check before each proxy start. Standard bridge (NOT internal).

#### 1c. Dual-home the egress proxy

**File**: `cli/pkg/provider/remoteprovider/provider.go` — `startAgentEgressProxy`

After starting the proxy on the agent's internal network, connect to external network:
```go
p.connectNetwork(ctx, egressExtNetwork, proxyName)
```

#### 1d. Drop `-p` and add socat forwarding

**File**: `cli/pkg/provider/remoteprovider/docker.go`

- `runAgentContainer`: omit `-p` when `EgressEnforce` is true
- Add `startPortForwarder(agentName, port)` — gets container IP via `docker inspect`, starts socat on the host
- Add `stopPortForwarder(agentName)` — kills socat via PID file

```go
func (p *RemoteProvider) startPortForwarder(ctx context.Context, agentName string, port int) error {
    cName := containerName(agentName)
    netName := networkName(agentName)
    tpl := "{{(index .NetworkSettings.Networks \"%s\").IPAddress}}"
    ip, err := p.dockerRun(ctx, "inspect", "-f", fmt.Sprintf(tpl, netName), cName)
    // ...
    pidFile := fmt.Sprintf("/run/conga-fwd-%s.pid", cName)
    cmd := fmt.Sprintf(
        "nohup socat TCP-LISTEN:%d,bind=127.0.0.1,fork,reuseaddr TCP:%s:%d </dev/null >/dev/null 2>&1 & echo $! > %s",
        port, strings.TrimSpace(ip), port, pidFile)
    _, err = p.ssh.Run(ctx, cmd)
    return err
}
```

#### 1e. Network recreation during RefreshAgent

**File**: `cli/pkg/provider/remoteprovider/provider.go`

When refreshing, the network may need to change type (standard → internal or vice versa). The existing flow already stops the container and proxy. Add:
1. Disconnect router from network
2. Delete network
3. Recreate with correct `--internal` flag

#### 1f. Install socat during setup

**File**: `cli/pkg/provider/remoteprovider/setup.go`

Add `socat` to the Docker install script alongside `docker.io`.

#### 1g. Cleanup

**File**: `cli/pkg/provider/remoteprovider/provider.go`

- `RemoveAgent`: call `stopPortForwarder` before removing container/network
- `PauseAgent`: call `stopPortForwarder`
- `Teardown` / `cleanupDockerByPrefix`: `pkill -f 'socat.*conga' || true; rm -f /run/conga-fwd-*.pid`

---

### Phase 2: Local Provider (macOS)

#### 2a. `createNetwork` supports `--internal`

**File**: `cli/pkg/provider/localprovider/docker.go`

Same pattern as remote — add `internal bool` parameter.

#### 2b. Shared external network + dual-homed proxy

**File**: `cli/pkg/provider/localprovider/provider.go`

Same `conga-egress-ext` constant. Create during setup. Connect proxy after starting.

#### 2c. Forwarder container instead of socat

**File**: `cli/pkg/provider/localprovider/docker.go`

macOS Docker Desktop can't route to container IPs from the Mac host, so socat on the host won't work. Instead, use a lightweight forwarder container:

```go
func startPortForwarder(ctx context.Context, agentName string, port int) error {
    fwdName := "conga-fwd-" + agentName
    // Start on default bridge with -p, then connect to internal network
    args := []string{"run", "-d",
        "--name", fwdName,
        "--cap-drop", "ALL",
        "--security-opt", "no-new-privileges",
        "--memory", "32m",
        "--read-only",
        "-p", fmt.Sprintf("127.0.0.1:%d:%d", port, port),
        egressProxyImage, // alpine with socat
        "socat", fmt.Sprintf("TCP-LISTEN:%d,fork,reuseaddr", port),
        fmt.Sprintf("TCP:%s:%d", containerName(agentName), port),
    }
    dockerRun(ctx, args...)
    // Connect forwarder to agent's internal network so it can reach the agent
    connectNetwork(ctx, networkName(agentName), fwdName)
}
```

The egress proxy image needs `socat` added: update `EgressProxyDockerfile()` to install both `tinyproxy` and `socat`.

#### 2d. Drop `-p` from agent container, lifecycle cleanup

Same pattern as remote: omit `-p` when enforcing, clean up forwarder in `RemoveAgent`, `PauseAgent`, `Teardown`.

---

### Phase 3: AWS Provider

#### 3a. Bootstrap script

**File**: `terraform/user-data.sh.tftpl`

- `generate_network()` function adds `--internal` when egress is enforced
- `start_agent()` omits `-p`, starts socat forwarder (same as remote — Linux host)
- `conga-egress-ext` network created during bootstrap
- Proxy connected to both networks
- socat PID files for systemd cleanup

#### 3b. Refresh scripts

**Files**: `cli/scripts/refresh-user.sh.tmpl`, `refresh-all.sh.tmpl`

- Stop socat before container restart
- Restart socat after container start with new IP

#### 3c. Systemd integration

- ExecStopPost: kill socat forwarder
- ExecStartPost: start socat forwarder after container is up

---

## Shared Changes

### Egress proxy image

**File**: `cli/pkg/policy/egress.go`

Add `socat` to the Dockerfile for the forwarder container (used by local provider):
```go
func EgressProxyDockerfile() string {
    return "FROM " + EgressProxyBaseImage + "\nRUN apk add --no-cache tinyproxy socat >/dev/null 2>&1\n"
}
```

### Egress external network helpers

**File**: `cli/pkg/policy/egress.go`

Add constant:
```go
const EgressExtNetwork = "conga-egress-ext"
```

---

## Files to Modify

| File | Phase | Changes |
|------|-------|---------|
| `cli/pkg/policy/egress.go` | 1 | Add `socat` to Dockerfile, add `EgressExtNetwork` constant |
| `cli/pkg/provider/remoteprovider/docker.go` | 1 | `createNetwork` internal flag, `runAgentContainer` drops `-p`, `startPortForwarder`/`stopPortForwarder` |
| `cli/pkg/provider/remoteprovider/provider.go` | 1 | All `createNetwork` call sites, `RefreshAgent` recreates network, proxy dual-homing, cleanup in Remove/Pause/Teardown |
| `cli/pkg/provider/remoteprovider/setup.go` | 1 | Add `socat` to install script |
| `cli/pkg/provider/localprovider/docker.go` | 2 | `createNetwork` internal flag, `runAgentContainer` drops `-p`, forwarder container helpers |
| `cli/pkg/provider/localprovider/provider.go` | 2 | Same patterns as remote, forwarder lifecycle |
| `terraform/user-data.sh.tftpl` | 3 | Network creation, socat, dual-homed proxy |
| `cli/scripts/refresh-user.sh.tmpl` | 3 | socat lifecycle in refresh |

---

## Architect Review

**Q: Does this introduce a new dependency?**
A: `socat` on Linux hosts (installed during setup). On macOS, no new host dependency — socat runs inside the existing egress proxy image.

**Q: How does this affect existing data models?**
A: No data model changes. `agentContainerOpts` gains no new fields — `EgressEnforce` already exists and controls the new behavior.

**Q: Is this pattern consistent with the rest of the codebase?**
A: Yes. The provider abstraction is preserved — remote uses host socat, local uses forwarder containers. Both use the same `--internal` + dual-homed proxy core. The `createNetwork` signature change mirrors the existing `runAgentContainer` conditional pattern.

**Q: What about the router?**
A: The router starts on the default bridge (internet access for Slack), then gets connected to each agent's network. When agent networks are `--internal`, the router is multi-homed: default bridge for Slack Socket Mode, internal networks for webhook delivery. This already works — no changes needed to router startup.

## QA Review

**Q: What happens if the container restarts and gets a new IP?**
A: On Linux, socat points to a specific IP — if the container gets a new IP, the old socat is stale. `RefreshAgent` stops old socat and starts new one with the fresh IP. On macOS, the forwarder uses Docker DNS (`TCP:conga-{agent}:port`), so IP changes are transparent.

**Q: What if socat crashes?**
A: The gateway becomes unreachable. `conga status` will show the agent running but the gateway won't respond. `conga admin refresh` restarts socat. Could add socat restart logic or health checking as future improvement.

**Q: What about DNS resolution inside the agent container?**
A: Docker's embedded DNS (127.0.0.11) still works on `--internal` networks. The agent can resolve hostnames — it just can't connect to the resolved IPs. This is correct behavior: `HTTPS_PROXY` env var tells Node.js to CONNECT through the proxy, so the agent sends the hostname to the proxy (which resolves it on the external network).

**Q: What if someone removes the egress policy while containers are running?**
A: Next `RefreshAgent` or `policy deploy` will recreate the network as standard bridge, remove the proxy, remove the socat/forwarder, and restart the container with `-p`. Graceful transition in both directions.

**Q: Network recreation during refresh — is there a window where the agent is unreachable?**
A: Yes, briefly. The container is stopped, network is recreated, container is restarted. This is the same window as today's refresh. The socat/forwarder starts after the container, adding a few hundred milliseconds.

---

## Verification Plan

### Automated (per phase)

1. `go build` succeeds
2. `go vet ./...` passes
3. Existing tests pass

### Manual — Remote Provider (Phase 1)

1. Set egress policy: `conga_policy_set_egress` with `api.anthropic.com`, `*.slack.com`, `*.slack-edge.com`, mode `enforce`
2. Deploy: `conga_policy_deploy`
3. Verify network is internal: `conga_container_exec` → check agent has no default route
4. Verify proxy is dual-homed: `docker network inspect conga-egress-ext` includes proxy
5. Verify gateway works: `conga_connect_help` → SSH tunnel → browser loads web UI
6. Verify allowed domain: agent responds to a question (Anthropic API reachable)
7. Verify blocked domain: agent fails to fetch disney.com
8. Verify bypass-proof: `docker exec conga-{agent} curl --noproxy '*' https://disney.com` → fails
9. Verify Slack works: DM the bot, get a response
10. Remove policy, redeploy: network becomes standard bridge, `-p` restored, gateway works

### Manual — Local Provider (Phase 2)

Same checks 1-8 on macOS Docker Desktop, verifying forwarder container exists and `-p` is on the forwarder, not the agent.

### Manual — AWS Provider (Phase 3)

Same checks 1-9, verified via SSM session on EC2 instance.
