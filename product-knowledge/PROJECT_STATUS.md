# GLaDOS System Status

This document reflects the *current state* of the codebase and project.
It should be updated whenever a significant change occurs in the architecture, roadmap, or standards.

## Project Overview
**Mission**: Hardened, per-user-isolated deployment of OpenClaw via pluggable providers. See [MISSION.md](MISSION.md).
**Current Phase**: Active Development

## Architecture
Pure infrastructure project — no application code. Go CLI + Terraform deploying OpenClaw as Docker containers via pluggable providers.

- **Provider model**: CLI uses `Provider` interface with implementations for AWS, local Docker, and remote (SSH)
- **AWS**: Single EC2 host with per-agent Docker containers, SSM access, Secrets Manager, zero ingress (~$10/mo)
- **Local**: Per-agent Docker containers on local machine, file-based secrets, no cloud services
- **Remote**: Per-agent Docker containers on any SSH host (VPS, bare metal, RPi), file-based secrets (~$5-10/mo)
- **Common**: Per-agent network isolation, optional Slack router, cap-drop ALL hardening

See [TECH_STACK.md](TECH_STACK.md) for full details.

## Current Focus

### 1. MVP Planning — 2 Users via Slack
*Lead: Architect*
- [x] **Mission defined**: `product-knowledge/MISSION.md`
- [x] **Security standards defined**: `product-knowledge/standards/security.md`
- [x] **Roadmap defined**: `product-knowledge/ROADMAP.md`
- [x] **Tech stack defined**: `product-knowledge/TECH_STACK.md`
- [x] **Epic 0**: Terraform foundation (S3 state + DynamoDB locks) — complete
- [x] **Epic 1**: VPC + networking — complete (31 resources)
- [x] **Epic 2**: IAM + secrets — complete (5 secrets populated)
- [x] **Epic 3**: EC2 + Docker bootstrap — complete, Slack connected, end-to-end verified
- [x] **Epic 4**: Config integrity + monitoring — complete (timer + CW agent + alarm)
- **Milestone**: Aaron's local gateway replaced by AWS deployment
- [x] **Epics 5+6**: Multi-user onboarding + Slack router — complete

### 2. Conga Line CLI — ✅ Complete
- [x] All 11 phases implemented and verified. See `specs/2026-03-18_feature_cruxclaw-cli/`

### 3. SSM-Driven Bootstrap Discovery — Specified, Ready for Implementation
*Lead: Architect + QA*
- [x] Requirements defined: `specs/2026-03-19_feature_ssm-driven-bootstrap-discovery/requirements.md`
- [x] Plan defined: `specs/2026-03-19_feature_ssm-driven-bootstrap-discovery/plan.md`
- [x] Spec defined: `specs/2026-03-19_feature_ssm-driven-bootstrap-discovery/spec.md`
- [x] Persona review passed (Architect + QA)
- [x] Standards gate passed (1 warning: IAM widening, accepted)
- [ ] Step 1: Unified SSM namespace (`/conga/agents/`) + config params
- [ ] Step 2: Widen IAM secrets policy for dynamic agents
- [ ] Step 3: Rewrite bootstrap for SSM discovery + update router.tf + CLI changes
- [ ] Step 4: Verify CLI compatibility + migration

### 4. CLI Hardening — Verified Complete
*See `specs/2026-03-19_feature_cli-hardening/` for full trace*
- Remaining deferred items: CLIContext struct migration, params_test.go, agent_test.go, executor command handler migration

### 5. Behavior Management — Verified Complete
*See `specs/2026-03-20_feature_behavior-management/` for full trace*

### 6. Conga Line Rename — Verified Complete
*See `specs/2026-03-20_feature_conga-line-rename/` for full trace*

### 7. Modular Deployment — Verified Complete
*See `specs/2026-03-21_feature_modular-deployment/` for full trace*

### 8. Agent Pause / Unpause — Verified Complete
*See `specs/2026-03-21_feature_agent-pause/` for full trace*

### 9. Remote Provider (formerly VPS) — ✅ Verified Complete, E2E Tested on Raspberry Pi
*See `specs/2026-03-22_feature_vps-provider/` for full trace*
- Full lifecycle verified on Raspberry Pi (Debian 13, ARM64, 905MB RAM): setup, add-user, status, logs, secrets, connect (SSH tunnel, HTTP 200), pause, unpause, teardown
- 3 bugs found and fixed during integration testing (SSH auth ordering, first-time setup flow, non-root sudo)
- [x] Phase 1: SSH foundation (`ssh.go`)
- [x] Phase 2: Docker helpers (`docker.go`)
- [x] Phase 3: Core provider + agent lifecycle (`provider.go`)
- [x] Phase 4: Secrets + integrity (`secrets.go`, `integrity.go`)
- [x] Phase 5: Setup wizard (`setup.go`)
- [x] Phase 6: Config + wiring (`config.go`, `root.go`, `go.mod`)
- [x] Phase 7: Documentation

### 10. CLI JSON Input — Verified Complete
*See `specs/2026-03-23_feature_cli-json-input/` for full trace*

### 12. Portable Policy Schema — ✅ Verified and Complete
*See `specs/2026-03-25_feature_policy-schema/` for full trace*

### 13. Egress Domain Allowlisting — ✅ Verified and Complete
*See `specs/2026-03-25_feature_egress-allowlist/` for full trace*

### 14. Channel Abstraction — Verified Complete
*See `specs/2026-03-26_feature_channel-abstraction/` for full trace*


### 15. MCP Policy Tools — ✅ Verified and Complete
*See `specs/2026-03-26_feature_mcp-policy-tools/` for full trace*

### 16. Network-Level Egress Enforcement — ✅ Verified and Complete
*See `specs/2026-03-26_feature_network-level-egress-enforcement/` for full trace*
- Phase 3 (AWS) completed as part of Feature #18 (Portable Egress Policy Compliance)

### 17. Channel Management CLI — ✅ Verified and Complete
*See `specs/2026-03-27_feature_channel-management-cli/` for full trace*

### 18. Portable Egress Policy Compliance — Verified Complete
*Lead: Architect + QA*
*See `specs/2026-03-28_feature_portable-egress-policy-compliance/` for full trace*
- [x] Requirements defined
- [x] Plan defined
- [x] Spec defined
- [x] Persona review passed (PM + Architect + QA)
- [x] Standards gate passed (2 warnings, 0 violations)
- [x] Phase 1: Default mode change — normalize empty to `enforce` (security-first)
- [x] Phase 2: Remote provider — respect `mode` field in ProvisionAgent, RefreshAgent, ensureEgressIptables
- [x] Phase 3: AWS bootstrap — parse `mode` in generate_egress_conf(), add iptables DROP rules + systemd hooks
- [x] Phase 4: Enforcement report — unify egressReport() to be mode-driven for all providers
- [x] Phase 5: Tests & documentation updates — all 17 packages pass

### 19. Non-Root Container Enforcement — Verified Complete
*Lead: Architect + QA*
*See `specs/2026-03-29_feature_non-root-containers/` for full trace*
- [x] Requirements defined: `specs/2026-03-29_feature_non-root-containers/requirements.md`
- [x] Plan defined: `specs/2026-03-29_feature_non-root-containers/plan.md`
- [x] Spec defined: `specs/2026-03-29_feature_non-root-containers/spec.md`
- [x] Persona review passed (Architect + QA)
- [x] Standards gate passed (8/8 checks clear, pre and post implementation)
- [x] Phase 1: Agent containers — `--user 1000:1000` across all providers (6 files)
- [x] Phase 2: Router containers — `--user 1000:1000` across all providers (3 files)
- [x] Phase 3: Security documentation update

