# Feature: Managed-Host Provisioning Engine (AWS → shared-Go convergence)

**Trace Log** — GLaDOS `plan-feature` workflow

- **Created**: 2026-06-13
- **Owner**: Aaron Stone
- **Status**: Specified (pre-implementation) — persona review APPROVE, standards gate PASS
- **Spec dir**: `specs/2026-06-13_feature_managed-host-provisioning-engine/`
- **Origin**: `audit/` scope-and-simplification review (2026-06-13), "Theme 3". Operator chose the
  "spirit of Option C, delivered safely" over a literal `AWS = remote-over-SSM` provider merge.

## One-line

Make the AWS provider provision agents by running Conga's **shared Go logic** (the same code the
remote provider already uses) over a deliberately tiny transport seam — `{PutFile, RunCommand}` —
instead of rendering and shipping hand-maintained bash scripts. Eliminate the `scripts/*.sh.tmpl`
provisioning family and shrink the 1,384-line boot `user-data.sh.tftpl` to minimal bootstrap.

## Active Personas

- **Architect** — the transport-contract seam, shared managed-host package boundary, the
  systemd-vs-docker-restart lifecycle decision, three-provider parity without forcing SSH≡SSM.
- **Product Manager** — scope discipline (this is NOT a provider merge), operator value,
  migration safety on a live fleet, success criteria, non-goals.
- **QA** — the central promise is *testability*: Go unit tests over untestable templated bash.
  Restart/refresh survival, egress fail-closed under the new path, per-slice live verification,
  byte/schema-equivalence of artifacts vs the remote-proven generators.

## Active Capabilities

- **GitHub** (`gh`) — PRs, CI, the two-repo provider release flow (`pkg/` change → tag congaline →
  `terraform-provider-conga` release).
- **conga MCP** — live introspection (`get_status`, `get_logs`, `container_exec`, `get_proxy_logs`)
  against the AWS fleet to verify each migrated slice produces the same host state.
- **AWS SSM** — host inspection (`aws ssm start-session`) for AWS-provider behavior and isolated-agent probes.
- _No browser/UI or DB tools relevant — this is an infra/provisioning-architecture feature._

## Session Log

- **2026-06-13** — Session start. Feature created from the `audit/` review. Personas selected
  (Architect, PM, QA) and capabilities recorded. Drafted `requirements.md` and `plan.md`.
- **2026-06-13** — Current-state verified directly (not assumed) during the audit's final pass.
  Key finding: the keystone pattern already ships — `pkg/provider/iptables` exposes pure logic
  (`AddRulesCmd`) behind `type ExecFunc func(cmd string) error`; the remote provider injects an
  SSH-backed `ExecFunc` (`sshIptablesExec`), AWS does not use it (bash `ExecStartPost` one-liner).
  Generalizing that seam IS the strategy. Parity table recorded in `requirements.md` §Current State.
- **2026-06-13** — Operator raised: VPS/remote "production" must also run unattended; local is the
  different case. Verified remote already requires systemd (`installDocker` uses `systemctl`) and has
  **no** unattended story today (no `docker --restart`; lazy `GetStatus` egress self-heal). **Resolved
  the lifecycle decision (Open Decision #1):** systemd is THE managed-host lifecycle for all non-local
  providers (remote + AWS), generated once in shared Go — an upgrade for remote, not just AWS plumbing.
  Reframed `requirements.md` around a managed-host (systemd) vs local (Docker) **deployment taxonomy**;
  added success criterion 5b (unattended parity), the remote systemd upgrade to scope, and slice 6.
  Also recorded a latent security drift this fixes (the AWS add-path unit lacks the egress `ExecStartPost`).
- **2026-06-13** — Operator future-proofing ask: reserve space/stubs for non-systemd hosts (Alpine)
  and document the extension approach. Added a third seam — **`HostSupervisor`** (init system) — to
  the architecture: engine emits a provider-agnostic `ServiceSpec`, systemd is the only built backend,
  `openrcSupervisor` reserved as a stub (`ErrUnsupportedSupervisor`). Wrote
  [`extension-host-supervisor.md`](./extension-host-supervisor.md) (three-seams model, ServiceSpec
  contract, systemd↔OpenRC mapping table, selection, host bring-up deltas, the additive-extension
  recipe). Added requirements §5c (reserved seam + boundary rule + YAGNI scope guard) and plan §0/§1b.
- **2026-06-13** — Operator tested a 3-MCP scenario (Linear in code → admin adds Google in-place →
  GitHub in code → deploy). Confirmed via #31's verified `$include` union that **all three survive
  today** — the runtime-merge half is already solved. Operator's real want clarified: an **accurate
  super-admin view of what's actually running**, with no strong opinion on mechanism. **Resolved
  decision #2 (integrity, prevention-first)** and **config ownership = Model C** (keep deep-merge +
  admin-survival; code is the authoritative record, no clobbering reconcile). Visibility direction:
  effective config from in-container `openclaw config get` (ground truth) + Conga provenance overlay,
  as a **follow-on** `show-config` enhancement; `conga agent pull` optional. Promoted **`ReadFile` to
  a core transport method**. Reconciled the earlier CLI-audit `show-config` finding (refine, don't
  remove). All design questions resolved → ready for `/glados:spec-feature`.

