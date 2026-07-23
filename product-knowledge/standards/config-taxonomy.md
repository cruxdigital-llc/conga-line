<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-06-10
To modify: Edit directly. This is the single source of truth for where per-agent
configuration lives. Update when introducing a new per-agent concern; do NOT
create a new config file/format without consulting the decision rule below.
-->

# Per-Agent Configuration Taxonomy

> **Conga Line is an open-source IaC tool with three runtime environments** (local Docker, remote SSH, AWS). Configuration touching agents spans several layers by design. This document is the canonical map: *for any new per-agent configuration concern, where does it go?*
>
> The goal: **a contributor can pick the right home in under 60 seconds** without reading source. Avoid introducing new files or formats. Extend in place.

## The taxonomy

| Layer | Concern | Location | Format | Provider scope | Authored by |
|---|---|---|---|---|---|
| **Infrastructure** | Agent existence, gateway port, egress allowlist (incl. private IPs), channel bindings, secret values, host resources | `terraform/environments/<env>/terraform.tfvars` `agents = {}` map | HCL (Terraform) | AWS (declarative). Local/remote use CLI flags (`conga admin add-user`). | Operator. Gitignored — only `.example` is committed. |
| **Cluster policy** | Egress allow/deny lists, routing rules, posture (enforce/validate), drift detection | `~/.conga/conga-policy.yaml` with per-agent overrides under `agents.<name>.*` | YAML | All providers | Operator. Lifecycle via `conga policy {validate,deploy,drift}`. Egress overrides are **additive** with the global lists (see `standards/egress-controls.md` — *Global + agent policies are additive*); routing and posture overrides still replace. |
| **Runtime overlay** | Model (provider, name, base_url), **subagents** (in-runtime delegation — secondary model + behavior knobs), prompts (SOUL/AGENTS/USER), future: memory, tools, limits, multi-modal model refs, fallback chains | `agents/<name>/agent.yaml` + `agents/<name>/*.md` | YAML + Markdown | All providers (provider-agnostic; same files produce same runtime config on local/remote/AWS) | Operator. Gitignored at the per-agent dir level — only `agents/_example/` and `agents/_defaults/` (role packages + runtime/type defaults) are committed. |
| **Runtime customization (declarative)** | The same un-modeled OpenClaw config as the admin layer below (`mcp.servers`, `skills`, `tools.allow/deny`, …), but **version-controlled in the repo** at two granularities: a **fleet baseline** for every agent and **per-agent** overrides. | **Fleet:** `agents/_defaults/<runtime>/fleet-custom.json` (committed). **Per-agent:** `agents/<name>/custom.json` (gitignored per-agent). Conga deploys them beside `openclaw.json` as `fleet-custom.json` / `agent-managed-custom.json` and references them in the `$include` array. | JSON (JSON5 tolerated) | All providers (OpenClaw deep-merges; later in the `$include` array wins, root wins over all) | **Operator, in the repo.** Re-synced from source on every provision/refresh/bind (propagation). Must **not** declare Conga-owned keys (`$include`/`channels`/`gateway`/`plugins`) — guarded pre-deploy (fail closed) and by the integrity check, and hash-verified on the host. See `specs/2026-06-10_feature_fleet-baseline-configuration/`. |
| **Runtime customization (admin drift)** | Any OpenClaw config Conga does **not** manage, applied as an **on-host hot-fix** (not version-controlled) — `mcp.servers`, `skills`, `tools.allow/deny`, `agents.defaults.sandbox`, `memorySearch`, `ui`, `cron`, etc. | `agent-custom.json` in the agent **data dir** (`~/.conga/data/<name>/` local, `/opt/conga/data/<name>/` remote/AWS), referenced from the managed `openclaw.json` via `$include` | JSON (JSON5 tolerated by OpenClaw) | All providers (OpenClaw deep-merges; **wins over the declarative layers** but the managed root wins over it) | **Provider materializes it empty (`{}`); the administrator edits it in place.** Conga never overwrites it except `conga agent rebaseline`. Same reserved-key guard as the declarative layer; **not** hashed (drift is expected). See `specs/2026-06-09_feature_infrastructure-only-simplification/`. |
| **Runtime persistence** | Identity (name, type, runtime choice, allocated port, channel binding state) | `~/.conga/agents/<name>.json` (local) / `/opt/conga/agents/<name>.json` (remote) / SSM `/conga/agents/<name>` (AWS) | JSON | Per-provider | **Materialized by the provider** at provision time, not hand-edited. |
| **Secrets** | API keys, OAuth tokens, channel bot tokens | Files mode 0400 (`~/.conga/secrets/agents/<name>/<key>` on local/remote) or AWS Secrets Manager (`conga/agents/<name>/<key>` on AWS) | Native | Per-provider | Operator. Authored via tfvars (AWS) or `conga secrets set` (local/remote). Never in git. |

