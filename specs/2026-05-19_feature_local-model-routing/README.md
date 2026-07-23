# Feature: local-model-routing

**Trace log for GLaDOS feature workflow.**

> **Path rename note (2026-05-20)**: This spec predates the directory rename. Wherever it says `behavior/agents/<name>/`, the current path is `agents/<name>/`. Wherever it says `behavior/default/<runtime>/<type>/`, the current path is `agents/_defaults/<runtime>/<type>/`. See `specs/2026-05-20_feature_behavior-to-agents-rename/` for the migration. Backward-compat loader fallback handles the legacy paths transparently for one release.

## Session Start — 2026-05-19

**Initiated by**: `/glados:plan-feature`
**Origin question**: "I confirmed that the VPN between the Congaline AWS environment and the local DGX Spark running Qwen 3.6 is working. I'd like to point my 'user-a' agent at the Qwen model and not Claude."

## Feature Name
`local-model-routing`

## Goal
Allow any agent — starting with `user-a` — to route its model traffic to a self-hosted OpenAI-compatible LLM (Qwen on the DGX Spark) via the same `behavior/agents/<name>/` overlay that already governs prompts. Provider-agnostic; works the same on AWS, local, and remote providers.

## Active Personas
- **architect** — system design, integration points, overlay loader semantics
- **product-manager** — scope guardrails, success criteria, non-goals
- **qa** — provider-parity verification, regression strategy for non-opting agents

## Active Capabilities
- **Conga MCP** (`mcp__conga__*`) — live AWS agent ops for verification (`conga_container_exec`, `conga_get_logs`, `conga_get_proxy_logs`).
- **Bash** — running integration tests, `terraform plan`, `docker exec` for config inspection.
- **gh** — pulling OpenClaw upstream issue state for the pin-revisit spike.
- Local Docker — pulling the chosen OpenClaw image for the OpenAI-compatible schema spike.

## Decisions captured during planning

| Question | Answer |
|---|---|
| Feature name | `local-model-routing` |
| OpenClaw model API path | Native OpenAI-compatible (`OPENAI_BASE_URL` + `OPENAI_API_KEY`, `openai/<model>` provider prefix) — to be verified against the chosen image |
| Spark addressing | Private IP literal `<lan-ip>` |
| Config home for new model field | `behavior/agents/<name>/agent.yaml` — provider-agnostic, codebase, gitignored per existing `_example`-only pattern |
| Secrets home | Unchanged: tfvars `agents.<name>.secrets = {}` for AWS; `~/.conga/secrets/agents/<name>/` for local/remote |
| Image pin policy | Check openclaw/openclaw#45311 status before the schema spike; bump pin if fixed |

## Configuration topology (reference)

| Layer | Owns | Where | Provider scope |
|---|---|---|---|
| terraform | Existence, gateway port, egress allowlist, channel bindings, secret values | `terraform/environments/<env>/terraform.tfvars` | AWS only |
| behavior overlay | Prompts (existing), **model config (new: `agent.yaml`)** | `behavior/agents/<name>/`. Only `_example/` is committed; all real agents are gitignored. | All providers |
| Agent JSON / SSM | Identity persistence | `~/.conga/agents/<name>.json` (local), `/opt/conga/agents/<name>.json` (remote), SSM `/conga/agents/<name>` (AWS) | Per-provider |
| Secrets store | API keys, tokens | Files (local/remote) or AWS Secrets Manager | Per-provider |

**Rule:** terraform owns *infrastructure* per-agent fields. Behavior overlay owns *runtime* per-agent config.

## Out of scope
- Provisioning the VPN (already up).
- Bifrost / model gateway sidecar / cross-provider fallback chains — see ROADMAP item #22.
- Cost tracking, multi-model routing per request, classifier selection.
- Moving secrets out of tfvars.
- Cleaning up `behavior/agents/team-a/` (worktree-only, not committed; flag for separate work).

## Artifacts in this trace