## Key Decisions (this phase)

1. **Not a provider merge.** Rejected literal Option C (`AWS = remoteprovider` with an SSM
   transport behind one interface). SSH and SSM have a real impedance mismatch (persistent/
   streaming/SFTP+reconnect vs async/30s-min/no-session/no-native-file-transfer). We share *logic*,
   not the *transport interface*. The only shared contract is the minimal `{PutFile, RunCommand}`.
2. **Generalize the existing `pkg/provider/iptables` `ExecFunc` pattern** to all host
   orchestration that AWS currently does in bash. The pattern is proven and shipped.
3. **Reuse, don't rebuild.** Most "derived artifacts" are already shared Go in `pkg/common` /
   `pkg/policy` and are used by remote; AWS re-derives them in bash. Converging is mostly deletion.
4. **Security boundaries are non-negotiable and stay with Conga**: channel allowlist, `$include`
   reserved-key guard, egress iptables fail-closed, secrets-as-env, root:root `0444` on managed files.
5. **Managed-host vs local taxonomy (RESOLVED 2026-06-13).** systemd is the managed-host lifecycle
   for **all non-local providers** (remote + AWS), generated once in shared Go — unattended,
   reboot-survivable, host-resident egress. This is an *upgrade* for remote (which has no unattended
   story today) and adds no dependency (remote already requires systemd). Local stays Docker-only.