> **Remote-MCP OAuth credentials** are a special case. OpenClaw stores each OAuth MCP server's credential (access + refresh token) as a per-**container** file at `/home/node/.openclaw/mcp-oauth/<server>-<hash>.json`, **outside** every layer above — it is not authored by the operator and not (yet) mirrored to the secrets store, so it is invisible to the declarative fleet and is lost on data-dir loss / expiry. Recover with `conga mcp login <server> --agent <name>`; find affected agents with `conga doctor`. Bringing this blob under the Secrets layer (capture + restore) is the Phase 2 work in `specs/2026-07-22_feature_mcp-oauth-credential-lifecycle/`.

## The custom-config layers (OpenClaw `$include`) — precedence

OpenClaw config that Conga does **not** model is layered via the managed
`openclaw.json`'s `$include` array. There are **four** effective layers; on a
conflicting scalar key the higher one wins, and distinct keys union:

| # | Layer | Source (edit here) | Deployed file | Version-controlled? |
|---|---|---|---|---|
| 1 (highest) | Managed root | generated by Conga | `openclaw.json` | — |
| 2 | Admin drift | edit on the host | `agent-custom.json` | No (host hot-fix) |
| 3 | Per-agent declarative | `agents/<name>/custom.json` | `agent-managed-custom.json` | Yes (gitignored per-agent) |
| 4 (lowest) | Fleet baseline | `agents/_defaults/<runtime>/fleet-custom.json` | `fleet-custom.json` | Yes (committed) |

Effective precedence: **root > admin-drift > per-agent > fleet.** Admin drift
is highest among the includes (sacrosanct, never clobbered — a host hot-fix
overrides repo-declared config); the Conga-owned root always wins over every
include. Inspect the live, ordered layers with **`conga agent show-config <name>`**
(CLI / `--output json` / MCP `conga_agent_show_config`).

**Which layer do I want?**
- Same MCP server / skill / tool for **every** agent → **fleet** (`_defaults/<runtime>/fleet-custom.json`), `conga refresh-all` to propagate.
- A declarative, reviewable override for **one** agent → **per-agent** (`agents/<name>/custom.json`), `conga refresh --agent <name>`.
- An urgent **on-host** hot-fix you'll reconcile later → **admin drift** (`agent-custom.json`), reset with `conga agent rebaseline`.

A reserved-key violation (`channels`/`gateway`/`plugins`/`$include`) in the fleet
or per-agent source **fails the deploy closed** — the bad file never reaches a
host (fleet blast-radius mitigation). The managed layers are hash-verified on the
host; admin drift is not.

### The fleet runtime baseline (`openclaw-defaults.json`)

The OpenClaw **runtime baseline** that feeds the managed root (`agents.defaults`
model, `tools.profile`, heartbeat, …) is operator-editable at
`agents/_defaults/openclaw/openclaw-defaults.json` (committed; synced to the AWS
host with the rest of the `agents/` tree). Editing it changes the fleet-wide base
without a binary/provider release. The binary keeps an embedded copy as a
fail-safe fallback if the file is absent or malformed. (Today only the Go
generation paths — `conga refresh` on AWS, plus local/remote — read the file;
unifying the AWS fresh-boot bash heredocs to consume it is a tracked follow-up.)

## Decision rule — answer these in order

When adding a new per-agent concern, walk this list top-to-bottom and stop at the first **yes**:

1. **Does it affect AWS infrastructure** — security groups, NACLs, IAM, EBS, SSM, EC2 sizing, Slack app routing topology, the agent's existence on the host? → **Infrastructure (tfvars).**
2. **Is it a security/policy decision** that has a global default with per-agent override semantics, and benefits from `validate/deploy/drift` lifecycle? → **Cluster policy (`conga-policy.yaml`).**
3. **Does the agent's runtime (OpenClaw, Hermes) consume it directly** to generate `openclaw.json` / `config.yaml`? Is it operator-authored and provider-agnostic? → **Runtime overlay (`agent.yaml`).**
4. **Is it computed by the provider at provision/refresh time** rather than authored by an operator? → **Runtime persistence (per-agent JSON / SSM).** You almost never extend this on purpose; it grows with new fields on `provider.AgentConfig`.
5. **Is it a credential value?** → **Secrets store.**

