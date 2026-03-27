# Specification: Network-Level Egress Enforcement

## Overview

Replace advisory `HTTPS_PROXY` env var enforcement with Docker `--internal` networks that physically prevent agent containers from reaching external IPs. The egress proxy is dual-homed across internal and external networks, acting as the sole internet gateway for each agent.

---

## Phase 1: Remote Provider

### 1.1 Shared Constants

**File**: `cli/internal/policy/egress.go`

```go
// EgressExtNetwork is the shared bridge network that gives egress proxy containers
// internet access. Standard (not internal) bridge.
const EgressExtNetwork = "conga-egress-ext"
```

Update `EgressProxyDockerfile()` to include `socat` (needed by local provider forwarder):

```go
func EgressProxyDockerfile() string {
    return "FROM " + EgressProxyBaseImage + "\nRUN apk add --no-cache tinyproxy socat >/dev/null 2>&1\n"
}
```

### 1.2 Network Creation with `--internal`

**File**: `cli/internal/provider/remoteprovider/docker.go`

Change `createNetwork` signature:

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

### 1.3 Agent Container: Drop `-p` When Enforcing

**File**: `cli/internal/provider/remoteprovider/docker.go`

In `runAgentContainer`, conditionally omit `-p`:

```go
if !opts.EgressEnforce {
    args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d", opts.GatewayPort, opts.GatewayPort))
}
```

When `-p` is omitted, the container is only reachable via Docker DNS on the internal network. Gateway access is provided by socat on the host (see 1.4).

### 1.4 Port Forwarding via socat

**File**: `cli/internal/provider/remoteprovider/docker.go`

New helpers:

```go
// startPortForwarder starts a socat process on the remote host that forwards
// localhost:port to the agent container's IP on the internal network.
// The host can always route to container IPs on Docker bridges (including
// --internal ones) because --internal only restricts the container's routing
// table, not the host's.
func (p *RemoteProvider) startPortForwarder(ctx context.Context, agentName string, port int) error {
    cName := containerName(agentName)
    netName := networkName(agentName)

    // Get container IP on the internal network
    tpl := fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", netName)
    output, err := p.dockerRun(ctx, "inspect", "-f", tpl, cName)
    if err != nil {
        return fmt.Errorf("getting container IP: %w", err)
    }
    ip := strings.TrimSpace(output)
    if ip == "" {
        return fmt.Errorf("container %s has no IP on network %s", cName, netName)
    }

    pidFile := portForwarderPidFile(cName)
    cmd := fmt.Sprintf(
        "nohup socat TCP-LISTEN:%d,bind=127.0.0.1,fork,reuseaddr TCP:%s:%d </dev/null >/dev/null 2>&1 & echo $! > %s",
        port, ip, port, pidFile)
    _, err = p.ssh.Run(ctx, cmd)
    return err
}

// stopPortForwarder kills the socat process for an agent's gateway forwarder.
func (p *RemoteProvider) stopPortForwarder(ctx context.Context, agentName string) {
    pidFile := portForwarderPidFile(containerName(agentName))
    p.ssh.Run(ctx, fmt.Sprintf(
        "if [ -f %s ]; then kill $(cat %s) 2>/dev/null; rm -f %s; fi",
        pidFile, pidFile, pidFile))
}

func portForwarderPidFile(containerName string) string {
    return fmt.Sprintf("/run/conga-fwd-%s.pid", containerName)
}
```

### 1.5 Dual-Homed Egress Proxy

**File**: `cli/internal/provider/remoteprovider/provider.go` — `startAgentEgressProxy`

After starting the proxy container on the agent's internal network, connect it to the external network:

```go
// Ensure external network exists
if !p.networkExists(ctx, policy.EgressExtNetwork) {
    if err := p.createNetwork(ctx, policy.EgressExtNetwork, false); err != nil {
        return fmt.Errorf("creating egress external network: %w", err)
    }
}

// ... existing docker run for proxy on agent network ...

// Connect proxy to external network for DNS resolution and internet access.
// Without this, the proxy is trapped on the --internal network and cannot
// resolve or reach external hosts (this was the bug in the previous attempt).
if err := p.connectNetwork(ctx, policy.EgressExtNetwork, proxyName); err != nil {
    return fmt.Errorf("connecting proxy to external network: %w", err)
}
```

### 1.6 Network Lifecycle in RefreshAgent

**File**: `cli/internal/provider/remoteprovider/provider.go` — `RefreshAgent`

After stopping the container and proxy, recreate the network with the correct flag:

```go
// Stop port forwarder
p.stopPortForwarder(ctx, agentName)

// Recreate network with correct --internal flag.
// The network type may need to change (standard → internal or vice versa)
// when the egress policy changes.
netName := networkName(agentName)
if p.networkExists(ctx, netName) {
    if p.containerExists(ctx, routerContainer) {
        p.disconnectNetwork(ctx, netName, routerContainer)
    }
    if p.containerExists(ctx, egressProxyContainer) {
        p.disconnectNetwork(ctx, netName, egressProxyContainer)
    }
    p.removeNetwork(ctx, netName)
}
p.createNetwork(ctx, netName, egressEnforce)
```

