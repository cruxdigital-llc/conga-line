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

- **2026-06-14** — **PR #68 opened + agent code-review pass; 4 findings fixed (commit `84b87a3`);
  fixes live-validated on `zach`.** Reviewer: no blocking; core R2 fail-safe/ordering verified correct.
  Fixed: **#1** add-user/add-team bail with an actionable `conga refresh` message if the network already
  exists (was: no-op static-create → proxy `--ip` exit-125 + removed proxy on re-provision over a legacy
  net); **#2** `ReconcileAgentNetwork` returns `migrated` bool → `RefreshAgent` makes the egress redeploy
  **fatal when the proxy was torn down** (non-fatal on steady-state); **#3** guarded the `pre-start.sh`
  flock fd open under `set -e`; **#4** corrected the `RefreshAll` Ctrl-C comment (next-iteration boundary).
  Reconcile tests assert the `migrated` bool. **Live test (validate on a held agent): `conga refresh
  zach`** migrated `172.22`→**`10.99.2`**, agent `.2`, **proxy `IPAMConfig` pinned `10.99.2.3`** (R1
  proven — collision structurally impossible), agent stayed up through the migration (R2 fail-safe),
  refresh succeeded with the now-fatal post-migration egress redeploy (#2), old-`172.22` rules flushed
  (0 orphans), 5-rule egress + DNS OK, unit enabled+active. `zach` is now migrated + reboot-safe.
  **Remaining rollout:** re-refresh aaron/nathan/congaline-team (migrated *pre-fix* → proxies NOT pinned
  → still reboot-fragile until re-refreshed); migrate nextgen-delivery + nvidia-team (still on
  auto-subnet); then the C5b host-reboot acceptance — all coordinated with the provider release.

- **2026-06-14** — **Provider released + full-fleet rollout + C5b reboot acceptance — feature operationally
  complete.** PR #68 merged to main. **Provider release:** `terraform-provider-conga` go.mod
  conga-line `v0.0.30`→`v0.0.31`, tagged `v0.1.9` (GoReleaser → registry); terraform pin bumped
  `0.1.8`→`0.1.9` in `production/main.tf` + `modules/congaline/main.tf` (PR #69).
  **Staged fleet rollout** (one agent at a time, no `refresh-all --force`): re-refreshed the 3
  pre-fix-migrated agents (aaron/nathan/congaline-team) to pin their proxies; migrated the 2 held
  auto-subnet agents (nextgen-delivery 172.20→`10.99.3`, nvidia-team 172.21→`10.99.5`). End state
  pre-reboot: **all 6 agents on static `10.99.x.2`, every proxy `IPAMConfig`-pinned `.3`.**
  **C5b — PASSED (the acceptance gate):** controlled `systemctl reboot` of prod host
  `i-024bf3a55563f9e88` → the entire fleet returned **completely unattended**: boot time moved
  `2026-06-11`→`2026-06-14 16:30`; **all 6 agents `active`/`running` with `restarts=0`** (R1 proven — no
  proxy-IP collision loop; R4 proven — no `pre-start.sh` thundering-herd timeout), each agent on `.2`,
  each proxy pinned `.3`, egress fail-closed re-applied (30 `10.99.x` rules, **0 `172.x`**, 6 `DROP`-last),
  router back up, DNS OK (user + team). The one-time **`172.x` DOCKER-USER orphan sweep** ran first
  (4 inert pre-migration DNS rules → 0). **Residual (cosmetic, out-of-scope):** `conga-session-metrics.service`
  (a timer-triggered CloudWatch publisher) logged one boot-race failure at `16:31` because it polled
  agent gateways before the flock-serialized fleet finished starting; it is `TriggeredBy` its timer and
  self-heals on the next tick — unrelated to agent reboot-safety. **All four requirements (R1–R4) +
  the umbrella criterion C5b are satisfied on the live fleet. Ready for `/glados:verify-feature`.**

## Verification (`/glados:verify-feature`, 2026-06-14)

### 1. Automated verification — ✅ ALL GREEN
- `go build ./...` OK · `go vet ./...` OK · `gofmt -l` clean · **`go test ./...` ALL PASS** (full-suite regression).
- Per-requirement test functions confirmed present + passing:
  - **R1** (proxy pin): `managedhost` `TestAgentContainer_EgressProxyWiring`; `awsprovider`
    `TestBuildAgentServiceSpec_UnitEquivalence` / `_ReservedKeyGuardWiring`; render assertions for
    `--ip`/`--subnet`/`10.99.0.3`/`IPAMConfig` across `engine_test.go` + `container_test.go`.
  - **R2** (`ReconcileAgentNetwork`): `network_reconcile_test.go` —
    `TestReconcile_{NoOpWhenSubnetMatches,CreateOnlyWhenAbsent,FailSafeAbortOnGhost,HappyPathOrdering}`
    (`migrated` asserted 12×; fail-safe-abort asserts **no** agent stop/rm on a persistent ghost).
  - **R3** (`refresh-all` per-agent timeout): `TestPerAgentRefreshCtx_DecoupledFromParentDeadline`.
  - **R4** (flock + `StartTimeoutSec`): `supervisor_test` (`StartTimeoutSec` ×4, `TimeoutStartSec` ×3);
    `pre-start` flock render assertion; `iptables` suite (DNS `dport 53` ×4, `DROP`-last ×6).
- **Live acceptance (the definitive check): C5b PASSED** on the prod fleet (recorded above) — the
  unit tests cover the seams; the reboot proved the whole behavior end-to-end.

### 2. Persona verification — all APPROVE
- **Architect** — **APPROVE.** R2 lands in `pkg/provider/managedhost` (the engine's transport seam),
  replacing the shell-string migration with tested Go — a direct application of the core philosophy
  ("logic in tested Go behind thin seams, not templated bash"). No new external dep; `ProxyIP`-enforced
  + `ServiceSpec.StartTimeoutSec` are in-memory/infra (no persisted schema, no `Provider`-interface
  change). **Open checkpoint #5 (the add-user/add-team static-subnet consumer concern) is resolved** by
  review #1's bail-on-existing-network. Fits the architecture cleanly.
- **QA** — **APPROVE.** The central promise — unattended reboot survival — is **empirically proven**
  (`restarts=0` across a real reboot, not just unit-mocked). Failure modes covered: fail-safe-abort
  (agent stays up on ghost), happy-path ordering (disconnect→stop→rm→network rm→create, step-verified),
  no-op, create-only. Both spec-gate QA amendments shipped (step-verified COMMIT; bounded `flock -w 240`
  < `TimeoutStartSec 300`). The `migrated` bool → fatal post-migration egress redeploy (review #2) closes
  the proxy-less window. Data-safety: reconcile touches only networks/containers; C5b confirmed agents
  returned **serving** (data mount intact). The `conga-session-metrics` boot-race is cosmetic +
  self-healing + out-of-scope — not an agent-reliability regression.
- **Product Manager** — **APPROVE.** Why/Who delivered (a reboot-trustworthy fleet); scope held tight
  (exactly the 4 defects; Slack `operator.write` deferred as planned). Success is measurable and **met**
  (C5b acceptance). The prioritized sequencing (remediate the 3 reboot-fragile agents first, then migrate
  the held 2, then the disruptive reboot in a controlled step) was honored; the interim
  "avoid-reboot-until-release" risk is now closed.

### 3. Standards Gate (post-implementation) — PASS
| Standard | Severity | Verdict |
|---|---|---|
| Agent Data Safety (architecture.md) | must | ✅ PASSES — reconcile touches networks/containers only; C5b confirmed agents returned serving (data intact) |
| Interface Parity (architecture.md) | must | ✅ PASSES — no new CLI/JSON/MCP surface; R3 internal per-agent default |
| Module Structure / Boundaries (architecture.md) | must | ✅ PASSES — R2 in `pkg/provider/managedhost`; `pkg/`→provider release shipped (v0.1.9) |
| Provider contract / Channel abstraction (architecture.md) | must | ✅ PASSES — AWS-internal; no interface/channel change |
| Egress: iptables all-modes, fail-closed (egress-controls.md) | must | ✅ STRENGTHENED — R1 removes the collision that left an agent crash-looping (= egress never applied); C5b showed 6 `DROP`-last + 0 `172.x` rules |
| Immutable config / perms (security.md P2) | must | ✅ PASSES — root:root 0444 unchanged |
| Secrets via env / #9627 (security.md) | must | ✅ PASSES — `--env-file` unchanged |
| Own the box, not behavior (security.md P8) | must | ✅ PASSES — infra reliability hardening |
| Config taxonomy (config-taxonomy.md) | should | ✅ PASSES — no new persisted config locus |

**Philosophy cross-check:** R2 (Go orchestration replacing the shell-string migration) **applies** the
core philosophy *"logic in tested Go behind thin seams, not templated bash."* No conflicts.
**Gate decision: PASS** — all `must` pass; no violations.

### 4. Spec retrospection + test synchronization
- **Spec alignment:** added **spec.md §15 (Post-implementation reconciliation)** recording the 4 as-built
  divergences from the design (the PR #68 review fixes), notably `ReconcileAgentNetwork`'s signature
  `(...) error` → **`(migrated bool, err error)`**.
- **Stale-reference scan:** clean. Every `agentNetworkMigrationCmd` mention in docs/specs correctly
  describes it as *deleted/replaced*; the only code hit is an intentional `// NOTE:` comment in
  `engine_test.go` documenting the replacement. The symbol itself is gone from `pkg/`. No standards file
  carries a stale code example for the changed symbols.
- **Fake alignment:** the `responder` fake-transport models `RunCommand` outputs; the fail-safe-abort
  test asserts the real prepare-then-commit invariant (no agent teardown on a persistent ghost).
- **New-method coverage:** `ReconcileAgentNetwork`, `RenderSystemdUnit`, `ServiceSpec.StartTimeoutSec`,
  `perAgentRefreshCtx` each have a corresponding test. No gaps vs. the sibling (parent-engine
  `BuildAgentServiceSpec` transport-driven tests).
- **Suite + lint re-run:** green.

### 5. Completion
**Feature COMPLETE.** R1–R4 implemented, unit-verified, merged (PR #68 `84b87a3`), provider-released
(v0.1.9), rolled out fleet-wide, and **C5b live-accepted**. Trace docs committed (PR #70). Status moved to
PROJECT_STATUS "Recent Changes"; ROADMAP updated. Remaining out-of-scope follow-up: the Slack
`operator.write` delegation-scope gap (its own future spec).