If two layers seem to apply, default to the lower number in the list — infrastructure beats policy beats overlay. The exception: if a concern is *both* a policy decision *and* operator-authored runtime config (e.g. "this agent uses model X, and only this model"), it goes in the overlay if the runtime is the primary consumer, in policy if cluster-wide enforcement is the primary consumer.

## Anti-patterns (never do these)

- ❌ **Runtime config (model, prompts, tools) in tfvars.** Breaks portability — `agent.yaml` must produce the same behavior on local/remote/AWS without invoking terraform.
- ❌ **Infrastructure config (ports, egress IPs, SSH host) in `agent.yaml`.** The CLI/terraform provisioning flow won't see these and the rendered config can disagree with the actual host state.
- ❌ **Secret VALUES in `agent.yaml`.** OpenClaw issue #9627 — secrets in disk-resident config files. Always go through the secrets store; reference by name (e.g. `openai-api-key`) and let `SecretNameToEnvVar` inject `OPENAI_API_KEY`.
- ❌ **A new YAML file per concern** (`tools.yaml`, `memory.yaml`, ...). Extend `agent.yaml` with a new versioned top-level key instead. The schema is designed to absorb growth (see `specs/2026-05-19_feature_local-model-routing/spec.md`).
- ❌ **Editing files under `~/.conga/agents/<name>.json` by hand.** That file is materialized by the provider; manual edits get clobbered on the next refresh. Use the provider's API or its source-of-truth (tfvars on AWS, CLI on local/remote).
- ❌ **Committing real agent definitions.** Only `agents/_example/`, `terraform.tfvars.example`, and `backend.tf.example` go in git. The gitignore already enforces this; do not bypass.
- ❌ **Changing the location or format of an existing layer** without a deprecation cycle. This document is the contract; the cost of moving is high.

## Worked examples

### Example 1: "I want agent X to use a custom LLM endpoint"
Walk the rule:
1. Affects AWS infra? **No.** The endpoint URL is application-layer, but its *reachability* (egress allowlist) IS infra → that part goes in tfvars `agents.<name>.egress_allowed_domains`. The URL itself does not.
2. Security/policy decision? **No** (the choice of *which* model is not a policy decision; the choice of which models are *allowed* would be).
3. Runtime-consumed, operator-authored, provider-agnostic? **Yes.** → **`agents/<name>/agent.yaml`** with `model: { provider, name, base_url }`.
4. (also) Credential? **Yes**, the API key → secrets store (`openai-api-key`). The `base_url` and `name` go in overlay; the key goes in secrets. Two homes, deliberately.

### Example 2: "I want to restrict agent X to a single Slack channel"
Walk the rule:
1. Affects AWS infra (channel bindings drive the router's `routing.json` and the security group's allowed messaging endpoints)? **Yes.** → **tfvars `channels.slack.bindings`**, not the overlay.

### Example 3: "I want a per-agent token budget cap"
Walk the rule:
1. Affects AWS infra? **No.**
2. Security/policy decision with global default? Possibly — if the cap is a uniform policy with per-agent overrides, → `conga-policy.yaml`. If it's purely per-agent runtime config without policy lifecycle, → `agent.yaml`. Both could be argued; pick `agent.yaml` for now (simpler), revisit if cost-policy enforcement becomes a thing (likely with Bifrost).

### Example 4: "I want agent X to use a custom prompt"
Walk the rule:
1. Affects AWS infra? No.
2. Policy decision? No.
3. Runtime-consumed, operator-authored, provider-agnostic? Yes. → **`agents/<name>/SOUL.md`** (or `AGENTS.md`, `USER.md`). Already the established pattern; don't duplicate in `agent.yaml`.

