# Feature: Fleet Baseline (+ Per-Agent Declarative) Configuration

**Trace Log** — GLaDOS `plan-feature` workflow

- **Created**: 2026-06-10
- **Owner**: <operator>
- **Status**: Implemented + verified (code **and** live T9.2). Pending: merge + provider release (R1). PR #61.
- **Spec dir**: `specs/2026-06-10_feature_fleet-baseline-configuration/`
- **Builds on**: `specs/2026-06-09_feature_infrastructure-only-simplification/` (the `$include` layering + `agent-custom.json` it shipped)

## One-line

Make custom OpenClaw config (MCP servers, skills, tools, …) **declarative and
version-controlled in the repo** at two granularities — a **fleet baseline**
applied to every agent, and **per-agent** config under `agents/<name>/` — deployed
by Conga via `$include` layering, composing with the existing on-host
admin-drift `agent-custom.json`.

## Scope reframe (operator, 2026-06-10)

The trigger was "every agent should have a baseline set," but the operator widened
it: *"the fleet baseline is ONE use case, but we may want agent-specific
configuration in the `agents/{agent}/` folders."* So this is really about a
**declarative custom-config layer in the repo** (the "configure MCP in code"
answer), with fleet + per-agent levels — not just a single fleet file.

## ✅ Verify-Feature Report (2026-06-10)

### Automated verification
- **Test suite**: `go test ./...` — **all green** (every package `ok`; no failures).
- **Linters**: `go vet ./...` clean; `gofmt -l` clean (fixed `pkg/common/config_layers.go` formatting during verify).
- **Real-binary smoke test**: rebuilt `bin/conga`; `conga agent show-config --help` renders the 4-layer doc; `conga json-schema agent.show-config` emits the output contract; MCP `conga_agent_show_config` registered + exercised by the tool-inventory test.

### Persona verification (Architect / PM / QA — review of implementation + tests)
- **Architect — APPROVE.** Reuses #30's verified `$include` + the array precedence; shared logic in `common` (resolver, validators, egress, layer-builder, de-embed loader) with provider packages holding only transport; 3-provider deploy+baseline+guard parity confirmed. The recommended effective-config view shipped (as a layered view — see §3.5/spec §14). De-embed keeps the embedded fallback (air-gap/tamper-safe).
- **Product Manager — APPROVE.** Both use cases served (fleet baseline + "MCP in code"); operator mental model covered by the `config-taxonomy.md` 4-layer subsection + `show-config`. Scope bounded (free-form, no typed schema). T2.4/T9.2/R1 clearly deferred, not silently dropped.
- **QA — APPROVE.** Required tests present: fleet blast-radius (reserved-key fleet source fails closed pre-deploy), fleet propagation + admin/per-agent override ordering (generator array order + deploy re-sync; runtime merge live-verified in spec phase), de-embed fallback (file/absent/malformed), reserved-key flagged in each layer, managed-include tamper detection, cross-layer egress dedup. New-method coverage complete (added a direct `ManagedCustomConfigFiles` test during verify). Sibling parity vs #30's `agent-custom.json` tests holds. **Caveat:** T9.2 (live on a real container) not yet run — see below.

### Standards Gate Report (post-implementation)
| Standard | Severity | Verdict |
|---|---|---|
| security.md — Configuration Integrity: channel allowlist is a security boundary | must | ✅ PASS — reserved-key guard on all 3 include layers × 3 providers; root wins (verified) |
| security.md — Configuration Integrity: hash-based integrity (all) | must | ✅ PASS — 2 managed layers hash-verified vs baseline; deploy+baseline+guard kept in sync (incl. AWS Go refresh + removal cleanup) |
| security.md — fleet blast radius | must | ✅ PASS — pre-deploy fail-closed validation; bad fleet source never reaches a host |
| security.md — secrets via env, egress additive | must | ✅ PASS — no secrets in custom files; egress-gap warnings additive |
| security.md — de-embed defaults integrity + safe fallback | must | ✅ PASS — embedded fallback on absent/malformed; synced file rides integrity-covered tree |
| architecture.md — Agent Data Safety | must | ✅ PASS — config-only; no reads/writes to agent data; removal cleans config baselines only |
| architecture.md — Interface Parity (CLI+JSON+MCP) | must | ✅ PASS *(after fix)* — `show-config` was missing its `json_schema.go` contract; **added during this gate** |
| architecture.md — Provider contract (all 3) | must | ✅ PASS — deploy/integrity/egress wired on local/remote/AWS |
| config-taxonomy.md — document the new layers | should | ✅ PASS — updated during implement (P8) |

