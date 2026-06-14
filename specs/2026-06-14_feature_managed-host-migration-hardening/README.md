# Feature: Managed-Host Migration Hardening (reboot/restart safety)

**Trace Log** — GLaDOS `plan-feature` workflow

- **Created**: 2026-06-14
- **Owner**: Aaron Stone
- **Status**: Planning (requirements + high-level plan)
- **Spec dir**: `specs/2026-06-14_feature_managed-host-migration-hardening/`
- **Parent feature**: `specs/2026-06-13_feature_managed-host-provisioning-engine/` (PR #67, merged `1b41c12`)
- **Origin**: Live-fleet migration of the just-merged managed-host engine surfaced real
  restart/reboot-safety regressions (this session). The migration moved AWS agents from Docker
  auto-subnets to deterministic static `10.99.<idx>.0/24` networks (agent `.2`, proxy `.3` reserved)
  + Go-generated systemd units. Migrating the live fleet — and a Docker daemon restart needed to clear
  a stale endpoint — bounced all 6 agents at once and exposed four defects (below).

## One-line

Make the static-IP managed-host engine **actually unattended-reboot/restart-safe** (criterion 5b of the
parent feature): pin the egress proxy off the agent's static IP, make the network migration robust +
non-destructive-on-failure, fix the `refresh-all` fleet timeout, and stop the `pre-start.sh`
thundering-herd — so a host reboot or a Docker daemon restart brings the whole fleet back cleanly.

## Active Personas

- **Architect** — network/IP determinism, the proxy-IP-pin contract, migration ordering + idempotency,
  ghost/foreign-endpoint handling, engine boundaries.
- **QA** — the central promise: reboot/restart survival. Adversarial restart scenarios (simultaneous
  fleet bounce, host reboot, docker daemon restart), no-collision invariants, partial-failure leaves
  the agent UP (not down), thundering-herd, byte-exact data safety across the bounce.
- **Product Manager** — scope discipline (hardening, not greenfield), live-fleet migration safety,
  sequencing: fix-first vs. finishing the remaining 3 un-migrated agents, and the current exposure of
  the 3 already-migrated agents.

## Active Capabilities

- **conga MCP / AWS SSM** — live fleet introspection + isolated-agent + full-fleet-restart verification
  on the AWS host.
- **GitHub (`gh`)** — PR + the two-repo provider-release flow (`pkg/` change → `terraform-provider-conga`).
- _No browser/UI or DB tools relevant — infra/provisioning hardening._

## The four defects (diagnosed live this session)

| # | Defect | Severity | Where |
|---|---|---|---|
| 1 | **Egress-proxy IP collision** — proxy runs `--restart always` with no `--ip`, so on a simultaneous restart it can grab the agent's static `.2` before the agent → `docker run --ip .2` fails (exit 125) → agent crash-loops. (Was review #6, wrongly downgraded to "advisory" — it's a reboot-survival correctness bug.) | **HIGH** | `deploy-egress.sh.tmpl`, `DeployEgress` (pass `ProxyIP`), `add-user/add-team.sh.tmpl` proxy creation |
| 2 | **Migration not robust + destructive-on-failure** — (a) `docker network rm` fails on a foreign/dangling endpoint (a stale `conga-router` bridge endpoint blocked `congaline-team`; clearing the *persisted* ghost required a fleet-bouncing docker daemon restart); (b) the migration removes the agent container **before** confirming recreate, so a failure leaves the agent **DOWN** instead of running on its old net. | **HIGH** | `agentNetworkMigrationCmd` (`pkg/provider/awsprovider/engine.go`) |
| 3 | **`refresh-all` global timeout** — one `--timeout` (default 5m) is shared across the whole fleet; it died after ~1.5 agents (`context deadline exceeded`). | **MED** | `RefreshAll` / CLI `admin refresh-all` |
| 4 | **`pre-start.sh` thundering herd** — a simultaneous fleet bounce runs N concurrent `aws s3 sync` + `deploy-agents.sh` in `ExecStartPre`, blowing the 120s `TimeoutStartSec` → crash-loop. | **MED** | `pre-start.sh` + the unit's `ExecStartPre`/`TimeoutStartSec` |

## Live-incident facts (ground truth, not assumed)

- Fleet recovered: all 6 agents `active`+`running`. **3 migrated** (aaron 10.99.0.2/proxy .3, nathan
  10.99.4.2/.3, congaline-team 10.99.6.2/.3). **3 held on old auto-subnets** (nextgen-delivery 172.20,
  nvidia-team 172.21, zach 172.22) — reboot-safe (no static IP to collide), deliberately NOT migrated.