After starting the agent container (when enforcing):

```go
// Start gateway port forwarder (replaces -p on internal networks)
if egressEnforce {
    if err := p.startPortForwarder(ctx, agentName, cfg.GatewayPort); err != nil {
        fmt.Fprintf(os.Stderr, "Warning: failed to start port forwarder: %v\n", err)
    }
}
```

### 1.7 Network Lifecycle in ProvisionAgent

**File**: `cli/internal/provider/remoteprovider/provider.go` — `ProvisionAgent`

Same pattern: `createNetwork(ctx, netName, egressEnforce)` and start port forwarder after container startup when enforcing.

### 1.8 Install socat During Setup

**File**: `cli/internal/provider/remoteprovider/setup.go`

In the `installDocker()` shell script, add `socat` alongside `docker.io`:

```bash
apt-get install -y -qq docker.io socat >/dev/null
# dnf:
dnf install -y -q docker socat
# yum:
yum install -y -q docker socat
# pacman:
pacman -S --noconfirm docker socat >/dev/null
```

### 1.9 Cleanup

**File**: `cli/internal/provider/remoteprovider/provider.go`

**RemoveAgent**: Add before container/network removal:
```go
p.stopPortForwarder(ctx, name)
```

**PauseAgent**: Add after stopping the container:
```go
p.stopPortForwarder(ctx, name)
```

**Teardown / cleanupDockerByPrefix**: Add socat cleanup:
```go
p.ssh.Run(ctx, "pkill -f 'socat.*conga' 2>/dev/null || true")
p.ssh.Run(ctx, "rm -f /run/conga-fwd-conga-*.pid")
```

Also disconnect containers from `conga-egress-ext` and remove that network:
```go
if p.networkExists(ctx, policy.EgressExtNetwork) {
    p.removeNetwork(ctx, policy.EgressExtNetwork)
}
```

### 1.10 Call Site Updates for `createNetwork`

All existing calls to `p.createNetwork(ctx, name)` must become `p.createNetwork(ctx, name, internal)`:

| Location | `internal` value |
|----------|-----------------|
| `ProvisionAgent` (line ~232) | `egressEnforce` |
| `RefreshAgent` (line ~521) | `egressEnforce` |
| `startAgentEgressProxy` (line ~896) | `true` (always internal when proxy is starting) |
| `ensureEgressProxy` (shared, line ~826) | `false` (shared proxy needs external access) |

Wait — `startAgentEgressProxy` currently calls `createNetwork` as a fallback. Since the caller (`ProvisionAgent` / `RefreshAgent`) already creates the network, this call should match the caller's intent. Pass `egressEnforce` (which is always `true` when this function is called).

---

## Phase 2: Local Provider

### 2.1 Network Creation with `--internal`

**File**: `cli/internal/provider/localprovider/docker.go`

```go
func createNetwork(ctx context.Context, name string, internal bool) error {
    args := []string{"network", "create", name, "--driver", "bridge"}
    if internal {
        args = append(args, "--internal")
    }
    _, err := dockerRun(ctx, args...)
    return err
}
```

### 2.2 Agent Container: Drop `-p` When Enforcing

Same conditional as remote in `runAgentContainer`.

### 2.3 Forwarder Container (macOS Gateway Access)

**File**: `cli/internal/provider/localprovider/docker.go`

