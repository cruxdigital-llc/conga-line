# Feature: Infrastructure-Only Simplification

**Trace Log** — GLaDOS `plan-feature` workflow

- **Created**: 2026-06-09
- **Owner**: <operator>
- **Status**: Planning
- **Spec dir**: `specs/2026-06-09_feature_infrastructure-only-simplification/`

## One-line

Narrow Conga's role to **infrastructure + initial baseline config only**: generate a
standard `openclaw.json` once at provision time, then let administrators customize it
(e.g. add an MCP server) with those changes **surviving restarts and refreshes**.

## Active Personas

- **Architect** — config-lifecycle redesign, Conga-owned vs admin-owned key boundary, three-provider parity.
- **Product Manager** — scope, operator value, success criteria, non-goals.
- **QA** — restart/refresh survival, drift edge cases, integrity-monitor interaction, test strategy.

## Active Capabilities

- **GitHub** (`gh`) — PRs, CI, release flow (in active use this session).
- **conga MCP** — live agent introspection (`get_status`, `get_logs`, `container_exec`) against the AWS fleet, useful for verifying restart-survival in the verify phase.
- **AWS SSM** — host inspection for AWS-provider behavior.
- _No browser/UI or DB tools relevant — this is a config-generation/infra feature, no UI surface._

## Session Log

- **2026-06-09** — Session start. Personas selected (Architect, PM, QA). Capabilities recorded.
- **2026-06-09** — Ran a very-thorough code exploration of the current config lifecycle (generation, regeneration call sites across all 3 providers, integrity/hash monitoring, `.bak`/`.last-good` artifacts, MCP-injection paths, generated-vs-persisted split). Findings captured in `requirements.md` §Current State.
- **2026-06-09** — Drafted `requirements.md` (goal, problem, success criteria, scope, constraints) and `plan.md` (high-level approach + key decisions deferred to spec).
- **2026-06-09** — Per operator request ("be circumspect about the OTHER things in openclaw.json, not just MCP"), ran a full config-surface research pass: exhaustive inventory of what Conga's generator writes (subagent) + authoritative upstream schema (Context7 `/openclaw/openclaw` + raw `configuration-reference.md`). Findings in `research-openclaw-config.md`. Two findings changed the design: (a) OpenClaw natively supports `$include` deep-merge with fail-closed read-only roots → Approach C is upstream-supported, not speculative; (b) config is JSON5 → Approach B (read-merge-write) would strip admin comments. Updated `plan.md` approaches + decisions accordingly.
- **2026-06-09** — Live-validated `$include` on the `user-a` production agent (image `2026.5.26`), isolated-copy first then live with byte-exact backup/restore. Confirmed: merges + validates; resolves top-level keys AND `mcp.servers`; survives restart + hot-reload; OpenClaw **fails closed (never flattens)** on owned-writes when root has `$include`; gateway does not owned-write at startup. user-a restored byte-identical (integrity sha256 re-matched baseline). Promoted **Approach C to recommended** (Conga owns root + admin-include); recorded the in-container `config set` trade-off. Findings in `research-openclaw-config.md` §5b; open questions #1/#3/#5 resolved or narrowed.
- **2026-06-09** — Operator asked whether to use the `openclaw` CLI instead of writing config directly. Evaluated **Approach D (Conga drives `openclaw config patch`)** live on `user-a` (isolated copy): `patch` does a validated, version-correct recursive merge with `null`-deletes and runs standalone — but **strips admin JSON5 comments** and needs per-change in-container execution. Verdict: **use the CLI for read-only validation (`config validate`/`schema`), not mutation; keep Approach C for ownership.** Captured in `research-openclaw-config.md` §5c; resolves open Q#4. Cleaned up all probe artifacts on user-a.