- Defect 1 reproduced: post-docker-restart, `conga-egress-aaron` held `10.99.0.2`; aaron `docker run`
  exit 125 crash-loop. Recovered by freeing `.2` + `conga refresh aaron` (proxy then landed on `.3`).
- Defect 2 reproduced: `congaline-team` left DOWN (container removed, network recreate blocked by a
  ghost `conga-router` endpoint that resisted disconnect-by-name/-id + router restart); only a
  `systemctl restart docker` cleared it.
- Defect 4 reproduced: after the docker restart, 5 agents crash-looped on `pre-start.sh` `ExecStartPre`
  120s timeouts until started one-at-a-time (staggered).
- The **3 already-migrated agents are currently reboot-fragile** (defect 1) until the proxy pin lands.

## Key Decisions (this phase)

1. **Tight hardening spec, not greenfield.** Scope is exactly the 4 reboot-safety defects above; the
   fixes are well-understood from the live incident.
2. **Slack `operator.write` delegation-scope gap is OUT of scope** — separate follow-up spec (it's
   gateway-auth + delegation, a different subsystem from reboot-safety). Recorded as a known issue.
3. **Reboot survival = criterion 5b of the parent feature.** The managed-host migration is not truly
   unattended-safe until #1, #2, #4 are fixed; #3 makes the fleet-wide operation usable.

## Files Created

- [requirements.md](./requirements.md)
- [plan.md](./plan.md)

## Session Log

- **2026-06-14** — `/glados:plan-feature`. Feature created from the live-migration incident (parent:
  managed-host-provisioning-engine). Personas: Architect, QA, PM. Slack-scope gap deferred to a
  separate spec. Drafted `requirements.md` + `plan.md`.
- **2026-06-14** — `/glados:spec-feature` complete. Drafted [spec.md](./spec.md). Two implementation
  forks resolved with the operator: **R2 = Go orchestration in `managedhost`** (transport-driven,
  prepare-then-commit, unit-testable — replaces the shell-string `agentNetworkMigrationCmd`); **R4 =
  `flock` serialize + `TimeoutStartSec` 120→300**. R1 also pulls `add-user`/`add-team` into static-subnet
  creation (required so the proxy can pin `.3`). Persona review + standards gate below.

## Spec Review & Standards Gate (pre-implementation)

### Persona Review — all APPROVE (2 QA amendments folded into the spec)

- **Architect** — **APPROVE**. No new external dep; R2 in `pkg/provider/managedhost` matches the
  engine's transport seam; `ServiceSpec.StartTimeoutSec` + `AgentNetwork.ProxyIP`-enforced are
  in-memory/infra (no persisted schema change, no `Provider` interface change). **Flag:** R1 broadens
  into the provision path (add-user/add-team static subnet — parent deferred this); confirm no other
  consumer relies on the auto-subnet and that static-create can't conflict with R2 reconcile (it
  no-ops on match). Tracked as open checkpoint #5.
- **QA** — **APPROVE after 2 amendments (now in spec):** (1) **R2 COMMIT** must verify
  `docker rm -f conga-<name>` succeeded *before* `network rm` (and `network rm` before `create`); on an
  unexpected COMMIT-phase failure, abort + rely on idempotent re-run (the only brief agent-down window,
  bounded to genuine docker faults — the foreign-endpoint case is handled fail-safe in PREPARE). (2)
  **R4** `flock -w 240` bounded wait (below `TimeoutStartSec=300`) so a stuck `pre-start.sh` can't
  deadlock the fleet; proceed on lock-timeout with on-disk behavior. Also confirmed: R3 `WithoutCancel`
  cancellation is open checkpoint #1; C5b includes the data-safety byte-check.
