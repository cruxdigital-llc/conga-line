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

**2b (done, 2026-06-13): heredoc removed; provision scripts are infra-only.**
`add-user.sh.tmpl`/`add-team.sh.tmpl` no longer generate openclaw.json (heredoc + `cp` + `jq $include`
+ managed-include baseline) or create/start the systemd unit or apply iptables — they now set up
per-agent INFRA ONLY (env, data dir, metadata, behavior deploy, network, egress proxy). RefreshAgent
owns config + unit + start + iptables + routing (regenerateAgentConfigOnInstance → refresh-user.sh).
`ProvisionAgent`'s RefreshAgent call is now **fatal** (it's the only thing that yields a running
agent). Tests: rewrote the add-user/add-team render tests to the infra-only shape; replaced
`assertOpenClawV5Shape` (bash heredoc) with `TestProvisionScriptsAreInfraOnly` (absence guard) +
`TestGenerateConfig_GatewayV5Shape` in `pkg/runtime/openclaw/config_test.go` (the Go generator owns
the gateway v5 shape now).
- **Live-verified on a throwaway (`slice2btest`)** — and the test **caught a real regression**:
  `refresh-user.sh` did `daemon-reload` + `restart` but never `systemctl enable`, so a 2b-provisioned
  agent ran but was **`disabled`** (would not survive a host reboot — breaks the unattended
  guarantee). Fixed: `refresh-user.sh` now `systemctl enable`s the unit (idempotent for existing
  agents; it's the only enable site now that provision scripts are infra-only) + a regression
  assertion. Re-verified: `slice2btest` came up Go-config (opus-4-8) + unit `enabled` + `running` +
  `ExecStartPost` iptables + no bridge attach; torn down clean. `go test ./...` (21 pkgs) green.

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

## Slices 4+3 — engine (decision: build together) — ✅ COMPLETE (B-1, B-2, B2.4, B-3 live-verified; B5 integrity-backstop slim done)

Chosen to build slices 4 (systemd-unit-via-supervisor) + 3 (static-IP egress) together because the
egress iptables lives in the unit, so the clean home for a deterministic egress command is a
Go-generated `ServiceSpec.Hooks.PostStart`.

**Increment A — engine core (done, 2026-06-13; pure Go, unit-tested, ZERO production wiring):**
- [x] `pkg/provider/managedhost/supervisor.go` — `ServiceSpec`, `LifecycleHooks`, `RestartPolicy`,
  `HostSupervisor` interface, `systemdSupervisor` (RenderUnit + DefineService[**daemon-reload+enable**,
  the slice-2b reboot-survival fix baked in]/Start/Stop/Restart/RemoveService/Status), reserved
  `openrcSupervisor` stub (`ErrUnsupportedSupervisor`). Boundary rule honored: no systemd-ism in `ServiceSpec`.
- [x] `network.go` — `PlanAgentNetwork(hostPort, base)` → deterministic `10.99.<idx>.0/24` per agent
  (agent `.2`, proxy `.3`); collision-free vs VPC `10.0.0.0/24` + Docker's `172.x` pool. (slice 3 foundation)
- [x] `guard.go` — `ReservedKeyGuardScript(includePaths)` → fail-closed reserved-key `PreStart` guard,
  key list generated from `common.ReservedCustomConfigKeys` (single source of truth); WARN+allow on
  unparseable JSON5 (spec §8). (integrity decision #2)
- [x] `supervisor_test.go` — 5 tests: network plan (idx/collision/VPC-avoid/range), guard (all keys +
  fail-closed + WARN), RenderUnit (shape + hook ordering), DefineService-enables-unit (regression
  guard), OpenRC-reserved. build/vet/gofmt + `go test ./...` (21 pkgs) green.

**Increment B — production swap (operator decision 2026-06-13: split into 3 live-verified steps
instead of a big-bang B1–B5; converge the agent container to `--env-file` when the Go RunCmd lands
in step 2):**

**Step B-1 — deterministic static-IP network + egress iptables (audit #7) — ✅ CODE COMPLETE (unit-verified).**
Scoped tightly to `refresh-user.sh.tmpl` + its Go wiring in `RefreshAgent` (the primary agent lifecycle
path: provision→refresh, refresh, and reboot via the unit's `ExecStartPost`). Collision-safe because
the agent always gets explicit `--ip .2` and the egress proxy is only ever (re)created *after* the agent
(so it lands on `.3`), so the proxy `--ip` pin is **not** required and `add-user`/`add-team`/`deploy-egress`
are left untouched this step (their auto-subnet network is migrated by the first `refresh-user` run).
- [x] **B1.1 — Go wiring (`pkg/provider/awsprovider/provider.go`).** `RefreshAgent` computes
  `managedhost.PlanAgentNetwork(agent.GatewayPort, common.BaseGatewayPort)` and the
  `iptables.AddRulesCmd`/`RemoveRulesCmd` strings (known IP → no discovery), passing
  `{SubnetCIDR, GatewayIP, AgentIP, IptablesAddCmd, IptablesRemoveCmd}` into the refresh template.
- [x] **B1.2 — `scripts/refresh-user.sh.tmpl`.** ExecStart binds `--ip {{.AgentIP}}`; unit
  `ExecStartPost`/`ExecStopPost` are the deterministic Go-generated `/bin/bash -c '<AddRulesCmd>'` /
  `'<RemoveRulesCmd>'` (replacing the two 10-retry `docker inspect` blocks); the plain `docker network
  create` becomes a **subnet-migration** block (recreate the net with `--subnet/--gateway` when the
  current subnet doesn't match — `systemctl stop` first so `Restart=always` can't race the `network rm`,
  remove agent+proxy to free it; steady-state refresh is a no-op so there's no egress gap); the inline
  post-restart iptables loop becomes the deterministic one-liner.
- [x] **B1.3 — Tests (`scripts/scripts_test.go`).** `refreshUserData(t, name, port)` helper renders the
  template through the **real** `PlanAgentNetwork` + `AddRulesCmd`/`RemoveRulesCmd` (no stale strings);
  `TestRefreshUserScriptTemplateRender` asserts `--ip 10.99.2.2`, subnet `10.99.2.0/24` create, the
  deterministic DROP rule, and the **absence** of `for i in $(seq 1 10)` + `NetworkSettings.Networks`
  (the discovery loop is retired). `TestProvisionScriptsDropBridgeRouterWiring`'s refresh-user render
  updated to the new struct.
- [x] **B1.4 — build/vet/gofmt + `go test ./...`** all clean/green; rendered unit eyeballed (well-formed
  systemd, literal IPs, correct `/bin/bash -c` single-quoting).
- [x] **B1.5 — Live verify on a throwaway — DONE (no release; branch `./bin/conga`).** Provisioned
  gateway-only `b1test` (port 18796 → idx 7) on the live fleet. Host (SSM `i-024bf3a55563f9e88`):
  container `--ip 10.99.7.2`, network `10.99.7.0/24 gw=10.99.7.1`, unit `ExecStart` binds the static IP,
  `ExecStartPost`/`ExecStopPost` are the deterministic Go iptables (literal IP), unit **enabled+active**,
  **discovery-loop residue count = 0**, DOCKER-USER rules correctly ordered (RETURNs above DROP).
  **Functional:** DNS resolves (`api.anthropic.com`→`2607:6bc0::10`); proxied egress connects
  (`PROXY OK 401` — TLS+Envoy path fine, 401 = no key); **direct (non-proxy) egress BLOCKED by the DROP
  rule** (`DIRECT BLOCKED (timeout)`). Torn down clean (roster back to 6; data/config/orphan-iptables
  swept). The throwaway provision exercised the subnet-migration path (add-user auto-subnet →
  ProvisionAgent→RefreshAgent migrate to 10.99.7.x); operator opted **not** to also migrate a real agent
  (`aaron`) this pass. **B-2 cleanup note:** `deploy-egress.sh` still adds port-53 DNS RETURN rules that
  `iptables.RemoveRulesCmd` doesn't remove → 2 orphan rules survived teardown (harmless: the IP is gone).
  Reconcile when `deploy-egress`'s discovery-loop iptables is retired in B-2.

**Step B-2 — replace the bash unit-gen with the Go `systemdSupervisor` (audit #8) — ✅ CORE CODE COMPLETE (unit-verified); deploy-egress reconciliation + live verify remain.**
- [x] **B2.1 — shared container-arg builder** (`managedhost/container.go`): `AgentContainer` →
  `Args()` argv + `SystemdExecStart(argv)` (double-quotes whitespace args, e.g. the `NODE_OPTIONS`
  `--require` value, the exact systemd-split hazard the bash unit avoided). Secrets via **`--env-file`**,
  never inline `-e KEY=VALUE` (#9627). `container_test.go`: core args, egress wiring, NODE_OPTIONS quoting.
- [x] **B2.2 — Go ServiceSpec + supervisor wiring** (`awsprovider/engine.go`): pure
  `buildAgentServiceSpec(agent, image, region)` builds the unit (After/Requires, PreStart=[pre-start.sh,
  rm -f, @openclaw/slack seed], ExecStart via the builder, PostStart/PostStop = deterministic B-1
  iptables, ExecStop, LogTarget, Restart=always/10, **no EnvironmentFile**); `defineAndStartAgentService`
  fetches the image (SSM `/conga/config/image`), runs the Go network-migration command, then
  `sup.DefineService` (write+daemon-reload+enable) + `sup.Restart`. `RefreshAgent` step 2 now calls it
  (replacing the refresh-user.sh SSM exec). `ssmTransport.RunCommand` ceiling 30s→**120s** (the
  `systemctl restart` blocks on the slow first-provision plugin-install ExecStartPre; matches the prior
  bash 120s).
- [x] **B2.3 — retire refresh-user.sh**: deleted `scripts/refresh-user.sh.tmpl` + `RefreshUserScript`
  embed + the scripts_test refs (helper/test/render entry); `provider_test.go` RefreshAgent-step-2
  snippet `RefreshUserScript`→`defineAndStartAgentService`. Added `managedhost.RenderSystemdUnit` (exported
  render wrapper) + `awsprovider/engine_test.go` **unit-equivalence test** (asserts every directive the
  bash unit had + the B-1 determinism, and the absence of `EnvironmentFile=`/`-e SECRET`/`seq 1 10`/
  `NetworkSettings.Networks`/`sed -i`/`docker network connect`). build/vet/gofmt + `go test ./...` green;
  rendered unit eyeballed (faithful equivalent, `--env-file` convergence).
- [x] **B2.4 — deploy-egress.sh reconciliation — DONE (live-verified).** `deploy-egress.sh` no longer
  touches iptables, `sed`-injects the unit, or attaches the router. Egress is owned by the Go unit's
  deterministic `ExecStartPost`/`ExecStopPost`; deploy-egress recreates the proxy + `systemctl restart`s
  the agent, which cycles those rules. Removed: the pre-restart discovery-removal block, the two `sed -i`
  unit injections (kept the unit-exists check), the post-restart 10-retry iptables block, the dead
  `docker network connect conga-router`, and the now-unused `AGENT_CONTAINER` var. Test rewritten:
  `TestDeployEgressScriptValidateModeAppliesIptables` → `TestDeployEgressScriptDelegatesEgressToUnit`
  (asserts the `systemctl restart` + the **absence** of `iptables -I/-D`, `seq 1 10`,
  `NetworkSettings.Networks`, `sed -i`, `docker network connect`).

- [x] **B-3 — reserved-key PreStart guard — DONE (live-verified).** `defineAndStartAgentService` PutFiles
  the generated guard (`managedhost.ReservedKeyGuardScript`, 0755) to
  `/opt/conga/bin/reserved-key-guard-<name>.sh` and wires it as the **2nd PreStart hook** (after
  `pre-start.sh` syncs the includes, before `docker rm -f`) — fail-closed (no leading `-`).
  `agentIncludePaths` = the 3 layers (agent-custom / fleet-custom / agent-managed-custom).
  `engine_test.go` `TestBuildAgentServiceSpec_ReservedKeyGuardWiring` asserts the order + fail-closed.
  `remove-agent.sh.tmpl` now also `rm`s the per-agent guard script (teardown gap fixed).
  **Live (b3test):** guard deployed 0755 + wired 2nd PreStart; running the deployed guard against crafted
  includes → `{}` exit 0; `{"channels":…}` → **exit 1 + FATAL "refusing to start"**; JSON5 comment →
  **exit 0 + WARN** (allow, don't down the agent); restored. Matches spec §8 exactly.

- **DNS-rule fix (caught by the B2.4/B-3 live verify — the important find).** Removing deploy-egress's
  iptables exposed that `iptables.AddRulesCmd` (3-rule set) is **insufficient on AWS**: with no port-53
  RETURN the throwaway lost DNS (`getent` failed; DOCKER-USER had exactly 3 rules). Docker's embedded
  resolver forwards to the **VPC resolver outside the per-agent subnet**, so that query is sourced from
  the container IP to a non-subnet address → hits the DROP. The old deploy-egress discovery loop had
  always silently supplied the DNS rules; b1/b2 only passed because of it. **Fix:** added udp/tcp
  `--dport 53 -j RETURN` to the shared `iptables` rule set (`egressRuleSpecs` →
  `AddRulesCmd`/`RemoveRulesCmd`/`CheckRulesCmd`), so the unit's `ExecStartPost` supplies DNS
  deterministically (and `RemoveRulesCmd` symmetry retires the B-1.5 orphan-DNS issue). `rules_test.go`
  updated (5 rules; DROP inserted first → bottom). Re-verified via `conga refresh`: DOCKER-USER = 5 rules
  in order, **DNS resolves**, teardown orphan-free. (Remote also gains the DNS rules — harmless/more
  correct; it had none before.)
- [x] **B2.5 — Live verify on a throwaway — DONE (no release; branch `./bin/conga`).** Provisioned
  `b2test` (port 18796 → 10.99.7.2). Host: the deployed unit is **exactly** the Go-built one —
  `EnvironmentFile=` count **0**, `--env-file` present, `--ip 10.99.7.2`, `-p 127.0.0.1:18796:18789`,
  egress proxy env + `NODE_OPTIONS` double-quoted as one arg, deterministic `ExecStartPost`/`StopPost`
  iptables; container running on 10.99.7.2; unit **enabled+active**. Since `refresh-user.sh` is deleted,
  this unit could only have come from the Go `systemdSupervisor`. Functional: DNS resolves, proxied
  egress connects (`PROXY OK 401`), direct egress **BLOCKED**. Torn down clean (roster back to 6;
  data/config/orphan-iptables swept). Existing-agent refresh not exercised this pass (throwaway-only, per
  the B-1.5 cadence choice).

**Step B-3 — deploy + wire the reserved-key PreStart guard (`guard.go`, integrity #2).** PutFile the
generated guard (0755) + add as the first `PreStart`. Live-verify it blocks an injected `channels`
include and WARN+allows an unparseable JSON5 include.

- [x] **B5 — Slim the periodic integrity backstop — DONE (terraform-only; deferred to next host cycle).**
  Removed the reserved-key `jq` loop from `check-config-integrity.sh` (in `user-data.sh.tftpl`): that
  boundary is now enforced **preventively** by B-3's fail-closed `ExecStartPre` guard, so the periodic
  timer no longer re-scans `$include` layers for Conga-owned keys. Kept (decision #6 — "keep, slimmed"):
  the SHA256 hash checks (root `openclaw.json` + the two managed include layers) as the on-host-tampering
  detective backstop, and the CloudWatch metric-filter/SNS alarm (still fires on the `CONFIG_INTEGRITY_
  VIOLATION` the hash checks emit). bash `-n` clean; reserved-key code gone (only the explanatory comment
  remains); loop structure intact. **No provider release** (terraform/ only, not pkg/). The audit-#4
  baseline-writer consolidation (boot vs deploy-agents both write baselines) is left to **slice 5** (boot
  reduction), where `deploy-agents.sh` becomes the single baseline writer.

### ✅ Increment B complete (B-1, B-2, B2.4, B-3, B5). Remaining feature work: slice 5 (boot reduction — highest risk, spike first) + slice 6 (remote systemd).

## Slice 5 — boot-script reduction (audit #3) — HIGHEST RISK
Reconstitute-from-persisted-EBS-artifacts; shrink `user-data.sh.tftpl` to install+secret-fetch+
reconstitution loop. **Gate on a host-replacement recovery test before deleting the old boot path**
(spec §11, checkpoint #1). *(Detailed at slice start; likely its own PoC/spike first.)*

## Slice 6 — remote systemd adoption + migration (remote unattended gap)
Switch remote from bare `docker run` to the shared systemd unit; remove the lazy `GetStatus`→iptables
self-heal; migrate existing remote deployments; reboot re-verify on the RPi/VPS target (criterion 5b).
*(Detailed at slice start; may run earlier to validate the supervisor over SSH first.)*