- **2026-06-09** — `/glados:spec-feature` started (branch `plan/infrastructure-only-simplification`, PR #57). Resolving the deferred decisions (root ownership, re-baseline UX, migration, integrity) before drafting `spec.md`.

- **2026-06-09** — Drafted `spec.md`. Pre-spec, ran two more isolated probes on `user-a` to settle load-bearing assumptions: (probe3) on **conflicting scalar** keys the **root wins** (Conga-owned values can't be overridden); (probe4) on **objects, deep-merge unions** — an include CAN **add** `channels.*` entries / new channel sections. The union result is a security finding (channel allowlist is a declared boundary). All probes isolated via `OPENCLAW_CONFIG_PATH`; user-a untouched, probes cleaned up.

- **2026-06-09** — `/glados:implement-feature` started. Capabilities: in-container `openclaw config validate`/`get` (for the §9 validation hook + tests), conga MCP `container_exec` + AWS SSM (live verify on AWS fleet). No UI/DB tools relevant. Created `tasks.md` breakdown for review before coding.

- **2026-06-09** — Impl P1+P2 landed. **C1 verified on `user-a`** (isolated): a missing `$include` target invalidates the whole config → helper must self-heal on every root write. Files: `pkg/runtime/runtime.go` (+`CustomConfigFileName()`), `pkg/runtime/openclaw/{config.go,container.go}` ($include injection + const + method), `pkg/runtime/hermes/config.go` (method→""), `pkg/provider/localprovider/{provider.go,channels.go}` (helper + 3 calls), `pkg/provider/remoteprovider/{provider.go,channels.go}` (helper + 3 calls), `pkg/provider/awsprovider/channels.go` (create-if-absent + root:root 0444 re-protect). Tests: `config_test.go` `TestGenerateConfig_IncludesAdminCustomFile`. Build/vet/gofmt clean; runtime+local+remote suites pass.

- **2026-06-09** — Impl P3 (security) + P4 (rebaseline) + docs landed (PR #57, commits through `cb9cd23`). P3: `common.ValidateAgentCustomConfig` forbids the include from declaring Conga-owned keys (`$include`/`channels`/`gateway`/`plugins`) — the load-bearing channel-allowlist control — wired into local+remote `RunIntegrityCheck` (JSON5-safe: surfaces `ErrCustomConfigUnparseable`, no unsafe comment-stripping). P4: `Provider.ResetAgentCustomConfig` (3 impls) + `conga agent rebaseline` (CLI+JSON+MCP). Docs: `config-taxonomy.md` gains the `agent-custom.json` locus (resolves gate `should` warning). **Remaining**: T3.4 AWS `check-config-integrity.sh` (tftpl jq), T2.6 AWS bootstrap `$include`+include creation (tftpl), T5.2 first-refresh advisory, T6.1/6.2 integration tests, T6.3 live verify (→ `/glados:verify-feature` + post-impl security gate). Full Go suite + vet clean.

- **2026-06-09** — AWS portion landed (T2.6 + T3.4, `user-data.sh.tftpl`). Bootstrap now injects `$include` via `jq` into the data-dir `openclaw.json`, creates `agent-custom.json` (root:root 0444), and re-baselines the integrity hash from the post-`$include` file. `check-config-integrity.sh` gained the reserved-key guard (jq: ALERT on `$include/channels/gateway/plugins`, WARN on invalid JSON). jq fragments verified locally. No `${}` interpolation hazards introduced; bare `$VAR` per tftpl convention. tftpl change → no provider release. **All providers now at parity.** Remaining: T5.2 advisory, T6.1/6.2 integration tests, T6.3 live verify (→ `/glados:verify-feature` + security re-audit).

- **2026-06-09** — `/glados:verify-feature` complete. Automated (suite+lint green), live acceptance (MCP-in-include survives restart on `user-a`) + integrity-guard verified on host jq, persona APPROVE, **post-impl standards gate PASS** (one documented residual). Spec retrospection recorded divergences in `spec.md` §14a. Added MCP-tool test `TestRebaselineAgent_Success` (sibling-parity gap). `PROJECT_STATUS` #30 + Recent Changes + ROADMAP updated. **Status: Implemented + Verified; pending merge of PR #57, provider release, deployed-path verification, and the T5.2 first-refresh advisory.**

- **2026-06-09** — PR #57 code review (code-reviewer agent). Two valid findings fixed: **(CRIT)** the AWS *first-provision* path (`scripts/add-user.sh.tmpl` + `add-team.sh.tmpl`, run via SSM by `ProvisionAgent` — separate from the boot tftpl and the Go refresh path) didn't inject `$include`/create `agent-custom.json` → added the same self-heal block (jq `$include`, create-if-absent, root:root 0444, re-baseline). **(IMP)** the AWS integrity-check jq's invalid-JSON WARN branch was dead code (`paste` masked `jq`'s exit) → rewrote to capture `jq` status independently (`KEYS=$(jq 'keys[]') ... grep -Ex`), verified all 5 cases incl. the now-reachable WARN. Finding #3 (provider release for the `pkg/` interface change) already tracked. Build + scripts/awsprovider tests pass.

- **2026-06-09** — Second-pass code review (fresh agent) on fix commit `84e5920`: both prior findings confirmed **correctly and completely fixed**, no new issues, broader diff clean — "ship it" (no findings ≥80 confidence). Verified idempotency on re-provision (jq `+` adds exactly one `$include`), `jq` availability, Go-template safety (single-brace), heredoc/`${}` safety, and cross-provider consistency (local 0644 vs AWS/remote root:root 0444 is the intentional documented difference).

## Verification (`/glados:verify-feature`, 2026-06-09)

### Automated
- `gofmt -l` clean; `go vet ./...` clean (only pre-existing modernize hints in untouched code).
- Full suite: **all 20 packages pass**. New tests: `$include` generation, `ValidateAgentCustomConfig` (incl. JSON5-unparseable), local `ensureAgentCustomConfig` + `ResetAgentCustomConfig`.

### Live acceptance (on `user-a`, image `2026.5.26`; backed up + byte-exact restored, integrity baseline re-MATCH)
- **MCP-in-include survives restart** ✅ — wrote `agent-custom.json` = `{mcp.servers.linear}` + `$include` into the managed root, `systemctl restart conga-user-a`, then `openclaw config get mcp.servers.linear.url` still resolved and the agent reached `ready`. This is the feature's acceptance criterion.
- **Integrity guard works on the host's actual `jq`** ✅ — the clean mcp include passes (empty match, no false positive); a simulated `{"channels":{...}}` include is flagged (`channels`). Confirms the T3.4 bootstrap-script logic on AL2023.
- _Scope caveat_: PR #57 is not yet deployed to the host, so this verifies the **end-state the feature produces** + the guard logic, not the deployed Go/bootstrap code path. Full deployed-path verification happens after the provider release + host cycle.

### Persona verification
- **Architect** — APPROVE. Fits the provider contract (all 3), `Own the box, not the behavior` principle realized; new `Runtime.CustomConfigFileName()` is a clean seam; no new external deps.
- **QA** — APPROVE with noted gaps: remote/AWS `ensureAgentCustomConfig`/`ResetAgentCustomConfig` are integration-level (not unit-tested), consistent with sibling lifecycle ops (PauseAgent etc.). The security regression (injected-channel detection) is covered for the Go validator + verified live for the AWS jq path.
- **PM** — APPROVE. Acceptance criterion met live; operator UX change (edit the include / `config set` fails closed) documented in config-taxonomy.

### Standards Gate (post-implementation) — security re-audit
| Standard | Verdict |
|---|---|
| security.md — channel allowlist boundary | ✅ PASS — include forbidden from declaring `channels` (Go validator local/remote + AWS jq, **verified live**); AWS re-protects `agent-custom.json` root:root 0444 so uid 1000 can't inject. **Residual**: an attacker writing JSON5 evades the key-name check (surfaced as WARN, not blocked) — compensated by AWS perms; the optional in-container `openclaw config get` variant (T3.5) would close it. Acceptable, documented. |
| security.md — secrets via env not config (#9627) | ✅ PASS (unchanged; include is mode 0444, not a secret store — documented) |
| architecture.md — Agent Data Safety | ✅ PASS — config-only; verified no data-dir mutation; `rebaseline` backs up only the include |
| architecture.md — Interface Parity | ✅ PASS — `rebaseline` CLI+JSON+MCP |
| architecture.md — Provider contract / Channel abstraction | ✅ PASS — all 3 providers; allowlist check keys off platform-agnostic bindings/keys |
| config-taxonomy.md doc sync | ✅ RESOLVED (new locus + Example 6) |

**Gate decision: PASS.** One documented residual (JSON5 key-name evasion) tracked as optional hardening T3.5; not blocking given the AWS perm control + WARN surfacing.

## Spec Review & Standards Gate (pre-implementation)

### Persona Review
- **Architect** — APPROVE (post-amendment). Caught two `must` gaps now fixed: missing **Data Safety** section (added §11a) and **Interface Parity** for `conga agent rebaseline` (now CLI+JSON+MCP, §5.4). No new external deps; uses OpenClaw's native `$include` + existing CLI; embodies the "Own the box, not the behavior" principle; agent record unchanged.
- **Product Manager** — APPROVE. Why/Who clear; acceptance criteria testable (add MCP → refresh → survives, §11); scope guarded (typed `mcp:` schema explicitly out). Note (non-blocking): the in-container `config set` fail-closed + edit-the-include workflow is an operator UX change → release notes/docs.
- **QA** — APPROVE (post-amendment). Edge cases covered (§10: missing include, invalid JSON5, override-attempt, hot-reload race). Reinforced the deep-merge-union channel-injection unhappy path; required a **security regression test** (added §11) asserting the effective-allowlist check fires on an injected channel.

### Standards Gate
| Standard | Severity | Verdict |
|---|---|---|
| security.md — channel allowlist = security boundary (Principle 1/2) | must | ❌→✅ **RESOLVED** — effective-allowlist validation (§5.5) + `agent-custom.json` read-only-to-agent (§12) |
| security.md — secrets via env, never config (#9627) | must | ✅ PASSES |
| architecture.md — Agent Data Safety | must | ❌→✅ **RESOLVED** — Data Safety section added (§11a) |
| architecture.md — Interface Parity | must | ❌→✅ **RESOLVED** — rebaseline CLI+JSON+MCP (§5.4) |
| architecture.md — Provider contract (all 3 providers) | must | ✅ PASSES (§5.2) |
| architecture.md — Channel abstraction (platform-agnostic) | must | ✅ PASSES (allowlist check keys off agent record bindings) |
| egress-controls.md — admin MCP endpoints need allowlisting | must | ✅ PASSES (§12 documents; mirror overlay egress-gap warning) |
| config-taxonomy.md — per-agent config split | should | ⚠️ WARNING — new locus (`agent-custom.json`) must be added to the taxonomy doc during implement |

**Gate decision**: all `must` items RESOLVED via spec amendments; one `should` warning logged (taxonomy doc sync). **PROCEED** to `/glados:implement-feature`. Note: the live security/effective-allowlist control should be re-audited at the post-implementation gate.

## Key Decisions (this phase)

1. **Feature framing** — "infrastructure only" = Conga owns infra + a one-time baseline; ongoing runtime-config ownership moves to the administrator. Name kept as given.
2. **Approach C (recommended, validated)** — `$include` layering, live-confirmed on `user-a`/`2026.5.26`: merges, validates, survives restart + hot-reload, fails closed (never flattens). Conga owns the root `openclaw.json`; admin owns an `$include`'d file edited directly. Remaining decision for spec: confirm root ownership + document the in-container `config set` trade-off.
3. **`openclaw` CLI: validation, not mutation** — `config patch` is validated/version-correct but strips admin comments and needs in-container exec (§5c). Use `config validate`/`schema` (read-only) to check Conga's generated file against the exact image; keep file-templating for ownership.
4. **Security-relevant** — changes the config-integrity monitor's contract; `product-knowledge/standards/security.md` review required before implementation.

## Files Created

- [requirements.md](./requirements.md)
- [plan.md](./plan.md)
- [research-openclaw-config.md](./research-openclaw-config.md) — full config-surface map + Conga footprint
- [spec.md](./spec.md) — detailed technical specification (Approach C; security-gated)

## Next Step

`/glados:implement-feature` — implement `spec.md` §5 across `pkg/runtime/openclaw`, the three
providers, the CLI (`conga agent rebaseline`), and integrity (incl. the §5.5 effective-allowlist
check). Land tests per §11, then `/glados:verify-feature` + the post-implementation security gate.
Reminder: `pkg/` change → `terraform-provider-conga` release.