- **Product Manager** — **APPROVE**. Why/Who clear (reboot-trustworthy fleet); scope tight (4 defects,
  Slack deferred); success measurable (C5b reboot acceptance). **Flag:** treat remediation of the 3
  already-migrated (reboot-fragile) agents as the **priority** step immediately post-release; schedule
  the disruptive C5b reboot test in a window; keep the "avoid reboot until release" interim risk
  surfaced (it's in Known Issues).

### Standards Gate Report

| Standard | Severity | Verdict |
|---|---|---|
| Agent Data Safety (architecture.md) | must | ✅ PASSES (§7 — reconcile touches networks/containers only, never the data dir; reboot byte-check test) |
| Interface Parity (architecture.md) | must | ✅ PASSES (§8 — no new CLI/JSON/MCP surface; R3 internal per-agent default) |
| Module Structure / Package Boundaries (architecture.md) | must | ✅ PASSES (R2 in `pkg/provider/managedhost`; `pkg/` → provider release noted) |
| Provider contract / Channel abstraction (architecture.md) | must | ✅ PASSES (AWS-internal; no interface/channel change) |
| Egress: iptables all-modes, fail-closed (egress-controls.md) | must | ✅ STRENGTHENED (R1 removes the collision that left an agent crash-looping = egress never applied; rule set unchanged) |
| Immutable config / perms (security.md P2) | must | ✅ PASSES (root:root 0444 unchanged) |
| Secrets via env / #9627 (security.md) | must | ✅ PASSES (`--env-file` unchanged) |
| Own the box, not behavior (security.md P8) | must | ✅ PASSES (infra reliability hardening) |
| Config taxonomy (config-taxonomy.md) | should | ✅ PASSES (no new persisted config locus) |

**Philosophy cross-check:** R2 (Go orchestration replacing the shell-string migration) **aligns** with
the core philosophy *"Logic in tested Go behind thin seams, not templated bash"*
(`observations/observed-philosophies.md`, 2026-06-13) — it's a direct application. No philosophy conflicts.

**Gate decision: PASS** — all `must` standards pass; no violations; no core-philosophy conflict. Two
non-blocking flags logged (add-user/add-team static-subnet consumer check; schedule the C5b reboot
window). Ready for `/glados:implement-feature`.

## Session Log (implement phase)

- **2026-06-14** — `/glados:implement-feature` started (branch `plan/managed-host-migration-hardening`).
  Drafted `tasks.md` (order R1→R2→R4→R3; live-verify release-gated). **R1 implemented, code-complete +
  unit-verified.** Pinned the egress proxy to its reserved `ProxyIP` (`.3`) at all three creation sites
  (`deploy-egress.sh.tmpl`, `add-user.sh.tmpl`, `add-team.sh.tmpl`), which required `add-user`/`add-team`
  to create the per-agent network on its **static subnet** (`--subnet/--gateway`) so the proxy can bind
  `.3`. `DeployEgress` now resolves the agent → `PlanAgentNetwork` → threads `ProxyIP`; `ProvisionAgent`
  threads `SubnetCIDR`/`GatewayIP`/`ProxyIP` into `provisionData`; `network.go` `ProxyIP` doc is now
  "enforced" with the reboot-collision rationale. Tests: all six render-test structs in `scripts_test.go`
  gained the fields + new assertions (proxy `--ip` pin at all sites; static-subnet create). build/vet/
  gofmt + `go test ./...` green. T1.8 live verify + the 3-agent remediation are **release-gated**
  (post `terraform-provider-conga` release). **Next: R2** (the robust `ReconcileAgentNetwork` Go
  orchestration — the core).
- **2026-06-14** — **R2, R4, R3 implemented; all four requirements code-complete + unit-verified.**
  - **R2** (the core): new `managedhost/network_reconcile.go` `ReconcileAgentNetwork` — prepare-then-commit,
    transport-driven. Clears foreign/dangling endpoints (the stale `conga-router` bridge that downed
    `congaline-team`) BEFORE touching the agent; aborts with an actionable error if a ghost persists,
    leaving the agent **running on its old net** (fail-safe). Step-verified COMMIT. Wired into
    `defineAndStartAgentService`; deleted the shell-string `agentNetworkMigrationCmd`. New
    `network_reconcile_test.go` (fake-transport `responder`): no-op / create-only / **fail-safe-abort
    (no agent stop/rm on ghost)** / happy-path ordering.
  - **R4**: `pre-start.sh.tmpl` bounded `flock -w 240` around the S3 sync; new
    `ServiceSpec.StartTimeoutSec` → `RenderUnit` (engine sets 300). Tests for the flock + the timeout.
  - **R3**: `RefreshAll` per-agent `perAgentRefreshCtx` (deadline-free parent, 6m each) + explicit
    operator-cancel guard. Decoupling test added.
  - build/vet/gofmt + `go test ./...` all green. **Live verify (T1.8 + R2/R3/R4 throwaway + the C5b
    reboot) and the 3-agent remediation are release-gated** (post `terraform-provider-conga` release),
    per the rollout (§11). Implementation complete; ready for the provider release + staged rollout +
    `/glados:verify-feature`.
