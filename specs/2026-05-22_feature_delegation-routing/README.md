# Delegation Routing — Trace Log

**Status**: ✅ Verified Complete (live-tested on AWS; chat smoke pending)
**Started**: 2026-05-22
**Branch**: `worktree-explore-agent-routing` (worktree off `main`)
**Lead**: TBD (pending persona selection)

## Overview

Two-tier delegation model where Opus is the primary orchestrator/personality for
all top-level agents, with delegation downward in two distinct shapes:

1. **Ephemeral delegations** — on-demand spawns by Opus for mechanical work
   (mostly Qwen): lookup, file ops, media gen coordination, data crunch,
   translation/formatting.
2. **Persistent role agents** — first-class agent entries with their own model
   + personality, bound to channels:
   - Qwen-backed: Ops, Data, Research
   - Opus-backed: Code/Dev, Writing

The naming for the two tiers ("sub-agent" vs "task agent" vs alternatives like
"delegation/persona", "worker/agent") is intentionally left open during
planning — see plan.md for the recommendation.

## Session Log

### 2026-05-22 — Session Start

- User invoked `/glados:plan-feature` after deciding to explore the routing
  strategy in a worktree off `main`.
- Working in worktree `.claude/worktrees/explore-agent-routing` on branch
  `worktree-explore-agent-routing`.
- Feature name confirmed: `delegation-routing` (spec dir
  `specs/2026-05-22_feature_delegation-routing/`).
- User deferred two open questions to GLaDOS exploration:
  1. Whether ephemeral and persistent delegations should be **unified** or
     remain **two distinct concepts** in the model.
  2. **Terminology** for the two tiers.
- Context anchors provided up front (CLAUDE.md, Feature #27 Local Model
  Routing live on AWS, channel × runtime compat matrix):
  - Per-agent model binding already lives in `agents/<name>/agent.yaml`
    (overlay, `model:` block) — schema v1, strict-keyed.
  - Telegram is Hermes-only post v2026.5; Slack works on both OpenClaw +
    Hermes.
  - Three providers (local/remote/AWS), all per-agent Docker containers.
  - Per-agent config split documented in
    `product-knowledge/standards/config-taxonomy.md`.

## Active Personas

- **Architect** — system integrity, fit with existing Runtime/Provider/overlay
  architecture, schema impact, technical debt risk.
- **Product Manager** — user value, scope, naming legibility for operators, MVP
  carve-out.
- **QA** — testability of multi-model agents, regression risk on Feature #27
  (Local Model Routing — live on AWS), edge cases (missing secret, unreachable
  endpoint, model fallback semantics under delegation).

## Available Capabilities (this codebase)

- Go test runner (`go test ./...`) — overlay validation, runtime config
  generation, channel binding.
- Code search / file ops — read-mostly during planning.
- Live AWS environment — Feature #27 is in production with one agent on a
  self-hosted LLM. Can dogfood new overlay shape against the same flow.
- No browser/UI work — this is pure runtime + config architecture.

## Decisions

Recorded in `plan.md` "Key Design Decisions" — short list here for the trace:

