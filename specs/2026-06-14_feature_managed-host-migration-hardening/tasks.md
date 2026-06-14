# Implementation Tasks — Managed-Host Migration Hardening

Implement order (spec §14): **R1 → R2 → R4 → R3**, each unit-tested. Live verification + the C5b reboot
acceptance are **release-gated** (post `terraform-provider-conga` release), per the rollout (spec §11).
`pkg/` changes → provider release. **R1 detailed below; R2–R4 are headers, detailed when each begins.**

---

## R1 — Pin the egress proxy to `ProxyIP` (.3) — eliminates the restart collision — ✅ CODE COMPLETE (unit-verified)

Smallest change, lowest risk, and it **unblocks remediation of the 3 reboot-fragile migrated agents**.
Pin the proxy at every creation site; that requires the network to be the static `10.99.x` subnet at
proxy-creation time, so `add-user`/`add-team` also move to static-subnet creation.

- [x] **T1.1 — `network.go`**: `AgentNetwork.ProxyIP` doc flipped advisory → **enforced** (pinned via
  `docker run --ip`; reboot-survival requirement, not cosmetic — rationale inline).
- [x] **T1.2 — `deploy-egress.sh.tmpl`**: proxy `docker run` gains `--ip "{{.ProxyIP}}"`.
- [x] **T1.3 — `DeployEgress` (provider.go)**: resolves the agent (`discovery.ResolveAgent` →
  `GatewayPort`) → `managedhost.PlanAgentNetwork` → threads `ProxyIP` into the template struct
  (re-added the `managedhost` import).
- [x] **T1.4 — `add-user.sh.tmpl` / `add-team.sh.tmpl`**: network create now uses
  `--subnet "{{.SubnetCIDR}}" --gateway "{{.GatewayIP}}"`; proxy `docker run` gains `--ip "{{.ProxyIP}}"`.
- [x] **T1.5 — `ProvisionAgent` (provider.go)**: computes `PlanAgentNetwork(cfg.GatewayPort, …)`;
  `provisionData` gains `SubnetCIDR`/`GatewayIP`/`ProxyIP` (both user + team cases).
- [x] **T1.6 — Tests**: `scripts_test.go` render tests updated (all four provision structs + both
  deploy-egress structs gain the fields); new assertions: proxy `--ip <ProxyIP>` at deploy-egress +
  add-user (`10.99.0.3`) + add-team (`10.99.1.3`); add-user/add-team network create asserts
  `--subnet/--gateway` (replaced the old auto-create marker).
- [x] **T1.7 — build/vet/gofmt + `go test ./...`** all green (full suite).
- [x] **T1.8 — Live verify (release-gated)**: ✅ DONE. `zach` migrated 172.22→`10.99.2` with proxy
  `IPAMConfig`-pinned `10.99.2.3`; agent stayed up through migration. Then remediated aaron/nathan/
  congaline-team (re-refresh → proxies pinned). **Proven fleet-wide by the C5b reboot below**
  (all 6 proxies pinned `.3`, agents `.2`, `restarts=0`).

---

## R2 — Robust, fail-safe network migration (`ReconcileAgentNetwork` Go orchestration) — ✅ CODE COMPLETE (unit-verified)
Replaced the shell-string `agentNetworkMigrationCmd` with `managedhost.ReconcileAgentNetwork(ctx, t,
name, net)` (new `network_reconcile.go`): inspect subnet → no-op on match / create-only if absent /
on mismatch PREPARE (force-disconnect foreign endpoints; abort-before-touching-agent if a ghost can't
be cleared; flush old-IP iptables) → COMMIT (stop → rm agent → rm proxy → step-verified `network rm` →
create). Wired into `defineAndStartAgentService`; `agentNetworkMigrationCmd` deleted (+ its engine_test).
Extended the fake transport with a `responder` hook; new `network_reconcile_test.go`: no-op,
create-only, **fail-safe-abort (asserts NO stop/rm of the agent when a ghost persists)**, happy-path
ordering (disconnect → stop → rm → network rm → create).

## R4 — `pre-start.sh` thundering-herd resilience — ✅ CODE COMPLETE (unit-verified)
`pre-start.sh.tmpl`: bounded `flock -w 240` on `/var/lock/conga-prestart.lock` around the S3 sync
(serializes a simultaneous fleet start; proceeds with on-disk behavior on lock-timeout so it can't
deadlock). New `ServiceSpec.StartTimeoutSec`, rendered by `RenderUnit` (default 120); engine sets **300**
(`< ` the 240 flock wait ceiling). Tests: `TestPreStartSerializesSync` (bounded flock before the sync);
`supervisor_test` (StartTimeoutSec honored + 120 default); engine unit-equiv asserts `TimeoutStartSec=300`.

## R3 — `refresh-all` per-agent timeout — ✅ CODE COMPLETE (unit-verified)
`RefreshAll` now uses `perAgentRefreshCtx(ctx)` = `context.WithTimeout(context.WithoutCancel(ctx), 6m)`
per agent (decoupled from the global `--timeout`); loop guards explicit operator cancel via
`errors.Is(ctx.Err(), context.Canceled)` (checkpoint #1 — `WithoutCancel` drops cancellation, so cancel
is handled separately; the global `DeadlineExceeded` deliberately does NOT bound the fleet). Test:
`TestPerAgentRefreshCtx_DecoupledFromParentDeadline` (expired parent → fresh ~6m per-agent deadline).

---

## Release + rollout (after the code lands; spec §11) — ✅ ALL DONE
1. ✅ provider release (`terraform-provider-conga` v0.1.9 / conga-line v0.0.31; terraform pin 0.1.8→0.1.9, PR #69).
2. ✅ Remediated the 3 pre-fix-migrated agents (aaron/nathan/congaline-team re-refreshed → proxies pinned).
3. ✅ Migrated the held agents (zach 172.22→10.99.2 as the R2 live test; nextgen-delivery 172.20→10.99.3;
   nvidia-team 172.21→10.99.5). All 6 on static `10.99.x`, every proxy pinned `.3`.
4. ✅ One-time `172.x` DOCKER-USER sweep (4 inert orphans → 0). ✅ **C5b PASSED**: controlled host reboot →
   unattended full-fleet return — all 6 `active`/`running`, `restarts=0`, agents `.2`, proxies pinned `.3`,
   egress re-applied (0 `172.x` rules), DNS OK. Feature operationally complete; ready for `/glados:verify-feature`.