### 20. Manifest Bootstrap — ✅ Verified and Complete
*See `specs/2026-03-30_feature_manifest-apply/` for full trace*
- `conga bootstrap` is additive-only, no state management. Policy section seeds `conga-policy.yaml` only on first run (existing policy file takes precedence).

### 21. Terraform Provider — Planned (Future Roadmap)
*Lead: Architect + PM + QA*
*See `specs/2026-03-30_feature_terraform-provider/` for full trace*
- [x] Requirements defined
- [x] Plan defined (resource model, architecture, implementation phases)
- [ ] Spec (deferred — implement when enterprise use case materializes)
- [ ] Implementation (8 phases: skeleton → core resources → channels → policy → data sources → import → tests → registry)

### 22. Model Routing — Planned (Future Roadmap)
*See `specs/2026-03-27_feature_model-routing/` for full trace*
- [x] Schema defined (`RoutingPolicy` in `pkg/policy/policy.go`)
- [x] Validation implemented
- [x] MCP tool scaffolding (`conga_policy_set_routing`)
- [ ] Spec (deferred — requires Bifrost integration design)
- [ ] Implementation (model selection logic, sidecar proxy, cost limits enforcement)

### 23. Agent Portability — Verified (Local Provider Complete)
*Lead: Architect + QA + PM*
*See `specs/2026-04-05_feature_agent-portability/` for full trace*
- [x] Requirements, plan, spec, persona review, standards gate
- [x] Phase 1-5: Runtime interface, OpenClaw extraction, local provider wiring, Hermes runtime, CLI changes
- [x] Phase 6: 38 runtime tests, all 16 test suites pass, go vet clean, gofmt clean
- [x] Verification: automated + persona + standards gate (post-impl) + spec retrospection
- [ ] Phase 6 (remaining): Remote & AWS provider integration
- [ ] Phase 6 (remaining): Routing webhook path parameterization for mixed-runtime Slack delivery
- [ ] Phase 1: Runtime interface & registry (`pkg/runtime/`)
- [ ] Phase 2: Extract OpenClaw runtime (`pkg/runtime/openclaw/`)
- [ ] Phase 3: Wire local provider to Runtime interface
- [ ] Phase 4: Hermes runtime implementation (`pkg/runtime/hermes/`)
- [ ] Phase 5: CLI & data model changes (`--runtime` flag)
- [ ] Phase 6: Remote & AWS provider integration
- [ ] Phase 7: Testing & verification

### 24. CLI Integration Tests — Verified Complete
*Lead: QA + Architect*
*See `specs/2026-04-07_feature_cli-integration-tests/` for full trace*
- [x] Requirements defined
- [x] Plan defined
- [x] Spec defined
- [x] Implementation: 4 test functions (48 subtests), test helpers, CI job
- [x] Verification: all checks pass, persona + standards gate approved

### 25. Remote Provider Integration Tests — Implemented, Expansion Planned
*Lead: QA + Architect*
*See `specs/2026-04-07_feature_remote-provider-integration-tests/` for full trace*
- [x] Requirements defined
- [x] Plan defined
- [x] Spec defined
- [x] Implementation: 3 test functions (42 subtests), test helpers, CI job
- [x] Verification: all tests pass (44s)

### 26. Remote Integration Test Coverage Expansion — Verified Complete
*Lead: QA + Architect*
*See `specs/2026-04-08_feature_remote-integration-coverage-expansion/` for full trace*
- [x] Requirements defined
- [x] Plan defined
- [x] Spec defined
- [x] Persona review passed (QA + Architect approve)
- [x] Standards gate passed (0 violations, 0 warnings)
- [x] Implementation: 4 test functions (36 subtests), 8 new helpers
  - [x] Phase 1: Error paths (6 subtests)
  - [x] Phase 2: Multi-agent (12 subtests) — port allocation, routing, RefreshAll, isolation
  - [x] Phase 3: Channel management (14 subtests) — AddChannel, Bind, Unbind, Remove
  - [x] Phase 4: Connect / SSH tunnel (4 subtests — ConnectInfo verification)
- [ ] Verification

### 27. Local Model Routing — ✅ Verified Complete (live-tested on AWS)
*Lead: Architect + PM + QA*
*See `specs/2026-05-19_feature_local-model-routing/` for full trace*

