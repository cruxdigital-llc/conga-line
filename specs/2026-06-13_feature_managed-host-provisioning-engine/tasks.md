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
- [x] **T1.6 — Live verify — DONE (no release needed).** Verified via the branch-built `./bin/conga`
  against the live fleet on both paths: `refresh aaron` (model→4-8 + overlay preserved, routing
  loopback, unit bridge-attach 1→0, ready) and fresh `add-user slice2test` (Go config 4-8, unit
  bridge-attach 0 + egress ExecStartPost present, no bridge refs), then slice2test torn down clean.
  The CLI binary + SSM-pushed embedded scripts exercise the branch `pkg/` directly — a public provider
  release is a *ship* step, not a test gate.
- [ ] **T1.7 — Provider release checkpoint.** `pkg/` changed (`awsprovider` + new `managedhost`) →
  `terraform-provider-conga` release before deployed-path verify. Batch with later slices.

---

## Slice 2 — openclaw.json + `$include` layers via engine (audit #2, #4) — ✅ FUNCTIONAL increment done; 2b cleanup remains

**2a (done, 2026-06-13): Go config authoritative on first provision.** `ProvisionAgent` now calls
`RefreshAgent` after the bash provision script (non-fatal). RefreshAgent regenerates openclaw.json +
`$include`/managed layers + env in Go (`regenerateAgentConfigOnInstance`, applying the canonical model
AND per-agent `agent.yaml` overlays — the static heredoc does neither), rewrites the systemd unit
consistently (refresh-user.sh, with `ExecStartPost` iptables), restarts, reconciles loopback routing,
and deploys the egress policy. So AWS agents now run the **Go-generated config from first provision**,
not just after a later `conga refresh`. Subsumes slice 1's standalone routing calls in `ProvisionAgent`.
Reuses the proven RefreshAgent path (low risk); non-fatal so provisioning never regresses. build/vet/
gofmt/`go test ./...` clean. Live verify release-gated.

**2b (remaining — now de-risked cleanup): physically remove the heredoc.** Because RefreshAgent now
regenerates the config on every provision, the `add-user.sh.tmpl`/`add-team.sh.tmpl` openclaw.json
heredoc + `cp` + `jq $include` + managed-include seeding + baseline are redundant (immediately
overwritten). Remove them; to avoid a no-config first start, also drop `systemctl start` from the
provision scripts and let RefreshAgent's refresh-user.sh do the first unit-write+start (it already
"recreates if missing"). Net: add-user/add-team shrink to data dir + egress proxy setup. Pair with the
live-verify gate (it changes the first-start sequence).

### Original grounding (kept for reference)

**Grounding (2026-06-13):** The Go config-gen path **already exists and is proven** —
`AWSProvider.regenerateAgentConfigOnInstance` (`channels.go:468`) generates openclaw.json + the
`$include`/managed layers + env + integrity baselines via `common.RuntimeGenerateAgentFilesWithOverlay`
(+ `readSharedSecrets`/`readAgentSecrets`, gateway-token preserve, root:root 0444 re-protect). It is
used by `RefreshAgent` (step 1, `provider.go:541`), then `RefreshAgent` runs `refresh-user.sh.tmpl`
(write unit + restart). So the Go path is the same proven shape as slice 1 — `ProvisionAgent` is the
holdout still using the `add-user.sh.tmpl`/`add-team.sh.tmpl` openclaw.json heredoc + `jq $include`.

**The ordering constraint (why slice 2 ≠ a one-line wire-up like slice 1):** `add-user.sh.tmpl`
*generates the openclaw.json heredoc AND `systemctl start`s the container in one SSM run* (lines
240-242). So the heredoc can't simply be deleted — the container would start with no config. Removing
it requires **reordering**: config must be on disk before the container starts (as `RefreshAgent`
already does: Go-config-first, then unit+start).

**Two implementation paths:**
- **Path A (real end-state — recommended).** Restructure the provision flow so config-gen precedes
  container start: split `add-user/add-team.sh.tmpl` into "create data dir + egress + systemd unit
  (no start)" → `ProvisionAgent` calls `regenerateAgentConfigOnInstance` (Go) → start the container.
  Then delete the openclaw.json heredoc + `cp` + `jq $include` from both scripts. Mirrors
  `RefreshAgent`. Higher blast radius (production first-provision flow) → pair with the live-verify
  gate; verify byte/schema-equivalence of the Go config vs the (now opus-4-8) heredoc first.
- **Path B (safe stepping-stone, optional).** Keep the heredoc; after `add-user.sh`, call
  `regenerateAgentConfigOnInstance` + restart (additive, mirrors slice 1). Makes Go config
  authoritative on first provision — notably **applies per-agent `agent.yaml` model overlays on first
  provision** (today they only apply after a `conga refresh` on AWS; the heredoc ignores the overlay).
  Wasteful double-start; does NOT remove the heredoc. Use only if we want the per-agent-overlay fix
  before the Path-A reorder.

**Tasks (Path A):**
- [ ] T2.1 — Split `add-user.sh.tmpl`/`add-team.sh.tmpl`: stop generating openclaw.json + the `jq
  $include`; create the unit but do NOT `systemctl start` (or gate start behind config-present).
- [ ] T2.2 — `ProvisionAgent`: after the (restructured) script creates the unit, call
  `regenerateAgentConfigOnInstance(ctx, instanceID, cfg)` (already exists), then start the container.
  Confirm data-dir exists before the Go upload (script must `mkdir` first).
- [ ] T2.3 — Equivalence test: Go-generated openclaw.json structurally matches the prior heredoc
  (post-opus-4-8) for a no-overlay agent; `$include` array present; per-agent overlay applied when set.
- [ ] T2.4 — Remove the now-dead heredoc lines; `build`/`vet`/`gofmt`/`go test ./...`.
- [ ] T2.5 — Live verify (isolated AWS agent): provision → confirm Go-generated config on host (model
  = canonical/overlay), container boots, `$include` resolves; tear down. (Release-gated.)
- Note: `add-user/add-team.sh.tmpl` are scripts/ (no provider release); the `ProvisionAgent` change is
  `pkg/` → release. Boot tftpl heredoc reduction stays in slice 5.

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
