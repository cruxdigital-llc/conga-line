# Requirements — Managed-Host Migration Hardening

- **Created**: 2026-06-14
- **Owner**: <operator>
- **Parent**: `specs/2026-06-13_feature_managed-host-provisioning-engine/` (PR #67, merged)
- **Status**: Planning

## Goal

The merged managed-host engine migrates AWS agents to deterministic static `10.99.<idx>.0/24`
networks + Go-generated systemd units. A live-fleet migration this session proved the engine works
per-agent — but is **not yet safe under a whole-fleet restart** (host reboot, Docker daemon restart,
or `refresh-all`). This feature closes that gap so the migration delivers its core promise:
**unattended reboot/restart survival (parent criterion 5b)** for every agent, and a fleet-wide
operation (`refresh-all`, reboot) that brings all agents back cleanly with no operator intervention.

Scope is exactly the four defects diagnosed live (below). It is a tight hardening pass, not new
capability.

## Functional requirements

### R1 — Egress proxy must never collide with the agent's static IP (HIGH)
- The per-agent Envoy egress proxy MUST bind the **reserved** `AgentNetwork.ProxyIP` (`.3`) via
  `docker run --ip`, not an auto-assigned address.
- **Why**: the proxy runs `--restart always`. On a simultaneous restart (host reboot / docker daemon
  restart), Docker brings the proxy up before the systemd-managed agent; with no `--ip` the proxy
  grabs the lowest free address — the agent's `.2` — and the agent's `docker run --ip .2` then fails
  (exit 125) → crash-loop. Observed live on `user-a`.
- Applies everywhere the proxy is created: `deploy-egress.sh.tmpl`, `add-user.sh.tmpl`,
  `add-team.sh.tmpl`, with `DeployEgress` / provision passing the planned `ProxyIP`.
- `AgentNetwork.ProxyIP` becomes an **enforced** assignment (upgrade from the "advisory" note added in
  the PR #67 review).
- **Success**: on any restart ordering, the agent always holds `.2` and the proxy always holds `.3`;
  no agent enters a `docker run --ip` (125) collision loop.

### R2 — Network migration must be robust and non-destructive on failure (HIGH)
- **R2a (clean foreign/dangling endpoints):** before `docker network rm`, the migration MUST
  force-disconnect any non-agent container still attached to the old network (e.g. a stale
  `conga-router` bridge endpoint), and tolerate/clear a *dangling* endpoint so `network rm` succeeds
  without a Docker daemon restart. If a ghost endpoint cannot be cleared, the migration MUST fail with
  a clear, actionable error **without having taken the agent down** (see R2b).
- **R2b (ordering — fail safe, not fail down):** the agent container MUST NOT be removed until the new
  network is confirmed creatable/created. A migration failure MUST leave the agent **running on its
  existing network**, never stopped-and-unstartable (observed live: `team-b` left DOWN because
  the container was removed before the blocked `network rm`).
- **Why**: the live migration left `team-b` down and required a fleet-bouncing
  `systemctl restart docker` to clear a persisted ghost endpoint.
- **Success**: a migration blocked by a foreign/dangling endpoint either (a) clears it and completes,
  or (b) aborts cleanly with the agent still serving on its old net; no path leaves an agent down, and
  no path requires a Docker daemon restart.

### R3 — `refresh-all` must apply a per-agent timeout (MED)
- `conga admin refresh-all` MUST bound each agent's refresh independently, not share one global
  `--timeout` across the whole fleet.
- **Why**: the default 5m global timeout was exhausted after ~1.5 agents (`context deadline exceeded`
  on the rest), so most of the fleet was never processed.
- **Success**: `refresh-all` processes every agent regardless of fleet size; a slow/failed agent does
  not starve the others; per-agent failures are collected and reported (current behavior) but with the
  full fleet attempted.

### R4 — `pre-start.sh` must survive a simultaneous fleet start (MED)
- A simultaneous fleet bounce (reboot / docker daemon restart) MUST NOT crash-loop agents via
  `ExecStartPre` timeouts.
- **Why**: N agents each running `aws s3 sync` + `deploy-agents.sh` concurrently in `ExecStartPre`
  exceeded the 120s `TimeoutStartSec` → repeated `start-pre operation timed out` → crash-loop until
  started one-at-a-time.
- Resolution options (decide in spec): host-side serialization/lock around the S3 sync, a more
  generous/﻿bounded `TimeoutStartSec`, retry/back-off, and/or making the sync resilient to contention.
- **Success**: a cold whole-fleet start (e.g. `reboot`) brings every agent to `active` with no manual
  staggering and no `ExecStartPre` timeout loop.

## Cross-cutting success criterion (the umbrella)

**C5b — Unattended whole-fleet reboot survival.** On the AWS host, `reboot` (and independently, a
`systemctl restart docker`) MUST bring **all** agents back to `active`+`running` with: agent on `.2`,
proxy on `.3`, egress iptables re-applied, no collision loop, no thundering-herd timeout, and **no
operator intervention**. This is the parent feature's criterion 5b; this feature is what makes it true.

## Data-safety requirement (architecture.md, must)

- None of these fixes may delete/recreate `/opt/conga/data/<name>/` contents. The migration's
  reordering (R2b) and proxy pin (R1) touch only networks/containers/units, never the data mount.
- **Test**: data dir byte-identical across a full reboot + a migration-failure-then-recover cycle.

## Constraints

- `pkg/` changes (`awsprovider`, `managedhost`, possibly shared helpers) → **`terraform-provider-conga`
  release** required before the deployed Terraform path uses them (two-repo coupling).
- Must remediate the **3 already-migrated agents** (user-a, user-c, team-b), which are
  reboot-fragile under R1 today, without a disruptive full-fleet bounce where avoidable.
- Egress security posture unchanged: iptables in all modes, fail-closed, root:root `0444` on managed
  files, secrets-via-`--env-file`.

## Non-goals (out of scope)

- **Slack `operator.write` delegation-scope gap** (`sessions_spawn` blocked from channel sessions) —
  separate follow-up spec (gateway-auth + delegation subsystem, not reboot-safety). Recorded as a
  known issue.
- Migrating the remaining 3 agents is a *rollout step gated on these fixes*, not a deliverable of the
  spec itself.
- No provider merge, no OpenRC, no boot-script reduction (parent slice 5), no new user-facing surface.
