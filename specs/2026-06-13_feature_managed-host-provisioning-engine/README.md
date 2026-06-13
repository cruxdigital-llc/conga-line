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