1. **Two tiers stay distinct** (Architect's call on user-deferred question).
   Tier 1 lives in `agent.yaml` (runtime config); Tier 2 lives as Conga agents
   provisioned via `--role` shorthand.
2. **Terminology**: "**delegate**" (Tier 1, ephemeral) + "**role agent**"
   (Tier 2, persistent). Avoids collisions with Anthropic Task tool /
   sub-agent vocabulary and with GLaDOS persona vocabulary.
3. **Tier 1 delegation is a runtime concern, not a Conga concern.** Conga
   ships the overlay shape; the runtime decides when to delegate. No
   Bifrost-style routing proxy in this feature.
4. **Role = curated overlay package** (Phase 3 Route A). No new
   `AgentConfig.Role` field. Roles are directories under
   `agents/_defaults/<runtime>/<role>/` shipped with SOUL.md, AGENTS.md,
   agent.yaml. `conga admin add-user --role X` is sugar.

## Files Created

- [requirements.md](requirements.md)
- [plan.md](plan.md)

## Open Questions Carried Into Spec

See `plan.md` "Open Questions To Close In spec.md" — seven items, with
upstream-capability (OpenClaw + Hermes delegation mechanism at `v2026.5.18`)
as the load-bearing first one.

## Next Step

`/glados:spec-feature` — start with the Phase 1 upstream capability check
before locking the v2 overlay shape.

### 2026-05-22 — Session Resume (Spec Phase)

User invoked `/glados:spec-feature` immediately after plan acceptance.
Re-read requirements.md and plan.md. Active personas unchanged
(Architect + PM + QA). First substantive work: the upstream capability
check (Phase 1 of plan.md) — load-bearing for the v2 overlay shape.

### 2026-05-22 — Phase 1 findings + decisions

[upstream-capability.md](upstream-capability.md) written. Both OpenClaw
v2026.5.18 and Hermes have mature native support for in-runtime
delegation (OpenClaw: `sessions_spawn` + `agents.defaults.subagents`;
Hermes: `delegate_task` + `delegation:`).

Decisions resolved with user:

- **Tier 1 rename: "delegate" → "subagent"** (OpenClaw upstream already
  uses "delegate" for org-identity agents — collision flagged in
  upstream-capability.md). user-a confirmed the rename.
- **Overlay shape: single sub-agent model, not a list** — matches both
  runtimes' single-string config; per-spawn overrides happen at runtime.
- **Tier 2 unchanged: "role agent"** — still distinct from upstream
  "delegate."
- **Egress: Option 3 (auto-derive + warn at provision time)**.
- **Role catalog: ship all 5** (Ops, Data, Research, Code/Dev, Writing).

### 2026-05-22 — Persona Review (post-spec.md)

**Acting as Architect** (priority: architecture, standards, performance):

- **Q: Does this introduce a new dependency?** No — reuses existing
  yaml.v3, no new imports. ✅
- **Q: How does this affect existing data models?** `AgentOverlay` gains
  one optional field (`Subagents *SubagentsOverlay`); `AgentConfig`
  unchanged; provider JSON/SSM persistence unchanged. Minimal blast
  radius. Verified the design honors Route A (no `Role` field). ✅
- **Q: Is this pattern consistent with the rest of the codebase?**
  Yes — schema v1→v2 bump uses the exact migration mechanism Feature #27
  established. Generator helpers (`applySubagentsOverlay`) mirror
  `applyModelOverlay`. ✅
- **Concern raised**: spec says subagent model is **merged into the
  models allowlist** in OpenClaw (so `/model` switch can also reach it).
  Verify this is intended — could conflict with operators who want
  subagent invisible from chat. Architect verdict: **acceptable** —
  matches Feature #27's "additive allowlist" principle; lockdown remains
  an egress-policy concern, not an allowlist-trim concern.
- **Concern raised**: same-provider conflict rejection (primary +
  subagent both `openai` with different `base_url`s) is a real
  ergonomic limitation. v3 may need to relax this via per-provider-id
  scoping. Logged in "Out-of-scope."
- **Verdict: APPROVE** — design fits architecture; no debt incurred.

**Acting as Product Manager** (priority: user-value, scope, requirements):

- **Q: What problem does this solve for the user?** Qwen alone isn't a
  viable primary; Opus alone is too expensive. Two-tier delegation lets
  Opus drive personality + Qwen handle mechanical work. user-a's brief
  was explicit on this. ✅
- **Q: Is this critical for the MVP?** This IS the MVP for the
  delegation routing roadmap. The five-role catalog gives operators
  an immediate value (`--role code-dev` provisions a working Opus +
  Qwen agent in one command). ✅
- **Q: How will we measure success?** Spec lists 8 acceptance criteria
  in requirements.md success criteria. Quantifiable: (a) live-smoke a
  `--role code-dev` agent; (b) verify subagent spawn via runtime logs;
  (c) no regression on Feature #27 AWS production agent. ✅
- **Concern raised**: spec's role catalog table has the same model name
  (`qwen-2.5-72b-instruct`) hardcoded for multiple roles. Operators
  running on Spark/LiteLLM may have a different model name. PM verdict:
  **acceptable** — the role package ships defaults; per-agent overlay
  override is documented in spec section "Role catalog (initial five)".
- **Concern raised**: `role-` prefix in directory names is a minor
  taxonomy choice. Alternative was a `roles/` subdirectory. The current
  flat structure under `agents/_defaults/<runtime>/` keeps roles next
  to `user`/`team` types — coherent. Accepted.
- **Verdict: APPROVE** — scope is right-sized; success is measurable.

**Acting as QA** (priority: testing, edge-cases, regression):

- **Q: What happens if input is empty/null/invalid?** Edge cases table
  in spec covers: v1+subagents key, v2+empty subagents, missing inner
  model, anthropic-as-subagent, same-provider-conflict. ✅
- **Q: How do we handle network failures here?** Subagent endpoint
  unreachable: documented (provision succeeds, runtime errors surface
  in chat). Egress proxy down: documented (self-heal). Egress missing:
  documented (warn at provision, 403 at runtime). ✅
- **Q: Is this covered by existing integration tests?** New test
  surface explicitly listed in spec § "Test plan summary" (≈30 new
  test cases). Existing Feature #27 tests must remain green
  (regression). ✅
- **Concern raised**: Hermes degraded-mode warning is fired once per
  process via `sync.Map` (matches `overlayWarningOnce` pattern). Is this
  visible enough? Operators running `conga refresh-all` may miss
  per-agent warnings in stderr. QA verdict: **acceptable for v2**
  — same visibility pattern as Feature #27's "missing version" and
  "nonstandard base_url" warnings. If observability becomes important,
  spec lists it in "Out-of-scope" (subagent observability).
- **Concern raised**: spec § "Edge cases" mentions "operator passes
  `--role X` to an existing agent that has agent.yaml; existing files
  preserved." QA wants explicit test for this (idempotency of `--role`
  on a provisioned agent). Added to Phase 6 acceptance criteria
  implicitly; **promote to explicit Phase 6 test**.
- **Verdict: APPROVE with note** — promote the `--role` idempotency
  case to an explicit test in Phase 6.

### Synthesis