**Gate decision: PASS.** One `must` gap (Interface Parity — `json_schema.go` entry for `show-config`) was found and fixed during the gate; re-verified via the real binary. No remaining violations.

### Spec retrospection
Reconciled `spec.md` §14 with the as-built divergences (all intentional, traced): de-embed location/scope (`agents/_defaults/openclaw/`, Go-only, `overlayBehaviorDir` on local); §3.5 layered view instead of synthesized merge; baseline lifecycle (AWS Go refresh + removal cleanup); §12 checkpoints C1–C3 resolved. Standards-doc audit: no stale code examples (`ValidateAgentCustomConfig` retained as wrapper; `config-taxonomy.md` already current; security.md integrity entries still accurate).

### Test synchronization
No stale references (the only `ValidateAgentCustomConfig` test references the still-present wrapper). All new public methods covered; added a direct `ManagedCustomConfigFiles` test to close the one gap. Fakes/mocks: the MCP tool-inventory mock surfaces `conga_agent_show_config` correctly; no behavioral fakes diverge. Sibling (#30 `agent-custom.json`) coverage matched. Full suite + vet re-run green.

### ✅ T9.2 Live verification — PASSED (local Docker, OpenClaw 2026.5.26, 2026-06-10)
Brought up a real gateway-only local agent (`testfleet`) on the pinned image and verified end-to-end, then fully torn down (agent + container + network + test files removed):

| Check | Result |
|---|---|
| `$include` array deployed | `["fleet-custom.json","agent-managed-custom.json","agent-custom.json"]` ✓ |
| Layers deployed from **live repo** sources | `repo_path` auto-set → `overlayBehaviorDir()` read `<repo>/agents/...` (confirms review-fix #2) ✓ |
| `conga agent show-config` (CLI + `--output json`) | 4 layers, correct precedence/role/owner ✓ |
| **Union** (OpenClaw effective merge) | `mcp.servers` = fleetmcp (fleet) + agentmcp (per-agent) + shared (both) ✓ |
| **Per-agent > fleet** | `mcp.servers.shared.url` = `agent.example` (per-agent won) ✓ |
| **Admin-drift > per-agent > fleet** | after host edit, `shared.url` = `admin.example`; union intact ✓ |
| **Fleet propagation** on `refresh` | edited fleet source → `fleetv2` appeared in deployed file + effective merge ✓ |
| **Baseline freshness** | post-refresh managed-include baseline hash == deployed-file hash (no false integrity violation — local analog of blocker-fix #1) ✓ |
| **Pre-deploy fail-closed** (T6.1) | reserved-key (`channels`) in fleet source aborted the refresh with a clear error; bad content never reached the host; no channel injected ✓ |
| **Egress-gap warning** (T6.2) | refresh warned on `mcp.servers.*.url` hosts not in the allowlist ✓ |
| **Orphan-baseline cleanup** on removal | all `testfleet*.sha256` (incl. the 2 managed-include baselines) removed (confirms review-fix `e47e225`) ✓ |

Not separately triggered: the periodic host-tamper hash + reserved-key integrity check (`RunIntegrityCheck` has no CLI entrypoint) — unit-covered, and the baseline-freshness hash match above exercises the same hash machinery. macOS note: iptables egress rules don't apply on Docker Desktop (documented limitation); container ran healthy regardless.

**Now codified as automated tests** (closing the test-sync gap vs the #30 behavior-overlay sibling, which had an integration test where #31 did not):
- **Unit — composition contract**: `TestCompositionPrecedenceContract` (`pkg/common`) pins that the generator's deployed `$include` array (low→high, later-wins) and the `show-config` precedence view (high→low) are exact reverses and match root > admin-drift > per-agent > fleet. Guards against silent drift between the two representations OpenClaw's merge depends on.
- **Integration — configuration application**: `TestFleetAndPerAgentConfig` (`internal/cmd`, `//go:build integration`) reproduces the entire T9.2 flow against a real OpenClaw container — include-array order, layers-from-sources, union + per-agent>fleet + admin-drift>per-agent (via in-container `openclaw config get`), fleet propagation on refresh, `show-config` JSON, reserved-key fail-closed, and managed-baseline cleanup on removal. **13/13 subtests pass** (verified live, 42s). Hermetic: sources written under a guarded temp path in the repo tree and cleaned up; agent + containers torn down.

### ⏳ Remaining (after merge)
- **R1 (post-merge)** — `terraform-provider-conga` release (`pkg/` changed).
- **T2.4 (follow-up)** — AWS bash boot-path de-embed unification.

## ⏸️ RESUME HERE (next session)

Implementation is **partially complete on branch `plan/fleet-baseline-configuration` (PR #61, NOT merged)**. Re-run `/glados:implement-feature` and continue from `tasks.md`.

- **Done + committed + green**: P1 (generator 3-layer `$include`), P3 (`ResolveCustomConfigSources`), **P4 Go paths** (local/remote/AWS-regenerate deploy `fleet-custom.json` + `agent-managed-custom.json`), **P2 Go de-embed** (`openclaw-defaults.json` loader + embedded fallback; file lives at `agents/_defaults/openclaw/openclaw-defaults.json`, reuses fleet-custom's sync — no new terraform). The feature works via `conga refresh` today; `$include`-array precedence is live-verified (root > admin-drift > per-agent > fleet).
- **Done + committed + green (this session)**: **T4.4** AWS bash fresh-deploy path — `deploy-agents.sh` deploys fleet-custom.json + agent-managed-custom.json from S3-synced sources (root:root 0444, openclaw-gated); 3-element `$include` jq at all 3 config-write sites (add-user/add-team/user-data.tftpl). **P5** integrity — reserved-key guard on all 3 include layers + hash-verify the 2 managed layers vs deployed baseline, across local Go + remote Go + AWS (`check-config-integrity.sh` loop, `deploy-agents.sh` baselines). New runtime method `ManagedCustomConfigFiles()`; `common` refactored to filename-generic `ValidateCustomConfigKeys`/`ClassifyIncludeValidation`. Tests across all paths.
- **P6 done this session**: pre-deploy fail-closed validation (`ValidateManagedConfigSources` — reserved-key violation in fleet/per-agent aborts the deploy before any write, all 3 paths) + custom-config egress-gap warnings (`WarnCustomConfigEgressGaps` over `mcp.servers.*.url`, all 6 provision/refresh sites). Tests green.
- **P7 done this session**: `conga agent show-config` — **layered view** (operator-chosen: 4 deployed layers read live via ContainerExec, labeled by precedence/role/owner; no synthesized merge). CLI + `--output json` + MCP `conga_agent_show_config`. Shared `common.EffectiveConfigSpecs`/`BuildConfigLayers`. Tests green.
- **P8 done this session**: T8.1 migration is automatic (generator always emits the 3-element array; deploy always creates empty managed files → first refresh rewrites #30's 1-element form, agent-custom.json untouched). T8.2 `config-taxonomy.md` updated — declarative vs admin-drift split, 4-layer precedence subsection, de-embed note, Example 6 rewrite.
- **P9 (deterministic) done this session**: `TestDeployManagedCustomConfig` — fleet+per-agent deployed from sources (or `{}`), re-synced each call, admin-drift untouched, bad fleet source fails closed (no partial write). All `go test ./...` green; `go vet` clean.
- **Implementation phase COMPLETE.** All code/docs/unit+integration(Go) tasks done across P1–P8 + P9-deterministic. **Next: `/glados:verify-feature`** for T9.2 (live fleet propagation + override precedence + integrity + show-config on a real agent) and the post-implementation security re-audit. **Post-merge: R1** `terraform-provider-conga` release (this PR touches `pkg/`).
- **Still open (intentionally, not blockers for this PR)**: **T2.4** AWS bash-boot-path de-embed unification (tracked follow-up, operator-flagged); **T9.2** live verify (verify-feature); **R1** provider release (post-merge).
- **Tracked follow-up (do NOT lose — operator flagged)**: **T2.4** unify the AWS bash boot/provision path to consume the de-embedded `openclaw-defaults.json` (the conga Go binary never runs on-host, so the bash heredocs still hardcode defaults inline; a fresh AWS boot reflects edits only after the first `conga refresh`). Likely pairs naturally with T4.4 (same bash files).
- **Merge gate cleared**: P5 (security guard) + P6 (blast-radius pre-deploy validation) are both in. PR #61 can merge once P7–P9 + the verify/security gates pass.
- **Gotchas**: `git checkout plan/fleet-baseline-configuration` first (work is not on main). For live AWS work, the conga MCP server holds stale SSO creds → restart it (or use a freshly-built `bin/conga` + `aws ssm` directly); re-`aws sso login --profile openclaw` when the token expires.

## Active Personas
- **Architect** — config-layering model, `$include`-array precedence, where each layer is sourced/synced/deployed, embed→file, three-provider parity.
- **Product Manager** — scope vs. the existing config taxonomy, operator value, success criteria.
- **QA** — merge/precedence edge cases, fleet propagation correctness, egress/secrets fleetwide, integrity of managed vs admin layers.

## Active Capabilities
- **GitHub** (`gh`), **conga MCP** (now on v0.0.28), **AWS SSM** — for live validation of `$include`-array precedence (the load-bearing unknown), mirroring how feature #30 was empirically driven.
- No UI/DB tools relevant.

## Key decisions (this phase)
1. **Scope = fleet baseline + per-agent declarative config**, both in the repo (`agents/_defaults/…` and `agents/<name>/…`), layered via `$include`.
2. **Fold in de-embedding `openclaw-defaults.json`** so fleet defaults are a host/S3 file editable without a binary rebuild + provider release (long-standing logged debt).
3. Anchor on the `$include`-array mechanism (extends feature #30's verified single-include `$include`).

## Session log
- **2026-06-10** — Session start. Personas (all 3). Scope reframed to fleet + per-agent declarative config; de-embed folded in. Confirmed `openclaw-defaults.json` is `//go:embed`'d at `pkg/runtime/openclaw/config.go:14`.

## Files
- [requirements.md](./requirements.md)
- [plan.md](./plan.md)
- [spec.md](./spec.md) — detailed spec (4-layer model, verified precedence, de-embed, deploy/integrity)

- **2026-06-10** — `/glados:spec-feature` started. **Live-verified the `$include`-array precedence** on `user-a`/`2026.5.26` (isolated copy via `OPENCLAW_CONFIG_PATH`, driven through `aws ssm`/`docker exec` because the MCP server held stale SSO creds): **later-in-array wins** (per-agent over fleet), **includes union** (distinct keys from all layers compose), and the **managed root still wins over all includes** (`gateway.port` stayed 18789). The 4-layer model is viable as planned: root > admin-drift > per-agent > fleet. Drafted `spec.md`.

- **2026-06-10** — `/glados:implement-feature` started. Capabilities: in-container `openclaw config validate/get`, conga MCP (needs restart to clear stale SSO from earlier — use freshly-built `bin/conga` + `aws ssm` directly meanwhile), AWS SSM for live verify. Created `tasks.md` (9 phases) for review before coding.

- **2026-06-10** — **Second-pass code review (fresh pr-review-toolkit agent) + fix.** Independent re-review **verified all 3 prior fixes correct and complete** (AWS managed-baseline hash format traced end-to-end; local `overlayBehaviorDir` switch complete with no stray `behaviorDir()` #31 uses; add-user/add-team `{}` fallback mutually exclusive with deploy-agents.sh). Confirmed no issues in: generator/de-embed, hash-format consistency per provider, 3-provider deploy+baseline+guard parity, rebaseline non-interaction, pre-deploy fail-closed, tftpl escaping. One new **should-fix**: orphaned managed-include baselines on `RemoveAgent` (root `.sha256` was cleaned but not the 2 managed-include baselines) — fixed across all 3 providers (local + remote derive the names from `ManagedCustomConfigFiles()`/`managedIncludeBaselinePath` before the record is deleted; `remove-agent.sh.tmpl` adds the two `rm -f`). Low impact (orphaned files only; integrity checks iterate live agents, re-created same-named agents get baselines rewritten before any check) but restores cleanup consistency. Nits confirmed non-issues. Tests + vet green.

- **2026-06-10** — **Code review (pr-review-toolkit) + fixes.** Reviewer (diff `279a4b4..HEAD`) confirmed the security controls (reserved-key guard on all 3 layers × 3 providers, pre-deploy fail-closed, hash scope, terraform escaping, gitignore) are correct. Fixed 3 findings: **(BLOCKER)** AWS Go refresh (`regenerateAgentConfigOnInstance`) re-uploaded the managed layers but only rewrote the *root* baseline — now writes the `<name>-<file>.sha256` managed-include baselines too, restoring hash symmetry with `deploy-agents.sh` + local/remote (without it, every content-changing `conga refresh` on AWS would trip a false integrity violation). **(should)** local provider read the #31 sources + runtime defaults from the `~/.conga` snapshot — switched the 5 sites to `overlayBehaviorDir()` (live repo) so edits propagate on `conga refresh` without re-running `admin setup`, matching `agent.yaml` + the spec §5 flow + the codebase-config operator preference. **(should)** AWS `add-user`/`add-team` wrote the 3-element `$include` but created the managed targets only if `deploy-agents.sh` was present — added a `{}` fallback (root:root 0444) so a missing target can never invalidate the config. Nit #4 (ContainerExec stdout/stderr) verified non-issue (`dockerRun` returns stdout only). Added a scripts render assertion for the fallback. All `go test ./...` green, `go vet` clean.

- **2026-06-10** — `/glados:implement-feature` resumed (session 2). **Drove P2 → P9 to completion.** Order: P2 (de-embed) → T4.4 (AWS bash fresh-deploy) → P5 (integrity guard+hash) → P6 (pre-deploy fail-closed + egress warnings) → P7 (show-config) → P8 (docs) → P9 (deterministic tests). Six feature commits (9012f34, 368b02b, 1201db7, 7e0771b, 2ca8cf8, 51fcfa3) + this trace. All `go test ./...` green, `go vet` clean throughout. **Full file inventory (since resume point 279a4b4):**
  - **Generator/runtime**: `pkg/runtime/runtime.go` (ConfigParams.RuntimeDefaults + ManagedCustomConfigFiles iface), `pkg/runtime/openclaw/config.go` (de-embed loader+fallback, ManagedCustomConfigFiles), `pkg/runtime/hermes/config.go` (ManagedCustomConfigFiles nil), `pkg/runtime/openclaw/config_test.go`.
  - **common**: `config.go` (thread RuntimeDefaults), `custom_config.go` (ResolveRuntimeDefaults, ValidateCustomConfigKeys/ClassifyIncludeValidation/ValidateManagedConfigSources), `egress_check.go` (CheckCustomConfigEgress/WarnCustomConfigEgressGaps), `config_layers.go` (EffectiveConfigSpecs/BuildConfigLayers) + tests for each.
  - **providers**: local/remote `integrity.go` (guard all 3 + hash managed 2 + baselines), local/remote `provider.go` + aws `channels.go`/`provider.go` (RuntimeDefaults wiring, pre-deploy fail-closed, egress warnings), `localprovider/customconfig_test.go`.
  - **AWS bash**: `scripts/deploy-agents.sh.tmpl` (deploy managed layers + baselines), `scripts/add-user.sh.tmpl`/`add-team.sh.tmpl` + `terraform/.../user-data.sh.tftpl` (3-element $include; integrity loop over 3 layers + hash managed 2), `scripts/scripts_test.go`.
  - **CLI/MCP**: `internal/cmd/agent_behavior.go` (show-config), `internal/mcpserver/tools_config.go` + `tools.go` + `server_test.go` (conga_agent_show_config).
  - **seed/docs**: `agents/_defaults/openclaw/openclaw-defaults.json` (committed seed), `product-knowledge/standards/config-taxonomy.md`, `product-knowledge/observations/observed-philosophies.md` (pattern-observer: "show source-of-truth, don't re-implement upstream semantics").

- **2026-06-10** — `/glados:implement-feature` resumed. **P2 Go de-embed landed + green.** Two operator scope decisions: (1) Go de-embed now, AWS bash boot-path unification deferred to tracked follow-up T2.4 (conga binary never runs on-host per `user-data.sh.tftpl:1367`, so the embed is Go-only); (2) editable file at `agents/_defaults/openclaw/openclaw-defaults.json` reusing fleet-custom's existing S3/snapshot sync — no new terraform (resolves checkpoint C2). Changes: `ConfigParams.RuntimeDefaults` field; `GenerateConfig` prefers it (valid JSON) else embed, malformed→warn+embed; `common.ResolveRuntimeDefaults`; wired 3 Go gen sites (local ×2, `RuntimeGenerateAgentFilesWithOverlay` for remote+AWS); seeded committed source file; unit tests T2.3. Modified: `pkg/runtime/runtime.go`, `pkg/runtime/openclaw/config.go`, `pkg/common/custom_config.go`, `pkg/common/config.go`, `pkg/provider/localprovider/provider.go`, `agents/_defaults/openclaw/openclaw-defaults.json` (new), `pkg/runtime/openclaw/config_test.go`, `pkg/common/custom_config_test.go`. Files changes logged here per trace requirement.

## Spec Review & Standards Gate (pre-implementation)

### Persona Review
- **Architect** — APPROVE. Reuses #30's verified `$include` + the now-verified array precedence; fits the config taxonomy as a new declarative layer; de-embed-with-embedded-fallback is sound; parity covered (all 3 providers + AWS tftpl + provision scripts). Concern: 4 layers is a lot of cognitive load → recommends the **effective-config view (§3.5) ship *in* this feature**, not deferred.
- **Product Manager** — APPROVE. Serves both use cases (fleet baseline + "MCP in code"); criteria testable; scope bounded (free-form, no typed schema). Note: operator mental model needs the `config-taxonomy.md` update.
- **QA** — APPROVE with required tests (in §9): **fleet blast-radius** (bad fleet file rejected pre-deploy), **fleet propagation** (one file → all agents), **per-agent overrides fleet / admin overrides per-agent**, and the **de-embed fallback** (absent file → embedded).

### Standards Gate (pre-implementation)
| Standard | Severity | Verdict |
|---|---|---|
| security.md — reserved-key guard on every layer (channel allowlist boundary) | must | ✅ PASS (§3.4; root-wins verified) |
| security.md — **fleet blast radius** (one file → all agents) | must | ✅ PASS *given* pre-deploy validation (fail closed) + staged rollout (§3.3/§11) |
| security.md — de-embed defaults integrity + safe fallback | must | ✅ PASS (embedded fallback retained; synced file integrity-covered, §11) |
| security.md — secrets via env, egress additive | must | ✅ PASS (§11) |
| architecture.md — Agent Data Safety | must | ✅ PASS (§10) |
| architecture.md — Interface Parity | must | ⚠️ CONDITIONAL — *if* `conga agent show-config` ships, it must be CLI+JSON+MCP (§3.5). No new command otherwise. |
| architecture.md — Provider contract (all 3) | must | ✅ PASS (§7) |
| config-taxonomy.md — document the new layers | should | ⚠️ WARNING — taxonomy doc update required during implement (new fleet/per-agent layers). |

**Gate decision: PASS.** No blocking `must` violations. Two items to honor during implementation: the config-taxonomy doc update (should), and Interface Parity *if* the effective-config view ships. Re-audit the fleet blast-radius + reserved-key controls at the post-implementation gate.

## Next step
`/glados:implement-feature` — generator `$include` array, de-embed `openclaw-defaults.json` with embedded fallback, source resolver + per-provider deploy (all 3 #30 write paths incl. AWS tftpl + provision scripts), integrity extension to all layers, tests per spec §9. Then `/glados:verify-feature` + security re-audit. `pkg/` change → provider release.