6. **Config integrity = prevention-first (RESOLVED 2026-06-13).** Reserved-key guard becomes a
   fail-closed `PreStart` hook (key list generated from `common.ReservedCustomConfigKeys`); perms
   stay; periodic SHA256 backstop slimmed (drop audit-#4 dual-baseline coupling). Preventive control
   converges across managed-host providers; detective/alerting stays provider-appropriate. One
   slimming detail open for spec: keep vs drop AWS CloudWatch/SNS alerting (lean: keep, slimmed).
7. **Config ownership = Model C + visibility-via-OpenClaw (RESOLVED 2026-06-13).** `$include`
   deep-merge + admin-survival stays; code is the authoritative *record* (no clobbering reconcile).
   Super-admin "what's running" visibility = effective config from in-container `openclaw config get`
   (ground truth; Conga doesn't re-derive the merge) + Conga provenance overlay — a **follow-on**
   enhancement to `show-config`, not this feature. `conga agent pull` is optional remediation.
   `ReadFile` promoted to a **core** transport method so the engine doesn't foreclose this.
8. **Decisions still deferred to `spec.md`**: how far to reduce `user-data.sh.tftpl`; exact transport
   contract shape (interface vs struct of funcs); remote migration + reboot re-verification protocol;
   the one integrity slimming detail (#6).

## Files Created

- [requirements.md](./requirements.md)
- [plan.md](./plan.md)
- [extension-host-supervisor.md](./extension-host-supervisor.md) — the reserved init-system seam
  (`HostSupervisor`/`ServiceSpec`), systemd-as-backend-#1, and the theoretical recipe for adding a
  non-systemd backend (OpenRC/runit/s6) for lightweight hosts (Alpine).
- [spec.md](./spec.md) — detailed technical specification (transport interface, `ServiceSpec`/
  `HostSupervisor`, shared `pkg/provider/managedhost` package, 6-slice migration, integrity model,
  Data Safety, Interface Parity, security gate, open checkpoints).

## Session Log (spec phase)

- **2026-06-13** — `/glados:spec-feature` complete. Drafted `spec.md` on top of the plan artifacts;
  resolved deferred questions: transport = Go **interface** `{PutFile, RunCommand, ReadFile}`; shared
  package = new **`pkg/provider/managedhost`**; boot reduction = **reconstitute-from-persisted-EBS-
  artifacts** (keeps no-host-binary + unattended replacement; flagged highest-risk, gated on a
  replacement-recovery test). Reconciled the `audit/cli-surface-audit.md` show-config finding
  (refine, not remove). **Persona review**: Architect APPROVE (flag: §11 boot-reconstitution spike
  before slice 5); PM APPROVE (batch the 6 slices into provider-release checkpoints); QA APPROVE
  **after two amendments** — (1) guard on unparseable JSON5 include = WARN+allow (don't down the agent;
  perms compensate), (2) partial-failure idempotency + no half-egress window (§12.1). Both amended
  into the spec + tests added (§14). **Standards gate: PASS** — all `must` pass; 2 non-blocking items
  (egress-controls.md doc-sync when the bash parser is deleted; confirm config-taxonomy unit-artifact
  locus). Ready for `/glados:implement-feature` (slice 1 = routing.json loopback proof + live-bug fix).

## Session Log (implement phase)

- **2026-06-13** — `/glados:implement-feature` started (branch `plan/managed-host-provisioning-engine`,
  commit `78687a8`). Capabilities: conga MCP + AWS SSM (live isolated-agent verify), `gh` (provider
  release). Grounded slice 1 in the real code: the AWS Go path **already has** `regenerateRoutingOnInstance`
  (`channels.go:601`, loopback via `common.GenerateRoutingJSON(..., LoopbackWebhookResolver(""))`) and
  `RefreshAgent` already calls it — but `ProvisionAgent` (`provider.go:225`) runs the `add-user.sh.tmpl`
  SSM script (bash `node -e` bridge-form routing + `docker network connect`) and never calls the Go
  path. So slice 1 is a **smaller, lower-risk** fix than the spec implied: strip bash routing from the
  provision scripts + route `ProvisionAgent` through the existing Go loopback reconcile. Drafted
  `tasks.md` with a scope question (does slice 1 also seed `pkg/provider/managedhost`, or defer to
  slice 2?). Paused for breakdown review per workflow.

- **2026-06-13** — **Slice 1 implemented (Option B), code complete + unit-verified.** Files:
  - **New** `pkg/provider/managedhost/`: `transport.go` (`Transport` interface `{PutFile, RunCommand,
    ReadFile}` + `ExecFuncFor`), `routing.go` (`WriteRoutingJSON`), `transport_test.go` (in-memory
    fake + 3 tests).
  - **New** `pkg/provider/awsprovider/transport.go`: `ssmTransport` adapter (`var _ managedhost.Transport`).
  - **Mod** `pkg/provider/awsprovider/channels.go`: `regenerateRoutingOnInstance` refactored through
    the seam (`managedhost.WriteRoutingJSON`); managedhost import.
  - **Mod** `pkg/provider/awsprovider/provider.go`: `ProvisionAgent` now reconciles routing (Go
    loopback) + restarts the router after the provision script (non-fatal, mirrors `RefreshAgent`).
  - **Mod** scripts `add-user.sh.tmpl`, `add-team.sh.tmpl`, `refresh-user.sh.tmpl`,
    `refresh-all.sh.tmpl`: stripped bash routing (`node -e`) + bridge attach (`docker network
    connect conga-router`) + unit `ExecStartPost` connect; refresh-all now *deletes* deprecated
    connect lines from old units.
  - **Mod** `scripts/scripts_test.go`: `TestProvisionScriptsDropBridgeRouterWiring` regression guard.
  - Verification: `go build`/`vet`/`gofmt -l`/`go test ./...` all clean/pass. T1.6 live verify + T1.7
    provider release DEFERRED to verify/release phase (deployed path needs the `pkg/` release first).
  - Pattern-observer: logged a `preferred` philosophy (logic in tested Go behind thin seams, not
    templated bash) to `product-knowledge/observations/observed-philosophies.md` (pending).

- **2026-06-13** — **opus 4.8 fleet default** (commit `4b488c2`). Operator asked whether the engine
  change prohibits operator model control → it's the opposite: AWS is the one place "provide the
  model" is broken (static bash heredoc ignores both the canonical default and the per-agent
  `agent.yaml` overlay); slice 2 fixes that. Bumped `claude-opus-4-7`→`claude-opus-4-8` in all 6
  active config locations (live + embedded canonical JSON, 2 add scripts, 2 boot-tftpl sections) +
  tests/comment/example. Per-agent override remains via `agent.yaml model:`. build/vet/gofmt/suite clean.
- **2026-06-13** — **Slice 2 grounded; deliberately paused before the production change.** Found the
  Go config-gen method **already exists + is proven**: `regenerateAgentConfigOnInstance`
  (`channels.go:468`), used by `RefreshAgent` step 1. `ProvisionAgent` is the holdout on the bash
  heredoc — same shape as slice 1 — BUT with an ordering constraint: `add-user.sh.tmpl` generates the
  config heredoc AND `systemctl start`s the container in one SSM run, so removing the heredoc needs a
  **provision-flow reorder** (config-on-disk before container start, as `RefreshAgent` does). That's
  the highest-blast-radius change in the feature (every new AWS agent's boot). Recorded Path A (the
  reorder, recommended) + Path B (safe stepping-stone) + tasks in `tasks.md`. Stopped here rather than
  rush the production first-provision flow at the end of a long session — resume slice 2 deliberately.

- **2026-06-13** — **Slice 2a implemented: Go config authoritative on first provision.** After
  reading `RefreshAgent` end-to-end (regenerate Go config → refresh-user.sh unit+restart with
  `ExecStartPost` iptables → routing → egress policy — all proven), wired `ProvisionAgent` to call
  `RefreshAgent` (non-fatal) after the bash provision. AWS agents now run the Go-generated config
  (canonical model + per-agent `agent.yaml` overlay) from first provision, not just after a later
  `conga refresh`. Subsumes slice 1's standalone `ProvisionAgent` routing calls (RefreshAgent step 3).
  Mod: `pkg/provider/awsprovider/provider.go`. build/vet/gofmt/`go test ./...` clean. **2b (heredoc
  physical removal) is now de-risked cleanup** — the Go config overwrites the heredoc on every
  provision; removal also drops `systemctl start` from the scripts (RefreshAgent does first start).
  Live verify release-gated.

- **2026-06-13** — **Live-verified slices 1 + 2a + opus-4.8 on `aaron` (no release).** Built the
  branch `./bin/conga` and ran `conga refresh --agent aaron` against the live AWS fleet (host
  `i-024bf3a55563f9e88`) — confirming a public provider release is NOT needed to test (the CLI binary
  + SSM-pushed embedded scripts exercise the branch `pkg/` code directly). Before→after on the host:
  model.primary `claude-opus-4-7`→**`claude-opus-4-8`** (qwen36 subagent overlay **preserved** in the
  models allowlist — Go gen honors per-agent overlays); `$include` 3-layer array intact; `routing.json`
  stayed loopback (no regression from the slice-1 seam refactor); **unit bridge-attach count 1→0**
  (slice-1 strip cleaned the deprecated `network connect conga-router` ExecStartPost on unit rewrite);
  container returned **ready** (clean restart). Covers the RefreshAgent path (= the substance of
  slices 1/2a/model-bump); a fresh `add-user` (ProvisionAgent→RefreshAgent wiring + stripped add-user.sh
  on a new agent) is the remaining live check, deferred to `/glados:verify-feature` (throwaway agent).
  `aaron` left on opus-4.8 (desired); egress in `validate` (local-policy redeploy during refresh).

- **2026-06-13** — **Fresh-provision path live-verified (throwaway `slice2test`, no release).** Built
  `./bin/conga` from the branch, `admin add-user slice2test` (gateway-only) on the live fleet. Host
  checks: openclaw.json model.primary `anthropic/claude-opus-4-8` (Go gen on first provision),
  `$include` 3-layer array, `hasChannels:false`; systemd unit **bridge-attach count 0** + **egress
  `ExecStartPost` iptables present** (the unified refresh-user unit — slice 1 strip + slice 2a
  `ProvisionAgent`→`RefreshAgent`); routing.json had no `conga-router`/bridge refs; container running.
  Then **fully torn down**: `remove-agent --force --delete-secrets` (container/unit/egress/SSM param
  gone; roster back to the 6 real agents) + manual sweep of the persisted data dir + `config/slice2test*`
  leftovers (data preserved by default per Agent Data Safety — expected). Slices 1 + 2a now verified on
  **both** the refresh path (aaron) and the fresh-provision path (slice2test).

- **2026-06-13** — **Slice 2b implemented + live-verified: provision scripts are infra-only.**
  `add-user.sh`/`add-team.sh` stripped of the openclaw.json heredoc + `jq $include` + baseline + unit
  creation + `systemctl start` + imperative iptables — now infra-only (env, data dir, metadata,
  behavior, network, egress proxy). RefreshAgent owns config+unit+start+iptables+routing;
  `ProvisionAgent`'s RefreshAgent call flipped to **fatal**. Tests: rewrote add-user/add-team render
  tests to infra-only; replaced `assertOpenClawV5Shape` with `TestProvisionScriptsAreInfraOnly` +
  `TestGenerateConfig_GatewayV5Shape` (Go generator now owns the gateway v5 shape). **Throwaway live
  test (`slice2btest`) caught a real regression**: refresh-user.sh never `systemctl enable`d the unit,
  so a 2b-provisioned agent ran but was `disabled` (no reboot survival — breaks the unattended
  managed-host guarantee). Fixed (refresh-user.sh now enables, idempotent) + regression assertion;
  re-verified `enabled` + `running` live, then torn down clean (roster back to 6). `go test ./...`
  (21 pkgs)/vet/gofmt green. Audit #2 (heredoc dedup) + #8 (unit divergence) retired on the add path.

- **2026-06-13** — **Slices 4+3 engine core built (operator chose to build them together).** Pure Go,
  unit-tested, **zero production wiring** (the AWS path still uses refresh-user.sh until the swap, so
  no production risk yet). New in `pkg/provider/managedhost/`: `supervisor.go` (`ServiceSpec` +
  `LifecycleHooks` + `RestartPolicy` + `HostSupervisor` interface + `systemdSupervisor` with
  RenderUnit/DefineService[daemon-reload+**enable**]/Start/Stop/Restart/Remove/Status + reserved
  `openrcSupervisor` stub returning `ErrUnsupportedSupervisor`); `network.go`
  (`PlanAgentNetwork` → deterministic `10.99.<idx>.0/24`, collision-free vs VPC `10.0.0.0/24` + Docker
  `172.x` — slice 3); `guard.go` (`ReservedKeyGuardScript`, fail-closed reserved-key PreStart guard
  generated from `common.ReservedCustomConfigKeys`, WARN+allow on JSON5 — integrity #2). 5 unit tests
  (network/guard/RenderUnit/enable-regression/openrc-reserved). build/vet/gofmt + `go test ./...`
  (21 pkgs) green. **Next: increment B — the production swap** (network `--subnet`/`--ip`, replace
  refresh-user.sh's bash unit with the Go supervisor, deploy the guard, live-verify on a throwaway).

- **2026-06-13** — **Increment B split into 3 live-verified steps** (operator decision: not a big-bang
  B1–B5; converge the agent container to `--env-file` when the Go RunCmd lands in step B-2). Grounding
  the production swap surfaced that "step 1 = small bash change" understated it — determinism is
  duplicated across `refresh-user.sh`, `deploy-egress.sh`, `add-user.sh`, `add-team.sh`, and most of it
  gets rewritten in Go in step B-2. So **B-1 was scoped tightly to `refresh-user.sh` + its Go wiring** —
  the primary agent lifecycle path (provision→refresh, refresh, reboot via the unit's `ExecStartPost`).
  **B-1 implemented, code-complete + unit-verified:**
  - **Mod** `pkg/provider/awsprovider/provider.go`: `RefreshAgent` computes
    `managedhost.PlanAgentNetwork(agent.GatewayPort, common.BaseGatewayPort)` + `iptables.AddRulesCmd`/
    `RemoveRulesCmd` and threads `{SubnetCIDR, GatewayIP, AgentIP, IptablesAddCmd, IptablesRemoveCmd}`
    into the refresh template (added `iptables` + `managedhost` imports).
  - **Mod** `scripts/refresh-user.sh.tmpl`: ExecStart binds `--ip {{.AgentIP}}`; unit
    `ExecStartPost`/`ExecStopPost` are the deterministic Go-generated `/bin/bash -c '<AddRulesCmd>'`/
    `'<RemoveRulesCmd>'` (retiring the two 10-retry `docker inspect` blocks); the network create becomes
    a subnet-migration block (recreate with `--subnet/--gateway` on mismatch — `systemctl stop` first so
    `Restart=always` can't race the `network rm`; steady-state refresh is a no-op → no egress gap); the
    inline post-restart iptables loop is now the deterministic one-liner.
  - **Mod** `scripts/scripts_test.go`: `refreshUserData` helper renders through the **real**
    `PlanAgentNetwork`/`AddRulesCmd`/`RemoveRulesCmd`; asserts `--ip 10.99.2.2`, subnet create, the
    deterministic DROP rule, and the **absence** of the discovery loop (`for i in $(seq 1 10)` +
    `NetworkSettings.Networks`).
  - **Collision-safety reasoning (why no proxy `--ip` pin needed this step):** the agent always binds
    explicit `--ip .2`; the egress proxy is only ever (re)created by `deploy-egress` *after* the agent is
    up (RefreshAgent step 4), so it takes `.3`; on reboot the proxy (`--restart always`, not recreated)
    keeps its IP — no race for `.2`. So `add-user`/`add-team`/`deploy-egress` are untouched this step.
  - **DNS equivalence confirmed safe:** the Go `AddRulesCmd` omits the bash's explicit port-53 RETURN
    rules, but the **remote provider runs this exact rule set in production** (`remoteprovider`
    `addEgressIptablesRules` → `iptables.AddRules`) — the embedded resolver is loopback `127.0.0.11`,
    never traversing DOCKER-USER. B-1.5 live-verify still checks DNS+egress explicitly.
  - build/vet/gofmt + `go test ./...` green; rendered unit eyeballed (well-formed, literal IPs, correct
    `/bin/bash -c` single-quoting).

- **2026-06-13** — **B-1.5 live-verified on a throwaway (`b1test`, no release).** Built branch
  `./bin/conga`, `admin add-user b1test` (gateway-only, port 18796 → subnet idx 7) on the live fleet.
  Host (SSM `i-024bf3a55563f9e88`): container `--ip 10.99.7.2`, network `10.99.7.0/24 gw=10.99.7.1`,
  unit `ExecStart` binds the static IP, `ExecStartPost`/`ExecStopPost` carry the deterministic Go
  iptables (literal IP — no discovery loop, residue count 0), unit **enabled+active**. **Functional
  egress proof:** DNS resolves (`api.anthropic.com`→`2607:6bc0::10`); proxied request connects
  (`PROXY OK 401`); **direct non-proxy egress BLOCKED by the DROP rule** (`DIRECT BLOCKED (timeout)`).
  The throwaway provision exercised the subnet-migration path end-to-end (add-user auto-subnet →
  ProvisionAgent→RefreshAgent migration to 10.99.7.x). Operator opted **not** to also migrate a real
  agent (`aaron`) this pass. Torn down clean (roster back to 6; swept data/config + 2 orphan DNS RETURN
  rules). **B-2 cleanup note logged:** `deploy-egress.sh` still adds port-53 DNS RETURN rules that
  `RemoveRulesCmd` doesn't clean (harmless orphans after teardown — the IP is gone); reconcile when
  `deploy-egress`'s discovery-loop iptables is retired in B-2. **Step B-1 complete. Next: B-2** (replace
  the bash unit-gen with the Go `systemdSupervisor`, converge to `--env-file`).

- **2026-06-13** — **Step B-2 core implemented: bash unit-gen replaced by the Go systemd engine
  (audit #8); code-complete + unit-verified.** `refresh-user.sh.tmpl` **deleted** — `RefreshAgent`
  step 2 now builds the agent's docker-run command + systemd unit entirely in Go and applies them via
  the `ssmTransport`. New/changed:
  - **New** `pkg/provider/managedhost/container.go` — `AgentContainer` (the shared container-arg
    builder the spec called for) → `Args()` argv + `SystemdExecStart(argv)`, which double-quotes any
    whitespace-bearing arg. That's the load-bearing detail: the `NODE_OPTIONS=… --require …` value must
    be one systemd arg or systemd would split it and docker would see a stray `--require` flag (the
    exact thing the old bash unit's `NODE_OPTIONS="…"` quoting handled). Secrets travel via
    **`--env-file`** (the operator's B-2 convergence decision), never inline `-e KEY=VALUE` (#9627).
  - **New** `pkg/provider/awsprovider/engine.go` — pure `buildAgentServiceSpec(agent, image, region)`
    assembles the `ServiceSpec` (After/Requires, PreStart=[pre-start.sh, `-docker rm -f`,
    `-…plugins install @openclaw/slack`], ExecStart via the builder, PostStart/PostStop = the B-1
    deterministic iptables, ExecStop, LogTarget, Restart=always/10, **no EnvironmentFile**);
    `defineAndStartAgentService` fetches the image (SSM `/conga/config/image`), runs the Go
    network-migration command (same recreate-on-subnet-mismatch logic as B-1, now in Go), then
    `systemdSupervisor.DefineService` (write + daemon-reload + enable) + `Restart`.
  - **Mod** `pkg/provider/awsprovider/provider.go` — `RefreshAgent` step 2 calls
    `defineAndStartAgentService` (was: render+exec `RefreshUserScript`); dropped the now-unused
    `iptables`/`managedhost` imports + stale comments.
  - **Mod** `pkg/provider/awsprovider/transport.go` — `ssmTransport.RunCommand` ceiling 30s→**120s**:
    `systemctl restart` blocks on the unit's plugin-install ExecStartPre, which runs full npm install
    (~30-60s) on a fresh agent's empty data dir. The timeout is a completion ceiling (fast commands
    still return immediately) and the payload stays a tiny command string, so SSM discipline holds.
  - **Mod** `pkg/provider/managedhost/supervisor.go` — added exported `RenderSystemdUnit(spec)` (wraps
    the backend's RenderUnit) for equivalence testing + future effective-config views.
  - **Deleted** `scripts/refresh-user.sh.tmpl` + `RefreshUserScript` embed + the scripts_test refs;
    `provider_test.go` step-2 snippet updated. **New** `engine_test.go` unit-equivalence test asserts
    every directive the bash unit carried + the B-1 determinism, and the **absence** of
    `EnvironmentFile=`, inline secrets, the `seq 1 10` discovery loop, `NetworkSettings.Networks`,
    `sed -i`, and `docker network connect`. `container_test.go` covers the builder + the NODE_OPTIONS
    quoting hazard.
  - build/vet/gofmt + `go test ./...` green; rendered unit eyeballed — faithful equivalent of the
    retired bash unit with the `--env-file` convergence.
  - **Remaining in B-2:** `deploy-egress.sh` reconciliation (drop the dead `docker network connect
    conga-router`; drop the now-dead `sed -i` unit injection — safely skipped against the Go unit since
    its `grep -q HTTPS_PROXY` guard matches; make its iptables deterministic to stop orphaning the
    port-53 rules). Non-blocking for the B-2 live verify (the sed is a no-op against the Go unit).
    **Next gate: B-2 live verify on a throwaway** (Go unit on host, `--env-file`, deterministic egress,
    enabled+running, DNS+egress functional, direct egress blocked), release-gated like prior slices.

- **2026-06-13** — **B-2 core live-verified on a throwaway (`b2test`, no release).** Provisioned
  `b2test` (port 18796 → 10.99.7.2) with the rebuilt branch binary. The deployed
  `/etc/systemd/system/conga-b2test.service` is **exactly** the Go-built unit — `EnvironmentFile=`
  count 0, `--env-file` present (count 1), `--ip 10.99.7.2`, `-p 127.0.0.1:18796:18789`, egress proxy
  env + the double-quoted `NODE_OPTIONS`, deterministic `ExecStartPost`/`StopPost` iptables; container
  running on 10.99.7.2; unit enabled+active. refresh-user.sh is gone, so the unit could only have come
  from the Go systemdSupervisor — the swap is confirmed on a real host. Functional egress proof
  unchanged from B-1.5: DNS resolves, proxied egress connects (`PROXY OK 401`), direct egress BLOCKED.
  Torn down clean (roster back to 6; data/config/orphan-iptables swept). **Step B-2 core complete.**
  Remaining: B2.4 `deploy-egress.sh` reconciliation (dead router-connect + dead sed + deterministic
  iptables) — non-blocking polish; then Step B-3 (PreStart reserved-key guard).

- **2026-06-13** — **B2.4 + B-3 implemented & live-verified; a real AWS DNS regression caught and
  fixed.**
  - **B2.4 (`deploy-egress.sh`):** removed the pre-restart iptables discovery/removal, both `sed -i`
    unit injections (kept the unit-exists check), the post-restart 10-retry iptables block, the dead
    `docker network connect conga-router`, and the unused `AGENT_CONTAINER`. Egress is now owned by the
    Go unit's deterministic `ExecStartPost`/`ExecStopPost`; deploy-egress recreates the proxy +
    `systemctl restart`s the agent, which cycles the rules. Test renamed to
    `TestDeployEgressScriptDelegatesEgressToUnit` (asserts restart + absence of the old machinery).
  - **B-3 (reserved-key guard):** `defineAndStartAgentService` PutFiles the generated guard
    (`managedhost.ReservedKeyGuardScript`, 0755) and wires it as the 2nd PreStart hook (after
    `pre-start.sh` syncs includes, before container create), fail-closed (no leading `-`).
    `remove-agent.sh.tmpl` now removes the per-agent guard script too. `engine_test.go` asserts the
    order + fail-closed.
  - **DNS regression (the important catch):** the throwaway came up with exactly the 3 `AddRulesCmd`
    rules and **DNS failed** — Docker's embedded resolver forwards to the VPC resolver *outside* the
    per-agent subnet, so that query (sourced from the container IP to a non-subnet addr) hit the DROP.
    The old deploy-egress discovery loop had always silently added the port-53 RETURN rules; b1/b2 only
    passed because of it. Removing deploy-egress's iptables exposed the gap. **Fix:** added udp/tcp
    `--dport 53 -j RETURN` to the shared `iptables` rule set (`egressRuleSpecs` →
    `AddRulesCmd`/`RemoveRulesCmd`/`CheckRulesCmd`); the unit's `ExecStartPost` now supplies DNS
    deterministically and the `RemoveRulesCmd` symmetry retires the B-1.5 orphan-DNS rules. This
    corrects the B-1.5 README note's premise: the explicit DNS rules are **required** on AWS (not
    legacy). `rules_test.go` updated (5 rules; DROP first/bottom).
  - **Live verify (throwaway `b3test`):** guard deployed 0755 + 2nd PreStart; after the fix
    DOCKER-USER = 5 rules in order and **DNS resolves**; the deployed guard exits 1 + FATAL on an
    injected `{"channels":…}`, exits 0 + WARN on a JSON5 comment, exit 0 on `{}`. Torn down clean —
    **iptables orphan-free** (the new `RemoveRulesCmd` covers DNS). build/vet/gofmt + `go test ./...`
    green. **Slices 4+3 production swap (B-1, B-2, B2.4, B-3) complete.** Optional remaining: B5
    (slim the periodic integrity backstop). Then slice 5 (boot reduction) / slice 6 (remote systemd).

## Spec Review & Standards Gate (pre-implementation)

### Persona Review
- **Architect** — APPROVE. Generalizes the shipped `iptables.ExecFunc` seam (consistent); no new
  external dep; `ServiceSpec` in-memory (no schema change); engine in `pkg/`, transport in providers;
  `Provider` interface unchanged. **Flag:** §11 boot reconstitution is novel — PoC/spike before slice 5.
- **Product Manager** — APPROVE. Why/Who clear; scope fenced (no merge/visibility/pull/OpenRC);
  user-visible wins (slice 1 Slack delivery, slice 6 VPS reboot). **Flag:** batch slices into
  provider-release checkpoints (two-repo tax).
- **QA** — APPROVE after 2 amendments (now in spec): guard-on-unparseable (§8) and partial-failure
  idempotency / no half-egress (§12.1); tests added (§14).

### Standards Gate Report
| Standard | Severity | Verdict |
|---|---|---|
| Agent Data Safety (architecture.md) | must | ✅ PASSES (§13) |
| Interface Parity (architecture.md) | must | ✅ PASSES (§15 — no new surface) |
| Module Structure (architecture.md) | must | ✅ PASSES (§5) |
| Own the box, not behavior (security.md P8) | must | ✅ PASSES (on-mission) |
| Immutable config (security.md P2) | must | ✅ PASSES (perms preserved) |
| Channel allowlist = boundary (security.md) | must | ✅ STRENGTHENED (preventive guard) |
| Egress iptables all-modes fail-closed (egress-controls.md) | must | ✅ STRENGTHENED (static IP) |
| Secrets via env / #9627 (security.md P5) | must | ✅ PASSES |
| Detect what you can't prevent (security.md P6) | should | ✅ PASSES (slimmed backstop) |
| Channel abstraction (architecture.md) | should | ✅ PASSES |
| Config taxonomy (config-taxonomy.md) | should | ✅ PASSES (no new locus) |

**Non-blocking:** ⚠️ egress-controls.md doc-sync when the bash egress parser is deleted (slice 5);
ℹ️ confirm config-taxonomy needs no "generated unit artifact" entry.

**Gate decision: PASS** — all `must` standards pass; 2 items logged, neither blocking.

## Next Step

`/glados:implement-feature` — start with **slice 1** (routing.json loopback via the engine: the proof
slice + live-bug fix for audit #1), landing the `Transport` interface + `pkg/provider/managedhost`
skeleton + fake-transport tests, then proceed slice by slice. Reminder: `pkg/` change →
`terraform-provider-conga` release required (batch slices into release checkpoints).
