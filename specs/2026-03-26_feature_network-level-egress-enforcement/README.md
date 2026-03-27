# Feature Trace: Network-Level Egress Enforcement

**Created**: 2026-03-26
**Status**: Planning
**Lead**: Architect + QA

## Active Personas
- **Architect** — validates Docker network architecture, dual-homed proxy design, cross-provider consistency
- **QA** — edge cases around DNS resolution, container restarts, socat reliability, cleanup, and bypass resistance

## Active Capabilities
- **Conga MCP Server** — live testing against remote VPS (72.60.70.39)
- **SSH** — direct host inspection for verification
- **Docker** — local provider testing on macOS

## Trace Log

| Timestamp | Phase | Decision / Event |
|-----------|-------|-----------------|
| 2026-03-26 | Init | Feature created from demo failure: agent bypassed HTTPS_PROXY to fetch disney.com |
| 2026-03-26 | Requirements | Goal: block all non-proxy egress at network level. Scope: all providers. |
| 2026-03-26 | Planning | Personas selected: Architect + QA |
| 2026-03-26 | Requirements | Created requirements.md — goal: block all non-proxy egress, all providers |
| 2026-03-26 | Plan | Created plan.md — 3-phase approach: remote (socat), local (forwarder container), AWS (socat + systemd) |
| 2026-03-26 | Plan | Architect review: no new data models, consistent with provider abstraction, router needs no changes |
| 2026-03-26 | Plan | QA review: identified socat crash risk, IP change handling, graceful policy removal transition |
| 2026-03-26 | Spec | Created spec.md — 3-phase implementation, edge cases, security boundary analysis |
| 2026-03-26 | Spec Review | **Architect**: Approved — provider contract preserved, shared logic correct, router unchanged |
| 2026-03-26 | Spec Review | **QA**: Approved with note — add socat availability check before starting forwarder |
| 2026-03-26 | Standards Gate | See report below |
| 2026-03-26 | Implementation | Phase 1 (remote): all 10 tasks complete, builds clean, vet passes |
| 2026-03-26 | Implementation | Phase 2 (local): all 8 tasks complete, builds clean, all tests pass (1 pre-existing failure in tools_env.go) |

## Files Modified

| File | Changes |
|------|---------|
| `cli/internal/policy/egress.go` | Added `EgressExtNetwork` constant, `socat` in Dockerfile |
| `cli/internal/provider/remoteprovider/docker.go` | `createNetwork` internal flag, `-p` conditional, `startPortForwarder`/`stopPortForwarder` |
| `cli/internal/provider/remoteprovider/provider.go` | All createNetwork call sites, RefreshAgent network recreation, proxy dual-homing, cleanup in Remove/Pause/Teardown |
| `cli/internal/provider/remoteprovider/setup.go` | `socat` added to Docker install script |
| `cli/internal/provider/localprovider/docker.go` | `createNetwork` internal flag, `-p` conditional, forwarder container helpers |
| `cli/internal/provider/localprovider/provider.go` | Same patterns as remote, forwarder lifecycle, cleanup |

## Standards Gate Report (Pre-Implementation)

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Provider contract is API boundary | architecture | must | ✅ PASSES — Provider interface unchanged, changes are internal to provider packages |
| Shared logic in common/policy | architecture | must | ✅ PASSES — EgressExtNetwork constant in policy package, not provider |
| No enforcement without policy | architecture | must | ✅ PASSES — --internal only when domains defined, standard bridge otherwise |
| Channel-agnostic policy | architecture | should | ✅ PASSES — No channel-specific changes |
| Zero trust the AI agent | security | must | ✅ PASSES — Network-level enforcement doesn't trust application behavior |
| Defense in depth | security | must | ✅ PASSES — Keeps HTTPS_PROXY env vars alongside network enforcement |
| Least privilege | security | must | ✅ PASSES — Forwarder: cap-drop ALL, 32MB, read-only. Proxy: same. |
| Immutable configuration | security | should | ✅ PASSES — Proxy config remains read-only mounts |
| Cooperative proxy enforcement (residual risk) | security | must | ✅ RESOLVES — This feature eliminates this accepted residual risk |
| Config format boundary | architecture | should | ✅ PASSES — No new config files |

**Gate Decision**: ✅ All checks pass. No violations. Proceed to implementation.
