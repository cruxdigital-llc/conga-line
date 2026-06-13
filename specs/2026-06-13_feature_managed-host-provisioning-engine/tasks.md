# Implementation Tasks — Managed-Host Provisioning Engine

6-slice migration (spec §9). Each slice: Go-test → isolated-agent verify → delete superseded bash.
**Slice 1 is detailed below for review; slices 2–6 are headers, detailed when each begins.**

> **Release note:** slice 1 touches `pkg/provider/awsprovider/provider.go` (a `pkg/` change) →
> `terraform-provider-conga` release. Script-only edits (`scripts/*.tmpl`) alone would not, but the
> `ProvisionAgent` change does. Batch slices into release checkpoints (PM gate).

---

## ⚠️ Scope decision for slice 1 (needs your call before coding)

Grounding the code revealed the loopback Go path **already exists** (`regenerateRoutingOnInstance`,
used by `RefreshAgent`); the bug is that `ProvisionAgent` doesn't use it. So two options:

- **Option A (recommended) — minimal bug fix.** Slice 1 fixes audit #1 by reusing the existing Go
  routing reconcile; introduce `pkg/provider/managedhost` + the `Transport` interface in **slice 2**
  (openclaw.json/`$include`), where there's substantial new code to justify the package. Lowest risk;
  decouples the urgent correctness fix from the engine refactor.
- **Option B — proof slice as originally spec'd.** Slice 1 *also* lands the `pkg/provider/managedhost`
  skeleton + `Transport` interface + fake-transport test, refactoring `regenerateRoutingOnInstance`
  through the seam. More upfront refactor; establishes the pattern immediately.

**DECISION (2026-06-13): Option B — follow the spec.** Slice 1 lands the `pkg/provider/managedhost`
skeleton + `Transport` interface + fake test, and routes AWS routing.json generation through the seam.

---

## Slice 1 — routing.json loopback (proof + live-bug fix, audit #1/#12) — ✅ CODE COMPLETE (unit-verified)

- [x] **T1.1 — Strip bash routing + bridge-attach from provision scripts.**
  - `add-user.sh.tmpl`: removed `node -e` routing block + `docker network connect` + unit `ExecStartPost` connect.
  - `add-team.sh.tmpl`: same (channel-keyed).
  - `refresh-user.sh.tmpl`: removed unit `ExecStartPost` connect + the reconnect block.
  - `refresh-all.sh.tmpl`: removed the per-agent `docker network connect`; the unit-patch now *deletes*
    any deprecated `ExecStartPost … network connect … conga-router` (positive cleanup of old units).
- [x] **T1.2 — Route `ProvisionAgent` through the Go loopback reconcile.** `awsprovider/provider.go`
  `ProvisionAgent` now calls `regenerateRoutingOnInstance` + `restartRouterOnInstance` after the
  script (non-fatal/`common.Warn`, mirrors `RefreshAgent`). SSM record is written at line 127 (before
  the reconcile), so the new agent is in the regenerated routing — verified by reading the code path.
- [x] **T1.3 — Tests.** `scripts_test.go` `TestProvisionScriptsDropBridgeRouterWiring` (no
  `docker network connect`/`node -e`/`cfg.members`/`cfg.channels`; refresh-all neither re-injects nor
  connects). `managedhost` `TestWriteRoutingJSON_Loopback` (loopback URL, no bridge form, mode 0644),
  `TestWriteRoutingJSON_PutFileErrorPropagates`, `TestExecFuncFor_RunsViaTransport`. (Note: an
  awsprovider-level ProvisionAgent assertion was *not* added — the thin `ssmTransport` adapter is
  compile-checked via `var _`; routing generation is unit-tested in `managedhost`; the deployed wiring
  is covered by T1.6 live verify. Avoids a heavy SSM mock for marginal value.)
- [x] **T1.4 — Build/vet/gofmt + full suite.** `go build` clean; `go vet` clean; `gofmt -l` clean;
  `go test ./...` **all pass**.
- [x] **T1.5 — Seed `pkg/provider/managedhost` (Option B).** `transport.go` (`Transport` interface
  `{PutFile, RunCommand, ReadFile}` + `ExecFuncFor` iptables adapter), `routing.go`
  (`WriteRoutingJSON`), `transport_test.go` (in-memory fake + tests). `awsprovider/transport.go`
  (`ssmTransport` adapter, `var _ managedhost.Transport`). `regenerateRoutingOnInstance` refactored
  through the seam.
- [ ] **T1.6 — Live verify (isolated AWS agent).** DEFERRED to verify/release phase (needs the
  provider release first for the deployed path). Provision a throwaway agent; confirm on-host
  `routing.json` loopback, unit has no `network connect`, router delivers, no bridge attach; tear down.
- [ ] **T1.7 — Provider release checkpoint.** `pkg/` changed (`awsprovider` + new `managedhost`) →
  `terraform-provider-conga` release before deployed-path verify. Batch with later slices.

---

## Slice 2 — openclaw.json + `$include` layers via engine (audit #2, #4)
Introduce/solidify `pkg/provider/managedhost` (Transport + artifacts); generate openclaw.json +
the 3 include layers in Go, `PutFile` them, drop the 4 bash heredocs + 3 bash `$include` self-heal
copies. *(Detailed at slice start.)*

## Slice 3 — egress: Envoy config + static-IP iptables via engine (audit #7)
Static per-agent IP at network create → deterministic egress command (no discovery loop); reuse
`pkg/provider/iptables` via the Transport-derived `ExecFunc`. *(Detailed at slice start.)*

## Slice 4 — systemd unit text via `systemdSupervisor` + preventive guard (audit #8 + integrity)
`ServiceSpec`/`HostSupervisor` + systemd backend (whole-unit write, no `sed`); reserved-key guard as
fail-closed `PreStart` (key list generated from `common.ReservedCustomConfigKeys`); guard-on-unparseable
= WARN+allow (spec §8); slim the periodic backstop. *(Detailed at slice start.)*

## Slice 5 — boot-script reduction (audit #3) — HIGHEST RISK
Reconstitute-from-persisted-EBS-artifacts; shrink `user-data.sh.tftpl` to install+secret-fetch+
reconstitution loop. **Gate on a host-replacement recovery test before deleting the old boot path**
(spec §11, checkpoint #1). *(Detailed at slice start; likely its own PoC/spike first.)*

## Slice 6 — remote systemd adoption + migration (remote unattended gap)
Switch remote from bare `docker run` to the shared systemd unit; remove the lazy `GetStatus`→iptables
self-heal; migrate existing remote deployments; reboot re-verify on the RPi/VPS target (criterion 5b).
*(Detailed at slice start; may run earlier to validate the supervisor over SSH first.)*
