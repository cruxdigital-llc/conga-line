# Technical Specification — Managed-Host Migration Hardening

- **Created**: 2026-06-14
- **Owner**: Aaron Stone
- **Status**: Specified (pre-implementation)
- **Builds on**: `requirements.md`, `plan.md` (read first). Parent: `specs/2026-06-13_feature_managed-host-provisioning-engine/` (merged PR #67, `1b41c12`).
- **Scope**: the four restart/reboot-safety defects (R1–R4) diagnosed during the live fleet migration. Umbrella acceptance = **C5b (unattended whole-fleet reboot survival)**.

---

## 1. Summary

The merged managed-host engine gives each AWS agent a deterministic static `10.99.<idx>.0/24` network
(agent `.2`, proxy `.3` reserved) + a Go-generated systemd unit. It works per-agent but is not safe
under a **whole-fleet bounce** (host reboot / Docker daemon restart / `refresh-all`). This spec closes
four gaps so a cold fleet start returns every agent unattended:

- **R1** — pin the egress proxy to the reserved `.3` so it can't grab the agent's `.2`.
- **R2** — make the network migration **robust** (clear foreign/dangling endpoints) and **fail-safe**
  (a failure leaves the agent running on its old net, never down), implemented as a testable Go
  orchestration in `managedhost` (replacing the shell-string `agentNetworkMigrationCmd`).
- **R3** — give `refresh-all` a **per-agent** timeout instead of one fleet-wide deadline.
- **R4** — make `pre-start.sh` survive a simultaneous start via `flock` serialization + a
  `TimeoutStartSec` bump.

No new user-facing surface. Slack `operator.write` delegation gap is **out of scope** (separate spec).

---

## 2. R1 — Pin the egress proxy to `ProxyIP` (.3)

**Defect:** the proxy runs `--restart always` with no `--ip`. On a simultaneous restart the Docker
daemon brings the proxy up before the systemd-managed agent; the proxy auto-assigns the lowest free
address (`.2`), and the agent's `docker run --ip .2` then fails (exit 125) → crash-loop. Observed on
`aaron`.

**Design — pin the proxy at every creation site; make `ProxyIP` an enforced assignment.**

| Site | Change |
|---|---|
| `pkg/provider/managedhost/network.go` | `AgentNetwork.ProxyIP` doc flips from "advisory" → **enforced** ("the proxy is pinned via `docker run --ip` so it never collides with the agent's `.2` on a simultaneous restart"). |
| `scripts/deploy-egress.sh.tmpl` | proxy `docker run` gains `--ip {{.ProxyIP}}` (line ~81-83). **Primary fix** — deploy-egress recreates the proxy on every refresh / `policy deploy`, on the already-static `10.99.x` network. |
| `pkg/provider/awsprovider/provider.go` `DeployEgress` | resolve the agent (`discovery.ResolveAgent` → `GatewayPort`) → `managedhost.PlanAgentNetwork(port, common.BaseGatewayPort)` → add `ProxyIP` to the template struct. |
| `scripts/add-user.sh.tmpl` / `add-team.sh.tmpl` | (a) create the per-agent network with the **static** `--subnet {{.SubnetCIDR}} --gateway {{.GatewayIP}}` (not auto), (b) proxy `docker run` gains `--ip {{.ProxyIP}}`. |
| `pkg/provider/awsprovider/provider.go` `ProvisionAgent` | `provisionData` gains `SubnetCIDR`, `GatewayIP`, `ProxyIP` from `PlanAgentNetwork(cfg.GatewayPort, …)`. |

**Why add-user/add-team also get the static subnet:** pinning the proxy to `.3` requires the network to
already be the `10.99.x` subnet at proxy-creation time. Creating it static at provision also removes the
first-provision migration churn (R2's reconcile then sees subnet == desired → no-op). This pulls
`add-user`/`add-team` into the static-subnet world that the parent feature deferred — a net
simplification.

**Invariant (testable):** every proxy `docker run` carries `--ip <ProxyIP>` (`.3`); the agent's
`--ip` is `.2`; the two never coincide under any restart ordering.

**Remediation of the 3 already-migrated agents (aaron, nathan, congaline-team):** one
`conga refresh <agent>` each post-release re-creates the proxy with the pin (engine starts the agent →
`.2`, then deploy-egress recreates the proxy → now explicitly `.3`). Until then they are reboot-fragile
(known issue; avoid a host reboot).

---

## 3. R2 — Robust, fail-safe network migration (Go orchestration)

**Defect:** `agentNetworkMigrationCmd` (one shell string) (a) can't remove the old network when a
**foreign/dangling** endpoint is attached (a stale `conga-router` bridge endpoint blocked
`congaline-team`; the persisted ghost needed a fleet-bouncing `docker` daemon restart), and (b) removes
the agent container **before** the network is recreatable, so a blocked `network rm` left the agent
**DOWN** and unstartable.

**Design — replace the shell string with a Go orchestration in `managedhost`, transport-driven and
unit-testable.** New (illustrative) contract:

```go
// pkg/provider/managedhost/network_reconcile.go
//
// ReconcileAgentNetwork ensures conga-<name> exists on net.SubnetCIDR. It is
// PREPARE-THEN-COMMIT: all potentially-blocking work (clearing foreign/dangling
// endpoints) happens BEFORE the agent container is touched, so any failure leaves
// the agent running on its existing network (never stopped-and-unstartable).
func ReconcileAgentNetwork(ctx context.Context, t Transport, name string, net AgentNetwork) error
```

**Algorithm:**

1. **Inspect.** `docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' conga-<name>`.
   If it already equals `net.SubnetCIDR` → **return nil** (no-op; steady-state refresh, no churn, no egress gap).
2. **PREPARE (non-destructive; abort here leaves the agent running):**
   - Enumerate containers attached to the old network. **Force-disconnect every *foreign* endpoint** —
     anything that is **not** `conga-<name>` and **not** `conga-egress-<name>` (notably `conga-router`,
     which is `--network host` now, so its bridge endpoints are dead weight). Disconnect by name, then
     by endpoint ID for a dangling record.
   - If a foreign endpoint **cannot** be cleared (persisted ghost — only a `docker` daemon restart would
     fix it), **return an actionable error WITHOUT having touched the agent**: e.g. *"network conga-<name>
     has an unclearable endpoint <id> (persisted Docker ghost); left agent running on its old subnet —
     clear with `systemctl restart docker` in a maintenance window, then re-run `conga refresh <name>`."*
   - Flush stale `DOCKER-USER` rules keyed on the old agent IP (the existing flush logic), so the
     auto-subnet→static migration doesn't orphan rules.
3. **COMMIT (blockers already cleared, so `network rm` will succeed):**
   - `systemctl stop conga-<name>` → `docker rm -f conga-<name>` (removes the agent's own endpoint) →
     `docker rm -f conga-egress-<name>` (removes the proxy's endpoint) → `docker network rm conga-<name>`
     → `docker network create --driver bridge --subnet net.SubnetCIDR --gateway net.GatewayIP conga-<name>`.
   - **Verify each destructive step (QA):** assert `docker rm -f conga-<name>` succeeded *before*
     `docker network rm`, and assert `network rm` succeeded *before* `network create`. The endpoints
     left after the two `rm`s are only the agent's + proxy's (both now gone) + any foreign endpoint —
     which PREPARE already cleared — so `network rm` succeeds in the normal case. If `network rm`
     *unexpectedly* fails here (a foreign endpoint reappeared, or a transient docker error), **abort
     with the error**: at this point the agent is stopped (the one COMMIT window where it is briefly
     down), but `ReconcileAgentNetwork` is **idempotent and re-runnable** — a re-run (or the next
     `conga refresh`) completes once docker is healthy. This residual window is bounded to genuine
     docker-daemon faults, not the foreign-endpoint case PREPARE handles.
   - The agent + proxy are re-created downstream: `defineAndStartAgentService` restarts the agent
     (`--ip .2`); `DeployEgress` recreates the proxy (`--ip .3`, R1).

**Why this is fail-safe:** the only thing that can block `network rm` is a *foreign* endpoint, and those
are cleared (or the run aborts) in PREPARE — *before* the agent is stopped. The agent's own endpoint is
never a blocker (it's removed with the container in COMMIT). So a ghost-endpoint case now aborts with
the agent **still serving**, not down (the `congaline-team` failure mode is impossible).

**Wiring:** `defineAndStartAgentService` calls `managedhost.ReconcileAgentNetwork(ctx, t, agent.Name,
net)` in place of `t.RunCommand(agentNetworkMigrationCmd(...))`. On error it returns immediately
(before `DefineService`/`Restart`), leaving the agent on its old unit/container. `agentNetworkMigrationCmd`
is deleted.

**Idempotency / data safety:** never touches `/opt/conga/data/<name>/`. A re-run after an abort is
clean (PREPARE is idempotent; COMMIT only runs once blockers are clear).

---

## 4. R3 — Per-agent timeout in `refresh-all`

**Defect:** `RefreshAll` loops `p.RefreshAgent(ctx, a.Name)` with the **shared** `ctx`, which carries
the CLI's global `--timeout` (default 5m) as a single fleet-wide deadline → exhausted after ~1.5 agents
(`context deadline exceeded` on the rest).

**Design:** in `RefreshAll`, give each agent its own deadline derived from a **deadline-free** copy of
the parent context:

```go
for _, a := range activeAgents {
    aCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), perAgentRefreshTimeout)
    err := p.RefreshAgent(aCtx, a.Name)
    cancel()
    // existing per-agent error collection + final summary unchanged
}
```

- `context.WithoutCancel(ctx)` (Go ≥1.21) keeps context **values** but strips the global deadline, so
  one slow/failed agent can't starve the rest.
- `perAgentRefreshTimeout` default ≈ **6m** (a full single-agent refresh — config-gen + reconcile +
  unit + restart-with-plugin-install + routing + egress — runs ~3–4m; 6m is headroom). No new CLI flag
  (keeps interface parity); the inner `ssmTransport` 120s per-command bound is unchanged.
- Cancellation propagation: if the operator Ctrl-C's, the parent cancel still aborts (WithoutCancel
  drops the deadline, not explicit cancellation — confirm during implementation; if it also drops
  cancellation, thread an explicit select on `ctx.Done()` between agents).

**Result:** every agent is attempted regardless of fleet size; failures collected + reported (current
behavior), full fleet covered.

---

## 5. R4 — `pre-start.sh` thundering-herd resilience

**Defect:** a simultaneous fleet start runs N concurrent `aws s3 sync` + `deploy-agents.sh` in
`ExecStartPre`, blowing the 120s `TimeoutStartSec` → `start-pre operation timed out` crash-loop until
started one-at-a-time.

**Design — `flock` serialization + `TimeoutStartSec` bump:**

- **`scripts/pre-start.sh.tmpl`:** wrap the S3 sync + `deploy-agents.sh` section in a host-wide
  `flock` so concurrent `ExecStartPre` runs **queue** instead of contending:
  ```sh
  exec 9>/var/lock/conga-prestart.lock
  # Bounded wait (QA): a stuck holder must not deadlock the whole fleet's start.
  # On lock-timeout, proceed WITHOUT the exclusive sync — the agent starts on its
  # existing on-disk behavior (the sync is a best-effort refresh, not a start gate).
  flock -w 240 9 || echo "WARN: conga-prestart lock wait timed out; proceeding with on-disk behavior" >&2
  …existing aws s3 sync + deploy-agents.sh…
  # lock released when pre-start.sh exits (fd 9 closes)
  ```
  Normal single-agent refresh: lock uncontended → zero slowdown. Cold reboot: agents serialize → each
  sync runs fast solo; the last waits ≈ N × sync-time, bounded by `-w 240`. The `-w` ceiling sits below
  `TimeoutStartSec=300` so a lock wait never trips the unit's start timeout.
- **`TimeoutStartSec` 120 → 300** so the last-in-line agent has headroom under serialization. Rendered
  by `systemdSupervisor.RenderUnit` (currently hard-coded `TimeoutStartSec=120`): add a
  `StartTimeoutSec int` field to `ServiceSpec` (default 300 set by the engine; `RenderUnit` emits the
  field's value, falling back to 120 if unset for back-compat).
- **Plugin-install `ExecStartPre`** (`docker run --rm … openclaw plugins install`) is **not** serialized:
  on a reboot the plugin is already on the persisted data dir → the install short-circuits fast. (If a
  cold start proves it also contends, fold it under the same lock — open checkpoint.)

**Success:** a host `reboot` brings every agent to `active` with no manual staggering and no
`ExecStartPre` timeout loop.

---

## 6. C5b — acceptance protocol (the umbrella)

On the AWS host, after R1–R4 + the fleet fully migrated:

1. **Host reboot** (`sudo reboot` via SSM) → within a few minutes, **all** agents `active`+`running`,
   each agent on `.2`, each proxy on `.3`, egress iptables re-applied (5-rule set incl. DNS), router up,
   **no operator intervention**, no crash-loops.
2. **Docker daemon restart** (`systemctl restart docker`) → same outcome (independent path).
3. **Data safety:** `/opt/conga/data/<name>/` byte-identical before/after the reboot (sample 1–2 agents).

This is the parent feature's criterion 5b and the go/no-go for "the migration is unattended-safe."

---

## 7. Data safety (architecture.md, must)

No requirement touches `/opt/conga/data/<name>/`. R2's reconcile removes only containers + the bridge
network; R1/R3/R4 don't touch storage. **Test:** §6.3 byte-identical check across a reboot + an R2
abort-then-recover cycle.

## 8. Interface parity (architecture.md, must)

**No new user-facing command, flag, JSON field, or MCP tool.** R3 uses an internal per-agent default
(no `--per-agent-timeout` flag). R1/R2/R4 are internal mechanics. Parity preserved by construction.

## 9. Security considerations (GATES implementation)

| Standard | Verdict | Note |
|---|---|---|
| Egress: iptables all-modes, fail-closed | ✅ preserved/strengthened | R1 removes the collision that left an agent crash-looping (egress never applied); rule set unchanged |
| Immutable config / perms (P2) | ✅ preserved | no change to root:root 0444 managed files |
| Secrets via env (#9627) | ✅ preserved | `--env-file` unchanged |
| Reserved-key guard (boundary) | ✅ preserved | unchanged; R2 abort path doesn't bypass it |
| Own the box, not behavior (P8) | ✅ on-mission | pure infra reliability hardening |
| Foreign-endpoint disconnect (R2) | ✅ safe | only disconnects dead `conga-router` bridge endpoints (router is `--network host`); never the agent's live egress path |

## 10. Testing strategy

- **R1 (unit):** rendered proxy `docker run` carries `--ip <ProxyIP>` at all three sites (deploy-egress,
  add-user, add-team); add-user/add-team network create uses `--subnet/--gateway`; `DeployEgress`
  resolves `ProxyIP` from `PlanAgentNetwork`.
- **R2 (unit, fake transport):** no-op when subnet matches; foreign endpoint force-disconnected before
  any stop/rm; **injected unclearable ghost → returns error and the fake records NO `systemctl stop` /
  `docker rm` of the agent** (fail-safe); happy path issues stop→rm→rm→network rm→create in order; old-IP
  iptables flush emitted.
- **R3 (unit):** `RefreshAll` derives a per-agent `context.WithTimeout` (deadline-free parent); a
  fake-slow first agent does not consume the others' budget.
- **R4 (unit + render):** `pre-start.sh` contains the `flock` guard around the S3 sync; rendered unit
  has `TimeoutStartSec=300`.
- **Integration / live (isolated throwaway):** (a) migrate a throwaway whose old net has a deliberately
  attached foreign container → completes; (b) inject an unclearable endpoint → aborts with the agent
  still running; (c) `systemctl restart docker` → throwaway returns agent `.2`/proxy `.3`, no collision.
- **Acceptance (C5b):** full-fleet reboot + docker daemon restart, per §6.

## 11. Rollout / sequencing (PM)

1. Land R1–R4 (one PR) → `go build/vet/gofmt/test ./...` green → **`terraform-provider-conga` release**
   (`pkg/` changed: `managedhost`, `awsprovider`).
2. **Remediate the 3 already-migrated agents** (R1 pin): `conga refresh` each (one at a time, verify
   `.2`/`.3`).
3. **Migrate the remaining 3** (nextgen-delivery, nvidia-team, zach) — individually, hardened path
   (R2), verify each.
4. **Prove C5b** — controlled host `reboot`; confirm unattended full-fleet return. Then a one-time
   `172.x` `DOCKER-USER` sweep is safe (all agents on `10.99.x`).

Interim (pre-release): the 3 migrated agents are reboot-fragile — **avoid a host reboot**; do not migrate
the held 3.

## 12. Out of scope

Slack `operator.write` delegation-scope gap (separate spec); provider merge; OpenRC; boot-script
reduction (parent slice 5); remote systemd (parent slice 6); any new user surface.

## 13. Open implementation checkpoints (resolve during implement)

1. **`context.WithoutCancel` cancellation** — confirm it strips only the deadline; if it also drops
   Ctrl-C propagation, add an explicit `ctx.Done()` check between agents (R3).
2. **`perAgentRefreshTimeout` value** — lean 6m; validate against the slowest observed single-agent
   refresh.
3. **Plugin-install serialization** — only fold the plugin-install `ExecStartPre` under the `flock` if a
   cold-reboot test shows it contends (default: leave it out; it short-circuits on warm data dirs).
4. **R2 location** — `managedhost/network_reconcile.go` (new) vs extending `network.go`; either is fine,
   keep the orchestration transport-driven + fake-testable.
5. **add-user/add-team static subnet** — confirm no other consumer relies on the old auto-subnet behavior.

## 14. Handoff

`/glados:implement-feature` — implement R1 (proxy pin, smallest + unblocks the migrated-agent
remediation) → R2 (the robustness core) → R4 → R3, each unit-tested; then the provider release + the
staged rollout (§11) + the C5b reboot acceptance. Reminder: `pkg/` change → `terraform-provider-conga`
release.
