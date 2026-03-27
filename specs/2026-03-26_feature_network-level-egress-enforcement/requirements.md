# Requirements: Network-Level Egress Enforcement

## Problem Statement

Egress proxy enforcement currently relies on `HTTP_PROXY`/`HTTPS_PROXY` environment variables, which are advisory. During a demo, the OpenClaw agent fetched disney.com despite it not being in the egress allowlist. The proxy was working (curl via proxy returned 403), but the application bypassed it by making direct connections. Node.js's built-in `fetch()`, Playwright/Chromium, and spawned child processes do not reliably honor proxy environment variables.

A previous attempt using `--internal` Docker networks ran into DNS resolution failures — the egress proxy was only on the internal network and couldn't resolve or reach external hosts.

## Goal

Agent containers must be **physically unable** to reach any external IP address except through the tinyproxy egress proxy. Enforcement must be at the Docker network level, not dependent on application behavior.

## Success Criteria

1. **Network isolation**: An agent container on an `--internal` Docker network cannot route to any IP outside the bridge subnet, regardless of whether `HTTPS_PROXY` is set or honored.
2. **Allowed traffic passes**: Requests to whitelisted domains (e.g., `api.anthropic.com`, `*.slack.com`) succeed via the proxy.
3. **Blocked traffic fails**: Requests to non-whitelisted domains fail with a connection/network error (not a proxy 403 — the container literally cannot reach the domain).
4. **Bypass-proof**: `curl --noproxy '*' https://disney.com` inside the agent container fails with "network unreachable" or equivalent.
5. **DNS resolution works**: The egress proxy can resolve external hostnames. Agent containers can resolve the proxy's container name via Docker DNS.
6. **Gateway access preserved**: The web gateway remains accessible from localhost despite the agent being on an internal network.
7. **Router connectivity**: The Slack event router can still deliver webhook events to agent containers on internal networks.
8. **All providers**: Remote, local, and AWS providers all enforce egress at the network level when policy is active.
9. **Graceful degradation**: When no egress policy is defined or no domains are listed, networks remain standard bridge (no enforcement, no proxy, same as today).
10. **Clean lifecycle**: socat/forwarder processes are cleaned up on agent removal, pause, and teardown. No orphaned processes or networks.

## Non-Goals

- Logging/alerting on blocked bypass attempts (future work)
- Custom seccomp profiles to further restrict raw socket access
- Per-agent DNS filtering (DNS resolution is allowed; routing is blocked)

## Constraints

- **macOS Docker Desktop**: Container IPs are not routable from the Mac host. `-p` port publishing does not work with `--internal` networks. Gateway forwarding needs a container-based approach.
- **Linux (remote/AWS)**: The host can route to container IPs on Docker bridges. `socat` on the host can forward to internal-network containers.
- **Docker `--internal` semantics**: Removes the default gateway from the container's routing table. Does NOT prevent the host's network namespace from reaching the bridge subnet. Docker's embedded DNS (127.0.0.11) still works for container name resolution.
- **Dual-homed proxy**: The egress proxy must be on both the internal agent network (to receive requests) and an external network (to forward allowed requests). A single-network proxy cannot resolve or reach external hosts.