All three personas APPROVE. One QA note: explicit test for `--role`
idempotency (running `--role X` on an existing customized agent
preserves customizations). Adding that to spec.md Phase 6 acceptance.

### 2026-05-22 — Standards Gate Report (pre-implementation)

Standards scanned: `architecture.md`, `config-taxonomy.md`,
`egress-controls.md`, `security.md`. No `index.yml`; standards treated
per their explicit severity markers (`Severity: must` for Agent Data
Safety, Interface Parity, Module Structure; everything else severity:
should). No `philosophies/` directory.

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Provider contract / interface parity | architecture.md | must | ✅ PASSES — CLI + JSON + MCP all updated for `--role` |
| Agent Data Safety | architecture.md | must | ✅ PASSES — explicit § "Agent Data Safety" in spec; no data dir touches |
| Module Structure (`pkg/` vs `internal/`) | architecture.md | must | ✅ PASSES — overlay types in `pkg/runtime/`, helpers in `pkg/common/`, CLI in `internal/cmd/`, MCP in `internal/mcpserver/` |
| CLI Conventions | architecture.md | should | ✅ PASSES — `--role` flag uses Cobra; mutex with `--type` documented |
| Config Format Boundary | architecture.md | should | ✅ PASSES — YAML for operator-authored overlay (existing pattern), no new config file |
| Package Boundaries | architecture.md | should | ✅ PASSES — `pkg/runtime/` owns overlay types, `pkg/common/` owns shared helpers, providers stay transport-only |
| Channel abstraction (no new Slack coupling) | architecture.md | should | ✅ PASSES — feature is channel-agnostic |
| Testing Conventions | architecture.md | should | ✅ PASSES — table-driven tests, `t.TempDir()`, real behavior |
| Per-agent config taxonomy: decision rule | config-taxonomy.md | should | ✅ PASSES — subagent declaration is runtime-consumed, operator-authored, provider-agnostic → `agent.yaml` is the right layer |
| Anti-pattern: no secret VALUES in agent.yaml | config-taxonomy.md | should | ⚠️ INITIAL WARNING — spec was silent on subagent's API key storage. **Resolved** by adding § "Secrets handling for subagent providers" to spec.md explicitly stating: existing `openai-api-key` secret reused; no new secret names |
| Anti-pattern: no new YAML file per concern | config-taxonomy.md | should | ✅ PASSES — extends `agent.yaml` with `subagents:` (v2 schema bump) |
| Secrets via env vars, never in config | security.md | must | ✅ PASSES — subagent API key flows via existing `OPENAI_API_KEY` env injection |
| Egress secure-by-default | security.md / egress-controls.md | must | ✅ PASSES — subagent endpoint requires explicit egress allowlist entry; spec auto-derives + warns at provision (Option 3) but does not auto-add (preserves operator authority over egress) |
| Universal baseline (container hardening, etc.) | security.md | must | ✅ PASSES — no container, port, or capability changes |
| Pinned image | security.md | must | ✅ PASSES — feature targets the already-pinned `v2026.5.18` |

**Summary**: 0 ❌ VIOLATIONS, 1 ⚠️ WARNING (resolved during the gate
by adding § "Secrets handling for subagent providers" to spec.md),
0 ℹ️ NOTES.

**Gate decision: PROCEED** — spec ready for implementation.

### 2026-05-22 — Session Resume (Implementation Phase)

User invoked `/glados:implement-feature`. Re-read spec.md (5 sections,
8 phases, ~30 tests planned). Active capabilities for this phase:

- `go test ./...` (full test suite, currently green)
- `gh` CLI (for upstream OpenClaw / Hermes lookups)
- File ops + code search
- Live AWS environment via the conga MCP server (NOT engaged unless
  user-a explicitly asks for a live smoke this session)

No browser/UI capability needed (pure runtime + config work).

### 2026-05-22 — Phase 1 implementation complete

**Scope chosen (with user-a)**: Phase 1 only (smallest verifiable
change); commit per phase.

**Files modified**:
- [pkg/runtime/overlay.go](../../pkg/runtime/overlay.go) — bumped
  `CurrentOverlaySchemaVersion` 1 → 2; added `SubagentsOverlay`
  struct + validation; added v1-with-subagents friendly rejection;
  added cross-block same-provider-conflict check.
- [pkg/runtime/overlay_test.go](../../pkg/runtime/overlay_test.go) —
  updated existing version test to accept v2 / reject v3; added 8
  new test functions covering subagents validation surface.
- [pkg/common/overlay_agent_test.go](../../pkg/common/overlay_agent_test.go)
  — replaced `Version2Rejected` with `Version2Accepted` and
  `Version3Rejected`; added 5 new tests for loader-level v2 + subagents
  flow (happy path, v1-with-subagents friendly error, strict-key
  inside subagents block, primary+subagent shape, same-provider
  conflict).

**Files NOT modified** (verified during implementation):
- `pkg/common/overlay_agent.go` — no edits needed. The friendly
  v1+subagents rejection happens at `AgentOverlay.Validate()` time;
  the loader's existing `fmt.Errorf("%s: %w", path, err)` wraps it
  with the file path. `subagents` was never in `reservedTopLevelKeys`.