On macOS Docker Desktop, the host cannot route to container IPs (they're inside a Linux VM). Use a lightweight forwarder container:

```go
const forwarderPrefix = "conga-fwd-"

func forwarderName(agentName string) string {
    return forwarderPrefix + agentName
}

// startPortForwarder starts a socat container that forwards gateway traffic
// from a published port to the agent container on the internal network.
// The forwarder is started on the default bridge (with -p) then connected
// to the agent's internal network so it can reach the agent by container name.
func startPortForwarder(ctx context.Context, agentName string, port int) error {
    fwdName := forwarderName(agentName)
    target := containerName(agentName)

    if containerExists(ctx, fwdName) {
        removeContainer(ctx, fwdName)
    }

    args := []string{"run", "-d",
        "--name", fwdName,
        "--cap-drop", "ALL",
        "--security-opt", "no-new-privileges",
        "--memory", "32m",
        "--read-only",
        "-p", fmt.Sprintf("127.0.0.1:%d:%d", port, port),
        policy.EgressProxyImage,
        "socat",
        fmt.Sprintf("TCP-LISTEN:%d,fork,reuseaddr", port),
        fmt.Sprintf("TCP:%s:%d", target, port),
    }

    if _, err := dockerRun(ctx, args...); err != nil {
        return fmt.Errorf("starting port forwarder: %w", err)
    }

    // Connect to agent's internal network so socat can resolve the agent hostname
    return connectNetwork(ctx, networkName(agentName), fwdName)
}

func stopPortForwarder(ctx context.Context, agentName string) {
    fwdName := forwarderName(agentName)
    if containerExists(ctx, fwdName) {
        removeContainer(ctx, fwdName)
    }
}
```

The forwarder uses Docker DNS to resolve `conga-{agent}` on the internal network, making it resilient to container IP changes.

### 2.4 Dual-Homed Proxy + External Network

Same pattern as remote: ensure `conga-egress-ext` exists, connect proxy to it after startup.

### 2.5 Call Site Updates

Same as remote: all `createNetwork` calls gain `internal` parameter. Port forwarder started/stopped alongside container lifecycle.

### 2.6 Cleanup in Teardown

In `cleanupDockerByPrefix`: the `conga-` prefix already catches `conga-fwd-*` containers and `conga-egress-ext` network. No special handling needed beyond what exists.

---

## Phase 3: AWS Provider

### 3.1 Bootstrap Script

**File**: `terraform/user-data.sh.tftpl`

In the network creation section, add `--internal` when egress is enforced:

```bash
create_agent_network() {
    local name="$1"
    local internal="$2"  # "true" or "false"
    local args="docker network create ${name} --driver bridge"
    if [ "$internal" = "true" ]; then
        args+=" --internal"
    fi
    eval $args
}
```

In the agent startup section:
- Omit `-p` when egress is enforced
- Start socat after container startup (same as remote provider)
- Create `conga-egress-ext` network and dual-home the proxy

### 3.2 Refresh Scripts

**Files**: `cli/scripts/refresh-user.sh.tmpl`, `refresh-all.sh.tmpl`

Add socat stop/start around container restart.

### 3.3 Systemd Integration

Add to agent systemd units:
- `ExecStopPost`: kill socat forwarder
- `ExecStartPost`: start socat forwarder (get fresh container IP)

---

## Edge Cases

### DNS Resolution

| Scenario | Behavior |
|----------|----------|
| Agent resolves `conga-egress-{agent}` | Works — Docker DNS (127.0.0.11) on internal network |
| Agent resolves `api.anthropic.com` | Works — Docker DNS forwards to host DNS. But connection fails (no route). |
| Proxy resolves `api.anthropic.com` | Works — proxy is on external network with full DNS + routing |
| Agent resolves non-existent domain | DNS NXDOMAIN — same as today |

### Container Restart

| Scenario | Behavior |
|----------|----------|
| Agent container restarts (remote) | socat becomes stale (points to old IP). `RefreshAgent` restarts socat. |
| Agent container restarts (local) | Forwarder uses Docker DNS — transparent. No action needed. |
| Proxy container restarts | Agent's HTTPS_PROXY still points to correct hostname. Reconnects. |
| Docker daemon restarts | All containers restart. socat PIDs are stale. `RefreshAll` cleans up. |

### Policy Transitions

| Transition | Network Change | Action |
|-----------|---------------|--------|
| No policy → enforce | standard → `--internal` | RefreshAgent recreates network, starts proxy + forwarder |
| Enforce → no policy | `--internal` → standard | RefreshAgent recreates network, removes proxy + forwarder, restores `-p` |
| Enforce → validate (local only) | `--internal` → standard | Same as above |
| Change allowed domains | No network change | Proxy config regenerated, proxy restarted |

### Failure Modes

| Failure | Impact | Recovery |
|---------|--------|----------|
| socat crashes (remote) | Gateway unreachable via SSH tunnel | `conga admin refresh {agent}` restarts socat |
| Forwarder container crashes (local) | Gateway unreachable from localhost | `conga admin refresh {agent}` restarts forwarder |
| External network deleted | Proxy loses internet, all requests fail | `conga admin refresh {agent}` recreates network |
| `--internal` network deleted while running | Container loses network. All communication stops. | `conga admin refresh {agent}` recreates everything |

### Security Boundary

| Attack Vector | With `--internal` | Without (current) |
|--------------|-------------------|-------------------|
| `curl https://disney.com` | Fails — no route to host | Succeeds (bypasses proxy) |
| `curl --noproxy '*' https://disney.com` | Fails — no route to host | Succeeds |
| Raw TCP socket to external IP | Fails — no route to host | Succeeds |
| DNS resolution of external domain | Succeeds but connection fails | Succeeds + connection succeeds |
| Connect to proxy on port 3128 | Succeeds (allowed — same network) | Succeeds |
| Connect to another agent's container | Fails — different network | Fails — different network |

---

## Standards Compliance

The security standards document (`product-knowledge/standards/security.md`) explicitly lists "Cooperative proxy enforcement" as a Medium-severity accepted residual risk with the note: "`--internal` Docker networks (blocks all direct egress) are the upgrade path." This feature implements that upgrade path and should update the risk entry to reflect the new enforcement level.

After implementation, update the security standards:
- Move "Cooperative proxy enforcement" from Accepted Residual Risks to the Universal Baseline table
- Update the Egress Policy section to reflect network-level enforcement
- Update the Enforcement Escalation table to replace "Squid" with "tinyproxy" and note `--internal` networks