Minimal precursor to the planned Bifrost / Model Routing work (#22). Per-agent model override via a new `agents/<name>/agent.yaml` file in the existing overlay directory. Provider-agnostic across local/remote/AWS; supports `ollama` (native, no `/v1`) and `openai` (OpenAI-compatible) providers. Schema versioned (`version: 1`) with strict-key YAML parsing. Allowlist is **additive** — runtime default stays available so operators can `/model` switch mid-conversation.

- [x] Requirements, plan, Phase-0 spike, spec, architect durability deep-dive, persona review, pre-impl standards gate
- [x] Phases 2–5, 7 — types (`pkg/runtime/overlay.go`), loader (`pkg/common/overlay_agent.go`), generator (`pkg/runtime/openclaw/config.go` `applyModelOverlay`), provider wiring (local/remote/aws), docs (README "Per-Agent Model Routing" section, `_example/agent.yaml.example`, `config-taxonomy.md`, CLAUDE.md, ROADMAP cross-link, terraform/README)
- [x] **Live-tested on AWS**: a real production agent now defaults to a self-hosted LLM via LiteLLM, with `/model` switching to/from the runtime default. Two bugs caught and fixed during testing: missing `models[]` array (OpenClaw schema requirement) and clobbering-vs-additive allowlist behavior.
- [x] Post-impl verification: full test suite passes (forced uncached); `go vet` clean; `gofmt` clean; persona review (architect + PM + QA all PASS); post-impl standards gate (0 ❌ violations, 0 ⚠️ warnings — the pre-impl Config Format Boundary warning was resolved as a deliverable).
- [x] Observation logged for `/glados:recombobulate`: runtime config generators need integration tests against the actual runtime schema, not just internal-shape golden assertions.
- [ ] Phase 1 — Image pin bump (`v2026.3.11` → `v2026.5.18`): no longer a hard prerequisite (the older image accepts the rendered config); deferred as desirable-not-blocking.
- [ ] Phase 6 — AWS bootstrap shell: ✂️ SCOPED OUT (overlay consumed at config-gen time on the operator's machine; the `regenerateAgentConfigOnInstance` upload path carries the result).
- [ ] Phase 8 — Provider release (per CLAUDE.md `pkg/` change protocol): operator step, post-merge.

### 28. OpenClaw Upgrade Latest — Code Landed + Migration Applied, AWS Bootstrap Auth Deferred
*Lead: Architect + QA*
*See `specs/2026-05-21_feature_openclaw-upgrade-latest/` for full trace*

Bump the OpenClaw image pin `v2026.3.11` → `v2026.5.18`. Upstream Slack
socket-mode regression (`openclaw/openclaw#45311`) that gated the pin
is CLOSED — fix shipped in `v2026.3.22` via PR #45953. Scope **expanded
during live S3 smoke** — the new image enforces two safety checks the
old one didn't, both caught only by booting the container with our
rendered config: (1) `gateway.mode=remote` is rejected without
`--allow-unconfigured` — fixed by switching to `mode=local` (the
documentation in CLAUDE.md was wrong about which knob controls
0.0.0.0 binding; it's `gateway.bind`, not `gateway.mode`), and (2) a
non-loopback gateway is rejected without auth — fixed by generating a
token at provision time in both local and remote ProvisionAgent paths
(was a latent security gap; old image silently allowed it).
- [x] Requirements defined
- [x] Plan defined
- [x] Spec defined — 5 phases
- [x] Persona review passed (Architect + QA)
- [x] Pre-implementation standards gate passed (caught 9 additional
  hardcoded locations; all absorbed into Phase 1)
- [x] **Phase 0**: upstream changelog audit complete —
  `changelog-review.md` published; 0 Blocking entries across 35+
  releases / 7,173 lines; only `### Breaking` section in window was
  BlueBubbles iMessage removal (irrelevant)
- [x] **Phase 1**: single commit `685649e` on `chore/upgrade-openclaw`
  — 14 files / ~20 line edits across A. defaults, B. docs/standards,
  C. tests/CI; `go build/vet/gofmt` clean, full test suite passes
- [x] **Phase 3 (S3 only, local provider, live)**: live smoke uncovered
  two latent compatibility issues with the new image — applied
  migrations (gateway.mode=local + provision-time gateway token); S3
  re-verified clean. AWS bootstrap auth gap deferred (separate fix —
  user-data heredocs need bash-side token generation).
- [ ] **Phase 3** for S1/S2 (Slack), S4 (model overlay), S5 (egress) —
  operator task on real environments
- [ ] **Phase 3 follow-up**: AWS user-data heredocs need
  `gateway.auth.token` injection so first-boot agents satisfy the new
  image's non-loopback-bind-requires-auth check
- [ ] **Phase 4**: opt-in per-environment rollout — operator task
- [ ] **Phase 4**: opt-in per-environment rollout — operator task
- [ ] **Phase 5**: docs/memory hygiene + 7-day soak window before any
  fast-follow or dependent feature work

### 29. Delegation Routing — ✅ Verified Complete (live-tested on AWS)
*Lead: Architect + PM + QA*
*See `specs/2026-05-22_feature_delegation-routing/` for full trace*

Two-tier delegation. **Tier 1** (subagents, ephemeral, in-runtime):
v2 `agent.yaml` `subagents:` block translates to OpenClaw's
`agents.defaults.subagents` / Hermes' `delegation:`; runtime owns
the spawn decision via `sessions_spawn` / `delegate_task` tools.
**Tier 2** (role agents, persistent): 5 role packages under
`agents/_defaults/<runtime>/role-*/` (Ops/Data/Research on Qwen,
Code-Dev/Writing on Opus + Qwen subagent), provisioned via
`conga admin add-user|add-team --role <slug>` (CLI + JSON + MCP).
No new Conga data-model concept.

- [x] Requirements + Plan + Phase 1 upstream capability check (both
  runtimes confirmed) + Spec (8-phase contract)
- [x] Persona review (Architect + PM + QA APPROVE) +
  pre-impl standards gate (0 violations, 1 warning resolved inline)
- [x] Phase 1 — Schema v2 + `SubagentsOverlay` type + validation
- [x] Phase 2 — OpenClaw generator emits
  `agents.defaults.subagents.{model, delegationMode, maxConcurrent}`
  + merged models allowlist + extended `models.providers`
- [x] Phase 3 — Hermes generator emits `delegation:` block with
  degraded-mode warning for unsupported openai endpoints
- [x] Phase 4 — `CheckOverlayEgress` + `WarnOverlayEgressGaps` +
  per-provider wiring (auto-derive + warn at provision)
- [x] Phase 5 — 10 role packages (5 × 2 runtimes) + integrity test
- [x] Phase 6 — `--role` flag across CLI + JSON schema + MCP tool;
  idempotency preserved
- [x] Phase 7 — Docs (`agent.yaml.example` v2 bump,
  `config-taxonomy.md` worked example #5, `CLAUDE.md` Delegation
  Model section)
- [x] Phase 8 — Verification: full suite green (21 packages), vet
  clean, gofmt clean. **Live-tested on AWS**: all 5 fleet overlays
  migrated to v2 (Opus primary + Qwen subagent on Spark LiteLLM);
  aaron deployed and running on Opus 4.7; deployed openclaw.json
  inspected and confirmed correct (`subagents` block,
  models allowlist, providers).
- [x] **Bonus mid-session**: bumped runtime default Opus 4-6 → 4-7
  (commit `3505f20`). aaron now runs `claude-opus-4-7
  (thinking=medium, fast=off)`.
- [x] No regression on Feature #27 — byte-equality guard
  (`TestGenerateConfig_V2NoSubagentsBlock_IdenticalToV1`) holds.
- [x] `/glados:verify-feature` passed (post-impl standards gate
  PASS, persona verification APPROVE)
- [ ] **Pending**: live chat smoke (Aaron DM → aaron, observe
  subagent spawn in container logs)
- [ ] **Follow-up (task #24)**: lift runtime defaults out of
  `//go:embed` into `agents/_defaults/<runtime>/runtime-defaults.json`;
  fix worktree-vs-parent CWD silent-wrong in
  `resolveAWSBehaviorDir()` / `ResolveOperatorBehaviorDir()`

### 30. Infrastructure-Only Simplification — ✅ Verified Complete (deployed + live-verified on the AWS fleet)
- **Goal**: narrow Conga to infra + a one-time baseline config; let administrators customize each
  agent's `openclaw.json` (e.g. add the Linear MCP server) with edits that **survive restarts/refresh**.
- **Problem**: config generation is stateless full-file regeneration on every
  provision/refresh/restart/bind, so admin edits are wiped. No custom-config injection path exists.
- **Approach C (recommended, live-validated on `aaron`/`2026.5.26`)**: layered config via OpenClaw's
  native `$include`. Conga owns the root `openclaw.json` (regenerated wholesale) with `$include` →
  an admin-owned file edited directly; OpenClaw deep-merges. Confirmed: merges + validates, survives
  restart + hot-reload, **fails closed (never flattens)** on owned-writes, gateway doesn't owned-write
  at startup. Trade-off: in-container `openclaw config set`/`configure` refuses root writes while
  `$include` present (edit the include / use Conga CLI). Beats Approach B (read-merge-write), which
  would strip admin JSON5 comments. Conga's owned footprint is small: `gateway.*`, bound `channels.*`,
  `plugins.entries.*`, `agents.defaults.{model,models,subagents}`, team-discipline keys. ~20 of
  OpenClaw's ~26 sections (mcp, skills, tools.allow/deny, sandbox, memorySearch, cron, hooks, ui,
  browser, …) are fully admin territory.
- **`openclaw` CLI considered (Approach D)**: live-tested `openclaw config patch` — validated,
  version-correct recursive merge with `null`-deletes, runs standalone, but **strips admin JSON5
  comments** and needs in-container exec per change. Verdict: use the CLI for **read-only validation**
  (`config validate`/`schema`) against the exact image, **not mutation**; keep file-templating (C) for
  ownership.
- **Security-relevant**: changes the config-integrity monitor's whole-file-hash contract →
  `security.md` review gating implementation.
- **Status**: `/glados:spec-feature` complete — `spec.md` drafted, persona-reviewed (architect/PM/QA
  APPROVE post-amendment), standards gate **PASSED** (all `must` resolved). Decisions locked: Conga
  owns root + `$include` admin file; root wins on scalar conflicts but objects union (so an include
  can ADD `channels.*` → security finding) → integrity hashes root **+ validates the effective
  channel allowlist**; `conga agent rebaseline` (CLI+JSON+MCP); regenerate-fresh migration;
  `agent-custom.json` read-only to the agent uid. The effective-allowlist control is re-audited at
  the post-implementation security gate. `pkg/` change → provider release.
- **Shipped (2026-06-09)**: implemented across all three providers + boot tftpl + provision scripts;
  full suite + two clean code reviews; **PR #57 merged → congaline v0.0.28 → provider v0.1.6
  (on the Terraform Registry) → terraform pinned to 0.1.6 (PR #58)**. **Deployed + verified on the
  AWS fleet (2026-06-10)**: refreshed all 5 agents (aaron, nvidia-team, nathan, zach,
  nextgen-delivery) through the real `regenerateAgentConfigOnInstance` code path — each got
  `$include` + a `root:root 0444 agent-custom.json`, model overlays + channel bindings preserved,
  integrity baselines re-MATCH, the deployed `check-config-integrity.sh` reports OK for all, and all
  containers came up healthy. **Optional follow-ups** (non-blocking, tracked in `tasks.md`): T5.2
  first-refresh operator advisory, T3.5 in-container allowlist hardening, T6.1/6.2 integration tests.
  See `specs/2026-06-09_feature_infrastructure-only-simplification/`.
- See `specs/2026-06-09_feature_infrastructure-only-simplification/` (`spec.md`).

### 31. Fleet Baseline (+ Per-Agent Declarative) Configuration — ✅ Implemented + Verified (code + live T9.2; PR #61, not merged)
- **Goal**: make custom OpenClaw config (MCP servers, skills, tools) **declarative + version-controlled
  in the repo** at two levels — a **fleet baseline** (all agents) and **per-agent** (`agents/<name>/`)
  — deployed by Conga via `$include` layering, composing with #30's on-host admin-drift `agent-custom.json`.
  Answers both "every agent needs a baseline set" and "configure an MCP server in code" (`agent.yaml` is
  strict-keyed and rejects `mcp`).
- **Approach (anchor)**: extend #30's verified `$include` to an **array** —
  `["fleet-custom.json", "agent-managed-custom.json", "agent-custom.json"]` — Conga deploys the managed
  fleet/per-agent files from committed sources (like behavior files); admin drift file unchanged.
  Layering: defaults → fleet → per-agent-declarative → admin-drift, with the managed root winning on
  Conga-owned keys. **Folds in de-embedding `openclaw-defaults.json`** (so fleet defaults change without
  a binary release).
- **Load-bearing unknown**: `$include`-**array** precedence (which include wins) — live-verify on the
  pinned image, like #30's root-wins check.
- **Status**: `/glados:implement-feature` + `/glados:verify-feature` complete on branch
  `plan/fleet-baseline-configuration` (PR #61, **not merged**). P1–P9 landed: generator `$include`
  array; de-embed w/ embedded fallback (`agents/_defaults/openclaw/openclaw-defaults.json`, Go paths);
  per-provider deploy (local/remote/AWS Go + AWS bash boot/provision); integrity reserved-key guard on
  all 3 layers + hash-verify the 2 managed layers; pre-deploy fail-closed (fleet blast-radius) +
  egress-gap warnings; `conga agent show-config` (layered view, CLI+JSON+MCP); `config-taxonomy.md`
  updated. **Two PR-review passes** (1 blocker + 3 should-fix, all fixed: AWS-refresh managed-baseline
  symmetry, local live-repo resolution, `$include`-target fallback, removal baseline cleanup).
  **Verify-feature**: full `go test ./...` + `go vet` + `gofmt` green; post-impl standards gate PASS
  (caught + fixed an Interface-Parity `json_schema.go` gap for show-config); spec reconciled (§14).
- **T9.2 live-verified** (local Docker, OpenClaw 2026.5.26): union + per-agent>fleet + admin-drift>per-agent>fleet
  merge precedence on a real container; fleet propagation on refresh with baseline rewritten in lockstep;
  pre-deploy fail-closed on a reserved-key fleet source; egress-gap warnings; orphan-baseline cleanup on
  removal; show-config CLI+JSON. Torn down after.
- **Remaining**: **R1** `terraform-provider-conga` release (post-merge, `pkg/` changed); **T2.4** AWS bash
  boot-path de-embed unification (tracked follow-up). See `specs/2026-06-10_feature_fleet-baseline-configuration/`.

### 32. Managed-Host Provisioning Engine (AWS → shared-Go convergence) — ✅ Engine MERGED (PR #67); slices 5/6 pending; reboot-safety hardening spun out → #33
> **Update 2026-06-14**: Slices 1, 2, 4+3 (the production swap: deterministic static-IP network, Go
> systemd unit via `systemdSupervisor`, `--env-file`, fail-closed reserved-key PreStart guard, slimmed
> integrity backstop) are **implemented, live-verified, and merged (PR #67, `1b41c12`)**. AWS agents now
> provision/refresh entirely through the Go managed-host engine; `refresh-user.sh.tmpl` is deleted.
> **Live-fleet migration (2026-06-14) surfaced reboot/restart-safety regressions** → new follow-up
> feature **#33 (Managed-Host Migration Hardening)**. 3 of 6 agents migrated (aaron/nathan/congaline-team
> on `10.99.x`, currently reboot-fragile until #33 lands); 3 held on old auto-subnets. **Slice 5**
> (boot reduction) and **slice 6** (remote systemd) remain. `pkg/` merged → `terraform-provider-conga`
> release still owed before the deployed Terraform path uses it.
- **Goal**: provision AWS agents by running Conga's **shared Go logic** (the code the remote provider
  already uses) over a minimal host transport, eliminating the hand-maintained bash provisioning
  scripts (`scripts/*.sh.tmpl`) and shrinking the 1,384-line boot `user-data.sh.tftpl`. Simplify +
  harden the infra so it can be relied on without heavy maintenance; put logic in Go (tested), not
  untestable templated bash; reuse one engine across managed-host providers.
- **Origin**: `audit/` scope-and-simplification review (2026-06-13), **Theme 3** — the surface of most
  recently chased bugs (router host-networking, `$include` self-heal drift, chown). Operator chose
  the "spirit of Option C, delivered safely" over a literal `AWS = remote-over-SSM` provider merge.
- **Approach (anchor)**: generalize the already-shipped `pkg/provider/iptables` `ExecFunc` seam (pure
  logic behind `type ExecFunc func(cmd string) error`; remote injects SSH, AWS doesn't use it) to all
  host orchestration. Shared logic sits above a minimal `{PutFile, RunCommand}` transport contract;
  the transport (SSM for AWS, SFTP+SSH for remote) is the **only** provider-specific seam. **Not** a
  provider merge and **not** an SSH≡SSM interface unification (real impedance mismatch). Most "derived
  artifacts" (`openclaw.json`, `routing.json`, `$include` layers, Envoy config) are already shared Go
  that AWS re-derives in bash — converging is mostly deletion. AWS transport primitives (`uploadFile`,
  `runOnInstance`) already exist.
- **First slice / live bug**: fix `audit` #1 — AWS add/refresh still write bridge-form routing.json +
  `docker network connect conga-router` + `ExecStartPost`, contradicting the v0.1.8 host-networking
  router migration. Slice 1 = Go-generated loopback routing.json via `PutFile`, dropping the bash.
- **Lifecycle decision RESOLVED**: systemd is THE managed-host lifecycle for **all non-local
  providers** (remote + AWS), generated once in shared Go — unattended, reboot-survivable,
  host-resident egress. Deployment taxonomy: managed-host (systemd: remote+AWS) vs local (Docker, dev).
  This is an *upgrade* for remote (no unattended story today; already requires systemd) and folds in
  audit #7 (static IP → deterministic egress `ExecStartPost`).
- **Three-seams architecture + reserved init seam**: engine talks to three abstractions —
  transport (SSH/SSM), **HostSupervisor** (init system), secrets/discovery. Engine emits a
  provider-agnostic `ServiceSpec`; **systemd is the only built backend**, `openrcSupervisor` reserved
  as a stub so a future lightweight-host (Alpine/OpenRC) scenario is additive. Extension guide:
  `specs/.../extension-host-supervisor.md`. YAGNI guard: build seam + systemd + stub + doc only.
- **Integrity decision RESOLVED (prevention-first)**: reserved-key guard → fail-closed `PreStart`
  hook (key list generated from `common.ReservedCustomConfigKeys`); perms stay; periodic SHA256
  backstop slimmed (drop audit-#4 dual-baseline coupling); preventive control converges across
  managed-host providers, detective/alerting provider-appropriate.
- **Config ownership = Model C + visibility**: `$include` deep-merge + admin-survival stays (code is
  the authoritative *record*, no clobbering reconcile). Super-admin "what's running" view =
  in-container `openclaw config get` (ground truth) + Conga provenance overlay → **follow-on**
  `show-config` enhancement (NOT this feature); `conga agent pull` optional. `ReadFile` promoted to a
  core transport method. Transport contract is now `{PutFile, RunCommand, ReadFile}`.
- **Spec resolved (2026-06-13)**: transport = Go interface `{PutFile, RunCommand, ReadFile}`; shared
  package = new `pkg/provider/managedhost`; boot reduction = reconstitute-from-persisted-EBS-artifacts
  (keeps no-host-binary + unattended replacement; highest-risk slice, gated on a replacement-recovery
  test); integrity guard-on-unparseable = WARN+allow; partial-failure = idempotent, no half-egress.
  **6-slice migration** (slice 1 = routing.json loopback proof + audit-#1 fix). Persona review APPROVE,
  standards gate PASS (2 non-blocking: egress-controls.md doc-sync on bash-parser deletion;
  config-taxonomy unit-artifact check). Next: `/glados:implement-feature`. See
  `specs/2026-06-13_feature_managed-host-provisioning-engine/spec.md`.
- **Status**: `/glados:plan-feature` complete (current-state verified directly). Next:
  `/glados:spec-feature`. `pkg/` change → `terraform-provider-conga` release required. See
  `specs/2026-06-13_feature_managed-host-provisioning-engine/`.

### 33. Managed-Host Migration Hardening (reboot/restart safety) — 🔨 Implemented (R1-R4 code-complete + unit-verified; live verify + rollout release-gated)
> **Update 2026-06-14**: All four requirements coded + unit-tested on branch `plan/managed-host-migration-hardening`:
> R1 proxy pinned to `.3` at all creation sites (+ add-user/add-team static-subnet create); R2
> `managedhost.ReconcileAgentNetwork` (prepare-then-commit, fail-safe — replaces the shell-string
> migration); R4 `flock -w 240` in pre-start.sh + `ServiceSpec.StartTimeoutSec`=300; R3 `refresh-all`
> per-agent timeout. build/vet/gofmt + `go test ./...` green. **Remaining: provider release →
> remediate the 3 reboot-fragile migrated agents → migrate the held 3 → C5b reboot acceptance →
> `/glados:verify-feature`.** Live verify is release-gated (CLI exercises branch `pkg/`, but the
> deployed path needs the release).
- **Goal**: make the static-IP managed-host engine (#32, PR #67) actually **unattended-reboot/restart-safe**
  (parent criterion 5b). A live-fleet migration (2026-06-14) proved the engine works per-agent but is not
  safe under a whole-fleet bounce (host reboot, Docker daemon restart, `refresh-all`).
- **Four defects (diagnosed live)**: (1) **HIGH** egress-proxy IP collision — proxy runs `--restart always`
  with no `--ip`, grabs the agent's static `.2` on a simultaneous restart → agent `docker run` exit 125
  crash-loop (was review #6, mis-marked "advisory"); fix = pin proxy to reserved `ProxyIP` (`.3`).
  (2) **HIGH** migration not robust + destructive-on-failure — `network rm` blocked by a foreign/dangling
  (`conga-router`) endpoint; container removed before recreate confirmed, leaving the agent DOWN
  (`congaline-team` needed a fleet-bouncing `docker` daemon restart). (3) **MED** `refresh-all` shares one
  global `--timeout` across the fleet (died after ~1.5 agents). (4) **MED** `pre-start.sh` thundering herd —
  N concurrent `aws s3 sync` in `ExecStartPre` blow the 120s timeout on a cold fleet start.
- **Scope**: tight hardening pass; Slack `operator.write` delegation-scope gap is a **separate** follow-up
  spec. Personas: Architect, QA, PM.
- **Sequencing**: land R1+R2(+R4) → provider release → remediate the 3 reboot-fragile migrated agents →
  migrate the remaining 3 → prove C5b via a controlled host `reboot`.
- **Implementation forks (resolved in spec)**: R2 = Go orchestration in `managedhost`
  (`ReconcileAgentNetwork`, prepare-then-commit, replaces the shell-string `agentNetworkMigrationCmd`);
  R4 = `flock -w 240` serialize + `TimeoutStartSec` 120→300; R1 also makes `add-user`/`add-team` create
  the static subnet (so the proxy can pin `.3`). Persona review APPROVE (2 QA amendments folded:
  R2 COMMIT step-verify + R4 bounded `flock`); standards gate **PASS** (no `must` violation; aligns with
  the "tested Go, not bash" core philosophy).
- **Status**: `/glados:spec-feature` complete, gate PASS. Next: `/glados:implement-feature` (order
  R1→R2→R4→R3). `pkg/` change → `terraform-provider-conga` release. Sequencing: land → release →
  remediate the 3 reboot-fragile migrated agents → migrate the held 3 → C5b reboot acceptance. See
  `specs/2026-06-14_feature_managed-host-migration-hardening/spec.md`.

### Backlog / Upcoming
- [ ] Horizon 2: Operational maturity (secret rotation, backups, dashboards)
- [ ] Horizon 3: Advanced hardening (GuardDuty, Config rules)

## Known Issues / Technical Debt
- CLI test coverage at ~27% (aws), ~28% (ui), ~10% (cmd) — see CLI Hardening spec (Phase 4). Deferred items: `params_test.go`, `agent_test.go`
- CLI `admin.go` split into 4 files — see CLI Hardening spec (Phase 5)
- Per-user API keys: each employee brings their own credentials and plugins
- Egress proxy enforcement uses HTTPS_PROXY env vars + iptables DROP rules to prevent bypass. See Network-Level Egress Enforcement spec (Feature 16)
- Open question: which OpenClaw skills/plugins to enable and sandbox requirements
- Agent defaults (`agents/_defaults/<runtime>/<type>/SOUL.md`, `AGENTS.md`) are manually maintained — will drift on OpenClaw image upgrades and need periodic reconciliation
- ~~**Reboot-fragile migrated agents (HIGH, tracked in #33)**~~ **RESOLVED 2026-06-14 (#33 shipped + C5b passed)**: the egress proxy is now `IPAMConfig`-pinned to `.3` (R1) and the network migration is fail-safe (R2). All 6 agents migrated to static `10.99.x` with pinned proxies; a controlled host reboot brought the entire fleet back unattended (`restarts=0`, agents `.2`, proxies `.3`, egress re-applied). A host reboot / docker daemon restart is now safe.
- **Slack-driven delegation blocked (separate follow-up)**: `sessions_spawn` (Qwen subagent) fails from Slack sessions with "missing scope: operator.write" — OpenClaw grants `operator.write` only to operator-surface clients (CLI/Control UI/`conga connect`), not untrusted channel (Slack) sessions. Works from `conga connect`. Not a Conga regression (config intact; fleet-wide incl. untouched agents); needs a gateway-auth/owner-scope spec. Also: aaron's subagent endpoint is a LAN IP reachable from the AWS host only via the VPC VPN route (10.0.0.17) — confirmed working from the operator surface.
- **Docker ghost endpoints**: per-agent bridge networks can accumulate dangling `conga-router` endpoints (pre-host-net-router era) + stale `172.x` DOCKER-USER rules across refreshes; a persisted ghost may need a `docker` daemon restart to clear. **#33 R2 shipped** — the migration now force-disconnects foreign/dangling endpoints before `network rm` and aborts fail-safe (agent stays up) if a ghost persists. The one-time `172.x` DOCKER-USER sweep was completed 2026-06-14 (4 inert orphans removed; fleet now 0 `172.x` rules).

## Recent Changes
- 2026-06-14: Managed-Host Migration Hardening (#33) **MERGED (PR #68, `84b87a3`) + released + rolled out + C5b PASSED — operationally complete.** Closed the four reboot-safety defects the live migration exposed: **R1** pin the egress proxy to its reserved `ProxyIP` (`.3`) via `docker run --ip` / `IPAMConfig` at every creation site (no more proxy-grabs-agent-`.2` exit-125 crash-loop); **R2** replaced the shell-string migration with `managedhost.ReconcileAgentNetwork` (Go, transport-driven, prepare-then-commit: force-disconnects foreign/dangling endpoints before `network rm`, aborts fail-safe leaving the agent UP if a ghost persists, returns a `migrated` bool → post-migration egress redeploy is fatal); **R3** `refresh-all` per-agent timeout (`perAgentRefreshCtx`, deadline-free parent, 6m each); **R4** `pre-start.sh` bounded `flock -w 240` + `TimeoutStartSec` 120→300 (no thundering-herd). **Provider released** (`terraform-provider-conga` v0.1.9 / conga-line v0.0.31; terraform pin 0.1.8→0.1.9, PR #69). **Staged rollout** (no `refresh-all --force`): all 6 agents migrated to static `10.99.x` with pinned proxies. **C5b acceptance PASSED** — controlled host reboot of `i-024bf3a55563f9e88` brought the whole fleet back **unattended**: all 6 `active`/`running`, `restarts=0`, agents `.2`, proxies pinned `.3`, egress re-applied (0 `172.x` rules), router up, DNS OK. The parent feature's criterion 5b (unattended reboot survival) is now true on the live fleet. Out-of-scope follow-up still open: the Slack `operator.write` delegation-scope gap. See `specs/2026-06-14_feature_managed-host-migration-hardening/`.
- 2026-06-14: Managed-Host Provisioning Engine **MERGED (PR #67, `1b41c12`)** — slices 1/2/4+3 production swap: AWS agents provision+refresh through the shared Go managed-host engine (deterministic static `10.99.<idx>` networks, Go `systemdSupervisor` unit, `--env-file`, fail-closed reserved-key PreStart guard, deterministic egress iptables incl. the AWS-required DNS rules, slimmed integrity backstop); `refresh-user.sh.tmpl` deleted. CI green; admin-merged. Agent code-review pass (8 findings fixed: migration rule-flush, bounded restart, routing reconcile on remove, etc.). **Live-fleet migration (2026-06-14) recovered but bumpy**: a docker daemon restart (to clear a persisted ghost endpoint blocking `congaline-team`) bounced all 6 agents and exposed reboot/restart-safety regressions → new feature **#33**. End state: all 6 `active`; 3 migrated to `10.99.x` (reboot-fragile until #33), 3 held on auto-subnets. **Owed**: `terraform-provider-conga` release before the deployed Terraform path uses the merged `pkg/`. See `specs/2026-06-13_feature_managed-host-provisioning-engine/`.
- 2026-06-11: Bugfix — Router host-networking (**implemented + verified, NOT deployed**). `conga-router` could no longer hot-attach to per-agent `/16` bridge networks on Docker 25.0.16 + kernel 6.1.174 (`docker network connect` → `route 0.0.0.0/0` conflict), so Slack events never reached agents (`Forward error … fetch failed`). Real fix (not a libnetwork workaround): made **host-networking + loopback delivery the standard router topology for all providers**. `WebhookTarget` gains `Loopback bool`; `GenerateRoutingJSON` emits `http://127.0.0.1:<agent host GatewayPort>/…` via the new shared `common.LoopbackWebhookResolver`. All routers run `--network host`; every per-agent `docker network connect … conga-router` removed (AWS `routerRestartScript` + `BindChannel`, local/remote `ensure*Router` + all connect/disconnect sites + the now-dead helpers, bootstrap `conga-router.service`/agent units + the `connect-router-networks.sh` script & `conga-router-networks.service` companion unit). **Scope: OpenClaw-only** loopback (Hermes' separate unpublished webhook port 8644 is a documented follow-up). New tests: `routing_test.go` (`TestGenerateRoutingJSON_Loopback`, `TestLoopbackWebhookResolver`) + AWS `TestRouterRestartScriptUsesSlackPath` extended (`--network host`, no `network connect`). `go build`/`vet`/`test ./...` + gofmt all green. **Remaining before operationally closed:** terraform-provider-conga release (`pkg/` changed) → `terraform apply` in a window (reconciles the live manual override into managed config). Until then prod host `i-024bf3a55563f9e88` carries an unmanaged override — **do not refresh/apply against it**. See `specs/2026-06-11_bugfix_router-host-networking/` + memory `project_router_bridge_route_conflict`.
- 2026-06-09: Infrastructure-Only Simplification (PR #57, **implemented + verified, pending merge/release**) — Conga now generates a managed `openclaw.json` that `$include`s an admin-owned `agent-custom.json`; OpenClaw deep-merges it (managed root wins on conflicting Conga-owned scalars), so admin customizations (e.g. an `mcp.servers` entry) **survive restarts and `conga refresh`**. New `Runtime.CustomConfigFileName()`; all three providers self-heal the include (`{}` if absent) on every config write — a missing `$include` target invalidates the config (verified live). New `conga agent rebaseline` (CLI+JSON+MCP) backs up + resets the include. Security: `common.ValidateAgentCustomConfig` forbids the include from declaring Conga-owned keys (`$include`/`channels`/`gateway`/`plugins`) — the channel-allowlist boundary — wired into local+remote `RunIntegrityCheck` and the AWS `check-config-integrity.sh` (jq); AWS re-protects the include root:root 0444. **Live-verified on `aaron`/`2026.5.26`**: MCP-in-include survives restart; integrity guard flags an injected channel. Post-impl standards gate PASS (one documented residual: JSON5 key-name evasion → optional in-container `openclaw config get` hardening, tasks T3.5). Whole feature was empirically driven — `$include` semantics, root-wins, fail-closed-on-owned-writes, and object-union all verified on the live image before/during build. Docs: new `config-taxonomy.md` locus + Example 6. **Remaining before "complete": merge PR #57 → `terraform-provider-conga` release → deployed-path live verification + first-refresh operator advisory (T5.2).** See `specs/2026-06-09_feature_infrastructure-only-simplification/`.
- 2026-05-22: Delegation Routing — two-tier delegation model. **Tier 1 (subagents, ephemeral, in-runtime)**: `agent.yaml` schema bumped to v2 with a new top-level `subagents:` block. OpenClaw generator emits `agents.defaults.subagents.{model, delegationMode, maxConcurrent}` + merges subagent into the models allowlist + extends `models.providers`; Hermes generator emits the `delegation:` block with `max_concurrent_children` / `max_spawn_depth` (Hermes-only knob) + degraded-mode warning for openai endpoints not matching its native provider adapters. Runtime owns the spawn decision via OpenClaw's `sessions_spawn` tool / Hermes' `delegate_task` tool. **Tier 2 (role agents, persistent)**: 5 role packages (`role-ops`/`role-data`/`role-research` Qwen-backed `type: user`; `role-code-dev`/`role-writing` Opus-backed with Qwen subagent, `type: team`) shipped under `agents/_defaults/<runtime>/role-*/`, provisioned via new `conga admin add-user|add-team --role <slug>` (CLI + JSON + MCP parity). **Mid-session bonus**: bumped runtime default Anthropic Opus 4-6 → 4-7. **Live-tested on AWS**: all 5 fleet overlays migrated to v2; aaron deployed on Opus 4.7 + Qwen subagent on Spark LiteLLM; deployed openclaw.json shape verified. **Architectural debt logged** for follow-up: (a) lift `openclaw-defaults.json` out of `//go:embed` so model bumps don't require a binary rebuild + MCP restart; (b) `resolveAWSBehaviorDir()` / `ResolveOperatorBehaviorDir()` silently pick up the wrong `agents/` dir when running from a git worktree. New packages: `pkg/common/egress_check.go` (auto-derive + warn at provision), `pkg/common/role_package.go` (`ApplyRolePackage` + `ResolveOperatorBehaviorDir`). New tests: ~76 across overlay, both generators, CLI, MCP, role-package machinery, role-defaults integrity. 21 packages pass, go vet / gofmt clean. Naming choice documented: matches OpenClaw upstream's "subagent" terminology to avoid colliding with their "delegate" (= org-identity agent) concept. See `specs/2026-05-22_feature_delegation-routing/`.
- 2026-05-20: Local Model Routing — per-agent LLM override via a new `agents/<name>/agent.yaml` overlay (schema v1, strict-key parsing). Supports `ollama` (native; no `/v1`) and `openai` (OpenAI-compatible; `/v1`) providers. Allowlist is additive — runtime default stays available for `/model` switching, `fallbacks: []` prevents silent auto-failover. New: `pkg/runtime/overlay.go` (types + validation), `pkg/common/overlay_agent.go` (loader), `applyModelOverlay` in `pkg/runtime/openclaw/config.go`. All three providers (local, remote, aws) load the overlay before `GenerateConfig`. New `product-knowledge/standards/config-taxonomy.md` documents the canonical per-agent config split (infra → tfvars, policy → `conga-policy.yaml`, runtime overlay → `agent.yaml`, persistence → JSON/SSM, secrets → secrets store). Live-tested on AWS — a production user agent now defaults to a self-hosted LLM via LiteLLM. Two bugs caught during live testing: missing `models[]` array (OpenClaw schema), clobbering-vs-additive allowlist. 22 packages pass, go vet/gofmt clean. Observation logged for `/glados:recombobulate`: runtime config generators need integration tests against actual runtime schema, not just internal-shape assertions. See `specs/2026-05-19_feature_local-model-routing/`.
- 2026-04-07: Per-Agent Behavior Configuration — replaced the base + team/user composition model with a simpler two-layer approach: shared defaults at `behavior/default/` and per-agent overrides at `behavior/agents/<name>/`. Agent files fully replace defaults (no concatenation). New CLI: `conga agent {list,add,rm,show,diff}`. Manifest-tracked deployments with deletion reconciliation. Terraform auto-refresh trigger restarts agents when behavior files change. ExecStartPre now syncs deploy-behavior.sh from S3. OpenClaw-only files supported (SOUL.md, AGENTS.md, USER.md) — arbitrary filenames not loaded by OpenClaw. Tested end-to-end on local and AWS. See `specs/2026-04-04_feature_per-agent-config-overlay/`.
- 2026-04-05: Agent Portability — new `Runtime` interface (`pkg/runtime/`) making the agent runtime pluggable alongside the existing `Provider` interface. OpenClaw runtime extracted from `pkg/common/` into `pkg/runtime/openclaw/` (zero behavioral change). Hermes Agent runtime implemented in `pkg/runtime/hermes/` (YAML config, port 8642, Python health detection). Local provider fully wired to Runtime interface. `--runtime openclaw|hermes` flag, runtime choice persisted during `conga admin setup`, inherited by `add-user`/`add-team`. Data model: `Runtime` field on `AgentConfig`, `Config`, `SetupConfig`, `Manifest`. 20 new files, 13 modified, 38 runtime tests, all 16 test suites pass. Remote/AWS provider wiring deferred. See `specs/2026-04-05_feature_agent-portability/`.
- 2026-03-30: Manifest Bootstrap — new `conga bootstrap <manifest.yaml>` command for one-shot environment provisioning. Declarative YAML manifest describes provider, setup, agents, secrets, channels, bindings, and initial egress policy. Optimized 6-step pipeline, each step idempotent. Secrets referenced via `$VAR` env var expansion from `--env` file, never stored in YAML. Existing `conga-policy.yaml` takes precedence over manifest policy section. New `pkg/manifest/` package (2 files, ~350 lines), CLI command, 25 unit tests. All 17 test packages pass. See `specs/2026-03-30_feature_manifest-apply/`.
- 2026-03-30: Bugfix — BindChannel/UnbindChannel router restart. Router was not restarted after `channels bind`/`unbind`, causing Slack messages to be silently dropped ("No route"). Added `restartRouter()`/`ensureRouter(ctx, true)` calls in both remote and local providers. Also made `connectNetwork` idempotent (ignore "already exists" errors). 4 files, all tests pass. See `specs/2026-03-30_bugfix_bind-channel-router-restart/`.
- 2026-03-29: Non-Root Container Enforcement — added explicit `--user 1000:1000` to all agent and router `docker run` commands across all 3 providers (local, remote, AWS). Router was running as root (`node:22-alpine` default); agent containers relied on fragile image `USER` directive. Also aligned AWS router with local/remote by adding missing `--tmpfs /tmp:rw,noexec,nosuid`. 7 files modified, 17 test packages pass. See `specs/2026-03-29_feature_non-root-containers/`.
- 2026-03-29: Secure-by-Default Egress — egress proxy now always deploys at agent provisioning time with deny-all posture (empty Lua allowlist = 403 on all domains). Policy file opens up specific domains. All three providers (local, remote, AWS) aligned. AWS provisioning scripts (add-user/add-team) updated to deploy proxy + iptables inline. Architecture principle 4 updated: "Secure by default, open by policy." Demo script updated for new flow. 11 files, 6 new tests. See `specs/2026-03-28_feature_portable-egress-policy-compliance/`.
- 2026-03-28: Portable Egress Policy Compliance — all three providers now respect the `mode` field in `conga-policy.yaml` egress section. Default changed from `validate` to `enforce` (security-first). Remote provider no longer hardcodes enforcement — checks mode like local. AWS bootstrap now parses mode, deploys proxy with Lua log-and-allow filter in validate mode (no iptables), and applies iptables DROP rules in DOCKER-USER chain in enforce mode (closing the cooperative-proxy-only gap). Systemd hooks (`ExecStartPost`/`ExecStopPost`) provide iptables resilience across container restarts. Enforcement report unified — all providers report based on mode, not provider name. 4 new tests, 9 files modified. New architecture standards added: Agent Data Safety (must), Interface Parity (must). See `specs/2026-03-28_feature_portable-egress-policy-compliance/`.
- 2026-03-26: Channel Abstraction — extracted all Slack-specific logic from core CLI into `pkg/channels/` behind a `Channel` interface. `AgentConfig.Channels []ChannelBinding` replaces `SlackMemberID`/`SlackChannel`. `SharedSecrets.Values map[string]string` replaces Slack-named fields. `--channel slack:ID` CLI flag replaces positional Slack ID args. Slack is the sole implementation in `channels/slack/`. All providers, CLI commands, MCP tools, routing, config generation, and behavior templates delegate to the channel interface. 5 new files, ~25 modified, 17 new test cases. Breaking change to agent JSON, SetupConfig JSON, and CLI args. AWS bootstrap scripts deferred. See `specs/2026-03-26_feature_channel-abstraction/`.
- 2026-03-26: Egress Domain Allowlisting — per-agent Envoy proxy for domain-based CONNECT filtering across all three providers. Unified enforcement mechanism with iptables DROP rules for network-level isolation. Policy-driven via `conga-policy.yaml` egress section. Local: validate (warn) or enforce (proxy + iptables) modes. Remote/AWS: always enforce when domains defined. Envoy handles HTTP CONNECT tunneling with Lua-based domain filtering. See `specs/2026-03-25_feature_egress-allowlist/`.
- 2026-03-25: Portable Policy Schema — `conga-policy.yaml` schema for declaring security and routing policy as a portable artifact. New `pkg/policy/` package with YAML parsing (`gopkg.in/yaml.v3`), validation (enum checks, domain format, unknown field rejection), per-agent override merging, and per-provider enforcement reporting. `conga policy validate` CLI command with `--file`, `--agent`, `--output json` support. 5 new files, 19 unit tests. See `specs/2026-03-25_feature_policy-schema/`.
- 2026-03-24: SSH Auto-Reconnect — MCP server's SSH connection now transparently recovers from stale/dead connections instead of requiring a Claude Code restart. Added `reconnect()`, `session()`, `sftpClient()` methods to `SSHClient` with single-retry semantics. 4 new tests with in-process SSH server. See `specs/2026-03-24_bugfix_ssh-reconnect/`.
- 2026-03-23: CLI JSON Input — `--json` and `--output json` flags for LLM/agent-driven CLI automation. All 20 commands support structured JSON input (replacing interactive prompts) and JSON output (replacing human-formatted text). Schema discovery via `conga json-schema <command>`. 4 new files, 18 modified, 25 unit tests. `SetupConfig` struct enables non-interactive `admin setup` across all providers. See `specs/2026-03-23_feature_cli-json-input/`.
- 2026-03-23: Remote Provider PR review fixes — 13 fixes across 7 files: `filepath.Join` → `posixpath.Join` for cross-platform remote path correctness, host key verification warning, shell injection fix in integrity log append, Docker install confirmation prompt, SSHKeyPath persistence, stale VPS naming cleanup, `Close()` method, `detectReadyPhase` tests. See `specs/2026-03-23_bugfix_remote-provider-pr-review/`.
- 2026-03-23: Remote Provider (renamed from VPS) — third provider implementation for managing OpenClaw agent clusters on any SSH-accessible host (VPS, bare metal, Raspberry Pi, Mac Mini, etc.). 7 new files (~2,100 lines): SSH client (connect, exec, SFTP, tunnel), remote Docker CLI helpers, full Provider interface (17 methods), file-based secrets, config integrity monitoring, setup wizard with Docker auto-install. 29 unit tests + full E2E lifecycle verified on Raspberry Pi (Debian 13, ARM64, 905MB RAM). 3 bugs found and fixed during integration: SSH auth ordering, first-time setup chicken-and-egg, non-root sudo. See `specs/2026-03-22_feature_vps-provider/`.
- 2026-03-21: Agent Pause / Unpause — per-agent pause/unpause via `conga admin pause/unpause`. Provider interface methods (`PauseAgent`, `UnpauseAgent`), both AWS (SSM scripts + parameter update) and local (Docker stop + JSON file). Routing excludes paused agents. `RefreshAll`, `CycleHost`, and bootstrap skip paused. `list-agents` shows STATUS column. See `specs/2026-03-21_feature_agent-pause/`.
- 2026-03-21: Modular Deployment — refactored CLI from hardcoded AWS to pluggable Provider interface. 16 new files, 15 modified. Provider interface (16 methods), common package (config/routing/behavior generation), AWS provider (wraps existing code, zero behavioral change), local Docker provider (file-based discovery, Docker CLI operations, secrets with mode 0400, config integrity monitoring), egress proxy for network isolation. New flags: `--provider aws|local`, `--data-dir`. 33 test cases added for common package. All existing tests pass. See `specs/2026-03-21_feature_modular-deployment/`.
- 2026-03-21: Conga Line Rename — comprehensive rebrand from "OpenClaw"/"CruxClaw" to "Conga Line". CLI binary `cruxclaw` → `conga`. Go module path, Terraform resources, SSM/Secrets/S3 paths (`/conga/`), Docker/systemd naming (`conga-`), host paths (`/opt/conga/`), CloudWatch namespace (`CongaLine`), GoReleaser, 80+ files across all layers. Upstream Open Claw references preserved. See `specs/2026-03-20_feature_conga-line-rename/`.
- 2026-03-20: Behavior Management — version-controlled behavior markdown (SOUL.md, AGENTS.md, USER.md) with S3 deployment pipeline, systemd ExecStartPre auto-sync, `admin refresh-all` CLI command. Superseded by Per-Agent Behavior Configuration (2026-04-07). See `specs/2026-03-20_feature_behavior-management/`.
- 2026-03-19: CLI Hardening — fixed 3 silent failure bugs, tightened Slack ID validation, added --timeout flag, AWS service interfaces for testability, HostExecutor interface for future local mode, 28 unit tests (7 test files), split admin.go into 4 files, human-readable uptime display, CI test/coverage steps. See `specs/2026-03-19_feature_cli-hardening/`.
- 2026-03-18: Open-source sanitization — removed all hardcoded environment-specific values (account IDs, Slack IDs, SSO URLs, usernames). Gitignored `backend.tf` + `terraform.tfvars` with `.example` files. Added `openclaw_image` variable. New `conga init` command for first-run config. Consolidated README. See `specs/2026-03-18_feature_open-source-sanitization/`.
- 2026-03-18: Conga Line CLI — implemented. Go CLI with 13 commands (auth, secrets, connect, refresh, status, logs, admin). Terraform SSM parameters for discovery. GoReleaser + GitHub Actions for releases. See `specs/2026-03-18_feature_cruxclaw-cli/`.
- 2026-03-17: SSM port forwarding for web UI — per-user `gateway_port`, localhost Docker binding, SSM output commands. Phase 2 (auth tokens, per-user SSM docs) pending.
- 2026-03-17: Epics 5+6 complete — multi-user onboarding, Slack event router, patched OpenClaw image (HTTP webhook fix), ECR, persistent EBS volume
- 2026-03-16: Epic 4 complete — config integrity timer, CloudWatch agent + alarm, SNS topic
- 2026-03-16: Epic 3 complete — EC2 host running, OpenClaw container healthy, Slack socket mode connected, local gateway decommissioned
- 2026-03-15: Epic 2 complete — IAM role + deny-dangerous policy, KMS key, 5 secrets populated
- 2026-03-15: Epic 1 complete — VPC + networking (31 resources: VPC, subnets, fck-nat ASG, zero-ingress SG, NACLs, flow logs)
- 2026-03-15: Epic 0 complete — Terraform foundation (S3 state backend + DynamoDB locks) verified and working
- 2026-03-15: GLaDOS initialized, mission defined, security standards + roadmap + tech stack created