**Implementation notes**:
- The `Version` switch in `Validate()` now accepts `0, 1, 2`
  explicitly. v1 documents continue to work unchanged.
- `subagents` validation reuses `ModelOverlay.validate()` for the
  inner model — so `anthropic` is already implicitly rejected (it's
  not in the existing provider enum). No special-case needed for v2's
  "no anthropic as subagent" rule.
- Same-provider conflict check uses trimmed-trailing-slash comparison
  so `https://api.openai.com/v1` and `https://api.openai.com/v1/` are
  treated as the same endpoint.

**Verification**:
- `go test ./pkg/runtime/... ./pkg/common/...` green
- `go test ./...` (full suite) green — no regressions
- `go vet ./...` clean
- `gofmt -l pkg/runtime/ pkg/common/` clean (one auto-fix applied
  during run, committed)
- Per-test verification: all 8 new validation tests pass; all 7 new
  loader tests pass (incl. updated v3-rejected case).

**Next**: commit Phase 1 on the worktree branch. Phases 2–8 deferred
to follow-up sessions per user-a's scope choice.

### 2026-05-22 — Phase 2 implementation complete

User said "continue" — implemented Phase 2 (OpenClaw generator).

**Files modified**:
- [pkg/runtime/openclaw/config.go](../../pkg/runtime/openclaw/config.go) —
  added `applySubagentsOverlay` helper; wired into `GenerateConfig`
  after the existing `applyModelOverlay` call. Imports `strings` for
  the trailing-slash normalization helper.
- [pkg/runtime/openclaw/config_test.go](../../pkg/runtime/openclaw/config_test.go)
  — added 7 new test functions covering the upstream config shape,
  delegationMode + maxConcurrent emission, max_spawn_depth filtering
  (Hermes-only), v2-without-subagents byte-equality with v1,
  allowlist merging, same-provider append, and same-provider conflict
  defense-in-depth.

