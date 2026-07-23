# Requirements: local-model-routing

## Goal

Allow any agent to route its model traffic to a self-hosted OpenAI-compatible LLM via the existing `behavior/agents/<name>/` overlay. First production use case: point the `user-a` user agent at Qwen 3.6 on the DGX Spark (`http://<lan-ip>:11434/v1`) over the existing WireGuard VPN. Same mechanism must work on AWS, local, and remote providers without divergence.

## Why now

- The VPN between the AWS VPC and user-a's Spark is up and healthy.
- The egress allowlist already includes `<lan-ip>` (global and `agents.user-a.egress_allowed_domains`); UDP/51820 is open in `egress_ports`. The network layer is solved.
- Per-agent secrets (`agents.user-a.secrets = {}` in tfvars) already convert kebab-case → SCREAMING_SNAKE env vars via `pkg/common/secrets.go` `SecretNameToEnvVar`. Adding `openai-api-key` already exports `OPENAI_API_KEY` to the container.
- The single remaining gap: `pkg/runtime/openclaw/openclaw-defaults.json:4` hardcodes `anthropic/claude-opus-4-6` and the generated `openclaw.json` has no per-agent override slot.

## Functional Requirements

### FR-1: `agent.yaml` is the per-agent runtime-config overlay file
- Location: `behavior/agents/<name>/agent.yaml`.
- Optional. Agents without the file behave exactly as today.
- The file is gitignored at the directory level (`behavior/agents/*/` already ignored; only `_example/` is committed). No real agent overlay is ever in git.
- Top-level keys are forward-compatible; this feature only defines `model:`.

### FR-2: `model:` block schema
```yaml
model:
  provider: openai        # required if block is present; enum: "openai", "anthropic" (no-op)
  name: qwen-3.6          # required when provider is set
  base_url: http://<lan-ip>:11434/v1   # optional; required for openai when not using OpenAI's hosted API
```
- All three fields validated:
  - `provider` ∈ a known set (start with `openai` and `anthropic`).
  - `name` non-empty when `provider` is set.
  - `base_url` parses as a valid URL when present.
- `api_key` is **not** a field in `agent.yaml`. Keys live in the existing secrets store.

### FR-3: Overlay loader extension (`pkg/common/`)
- The codepath that already discovers `behavior/agents/<name>/` for prompts also loads and parses `agent.yaml` when present.
- Parsed result surfaces to runtime config generation as a typed Go struct (e.g. `AgentOverlayConfig { Model *ModelConfig }`).
- Missing file: surfaces nil model; identical to today's behavior.
- Malformed YAML or invalid values: hard error at refresh time; do not silently fall back.

### FR-4: OpenClaw config overlay (`pkg/runtime/openclaw/`)
- `GenerateConfig` accepts the overlay's `Model` and, when present, overlays into the generated `openclaw.json`:
  - `agents.defaults.model.primary` ← `<provider>/<name>` (e.g. `openai/qwen-3.6`).
  - `agents.defaults.models["<provider>/<name>"]` ← per-model entry carrying `baseURL` (exact shape: see spike).
- When `Model` is nil, the generated `openclaw.json` is byte-identical to today's output for that agent.
- The `openclaw-defaults.json` template stays the Anthropic default; the overlay only changes keys that the user explicitly set.

### FR-5: API key flow (no changes)
- `openai-api-key` declared in tfvars `agents.<name>.secrets` (AWS) or set via `conga secrets set openai-api-key --agent <name> --value <key>` (local/remote).
- Existing `SecretNameToEnvVar` exports `OPENAI_API_KEY` to the container.
- `GenerateEnvFile` requires no changes.

### FR-6: Provider parity
- The same `behavior/agents/<name>/agent.yaml` produces equivalent results on AWS, local, and remote providers.
- "Equivalent" means: the rendered `openclaw.json` carries the same `model.primary` and `models[...]` entries on all three providers.