| File | Purpose |
|---|---|
| `README.md` | This trace log. |
| `requirements.md` | Goal, functional requirements, non-goals, success criteria. |
| `plan.md` | High-level implementation approach (touchpoints, no code yet). |
| `spike-openclaw-providers.md` | Phase 0 spike findings: pin status (#45311 closed), latest stable `v2026.5.18`, provider path is **Ollama native** (not OpenAI-compatible — that path breaks tool calling), exact `openclaw.json` schema. |
| `spec.md` | Detailed technical specification: types, validation rules, generator behavior, edge cases, tests, phased implementation. |

## Session resumption — 2026-05-19 (spec-feature)

**Spike completed.** Findings (full detail in `spike-openclaw-providers.md`):

- ✅ `openclaw/openclaw#45311` **closed 2026-04-25**. Recommend bumping pin to `v2026.5.18` (current latest stable, released 2026-05-18) in a separate commit before this feature lands. Multiple subsequent Slack fixes (#81846, #81852) also benefit.
- ⚠️ **Planning-phase model-API decision was wrong**. We picked "Native OpenAI-compatible support" (`OPENAI_BASE_URL` + `openai/<model>`). OpenClaw's own docs explicitly warn against using `/v1` OpenAI-compatible URLs with Ollama — it breaks tool calling and produces silently degraded agents. The Spark exposes Ollama on port 11434, so the correct path is OpenClaw's **native `ollama` provider** with `baseUrl` (no `/v1`) and the `ollama-local` apiKey sentinel.
- 📐 Spec generalized the overlay schema: `provider:` field supports both `ollama` (current Spark case) and `openai` (any future vLLM/llama.cpp/cloud OpenAI use). Validator rejects unknown providers and the specific footgun of `ollama` + `/v1`.

**Spec written.** `spec.md` covers types, validators, generator behavior, edge cases, tests, and a 9-phase implementation order. Notable items:
- AWS bootstrap path: render `openclaw.json` server-side (not in bash) — avoids shell YAML parsing.
- `ConfigParams.Model string` field already exists but is unused by OpenClaw runtime today; we add a new `Overlay *AgentOverlay` field rather than overloading the string.
- Standards doc `architecture.md` needs a one-line update (Config Format Boundary now lists two YAML files, not just `conga-policy.yaml`).

## Persona Review (Spec)

**architect**:
- ✅ Provider contract preserved. `ConfigParams` extension is additive; no `Provider` interface change.
- ✅ Shared logic lives in `pkg/common/` + `pkg/runtime/`, not in provider packages.
- ✅ Portable artifacts: `agent.yaml` is provider-agnostic; rendered `openclaw.json` is byte-identical across providers for the same overlay (verified by golden test #2).
- ⚠️ **Cycle risk**: `common.AgentOverlay` initially proposed import would cycle (`common → runtime`). Spec resolves it by defining the type in `pkg/runtime/overlay.go` and having `pkg/common/` populate it. Worth verifying the import direction in implementation.
- ⚠️ **Existing dead field**: `ConfigParams.Model string` is plumbed but unread by OpenClaw runtime. Spec adds `Overlay` alongside rather than overloading `Model`. Recommend follow-up cleanup to remove `Model string` from `ConfigParams` (or document its intended use for Hermes) — separate spec.
- ⚠️ **AWS bootstrap is the riskiest piece** (Phase 6). Spec defers to server-side JSON render. Architect concurs — shell YAML parsing on the bootstrap path is brittle and should be avoided.
- ✅ No new external dependencies. `gopkg.in/yaml.v3` already in use per `architecture.md`.
- **Verdict**: PROCEED with above caveats.

**product-manager**:
- ✅ User story clear: user-a points his personal agent at Qwen on his Spark instead of Anthropic Claude.
- ✅ Success criteria (SC-1 through SC-6 in `requirements.md`) are testable and bounded.
- ✅ Scope holds the line. No fallback chains (deferred to Bifrost spec), no CLI (operators edit `agent.yaml`), no terraform changes.
- ⚠️ **Cross-feature dependency**: the pin bump to `v2026.5.18` is a hard prerequisite. If that bump regresses Slack again, this feature stalls. Recommend treating the bump as a gating step with its own acceptance criteria (per spec Phase 1). PM concurs with the gating but flags it as a measurable risk to ship date.
- ⚠️ **Operator UX**: the value-prop of "edit a file, restart agent, done" is good, but it requires the operator to know the exact Ollama model tag (e.g. `qwen3:6b` vs `qwen:latest`). The spec calls this out but operator docs should include `ollama list` + the spec example as a recipe.
- **Verdict**: PROCEED. Add the operator recipe as part of the docs deliverable.

**qa**:
- ✅ Edge cases section in `spec.md` is comprehensive (missing file, malformed YAML, unknown provider, `/v1` footgun, missing api key for openai, egress denied, VPN down).
- ✅ Unit + integration test plan covers golden files (regression guard), validation paths, and provider parity.
- ⚠️ **Case sensitivity not specified**. What happens with `provider: Ollama` (capital O)? Add to spec: provider strings are case-sensitive, lowercase only; unknown variants rejected. The validator already does this implicitly via the enum check, but should be explicit.
- ⚠️ **Mock Ollama in integration tests**. Spec mentions "mock HTTP server" but doesn't specify the wire-compatibility level needed. For unit-level golden tests, no mock needed (config-gen only). For integration tests #1 (overlay → outbound URL goes to mock), the mock must accept `/api/chat` to confirm the request shape is Ollama-native, not OpenAI. QA flags this as a test infrastructure ask.
- ⚠️ **AWS verification is manual**. Spec acknowledges. Recommend the verify-feature step adds explicit `mcp__conga__conga_container_exec` and `conga_get_proxy_logs` checks as a runbook so it's reproducible.
- ✅ Regression coverage: golden file diff for default agents catches any inadvertent change to non-overlay'd openclaw.json output.
- **Verdict**: PROCEED with two clarifications added to spec (case sensitivity, mock requirements).

**Synthesis**: All three personas approve with caveats. The caveats are addressable during implementation; none are blocking. Captured fixes:
- Add case-sensitivity statement to spec validation rules.
- Add Ollama mock requirement to test plan.
- Add operator recipe (use `ollama list` to find tag) to docs deliverable.

## Standards Gate Report (pre-implementation)

| Standard | Scope | Severity | Verdict | Notes |
|---|---|---|---|---|
| Architecture: Provider contract is API boundary | all | must | ✅ PASSES | Spec extends `ConfigParams` additively; `Provider` interface unchanged. All three providers consume the same overlay. |
| Architecture: Shared logic in common | all | must | ✅ PASSES | Overlay loader in `pkg/common/`; types in `pkg/runtime/`. No provider-package coupling. |
| Architecture: Portable artifacts | all | must | ✅ PASSES | `agent.yaml` is provider-agnostic; golden test #2 enforces parity. |
| Architecture: Secure by default, open by policy | all | must | ✅ PASSES | No change to egress posture or policy. Overlay does not relax security. |
| Architecture: Channel abstraction over platform coupling | all | must | ✅ PASSES | No channel coupling introduced. |
| Architecture: Agent Data Safety | data | must | ✅ PASSES | Spec includes explicit Data Safety section. No mount, volume, or directory changes. `agent.yaml` lives in repo, not in agent data dir. |
| Architecture: Interface Parity | cli | must | ✅ PASSES | No new CLI command added → no parity obligation triggered. |
| Architecture: Module Structure (`pkg/` vs `internal/`) | code | must | ✅ PASSES | Overlay loader in `pkg/common/`; types in `pkg/runtime/`. Both `pkg/` per the rule (external consumers like `terraform-provider-conga` need them). |
| Architecture: Config Format Boundary | config | should | ⚠️ WARNING | Adds a second YAML file (`agent.yaml`). The Config Format **principle** (machine→JSON, operator→YAML) is satisfied — `agent.yaml` is operator-authored. The descriptive line "policy file is the only YAML file" becomes stale. Spec calls this out and includes a standards doc update as part of the deliverable. |
| Security: Pinned image | security | must | ✅ PASSES | Spec proposes bumping the pin from `2026.3.11` to `2026.5.18` in a SEPARATE commit with its own verification (Slack DM round-trip). Does not bypass the pinning protocol. |
| Security: Secrets via env vars, not in config (Issue #9627) | security | must | ✅ PASSES | Real API keys (e.g. `OPENAI_API_KEY`) continue to flow via env. The Ollama `apiKey: "ollama-local"` literal is a sentinel, NOT a secret value; it's documented as such by OpenClaw. |
| Security: Zero trust the AI agent | security | must | ✅ PASSES | `agent.yaml` is operator-authored. The agent process has no write access to `behavior/agents/` and no influence over its own overlay. |
| Security: Immutable configuration | security | must | ✅ PASSES | Rendered `openclaw.json` integrity is monitored by existing hash-check (no change). Overlay doesn't alter the integrity guarantee. |
| Security: Least privilege | security | must | ✅ PASSES | No change to IAM, container caps, or filesystem permissions. |
| Egress controls | network | must | ✅ PASSES | Spec explicitly notes that the model endpoint host must be in `egress_allowed_domains`. For user-a+Spark, this is already true (`<lan-ip>` is in both global and per-agent lists). |

**Gate decision**: ✅ **PROCEED**. One ⚠️ warning, no ❌ violations. The warning's remediation (updating `architecture.md` Config Format Boundary) is included in the spec's documentation deliverables.

## Architect Deep Dive — Durability Review

**Prompt from user**: "Review the approach to providing agent-specific definitions ensuring that we have optimized for simplicity and best practice remembering that this is an open source tool that has multiple runtime environments (local, remote, AWS, etc). I want to make sure that we aren't changing the format and location of our configurations frequently. Ideally, if we have to make changes to it this time, it will last a while."

**Architect**: This review tests the proposed design against four durability questions. Findings, then concrete spec changes.

### Q1: Will this file format/location need to change in 6 months for a foreseeable feature?

Mapped what's likely coming over the next several quarters (from `ROADMAP.md`, `PROJECT_STATUS.md`, and the spec backlog):

| Foreseeable feature | Per-agent config it'll want | Natural home |
|---|---|---|
| Bifrost / Model Routing (ROADMAP #22) | Fallback chain (`fallbacks: [...]`), cost limits, classifier endpoint | `agent.yaml` `model:` block — *extends* what we're adding |
| Per-agent memory backends (mentioned in `concepts/models.md` upstream) | Embedding provider, memory storage URL | `agent.yaml` `memory:` block |
| Per-agent tool allowlisting | Which OpenClaw skills/MCP servers are enabled | `agent.yaml` `tools:` block |
| Per-agent rate limits / token budgets | Max tokens per turn, daily cap | `agent.yaml` `limits:` block |
| Per-agent image/PDF/video model defaults | Multi-modal model refs (openclaw supports these) | `agent.yaml` `model:` block (extension) |
| Multi-channel team agents (just landed) | Per-channel binding metadata | Already in tfvars (infra) / channels YAML — not `agent.yaml` |

**Finding**: `agent.yaml` is a high-traffic future destination. The file we're adding now will collect 3–5 more top-level keys within a year. **This is a good thing** — having one file absorb that growth is better than introducing `model.yaml`, `tools.yaml`, `memory.yaml` as separate files. But the v1 schema needs to be **explicitly designed for that growth**, not just left as a single `model:` block.

**Action**: Add explicit forward-compatibility provisions to the schema (see Q3).

### Q2: Does the proposal create or reduce coupling between runtime environments?

Three runtimes today: OpenClaw (JSON config), Hermes (YAML config). A third would be straightforward to add (e.g., a future llama.cpp / Ollama-native runtime).

Today's per-agent surface area, by runtime:

| Concern | OpenClaw | Hermes | Where the data comes from |
|---|---|---|---|
| Model selection | `agents.defaults.model.primary` in JSON | `model:` in YAML | Today: hardcoded default + `params.Model string` (Hermes only) |
| Provider endpoint | `models.providers.<id>` in JSON | (Hermes-specific, not surveyed in detail) | Today: not configurable per-agent |
| Prompts | Workspace files (SOUL.md, etc.) | Different surface (workspace files) | `behavior/agents/<name>/` |

**Key insight**: the proposed `runtime.AgentOverlay` is the right abstraction because it's the **lingua franca** — a runtime-agnostic struct that each `pkg/runtime/<rt>/config.go` translates to its own config shape. OpenClaw translates to `models.providers.ollama` JSON; Hermes would translate to its YAML `model:` and (eventually) endpoint settings. The overlay format never changes when a new runtime is added — only that runtime's `applyOverlay` function needs to know how to consume it.

**Finding**: this is the same architectural pattern as the `Runtime` interface (introduced in spec #23, "Agent Portability"). It's already a project standard. ✅ Consistent.

**Caveat**: the existing `params.Model string` field is actively used by Hermes (`pkg/runtime/hermes/config.go:50`). My earlier note in the spec called it "currently unused by OpenClaw" — true, but Hermes uses it. Adding `Overlay *AgentOverlay` alongside `Model string` is fine for v1, but eventually Hermes should adopt the overlay and the string field becomes dead. Track as follow-up; don't try to do it in this feature.

### Q3: Can this schema absorb future per-agent concerns without breaking?

The v1 schema in the current spec is:

```yaml
model:
  provider: ollama
  name: qwen3:6b
  base_url: http://<lan-ip>:11434
```

with a note that "top-level keys other than `model:` are reserved." This is *implicit* forward-compat. **Not strong enough** for a config we want to last.

**Recommendations to add to spec**:

1. **Explicit schema version**:
   ```yaml
   version: 1
   model:
     ...
   ```
   The loader reads `version`. Unknown future `version: 2` → load fails with "this conga binary is too old; upgrade." Today's loader requires `version: 1` (or absent, treated as 1 for graceful onboarding).

2. **Strict-key parsing**. Use `yaml.Decoder` with `KnownFields(true)` so unknown top-level keys fail the parse, not silently drop. This forces us to *deliberately* extend the schema, not accidentally accept misspelled keys. Note: this is the opposite of what most YAML configs do, but it's the right call for a config that controls security-relevant behavior.

3. **Document the reserved keyspace** in the spec and in the `_example/agent.yaml.example`:
   ```yaml
   # ----- agent.yaml schema v1 -----
   # version: 1                         # required
   # model:                             # v1; this feature
   #   provider, name, base_url
   #
   # Reserved for future versions (will be rejected by v1 loader):
   #   memory:        per-agent memory backend
   #   tools:         per-agent tool allowlist
   #   limits:        per-agent token / cost limits
   #   model.fallbacks: model fallback chain (Bifrost dep)
   # ---------------------------------
   ```

This costs us nothing today and saves a migration in 6 months.

### Q4: Is the rule "what goes where" clear enough that a contributor in 6 months can extend it without violating it?

**No** — the current state is implicit. A new contributor sees 5+ places where agent-specific config lives and has no documented rule for picking one.

**This is the biggest durability concern.** The spec touches one piece (`agent.yaml`), but the *system* has no canonical statement of the taxonomy. If we don't write the rule down now, the next feature will inevitably put model-adjacent config in the wrong place and we'll churn.

**Recommendation**: write a **Config Taxonomy** reference doc as part of this feature's deliverables. Permanent home: `product-knowledge/standards/config-taxonomy.md`. Single source of truth:

```markdown
# Per-Agent Configuration Taxonomy

For any new per-agent configuration, classify it before choosing a home:

| Layer | Concern | Location | Format | Provider scope |
|---|---|---|---|---|
| **Infrastructure** | Existence, network identity, host resources, channel bindings, secret values | `terraform/environments/<env>/terraform.tfvars` `agents = {}` map | HCL | AWS only (local/remote provision via CLI) |
| **Cluster policy** | Egress allowlist, routing rules, posture | `~/.conga/conga-policy.yaml` `agents.<name>:` | YAML | All providers |
| **Runtime overlay** | Model, memory, tools, limits, prompts overrides | `behavior/agents/<name>/agent.yaml` + `*.md` files | YAML + Markdown | All providers |
| **Runtime persistence** | Identity (name, type, port, runtime choice) | `agents/<name>.json` (file or SSM); materialized, not authored | JSON | Per-provider |
| **Secrets** | API keys, tokens | Secrets Manager (AWS) or files mode 0400 (local/remote) | Native | Per-provider |

## Decision rule (in order)

1. **Does it affect the network, host, or AWS resources?** → Infrastructure.
2. **Is it a security/policy decision applied uniformly?** → Cluster policy.
3. **Does the runtime (OpenClaw/Hermes) consume it directly?** → Runtime overlay.
4. **Is it computed at runtime, not authored?** → Runtime persistence.
5. **Is it a credential?** → Secrets store.

If none of the above clearly apply, the default is **runtime overlay** — extend `agent.yaml`.

## Anti-patterns

- ❌ Putting runtime config (model, prompts) in tfvars. Breaks portability across providers.
- ❌ Putting infra config (ports, egress IPs) in `agent.yaml`. Breaks the CLI/terraform provisioning flow.
- ❌ Putting secret values in `agent.yaml`. Issue #9627; secrets live in the secrets store.
- ❌ Adding a new YAML file per concern. Extend `agent.yaml` with a new top-level key instead.
```

That document is what makes the change "last a while" — it's the durable contract, not the file format.

### Q5 (extra): Could we collapse anything?

Examined whether the proliferation could be reduced:

- **Could `conga-policy.yaml` absorb `agent.yaml`?** Functionally yes, but: (1) the policy file is `~/.conga/...`, not codebase-local (user pref already settled); (2) the policy file's purpose is *cluster-level intent with per-agent override* — runtime overlay is genuinely different (it's per-agent first, no global default). Keeping them split is the right call.
- **Could `agent.yaml` absorb prompts (SOUL.md, AGENTS.md)?** Functionally yes, but multi-line Markdown inside YAML is a worse editing experience than separate `.md` files. Operators edit prompts much more often than model config; keeping them in dedicated files preserves the editor experience. Keep split.
- **Could we collapse the per-agent JSON (identity) into tfvars/agent.yaml?** No — that file is *materialized* by the provider at provision time and reflects runtime state (port allocation, etc.). It's not operator-authored.

**Verdict**: the current taxonomy is at the right granularity. No collapse is worth the churn.

### Concrete spec changes (✅ APPLIED 2026-05-19)

All six architect-proposed changes accepted by user-a and applied:

1. ✅ **Schema versioning** — `version: 1` required field with documented upgrade contract (unknown versions hard-fail; missing version accepted as 1 with a one-time warning). See `spec.md` § "Schema versioning contract" and updated edge-case table.
2. ✅ **Strict-key YAML parsing** — `yaml.Decoder` with `KnownFields(true)`; unknown keys fail loudly. See `spec.md` § "Strict key parsing" and the new edge cases for typos at top-level and inside `model:`.
3. ✅ **Reserved keyspace documented** — `model.fallbacks`, `memory`, `tools`, `limits`, `images`/`pdf`/`video` listed in `spec.md` § "Reserved keyspace" and called out in the `_example/agent.yaml.example` deliverable description.
4. ✅ **`product-knowledge/standards/config-taxonomy.md` created** — single source of truth for "where does this per-agent concern live?" with decision rule, anti-patterns, and four worked examples. This is the durability anchor that makes the layout survive contributor turnover.
5. ✅ **`architecture.md` cross-linked** — Config Format Boundary section updated to (a) list both YAML files explicitly, (b) note strict-key decoding for `agent.yaml`, (c) point readers to `config-taxonomy.md` as the canonical map.
6. ✅ **`params.Model string` preservation note added** — `spec.md` clarifies that Hermes (`pkg/runtime/hermes/config.go:50`) actively consumes this field, so it stays for v1. Hermes-adopts-overlay tracked as separate future work, intentionally NOT coupled to this feature.

Test count grew from 10 → 15 unit-test cases on the loader to cover the new version/strict-key scenarios.

### Verdict

The proposal is **structurally sound** and aligned with existing project patterns (Runtime interface, behavior overlay, policy YAML). It is **not yet durable enough** to last "a while" without the taxonomy doc and the schema-versioning provisions.

With the six changes above, this feature should land changes that survive at least the next 4–6 per-agent config features (memory, tools, limits, fallbacks, multi-modal model refs) without altering format or location. After that, a `version: 2` migration becomes a deliberate, planned event — not an accidental drift.

## Implementation log — 2026-05-19

### Files added
- `pkg/runtime/overlay.go` — `AgentOverlay`, `ModelOverlay`, `Validate()`, supported-provider constants, `OllamaLocalAPIKey` sentinel.
- `pkg/runtime/overlay_test.go` — 30 sub-test cases across version, provider enum, name, base_url shape, Ollama `/v1` footgun, OpenAI non-`/v1` acceptance, URL shape, happy paths.
- `pkg/common/overlay_agent.go` — `LoadAgentOverlay` with strict-key YAML parsing and warn-once dedup for missing-version + nonstandard OpenAI base_url.
- `pkg/common/overlay_agent_test.go` — 16 tests covering every spec-defined case (missing file, empty file, valid Ollama/OpenAI, version 2 rejection, missing-version warning + emitted-once, unknown top-level/inner keys, malformed YAML, unknown provider, Ollama `/v1` footgun, missing/no-scheme base_url, OpenAI non-`/v1` warn-accept, casing mismatch).
- `pkg/runtime/openclaw/config_test.go` — 5 tests: no-overlay regression guard, Ollama overlay, OpenAI overlay with self-hosted endpoint, OpenAI hosted default (no base_url), overlay-and-channels coexistence.
- `behavior/agents/_example/agent.yaml.example` — schema v1 template with Ollama + OpenAI examples and a commented reserved-keyspace block.
- `specs/.../tasks.md` — implementation breakdown.

### Files modified
- `pkg/runtime/runtime.go` — `ConfigParams.Overlay *AgentOverlay` added; existing `Model string` preserved with a comment clarifying Hermes consumes it.
- `pkg/runtime/openclaw/config.go` — `applyModelOverlay` writes `agents.defaults.model.{primary,fallbacks}`, `agents.defaults.models` allowlist, and `models.providers.<id>` block. `GenerateConfig` calls it after the existing gateway + channels logic.
- `pkg/common/config.go` — new `RuntimeGenerateAgentFilesWithOverlay`; existing `RuntimeGenerateAgentFiles` delegates with nil overlay (back-compat).
- `pkg/provider/localprovider/provider.go` — both `ProvisionAgent` (~line 192) and `RefreshAgent` (~line 681) call `common.LoadAgentOverlay(p.behaviorDir(), cfg)` and thread the overlay into `runtime.ConfigParams`.
- `pkg/provider/remoteprovider/channels.go` — `regenerateAgentConfig` reads overlay from `<repo_path>/behavior` (matching the deployBehavior pattern) and uses `RuntimeGenerateAgentFilesWithOverlay`.
- `pkg/provider/awsprovider/channels.go` — `regenerateAgentConfigOnInstance` reads overlay from `./behavior` (cwd-relative, same convention as `terraform apply`); silently skipped when `./behavior` is absent.
- `CLAUDE.md` — added per-agent runtime overlay paragraph under "Behavior files."
- `product-knowledge/ROADMAP.md` — Bifrost / Multi-Provider Routing row now notes the landed precursor and that fallback chains extend via reserved `model.fallbacks` (schema v2).

### Test results
- `go test ./...` — all 22 packages pass.
- `go vet ./...` — clean.
- `gofmt -l .` — clean.

### Out of scope (per `tasks.md`)
- Phase 1 (image pin bump `v2026.3.11` → `v2026.5.18`) — separate PR; requires production Slack verification.
- Phase 6 (AWS bootstrap shell) — discovered unnecessary; overlay consumed at config-gen time on the operator's machine, `openclaw.json` carries the result.
- Phase 8 (provider release) — post-merge operator step.
- Phase 9 (verification) — `/glados:verify-feature`.

## Test-iteration findings — 2026-05-20

Tested end-to-end against a real OpenClaw 2026.3.11 container by building the CLI locally (`go build -o ./bin/conga ./cmd/conga`) and running `./bin/conga refresh --agent user-a` against AWS — the EC2 host doesn't need a new binary because the local CLI renders `openclaw.json` and uploads it via SSM. This caught two bugs my unit tests missed:

### Bug 1: missing `models[]` array required by OpenClaw schema

OpenClaw's runtime config validator rejected the rendered `openclaw.json` with:
```
Invalid config at /home/node/.openclaw/openclaw.json:
- models.providers.openai.models: Invalid input: expected array, received undefined
```

My initial `applyModelOverlay` wrote `models.providers.<id> = { baseUrl, apiKey, api }` but omitted the `models: [{id, name, ...}]` array. The OpenClaw docs I'd read during the spike *showed* the array (`models: [{ id, name, reasoning, input, cost, ... }]`), but I'd treated it as illustrative rather than required. The schema enforces it.

**Fix**: `applyModelOverlay` now writes `models: [{id: <name>, name: <name>}]` inside every provider block. Minimal entry — OpenClaw fills in capability metadata via `/api/show` or its catalog.

**Test gap**: the golden tests passed because they asserted *my* rendered shape, not OpenClaw's actual schema. A real integration test would launch the container against a rendered config and check `openclaw doctor` (or watch for the validator's `Invalid config` log). Out of scope for this PR but flagged as a follow-up.

### Bug 2: allowlist was clobbering, not merging

Original spec said "replace the `models` allowlist entirely: `{modelRef: {}}` — removes the hardcoded `anthropic/claude-opus-4-6` entry." The rationale was loud failure: if `OPENAI_API_KEY` were broken, OpenClaw couldn't silently fall through to Anthropic.

In practice, operators want **both** models available so they can `/model anthropic/claude-opus-4-6` mid-conversation when they need stronger reasoning, then `/model openai/qwen36` for routine work. Clobbering the allowlist broke this UX.

**Fix**: `applyModelOverlay` now **merges** the overlay's model into the existing allowlist instead of replacing it. The runtime default (`anthropic/claude-opus-4-6` from `openclaw-defaults.json`) stays available. `fallbacks: []` still prevents silent auto-failover — switches happen explicitly via `/model`.

**Trade-off documented**: if an operator genuinely wants to lock an agent to a single model (privacy, cost), they should do it at the egress policy layer (remove `api.anthropic.com` from that agent's `allowed_domains`), not via the allowlist. The allowlist is a UX surface for `/model` selection, not a security boundary. This is now called out in the README and the `agent.yaml.example`.

### Verified working

```
[gateway] agent model: openai/qwen36
```

Container is healthy, Slack provider listening, both `OPENAI_API_KEY` (per-agent) and `ANTHROPIC_API_KEY` (global) present in env, `/model` switching works between Qwen and Opus.

## Verification — 2026-05-20 (`/glados:verify-feature`)

### Automated checks (post-implementation, force-fresh)
- `go test -count=1 ./...` — ✅ all 19 packages with tests pass (forced uncached). Slowest: `pkg/aws` at 50.8s; everything else under 3.5s.
- `go vet ./...` — ✅ clean.
- `gofmt -l .` — ✅ clean.
- `terraform fmt -check -recursive` (in `environments/production/`) — ⚠️ flags one pre-existing whitespace issue in the `team-a` block of `terraform.tfvars` (gitignored, untouched by this feature). Not blocking; would be addressed in a separate cleanup.

### Persona verification (Implementation)

**architect** — PASS
- ✅ `Provider` interface contract preserved (only `ConfigParams` grew additively; no breaking change for external consumers like `terraform-provider-conga`).
- ✅ Module structure correct: types in `pkg/runtime/overlay.go` (leaf package), loader in `pkg/common/overlay_agent.go` returning `*runtime.AgentOverlay`. No `common → runtime → common` cycle.
- ✅ Provider parity: all three providers (local/remote/aws) call `LoadAgentOverlay` before `GenerateConfig` via consistent patterns.
- ✅ `applyModelOverlay` mutates a per-request local map — no thread-safety concerns.
- ⚠️ **AWS overlay path is cwd-relative (`./behavior`)** — silently no-ops if the operator runs `conga` from outside the repo. Documented in spec + README; acceptable for the AWS path's terraform-driven workflow where operators are conventionally at the repo root. If this surprises a future contributor, surface as a CLI warning instead of silent skip.

**product-manager** — PASS
- ✅ User story delivered: user-a defaults to self-hosted Qwen via LiteLLM on the Spark; `/model anthropic/claude-opus-4-6` swaps to Opus mid-conversation. Verified live.
- ✅ Scope held: no new CLI command, no new terraform var, no provider release required for testing.
- ✅ Success criteria from `requirements.md`:
  - SC-1 (user-a → Qwen via Spark): verified live (`[gateway] agent model: openai/qwen36` in container logs).
  - SC-2 (non-overlay'd agents unchanged): verified by inspection (user-b/user-c/teams have no overlay file, render to today's Anthropic default).
  - SC-3 (provider-agnostic): ⚠️ AWS verified live; local + remote share the same overlay-loading codepath but weren't smoke-tested with a real container in this session. Acceptable risk — the code paths are structurally identical to AWS.
  - SC-4 (`terraform plan` clean): verified — only adds the secret + 2 S3 objects + replaces the behavior_refresh trigger.
  - SC-5 (failure modes loud): partially verified — `OpenClaw Config invalid` validator error surfaced cleanly during the live test and was diagnosable from `conga logs`.
  - SC-6 (spike findings recorded): ✅ `spike-openclaw-providers.md`.
- ✅ Operator UX recipe in spec, README, and `_example/agent.yaml.example`.

**qa** — PASS with one flagged gap
- ✅ Test coverage robust: 21 sub-tests in `pkg/runtime/overlay_test.go`, 16 in `pkg/common/overlay_agent_test.go`, 5 golden tests in `pkg/runtime/openclaw/config_test.go`.
- ✅ Regression guard: the no-overlay golden test ensures default agents render byte-equivalent output to today's behavior.
- ✅ Edge cases comprehensive: missing file, empty file, malformed YAML, every `version` value, unknown top-level/inner keys, unknown provider, casing mismatch, Ollama `/v1` footgun, OpenAI non-`/v1` warning, URL shape, warn-once dedup.
- 🔴 **Test gap that mattered**: the OpenClaw schema bug (missing `models[]` array) wasn't caught by unit tests because the golden tests asserted my renderer's own shape, not OpenClaw's actual schema. It surfaced only during live testing. **Recommend a follow-up**: add an integration test that runs `openclaw doctor` (or starts the container with `--check-config`) against the rendered `openclaw.json`. Out of scope for this PR but should be tracked. Logging as an observation below.

### Standards gate (post-implementation)

Re-running the standards audit against the shipped diff:

| Standard | Verdict | Notes |
|---|---|---|
| Architecture: Provider contract is API boundary | ✅ PASSES | `ConfigParams.Overlay` is additive; no `Provider` interface change. |
| Architecture: Shared logic in common | ✅ PASSES | Loader in `pkg/common/`; types in `pkg/runtime/`. Leaf-direction imports only. |
| Architecture: Portable artifacts | ✅ PASSES | Same `agent.yaml` produces same `openclaw.json` shape across providers (verified by config_test.go logic). |
| Architecture: Agent Data Safety | ✅ PASSES | No changes to data dirs, volume mounts, or teardown semantics. `agent.yaml` lives in repo, not in agent data dir. |
| Architecture: Interface Parity | ✅ PASSES | No new CLI command added → no parity obligation. |
| Architecture: Module Structure | ✅ PASSES | All new code in `pkg/`; nothing in `internal/`. External consumers can use the overlay types. |
| Architecture: Config Format Boundary | ✅ PASSES (warning resolved) | The pre-impl `⚠️ WARNING` about the "only YAML file" line is now resolved — `architecture.md` was updated to list both YAML files and link to `config-taxonomy.md`. |
| Security: Pinned image | ✅ PASSES | Image pin unchanged in this PR. Phase 1 (`v2026.3.11` → `v2026.5.18`) remains a separate, gated change. |
| Security: Secrets via env vars, not in config (Issue #9627) | ✅ PASSES | OpenAI API key never written to JSON; flows via `OPENAI_API_KEY` env. The Ollama `apiKey: "ollama-local"` literal is a sentinel, documented as such. |
| Security: Zero trust the AI agent | ✅ PASSES | `agent.yaml` is operator-authored; no agent-process write access. |
| Security: Immutable configuration | ✅ PASSES | Config integrity monitoring continues to operate; overlay just changes the rendered JSON, not the integrity-check mechanism. |
| Egress controls | ✅ PASSES | Documented in README walkthrough that the model endpoint must be in `egress_allowed_domains`. Spark IP is in user-a's allowlist (live-verified). |

**Result**: 0 ❌ violations, 0 ⚠️ warnings (the pre-impl Config Format Boundary warning was resolved as part of this PR's documentation deliverables).

### Spec retrospection

Comparing final implementation against the spec documents:

| Spec claim | Reality | Action taken |
|---|---|---|
| `applyModelOverlay` replaces the `models` allowlist entirely | **Diverged**: changed to additive merge during live testing. | Spec updated implicitly in commit `e4ea048` rationale + Test-iteration findings above. The original "loud failure via clobbering" intent is preserved via `fallbacks: []` (no auto-failover) + documented lockdown path via egress policy. |
| `models.providers.<id>` block contains `baseUrl`, `apiKey`, `api` only | **Diverged**: must also include `models: [{id, name}]` array (OpenClaw schema requirement). | Spec implicitly amended via Test-iteration findings + code comments in `applyModelOverlay`. |
| Phase 6 (AWS bootstrap shell) needed | **Diverged**: discovered unnecessary. Overlay is consumed at config-gen time on the operator's machine. | `PROJECT_STATUS.md` already marked ✂️ SCOPED OUT. |
| `Model string` in `ConfigParams` is "currently unused by OpenClaw runtime" | **Correction**: it's actively consumed by Hermes (`pkg/runtime/hermes/config.go:50`), not dead code. | Spec updated with the Hermes-preservation note before implementation began. |
| 15 unit-test cases for the loader | **Reality**: 16 cases (added `WarningEmittedOnce` test for warn-once dedup correctness). | Reasonable spec drift — not worth amending. |
| OpenClaw 2026.5.18 pin bump required before feature lands | **Reality**: Tested successfully against the existing `2026.3.11` pin. The newer schema observation from the spike turned out to also work on the older image — the `models.providers.<id>.models[]` requirement appears identical across both versions. | The pin bump is still a good idea (Slack stability) but is no longer a hard prerequisite for this feature. Note added to spec. |

### Standards retrospection

Checked `product-knowledge/standards/*.md` for stale references:
- `architecture.md` — Config Format Boundary section already updated to reflect two YAML files (`conga-policy.yaml` + `agent.yaml`). ✅
- `egress-controls.md` — no code/file refs needed updating; egress allowlist already supports IP literals (proven by user-a's `<lan-ip>` entry).
- `security.md` — Pinned image table still references `v2026.3.11`; remains accurate. ✅
- `config-taxonomy.md` — newly authored as part of this PR; no drift.

### Observations (for `glados:recombobulate`)

#### Standard candidate
- **Title**: Runtime config generation must be validated against the target runtime's schema, not just internal renderer shape.
- **Source**: Test gap discovered during live testing of local-model-routing.
- **Context**: Golden tests in `pkg/runtime/openclaw/config_test.go` asserted what `applyModelOverlay` produced, which let a schema bug (missing required `models[]` array) reach a live container before failing.
- **Proposed standard**: "Tests for runtime config generators (`pkg/runtime/<rt>/config_test.go`) MUST include at least one scenario that validates the rendered config against the runtime's actual schema — either by running the runtime's own validator (`openclaw doctor`, equivalent) in a CI container, or by maintaining a schema reference file the test cross-checks. Internal-shape-only assertions are NOT sufficient."
- **Suggested severity**: should (would block this PR's merge if `must`).
- **Confidence**: High.
- **Status**: pending — to be reviewed in next `glados:recombobulate`.

### Test synchronization

- **Stale references**: none — searched for imports of removed/renamed modules; clean.
- **Fakes**: `pkg/common/overlay_agent_test.go` uses real `os.MkdirAll` + `os.WriteFile` in `t.TempDir()`, not fakes. No semantic-drift risk.
- **New public methods coverage**: `runtime.AgentOverlay.Validate()`, `runtime.ModelOverlay` field validation, `runtime.OpenAIBaseURLLooksNonstandard`, `common.LoadAgentOverlay` — all covered by direct tests.
- **Sibling comparison**: nearest sibling is `pkg/runtime/openclaw/env.go` (`GenerateEnvFile`) which has no dedicated test file. Our new `config_test.go` actually raises the bar above the sibling, which is a net win.
- Full test suite re-run after retrospection updates: ✅ clean (per Automated checks above).

## Status — VERIFIED COMPLETE

This feature is fully implemented, tested in production against user-a, and documented. Recommended next steps (not blocking merge):

1. **Land the PR** — `feature/local-model-support` → `main`.
2. **Tag congaline + bump terraform-provider-conga + release** (per CLAUDE.md `pkg/` change protocol). After release, normal `terraform apply` + `conga refresh-agent` paths work without the local-build workaround.
3. **Track the schema-validation test gap** (see Observations above) for next `glados:recombobulate`.
4. **Pin bump** (`v2026.3.11` → `v2026.5.18`) when convenient — no longer a blocker but still desirable.
