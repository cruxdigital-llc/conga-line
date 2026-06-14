# High-Level Plan — Managed-Host Migration Hardening

- **Created**: 2026-06-14
- **Status**: Planning (high-level; detailed design in `spec.md` via `/glados:spec-feature`)

## Approach (per requirement)

### R1 — Pin the egress proxy to `.3`
- `managedhost.PlanAgentNetwork` already reserves `ProxyIP` (`.3`). Thread it to every proxy-creation
  site and add `--ip <ProxyIP>` to the proxy `docker run`:
  - `DeployEgress` (Go) resolves the agent's `GatewayPort` → `PlanAgentNetwork` → passes `ProxyIP`
    (+ subnet/gateway) into `deploy-egress.sh.tmpl`; the proxy `docker run` gains `--ip {{.ProxyIP}}`.
  - `add-user.sh.tmpl` / `add-team.sh.tmpl` proxy creation: same `--ip`, fed from `ProvisionAgent`.
- Net invariant: agent always `--ip .2`, proxy always `--ip .3` — no race, no auto-assignment.
- **Remediation of the 3 already-migrated agents:** a single `conga refresh` per agent re-creates the
  proxy with the pin (the engine already starts the agent first → `.2`, then deploy-egress recreates
  the proxy → now explicitly `.3`). Low blast radius (one agent at a time).

### R2 — Robust, fail-safe network migration (`agentNetworkMigrationCmd`)
- **R2a:** before `docker network rm`, enumerate the old network's attached containers; force-disconnect
  any that are **not** this agent (notably `conga-router`, which is `--network host` now so its bridge
  endpoints are dead weight). Handle the dangling-endpoint case (disconnect-by-name → by-endpoint-id →
  surface a clear error if still stuck). The persisted-ghost case (only cleared by a daemon restart)
  must be detected and reported as an actionable error, **not** silently leave the agent down.
- **R2b:** reorder the migration so it is *prepare-then-commit*: verify the desired network can be
  created (or is already correct) **before** removing the agent container. On any failure prior to a
  confirmed recreate, leave the existing container/network intact and the agent running. Only when
  recreate is guaranteed do we stop+remove+recreate+restart. This turns "left down" into "left running
  on old net, refresh-retryable."
- Design choice for the spec: do the disconnect/cleanup in the Go-built migration command string, or
  promote the migration to a small Go orchestration in `managedhost` (with the transport) for
  testability — lean toward the latter so it's unit-testable with the fake transport.

### R3 — Per-agent timeout in `refresh-all`
- `RefreshAll` should give each `RefreshAgent` its own deadline (e.g. a per-agent `context.WithTimeout`
  derived from a per-agent budget) rather than inheriting one fleet-wide `--timeout`. Keep the existing
  per-agent error collection + final summary. Confirm the SSM-level timeouts (the engine's 120s
  `ssmTransport`) remain the inner bound.

### R4 — `pre-start.sh` thundering-herd resilience
- Options to evaluate in the spec (pick the simplest that holds under a cold reboot of N agents):
  1. **Host-side serialization** — a lightweight lock (e.g. `flock` on a shared lockfile) around the
     `aws s3 sync` so concurrent `ExecStartPre`s queue instead of contending.
  2. **Resilient sync** — bounded retry/back-off + a larger `TimeoutStartSec` (and/or
     `TimeoutStartSec=infinity` with a guard) so a slow-but-progressing sync isn't killed.
  3. **Stagger** — systemd-level (e.g. randomized `ExecStartPre` jitter) so N agents don't all hit S3
     at the same instant.
- Likely combination: `flock` serialization + a modest `TimeoutStartSec` bump. Must remain
  unattended-correct (no manual staggering) on a cold reboot.

## Sequencing & rollout (PM)

1. **Land the fixes first (this spec → PR).** Do NOT migrate the remaining 3 agents until R1+R2(+R4)
   are merged + provider-released — migrating now would only add more reboot-fragile agents.
2. **Remediate the 3 already-migrated agents** (R1 pin) — one `conga refresh` each, post-release.
3. **Migrate the remaining 3** (nextgen-delivery, nvidia-team, zach) individually with the hardened
   migration (R2) + per-agent refresh, then verify each (agent `.2`, proxy `.3`, healthy).
4. **Prove C5b** — once all 6 are migrated + pinned, do a controlled `reboot` of the host and confirm
   the whole fleet returns unattended (the acceptance test for the whole feature).

## Risk

- **Highest**: R2 reorder touches the production migration path (already bit us once). Must be
  unit-tested (fake transport) for the prepare-then-commit ordering + the foreign-endpoint cleanup, and
  isolated-agent live-verified before fleet use.
- **Medium**: R4 — `TimeoutStartSec`/`flock` interacts with systemd start semantics; verify under a
  real simultaneous N-agent start, not just one agent.
- **Low**: R1 (additive `--ip`), R3 (timeout plumbing).

## Testing strategy (QA, high-level)

- **Unit**: proxy `docker run` carries `--ip .3` (all 3 creation sites); migration command/orchestration
  force-disconnects foreign endpoints + is prepare-then-commit (fake transport injects a blocked
  `network rm` → assert the agent is left running, not removed); `refresh-all` per-agent timeout.
- **Integration / live (isolated)**: migrate a throwaway with a deliberately-attached foreign endpoint
  → completes or fails-safe; kill+restart docker daemon → throwaway returns with agent `.2`/proxy `.3`,
  no collision.
- **Acceptance (C5b)**: full-fleet `reboot` → all agents `active`, correct IPs, egress re-applied, no
  manual intervention. Data dir byte-identical across the reboot.

## Provider release

`pkg/` changes (awsprovider engine + managedhost + possibly RefreshAll) → tag congaline →
`terraform-provider-conga` release before the deployed path uses them. Batch R1–R4 into one release.

## Next

`/glados:spec-feature` — detailed design: the migration prepare-then-commit algorithm + foreign/ghost
endpoint handling, the `ProxyIP` threading across the 3 sites, the `refresh-all` timeout shape, the
chosen `pre-start.sh` resilience mechanism, and the C5b acceptance protocol + per-item tests.