### Example 5: "I want agent X to use Opus as its primary and delegate mechanical work to a cheap Qwen"
Walk the rule:
1. Affects AWS infra? **The endpoints' reachability** (egress allowlist) IS infra → both Anthropic and the LLM-proxy host must appear in `agents.<name>.egress_allowed_domains` (AWS tfvars) or `agents.<name>.egress.allowed_domains` (local/remote policy). The `conga admin add-user` flow warns at provision time when a declared overlay endpoint isn't allowed (see `pkg/common/egress_check.go`).
2. Security/policy decision? No.
3. Runtime-consumed, operator-authored, provider-agnostic? **Yes.** → **`agents/<name>/agent.yaml`** with a `subagents:` block (schema `version: 2`):
   ```yaml
   version: 2
   # No model: block — primary stays at the runtime default (Anthropic Opus from openclaw-defaults.json).
   subagents:
     model:
       provider: openai
       name: qwen-2.5-72b-instruct
       base_url: https://litellm.lan/v1
     delegation_mode: prefer
     max_concurrent: 4
   ```
   The five-role catalog at `agents/_defaults/<runtime>/role-*/` ships exactly this shape — the operator can copy one of the canonical roles by running `conga admin add-user --role role-code-dev <name>` (or `--role role-writing`). The CLI copies the role's `agent.yaml`, `SOUL.md`, `AGENTS.md`, and `USER.md.tmpl` into `agents/<name>/`, preserving any pre-existing customization.
4. Credential? The orchestrator's Anthropic API key flows through the existing `anthropic-api-key` secret. If the subagent's provider is `openai`, its key flows through the existing `openai-api-key` secret. **No new secret names are introduced for subagents** — the secret-name → env-var mapping is unchanged.

### Example 6: "I want to add the Linear MCP server to agent X"
Walk the rule:
1. Affects AWS infra? Only the endpoint's **reachability** — the MCP host must be in the egress allowlist (tfvars/policy), same as any new endpoint.
2. Policy decision? No.
3. Runtime-consumed, operator-authored, provider-agnostic, **and modeled by Conga**? No — Conga deliberately does **not** model `mcp.servers` (it would be endless to model every OpenClaw config concern). This is **Runtime customization** → pick the layer by scope (see *The custom-config layers* above):
   - **All agents** (declarative, committed) → `agents/_defaults/openclaw/fleet-custom.json`, then `conga refresh-all`.
   - **One agent** (declarative, reviewable, in the repo) → `agents/<name>/custom.json`, then `conga refresh --agent <name>`.
   - **On-host hot-fix** (not version-controlled) → edit `agent-custom.json` in the agent data dir directly; reset with `conga agent rebaseline <name>`.

   The content is the same in any layer:
   ```json
   { "mcp": { "servers": { "linear": { "url": "https://mcp.linear.app/sse", "transport": "sse" } } } }
   ```
   OpenClaw `$include`-merges them under the managed `openclaw.json` (root wins, then admin drift, then per-agent, then fleet); edits survive restarts and `conga refresh`. Don't put `channels`/`gateway`/`plugins` in any layer — those are Conga-owned, rejected pre-deploy (fail closed) and flagged by the integrity check. The MCP host must also be in the egress allowlist; `conga refresh`/provision warns at deploy when a declared `mcp.servers.*.url` isn't allowlisted. Note: with a root `$include`, in-container `openclaw config set` fails closed — edit the source files or use the Conga CLI.

## Why three layers (overlay vs policy vs infra) instead of one?

This was reviewed in the architect deep-dive (`specs/2026-05-19_feature_local-model-routing/README.md`). Summary:

- **Infrastructure** is AWS-specific and terraform-driven; collapsing it into a portable layer would lose the declarative AWS resource model.
- **Cluster policy** has unique lifecycle (validate/deploy/drift) that runtime overlay doesn't need.
- **Runtime overlay** is hand-edited per-agent without any global default; collapsing into policy would force operators to express "this agent uses this model" as a per-agent override of a non-existent global, which is awkward.

Each layer earns its place. The cost is more places to look; the taxonomy doc is the compensation.

## Extending this document

When adding a new per-agent concern:
1. Update the **decision rule** if the new concern doesn't fit cleanly (rare — usually it slots into runtime overlay).
2. Update the **taxonomy table** with the new column entry if you're adding a new layer (very rare; requires deliberate architectural decision).
3. Add a **worked example** for non-obvious concerns.
4. Cross-link from the relevant spec.

Do **not** create a "config-taxonomy-v2.md" or similar. Update this file in place; git history preserves the rationale.