**Key implementation decisions**:
- `max_spawn_depth` is read from the overlay but NOT emitted in the
  OpenClaw output (it's a Hermes-only knob). This matches spec.md's
  generator section. A regression test guards against accidental
  emission.
- Same-provider handling: when the primary already configured
  `models.providers.<id>` with the same base_url as the subagent,
  the generator appends the subagent model to the existing entry's
  `models[]` array rather than creating a duplicate provider entry
  or clobbering. Different base_urls trigger a defense-in-depth
  error (Validate catches this first in normal flow).
- Field omission: `delegationMode` and `maxConcurrent` are omitted
  from output when unset, so OpenClaw falls back to its own defaults
  (no zero values, no nulls — matches the existing pattern from
  `applyModelOverlay` for capability caps).

**Verification**:
- `go test ./pkg/runtime/openclaw/ -run 'Subagents|V2NoSubagents'`:
  all 7 new tests pass.
- `go test ./...` full suite: green.
- `go vet ./...`: clean.
- `gofmt -l pkg/runtime/ pkg/common/`: clean.
- Feature #27 regression check: existing `TestGenerateConfig_*Overlay`
  tests still pass unchanged.

**Next**: Phase 3 (Hermes generator). Commit Phase 2 first.

### 2026-05-22 — Phase 3 implementation complete

**Files modified/created**:
- [pkg/runtime/hermes/config.go](../../pkg/runtime/hermes/config.go)
  — added `applySubagentsOverlay` helper + `hermesKnownProviderHosts`
  list + `emitHermesDegradedWarning` (`sync.Map`-based dedup) +
  `stderrWriter` indirection for testability. Wired into the existing
  `GenerateConfig`. Imports `os` and `sync`.
- [pkg/runtime/hermes/config_test.go](../../pkg/runtime/hermes/config_test.go)
  — **new file** (Hermes had no test files before this phase). 9 test
  functions covering the no-overlay baseline, v2-without-subagents
  byte-equality, ollama transparent inheritance, openai degraded-mode
  warning, openrouter-host no-warning, Hermes-specific config key
  naming (`max_concurrent_children`, `max_spawn_depth`),
  `delegation_mode` filtering, and warning dedup.

**Key implementation decisions**:
- **`stderrWriter` indirection** instead of `os.Stderr` directly:
  lets tests redirect output to a pipe without touching the global,
  which would race with other tests if the package ever grows
  parallel tests.
- **`hermesKnownProviderHosts` is a simple substring list**, not a
  full URL parser. Easier to maintain; Hermes' enum is small (5 hosts)
  and a substring check is precise enough — `openrouter.ai` doesn't
  collide with any other entry.
- **No mapping attempted** from our overlay's `{ollama, openai}` enum
  to Hermes' `{openrouter, nous, zai, kimi-coding, minimax}` enum.
  Doing so would require new overlay metadata operators can't
  currently supply, and it bakes in assumptions about Hermes' adapter
  resolution that may change upstream. The degraded-mode warning is
  the honest answer.
- **`delegation_mode` actively filtered out** of Hermes output even
  if the operator set it in the overlay. The Hermes runtime doesn't
  recognize it and emitting it would just pollute the config. A
  dedicated regression test catches accidental emission.

**Verification**:
- `go test ./pkg/runtime/hermes/`: all 9 new tests pass.
- `go test ./...` full suite: green.
- `go vet ./...`: clean.
- `gofmt -l pkg/runtime/ pkg/common/`: clean.

**Generator parity achieved**: a v2 overlay with a `subagents:` block
now produces correctly-shaped config on both runtimes. Phases 4–8
(egress check, role packages, CLI flag, docs, verification) remain.

### 2026-05-22 — Phase 4 implementation complete

**Files created/modified**:
- [pkg/common/egress_check.go](../../pkg/common/egress_check.go) —
  new file. Three exported functions:
  - `CheckOverlayEgress(overlay, allowlist) []string` returns missing
    hostnames (primary + subagent endpoints not in the allowlist)
  - `FormatEgressGapWarning(agentName, host) string` returns the
    multi-line operator-facing warning text from spec.md
  - `WarnOverlayEgressGaps(w, overlay, allowlist, agentName)` writes
    one warning per gap to `w`; designed for provider use
- [pkg/common/egress_check_test.go](../../pkg/common/egress_check_test.go)
  — new file, 17 test functions covering nil/empty/hosted cases,
  primary/subagent gaps, case-insensitivity, wildcard match (subdomain
  yes, bare host no), port stripping, dedup, insertion order,
  malformed URL handling, and the two output helpers.
- [pkg/provider/localprovider/provider.go](../../pkg/provider/localprovider/provider.go)
  — added `common.WarnOverlayEgressGaps` call after egress policy
  load in `ProvisionAgent` (line ~306). Overlay already in scope.
- [pkg/provider/remoteprovider/provider.go](../../pkg/provider/remoteprovider/provider.go)
  — same one-line addition after egress policy load (line ~285).
- [pkg/provider/awsprovider/provider.go](../../pkg/provider/awsprovider/provider.go)
  — added best-effort overlay load via `resolveAWSBehaviorDir()`
  followed by the egress check. AWS `ProvisionAgent` doesn't
  normally load the overlay (that's done by `RefreshAgent`), so the
  check is wrapped in an existence guard that silently skips when
  the operator runs from outside the repo.

**Key implementation decisions**:
- **Allowlist matching mirrors `policy.MatchDomain`** but is
  re-implemented locally in `common` rather than importing `policy`.
  Architecture standard `pkg/common/` "does NOT own policy" — the
  ~10-line duplication of exact-match + `*.suffix` wildcard is the
  right tradeoff to keep the layering clean. If the matching rule
  ever evolves, both copies need to update in lockstep (test
  fixtures in both packages will catch drift).
- **Host extraction uses `net/url`** so port suffixes are stripped
  cleanly (`http://spark.lan:11434` → `spark.lan`). Malformed URLs
  are silently skipped rather than crashing — validation upstream
  rejects them before this point; this is defense-in-depth.
- **Dedup at the hostname layer**, not at the allowlist layer. If
  primary and subagent share an endpoint that's missing, the
  operator gets one warning, not two.
- **Best-effort on AWS**: AWS `ProvisionAgent` previously didn't
  load the overlay. Rather than restructure that flow, the check
  loads it once for the warning and silently skips when the local
  `agents/` directory isn't resolvable. The shell scripts that
  consume the overlay on the instance side are unchanged.
- **No new integration test added**: the unit tests cover the
  helper behavior thoroughly; the provider wiring is a one-line
  call. The existing `TestAgentLifecycle/add-user` (integration tag)
  exercises ProvisionAgent end-to-end and remains green apart from
  an unrelated Docker port collision on this machine (port 18789
  already bound by another Conga container). Phase 8 live smoke
  will exercise the warning with a real role-code-dev overlay.

**Verification**:
- `go test ./pkg/common/ -run 'CheckOverlayEgress|FormatEgressGap|WarnOverlayEgress'`:
  all 17 new tests pass.
- `go test ./...`: full non-integration suite green.
- `go vet ./...`: clean.
- `gofmt -l pkg/`: clean.

**Next**: Phase 5 (role packages — 10 directories under
`agents/_defaults/<runtime>/role-*/`). Commit Phase 4 first.

### 2026-05-22 — Phase 5 implementation complete

**Directories created** (10 — 5 roles × 2 runtimes):
- `agents/_defaults/openclaw/role-{ops,data,research,code-dev,writing}/`
- `agents/_defaults/hermes/role-{ops,data,research,code-dev,writing}/`

Each role package ships 6 files: `role.meta`, `agent.yaml`, `SOUL.md`,
`AGENTS.md`, `USER.md.tmpl`, `README.md` — 60 new files total.

**Role-to-type mapping** (from `role.meta`):
- `role-ops`, `role-data`, `role-research`: `type: user` (DM-driven,
  single operator)
- `role-code-dev`, `role-writing`: `type: team` (channel-driven,
  collaborative)

**Implementation note — primary model for Opus roles**: spec.md
mentioned `model.provider: anthropic` for the Opus-primary roles
(code-dev, writing). However, the overlay's `ModelOverlay.Provider`
enum is `{ollama, openai}` — anthropic isn't expressible as a primary
in v2. The right approach is to **omit the `model:` block entirely**,
which lets the runtime default (`anthropic/claude-opus-4-6` from
`openclaw-defaults.json`) apply. The two Opus role agent.yaml files
document this for operators. Small spec-vs-implementation correction;
no separate spec amendment needed — each Opus role's README explains
the rationale.

**Hermes mirror approach**: authored all 5 roles for OpenClaw first,
then `cp -r`'d to the Hermes tree, then applied two surgical edits
per role to match the existing pattern in
`agents/_defaults/hermes/{user,team}/`:
- SOUL.md: deployment paragraph now names Hermes Agent + Python +
  skill-based tooling
- AGENTS.md: Tools section now references the `skills/` directory
  rather than the OpenClaw `SKILL.md` / `TOOLS.md` filenames

Everything else is byte-identical between the OpenClaw and Hermes
versions — same personality, same workflows, same boundaries.

**New tests in `pkg/common/role_defaults_test.go`**:
- `TestRoleDefaults_AgentYAMLParses` walks the role tree and confirms
  every shipped `agent.yaml` passes the v2 loader (10 subtests
  pass). Also asserts the Qwen-vs-Opus split: Qwen roles declare a
  primary `model:`; Opus roles leave it unset to inherit the
  runtime default.
- `TestRoleDefaults_RoleMetaPresent` confirms every role-* directory
  ships a valid `role.meta` with `type: user` or `type: team`.

**Verification**:
- `go test ./pkg/common/ -run RoleDefaults`: all 10+1 subtests pass.
- `go test ./...`: full non-integration suite green.
- `go vet ./...`: clean.
- `gofmt -l pkg/`: clean.

**Next**: Phase 6 (CLI `--role` flag + JSON + MCP parity).

### 2026-05-22 — Phase 6 implementation complete

**New files**:
- [pkg/common/role_package.go](../../pkg/common/role_package.go) —
  `ApplyRolePackage(behaviorDir, agentName, roleSlug, runtimeName)`
  copies role defaults into the agent's overlay dir (preserving
  existing files); `ResolveOperatorBehaviorDir()` walks up from
  CWD looking for the conga-line repo; `readRoleMeta` + `availableRoles`
  helpers.
- [pkg/common/role_package_test.go](../../pkg/common/role_package_test.go)
  — 14 unit tests covering happy path, slug normalization,
  idempotency (the QA persona requirement), role-not-found with
  available-roles list, role.meta validation (4 malformed cases),
  runtime isolation, partial packages, and `ResolveOperatorBehaviorDir`
  walk-up.
- [internal/cmd/admin_provision_test.go](../../internal/cmd/admin_provision_test.go)
  — 5 CLI-side tests for `applyRolePackageIfRequested`: empty role
  is no-op, happy path (user role), type mismatch with actionable
  error, missing role, end-to-end idempotency.

**Files modified**:
- [pkg/common/behavior.go](../../pkg/common/behavior.go) —
  `resolveBehaviorFiles` now checks `<agentDir>/USER.md.tmpl` as a
  per-agent template override before falling back to the
  runtime/type default. Lets role-copied `.tmpl` files participate
  in the existing template rendering (with `{{.AgentName}}` and
  channel vars) at deploy time.
- [internal/cmd/admin.go](../../internal/cmd/admin.go) — `--role`
  flag added to both `add-user` and `add-team` commands.
- [internal/cmd/admin_provision.go](../../internal/cmd/admin_provision.go)
  — `applyRolePackageIfRequested` helper resolves role, validates
  declared type matches command intent, copies files, reports to
  stderr via swappable `cmdErrWriter`. Wired into both `adminAddUserRun`
  and `adminAddTeamRun` BEFORE `prov.ProvisionAgent`.
- [internal/cmd/json_schema.go](../../internal/cmd/json_schema.go)
  — `role` field added to `admin.add-user` and `admin.add-team`
  input schemas.
- [internal/mcpserver/tools_lifecycle.go](../../internal/mcpserver/tools_lifecycle.go)
  — `conga_provision_agent` MCP tool gained `role` and `runtime`
  string parameters. Handler mirrors CLI: calls `ApplyRolePackage`,
  validates declared type matches the `type` parameter, returns
  clear error messages on mismatch.

**Key implementation decisions**:
- **Mutex with command-level type, not a `--type` flag**: spec.md
  imagined a `--type` flag exists or will exist, but the CLI has
  separate `add-user`/`add-team` commands. Adapted: the mutex is
  between `--role` and the implicit command type. Errors guide the
  operator to the correct sub-command (e.g. `add-user --role
  role-code-dev` errors with "use `add-team --role role-code-dev`
  instead").
- **Slug normalization**: operators can pass `--role ops` or
  `--role role-ops` — both resolve to the same role package. Tested.
- **Role files copied (not symlinked)**: idempotent and survives
  removal of the source role package (operators can fork the
  defaults without breaking provisioned agents).
- **`ResolveOperatorBehaviorDir` is a free function in `pkg/common`**
  rather than a Provider interface method. Adding a Provider method
  would trigger a terraform-provider release per the CLAUDE.md
  release protocol; the free function is simpler and matches the
  pattern already used by `pkg/provider/awsprovider/channels.go`'s
  private `resolveAWSBehaviorDir`. Future refactor: have AWS provider
  call into the common version.
- **MCP tool parity**: the MCP handler implements the same flow as
  the CLI helper rather than calling the helper directly (the
  helper is in `internal/cmd/` and unimportable from
  `internal/mcpserver/`). Small duplication; both code paths share
  `common.ApplyRolePackage` for the actual work.

**Verification**:
- `go test ./pkg/common/`: 14 new role-package tests pass.
- `go test ./internal/cmd/`: 5 new CLI tests pass.
- `go test ./...` full suite: green.
- `go vet ./...`: clean.
- `gofmt -l pkg/ internal/`: clean (one auto-fix during the run).

**Next**: Phase 7 (docs — `agent.yaml.example` bump to v2,
`config-taxonomy.md` worked example, `CLAUDE.md` Delegation Model
section).

### 2026-05-22 — Phase 7 implementation complete

**Files modified**:
- [agents/_example/agent.yaml.example](../../agents/_example/agent.yaml.example)
  — bumped `version: 1` → `version: 2`; documented v1/v2 split in
  the header; replaced the reserved-keys note for `subagents` (now
  claimed by v2) with a full documented `subagents:` block showing
  all fields with per-runtime applicability notes
  (`delegation_mode`: OpenClaw-only; `max_spawn_depth`: Hermes-only;
  `max_concurrent`: universal); kept other reserved keys
  (memory/tools/limits/images/pdf/video).
- [product-knowledge/standards/config-taxonomy.md](../../product-knowledge/standards/config-taxonomy.md)
  — extended the runtime overlay row to mention `subagents` and to
  acknowledge that `agents/_defaults/` is now committed (role
  packages live under `_defaults/<runtime>/role-*/`); added Worked
  Example #5 ("Opus primary + Qwen subagent") with the full
  agent.yaml shape and a pointer to `--role role-code-dev`; bumped
  doc's Last Updated date.
- [CLAUDE.md](../../CLAUDE.md) — new "Delegation Model" section
  with: tier 1 (subagents) v2 overlay shape, tier 2 (role agents)
  catalog table, upstream vocabulary map (subagent = `sessions_spawn`
  / `delegate_task`; delegate = OpenClaw's org-identity concept),
  egress requirements, customization-before-first-use note. Also
  touched the per-agent overlay paragraph to reference v1 vs v2.

**Verification**:
- `go test ./...` full suite: green (docs changes don't affect tests
  directly, but the example file still parses through the v2 loader
  — covered by existing TestLoadAgentOverlay_V2* tests).
- `go vet ./...`: clean.
- `gofmt -l`: clean.
- Manual grep for stale "delegate" references in the Tier-1 sense:
  none found outside `upstream-capability.md` (which explicitly
  documents the upstream vocabulary collision).

**Next**: Phase 8 (verification — live smoke + verify-feature +
PROJECT_STATUS roll-forward).

### 2026-05-22 — Phase 8 live smoke + Opus 4-6→4-7 bump

**Static verification (8.1-8.3)**: full suite green, vet clean, gofmt
clean (21 packages).

**Fleet migration**: all 5 production overlay files migrated from
v1 (Qwen primary) to v2 (no `model:` block, `subagents:` block with
Qwen via Spark LiteLLM, `delegation_mode: prefer`, `max_concurrent: 4`).
Backups at `/tmp/conga-agents-backup-20260522-152510/`.

**Binary deployment**: built worktree code into both `~/go/bin/conga`
and the MCP-server-targeted `~/Development/crux/congaline/bin/conga`.

**AWS deploy of `user-a` — three iterations**:

1. **First refresh: failed** with `field subagents not found in type
   runtime.AgentOverlay` — MCP server still on the OLD binary. user-a
   `/mcp` reconnected to pick up the new build.
2. **Second refresh: succeeded but deployed empty config**. Root cause:
   the MCP server's CWD is this worktree, and `resolveAWSBehaviorDir()`
   prefers `./agents` (the worktree's `agents/`, which has only
   `_defaults/` and `_example/`, no per-agent overlays) over walking
   up to the parent checkout. The loader treated user-a's overlay as
   missing and emitted defaults-only config.
3. **Workaround**: `ln -s ~/Development/crux/congaline/agents/user-a
   agents/user-a` inside the worktree. Third refresh deployed the
   correct openclaw.json: `agents.defaults.subagents` block populated,
   models allowlist contains both `anthropic/claude-opus-4-*` +
   `openai/qwen36`, `models.providers.openai` correctly configured.

**Architecture debt logged** (task #24): the worktree-vs-parent CWD
behavior in `resolveAWSBehaviorDir()` and `common.ResolveOperatorBehaviorDir()`
silently picks up the wrong `agents/` dir when running from a worktree.
Worth a follow-up to detect git worktrees or honor a `CONGA_AGENTS_DIR`
env var.

**Opus 4-6 → 4-7 bump** (user-a asked mid-session): commit `3505f20`.
Updated 3 categories of files (code defaults, tests, first-boot JSON
templates) + operator docs. Refresh user-a; logs confirm `agent model:
anthropic/claude-opus-4-7 (thinking=medium, fast=off)`. The bump
required 8 file edits, a build cache invalidation, and a MCP restart —
exactly the friction follow-up #24 (move runtime defaults out of
`//go:embed`) would eliminate.

**Pending live-smoke step**: user-a's chat interaction with user-a via
Slack to exercise the subagent-spawn flow. Tracked for after verify.

### 2026-05-22 — Session Resume (Verify Phase)

User invoked `/glados:verify-feature`. Worktree at HEAD `3505f20`
(10 commits since main `b0be0f0`). Pre-step done: committed the
opus bump so verify operates on a clean diff.

### 2026-05-22 — Verify §2 automated: PASS

`go test ./...` (21 packages incl. 51s pkg/aws): all green.
`go vet ./...`: clean. `gofmt -l pkg/ internal/ scripts/ cmd/`:
clean. Diff vs main: 96 files changed, +7200 / -54.

### 2026-05-22 — Verify §3 persona: APPROVE (with 3 follow-ups)

- **Architect**: APPROVE. New concerns: worktree-CWD silent-wrong
  (task #24) + embedded-defaults bump friction (task #24, same).
- **PM**: APPROVE conditional on live chat smoke (tracked in #22).
- **QA**: APPROVE. AWS regen path not directly tested at the
  integration layer — gap noted (covered by helper unit tests
  + would be caught by live smoke).

### 2026-05-22 — Verify §4 post-impl standards gate: PASS

0 ❌ VIOLATIONS, 2 ⚠️ NOTES (informational only, both tracked in
task #24): worktree-CWD behavior + embedded-defaults bump friction.
Full table in the session transcript.

### 2026-05-22 — Verify §5 spec retrospection

Two divergences reconciled in `spec.md`:

1. **`--role` mutex semantics**: spec.md described mutex with a
   `--type` flag; CLI has no `--type` (separate `add-user`/`add-team`
   commands). Reconciled by documenting that the mutex is against
   the command-level type, with role.meta's declared type required
   to match. spec.md § "CLI changes" updated.
2. **Anthropic-as-subagent rejection**: spec.md described a custom
   error message; implementation relies on the existing
   `ModelOverlay.Provider` enum's generic `"unknown model provider"`
   error (already friendly). spec.md § "Egress integration /
   subagents.model.provider == anthropic?" updated to acknowledge
   the existing enum is sufficient.

**Standards-docs audit**: `config-taxonomy.md` already current
(Phase 7 updates include v2 schema + role packages). `architecture.md`
uses "delegate" only in the general programming sense (no Tier-1
collision). `security.md` and `egress-controls.md` unchanged — no
stale references.

No further spec edits needed.

### 2026-05-22 — Verify §6 test synchronization: PASS

- **Stale-reference scan**: 0 stale "delegate" Tier-1 references in
  new test files; only `delegation_mode` / `DelegationMode` /
  `delegate_task` (legitimate config field accesses and Hermes
  upstream tool name).
- **Fake/double alignment**: N/A. All new tests use real `t.TempDir()`
  + real file I/O + the real loader/generator. No mocks.
- **New public method coverage**: every new exported symbol
  (`SubagentsOverlay`, `CurrentOverlaySchemaVersion = 2`,
  `DelegationModeSuggest/Prefer`, `CheckOverlayEgress`,
  `FormatEgressGapWarning`, `WarnOverlayEgressGaps`,
  `ApplyRolePackage`, `ResolveOperatorBehaviorDir`) has at least
  one corresponding test. Most have several.
- **Sibling comparison**: subagents generator tests (7) vs existing
  model-overlay generator tests (5) — coverage parity, no obvious
  gaps. v2 loader tests parallel the v1 loader tests for the
  v2-specific surface.
- **Full suite**: re-run, all 21 packages green (incl. 51s pkg/aws).
- **Linting**: vet clean, gofmt clean.

### 2026-05-22 — Verify §7 completion: trace closed

- **PROJECT_STATUS.md**: feature entry #29 moved to ✅ Verified
  Complete with completion checklist; added entry to "Recent
  Changes" section dated 2026-05-22 documenting the full feature
  including the mid-session 4-6 → 4-7 bump and the two follow-ups
  logged.
- **ROADMAP.md**: extended the "Precursor (landed)" note under
  "Phase 2: Multi-Provider Routing + Promotion" to mention this
  feature alongside Feature #27 (Local Model Routing). The two
  features together set the stage for Bifrost/fallback-chain work.
- **Outstanding** (not blocking verify-feature):
  - Task #22 (Phase 8 live verification): user-a's container is
    deployed and serving but the **chat smoke** (user-a DMs user-a,
    Opus invokes `sessions_spawn`, Qwen runs, result lands back)
    hasn't been observed yet. Tracked.
  - Task #24 (follow-up): extract runtime defaults from
    `//go:embed` + fix worktree-vs-parent CWD silent-wrong.
    Tracked, separate feature.

**Status: Verified Complete.** Spec retrospection covered; standards
gate PASS; persona verification APPROVE; test synchronization PASS.