### FR-7: Documentation
- `behavior/agents/_example/agent.yaml.example` ships with the feature, with inline comments for each field and a note that the file is gitignored for real agents.
- `CLAUDE.md` "Behavior files" section gains a short paragraph explaining `agent.yaml`.
- `product-knowledge/ROADMAP.md` cross-links this feature as the minimal precursor to the planned Bifrost work (item #22 in PROJECT_STATUS).

### FR-8: No new public surfaces
- No new CLI commands.
- No new terraform variables.
- No new terraform-provider-conga resource attributes.
- No new egress policy fields.
- No new env var conventions.

The feature is intentionally implemented as **extensions to existing pipes**, not new ones.

## Non-Goals

- Provisioning, configuring, or documenting the WireGuard VPN itself (already operator-managed; bootstrap installs `wireguard-tools` but the tunnel is out of scope).
- Bifrost sidecar, cross-provider fallback, cost tracking, classifier selection, or multi-model-per-request routing. These belong to the planned Bifrost spec (ROADMAP #22).
- Changing the default model for any existing agent.
- Moving secrets out of tfvars.
- Cleaning up `behavior/agents/team-a/` (worktree-only directory; separate concern).

## Success Criteria

### SC-1: `user-a` reaches Qwen via the Spark
- After `terraform apply` + `conga refresh-agent user-a`, `mcp__conga__conga_container_exec --agent user-a` showing `cat /home/node/.openclaw/openclaw.json` reveals `model.primary` = `openai/qwen-3.6` and `models["openai/qwen-3.6"].baseURL` = `http://<lan-ip>:11434/v1`.
- A Slack DM to `user-a` produces a Qwen-shaped response (not Anthropic).
- `mcp__conga__conga_get_logs --agent user-a` shows the outbound URL is the Spark.
- `mcp__conga__conga_get_proxy_logs --agent user-a` shows Envoy's connect target is `<lan-ip>:11434`.

### SC-2: No regression for non-opting agents
- `user-b`, `user-c`, `team-c`, `team-a` all continue to route to `api.anthropic.com`.
- Their rendered `openclaw.json` diffs (before vs. after this feature merges) are empty.
- A Slack DM to a control agent still returns an Anthropic-shaped response.

### SC-3: Provider-agnostic
- A fresh `conga admin setup --provider local` on a dev machine that contains the same `behavior/agents/user-a/agent.yaml` produces an `openclaw.json` with the same model overlay (Spark URL).
- Same overlay applied via the remote provider produces the same `openclaw.json`.

### SC-4: Terraform untouched
- `terraform plan` from `terraform/environments/production/` is clean (no diff) after this feature merges, holding tfvars constant.
- No new variables in `terraform/modules/congaline/variables.tf`.
- No new attributes in `terraform-provider-conga`'s `conga_agent` resource.

### SC-5: Failure modes are loud
- Missing `agent.yaml` → silent no-op (same as today).
- Malformed `agent.yaml` → refresh fails with a clear error referencing the file path and the invalid field. No silent fallback to Anthropic.
- `provider: openai` with no `openai-api-key` secret set → container starts but the model call fails with an upstream auth error visible in `conga logs`. (Documented in the spec; not a runtime guard, because the secret check would require new policy plumbing.)

### SC-6: Spike findings recorded
- `specs/2026-05-19_feature_local-model-routing/spike-openclaw-openai.md` exists with:
  - openclaw/openclaw#45311 status (open / fixed-in-which-version).
  - Decision: stay on `2026.3.11` or bump the pin (with target tag).
  - The verified `openclaw.json` schema for OpenAI-compatible providers against the chosen image.

## Constraints & Assumptions

- **OpenClaw must natively support OpenAI-compatible providers** in the chosen image. If the spike reveals it does not, this feature is blocked pending Bifrost; we do NOT add a translator sidecar as part of this feature.
- The WireGuard VPN remains operator-managed.
- The Spark IP is stable. If it changes, both `terraform.tfvars` (`egress_allowed_domains`) and `behavior/agents/user-a/agent.yaml` (`base_url`) must be updated — documented but not automated.
- The `user-a` agent runs in single-user (`type = "user"`) mode; no cross-agent model sharing semantics needed.

## Open Questions (resolved in the spec phase)

1. **Pin status**: Is openclaw/openclaw#45311 fixed in a release after `2026.3.11`? Investigate via `gh issue view openclaw/openclaw#45311`.
2. **Exact `openclaw.json` shape** for OpenAI-compatible providers (depends on Q1's chosen version):
   - `agents.defaults.models["openai/<name>"]` with a `baseURL` field?
   - Top-level `providers.openai.baseURL`?
   - Pure env-var (`OPENAI_BASE_URL`) with no config-file slot?
   - Plugin block (`plugins.entries.openai`)?
3. **Validator strictness**: should we hard-fail at `conga refresh-agent` time when `provider: openai` is set but `openai-api-key` secret is not present, or allow the upstream failure to be the signal? (Lean toward the latter — keeps the loader free of secret-store knowledge.)
