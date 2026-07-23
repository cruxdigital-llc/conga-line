<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-07-15
To modify: Edit directly. Add new entries to "Active workarounds" as
upstream bugs surface. Move entries to "Resolved upstream" when the fix
ships in a version we've pinned to.
-->

# Upstream OpenClaw Issues — Active Workarounds

> Conga Line pins to a specific OpenClaw image tag for bisectability
> (see `CLAUDE.md`, image pin section). When an upstream bug bites us
> before it's fixed in a pinned version, we add a workaround in our own
> code or config and track it here. This document is the operator's
> answer to "why does Conga emit *this* unusual config?"

## Active workarounds

### #25592 — Team agents leak preamble text to Slack channels

**Upstream:** [openclaw/openclaw#25592](https://github.com/openclaw/openclaw/issues/25592) — open since 2026-02; still open at v2026.5.26.

**Symptom.** Bare `text` content blocks emitted by Claude *before* a tool call — preamble narration, "let me think about this" prose, decision-not-to-reply commentary, inter-tool acknowledgements — are delivered to the channel as visible Slack messages. Real example from `team-a` on 2026-05-27 (on v2026.5.18, before our fix):

> *"user-c is posting status updates — Linear tickets filed and Phase 1 MR up. Not directed at me, no question to answer. Just a team update. Let me capture the ticket references to memory and stay quiet."*

The leaked content is **not** an Anthropic `thinking` block. Those are tagged `isReasoning: true` and are suppressed by [#84319](https://github.com/openclaw/openclaw/issues/84319) (closed in v2026.5.20). What leaks here is plain assistant text that the model uses as a "scratchpad" before its tool calls. It bypasses every reasoning-suppression code path because it's not flagged as reasoning.

**Why it bites team agents specifically.** Team agents post in shared Slack channels with multiple humans watching. Any inter-message narration is professional embarrassment. User agents in DMs are 1:1 and a touch of preamble is acceptable.

**Conga workaround.** Two coordinated changes, both team-agents only:

1. **Config-side** — `applyTeamChannelDiscipline()` in `pkg/runtime/openclaw/config.go`:
    - `messages.groupChat.visibleReplies: "message_tool"` — gates delivery on an explicit `message()` tool call.
    - `tools.alsoAllow: ["message"]` — restores the `message` tool that `tools.profile: "coding"` (set in `openclaw-defaults.json`) strips out. Without this the agent would have no tool with which to deliver replies and every turn would silently drop.

2. **Prompt-side** — "Channel Discipline" section in every team-agent `AGENTS.md`:
    - `agents/_defaults/openclaw/team/AGENTS.md` (generic team default)
    - `agents/_defaults/openclaw/role-code-dev/AGENTS.md`
    - `agents/_defaults/openclaw/role-writing/AGENTS.md`
    - Per-agent `agents/<name>/AGENTS.md` for any team agent that has its own (e.g. `agents/team-a/AGENTS.md`).

    Tells the model: *only `message(...)` posts; bare text is internal; if you finish a turn without calling `message()` when you meant to reply, that reply is lost*.

**Why both.** Config alone causes silent drops when the model forgets to call the tool (see [#85384](https://github.com/openclaw/openclaw/issues/85384) and closed-not-planned [#77320](https://github.com/openclaw/openclaw/issues/77320)). Prompt alone doesn't suppress preamble because the model isn't perfectly disciplined. Together: config enforces, prompt teaches.

**Scope discipline.** The branch fires only when `params.Agent.Type == provider.AgentTypeTeam` in `GenerateConfig`. User agents stay on the looser defaults — silent-drop risk is higher in a 1:1 DM (a missed reply is noticed immediately) and the preamble cost is lower.

**Validation.** After refreshing a team agent, inside the container:
- `cat /home/node/.openclaw/openclaw.json | jq '.messages.groupChat.visibleReplies'` → `"message_tool"`
- `cat /home/node/.openclaw/openclaw.json | jq '.tools'` → has `profile: "coding"` AND `alsoAllow: ["message"]`
- `grep -c "Channel Discipline" /home/node/.openclaw/data/workspace/AGENTS.md` → `1`
- Logs should NOT contain `[agents/tool-policy] tool policy removed ... message` (that line indicates `coding` profile stripped the tool — meaning `alsoAllow` didn't land).

**Escape conditions — remove the workaround when:**
1. Upstream #25592 is fixed in a version we pin to. The fix could be (a) a config knob like `suppressInterToolText: true`, (b) an OpenClaw behavior change that treats bare-text-before-tool-call as internal, or (c) something else entirely — the issue lists three possibilities and the maintainers haven't picked one.
2. We move team agents off the `coding` profile. If we adopt `messaging` profile (which preserves `message` natively per [delegation-routing spec § upstream-capability](../../specs/2026-05-22_feature_delegation-routing/upstream-capability.md)), the `tools.alsoAllow` half becomes redundant — but `visibleReplies` still matters.

**Related open issues to watch:**
- [#85384](https://github.com/openclaw/openclaw/issues/85384) — *"message_tool_only group chats can go silent when final reply is emitted instead of message tool"*. The silent-drop side of our workaround.
- [#80458](https://github.com/openclaw/openclaw/issues/80458) — *"buildEmbeddedRunPayloads leaks 'commentary' phase text to channel delivery"*. Adjacent leak path, codex/phase-tagged providers only — doesn't currently bite us with Claude.

---

### CRIT-A (internal) — Hermes runtime ignores `agent.yaml` `model:` overlay

**Scope:** Conga-internal gap, not an upstream OpenClaw/Hermes bug. Logged here because the user-visible symptom (silently using a different model than the operator declared) is the same class as the upstream leaks tracked above, and the workaround pattern (degraded mode with a loud warning) is symmetric.

**Symptom.** The Hermes runtime config generator (`pkg/runtime/hermes/config.go`) consumes only `params.Overlay.Subagents`; until recently it did not read `params.Overlay.Model`. Operators following the role-package READMEs for `agents/_defaults/hermes/role-{ops,data,research}/` were instructed to set a `model:` block pointing at a Qwen endpoint — but the Hermes generator never emitted it. The agent silently used whatever was set as the runtime default during `conga admin setup` (typically Anthropic Opus), so:

- the "cheap model" cost saving never materialized
- the provision-time egress pre-flight warned about the `model.base_url` host not being in the allowlist, actively misleading operators into "fixing" their allowlist
- nothing actually used the endpoint

Closed cleanly: review pass-2 finding CRIT-A.

**Conga workaround (degraded mode).** Three parts:

1. **`applyModelOverlay` in `pkg/runtime/hermes/config.go`** — when `params.Overlay.Model` is non-nil, writes `cfg["model"] = provider + "/" + name` so `hermes /status` reflects operator intent. When the `base_url` isn't a recognized Hermes adapter host (`openrouter.ai`, `nousresearch.com`, `z.ai`, `kimi.com`, `minimax.com`), a one-time stderr warning explains the agent will fall back to whatever provider config Hermes has wired up at runtime (typically the setup-time default) and won't actually address the custom endpoint.

2. **Role packages** — `agents/_defaults/hermes/role-{ops,data,research}/agent.yaml` no longer ship a `model:` block. They're minimal `version: 2` shells. The corresponding READMEs explain that Hermes per-agent primary-model override happens via `cli-config.yaml` on the container, not via the Conga overlay, until the spec lands.

3. **Test in `pkg/runtime/hermes/config_test.go`** — `TestGenerateConfig_HermesModelOverlay_DegradedMode` + `_KnownAdapterHostNoWarning` + `_OverridesParamsModel` lock the behavior in.

**Validation.** After refreshing a Hermes agent with a `model:` overlay:
- `cat /home/node/.hermes/cli-config.yaml | grep '^model:'` shows `provider/name` from the overlay (not `params.Model`).
- `journalctl -u conga-<agent>` (or container logs) shows the one-time warning for custom `base_url` hosts.
- `pkg/common/role_defaults_test.go` enforces that Hermes Qwen roles ship **without** `model:` (the opposite of the OpenClaw assertion); a future role package that re-adds `model:` to a Hermes Qwen role will fail the test.

**Escape conditions.** Remove this degraded-mode path when:
- A spec — provisionally `spec/2026-XX-XX_feature_hermes-model-overlay/` — wires up real per-agent primary-model routing in Hermes (full parity with OpenClaw's `models.providers.<id>` block), with end-to-end testing against a real Hermes container.

Until that spec ships, the Hermes side of the role catalog is intentionally Anthropic-leaning. OpenClaw users get full delegation routing; Hermes users get subagent routing only.

---

### #73182 — Claude thinking default silently flipped to `medium` (cost regression)

**Upstream:** [openclaw/openclaw#73182](https://github.com/openclaw/openclaw/issues/73182) — open since 2026-04-28.

**Symptom.** OpenClaw v2026.4.22 raised the implicit default thinking level for reasoning-capable models (Claude Opus, Sonnet) from `off` to `medium`. Every turn now requests extended thinking from the Anthropic API even when the operator never asked for it. Anthropic spend doubles overnight. The boot banner shows it: `[gateway] agent model: anthropic/claude-opus-4-7 (thinking=medium, fast=off)`.

**Status.** We have NOT applied a config workaround for this yet. The leak symptom from #25592 was the user-visible issue; the cost symptom from #73182 is silent (you only notice it on the bill). The upstream fix discussion is ongoing and may add an `agents.defaults.reasoningDefault` knob (PR by deepujain landed partially — commit `0c9f84451a9f`).

**Mitigation options if cost becomes a blocker:**
- Set `agents.defaults.reasoningDefault: "off"` in `pkg/runtime/openclaw/openclaw-defaults.json` once the schema is finalized upstream.
- Or per-agent in `agents/<name>/agent.yaml` once that overlay supports the field.

**Watch:** Anthropic monthly spend on team-agent accounts. If the marginal cost is acceptable (thinking does measurably improve Opus output), do nothing; if not, apply the workaround above.

---

### #43767 — Heartbeat ignores `lightContext`, reloads full context every wake (cost)

**Upstream:** [openclaw/openclaw#43767](https://github.com/openclaw/openclaw/issues/43767) — open. Related: [#61395](https://github.com/openclaw/openclaw/issues/61395) (`lightContext` also fails to filter workspace files).

**Symptom.** With `agents.defaults.heartbeat.lightContext: true` set (expecting a cheap keep-alive), the heartbeat still loads the full agent context + accumulated conversation history on every wake. On a 55-minute interval that's ~26 full-context turns/day, 24/7. Each wake re-caches a large stable prefix that expires before the next wake (default 5-minute cache TTL), so the cost is almost entirely `input_cache_write` with near-zero `input_cache_read`.

Real example (2026-07): a team agent's standup poster ran on the heartbeat and cost **~$24/day (~$700/mo)** — ~96% of the org's Anthropic bill — even on days with zero channel messages, to post a message a few times a week. Full investigation: [agent-cost-and-runtime-config.md](./agent-cost-and-runtime-config.md).

**Conga workaround.** Don't use the heartbeat as a scheduler — use cron. For the affected team agent:
1. Created a standup cron job via `openclaw cron create`, delivered via announce to the team's channel — a **command** payload that deterministically emits the templated message (only the date is computed). Not an agent-turn payload: that transcribes the fixed template and can corrupt it (it dropped a mention's `@` in testing).
2. Emptied the agent's `HEARTBEAT.md` to comments-only so the heartbeat skips its API call (`reason=empty-heartbeat-file`).
3. Added a "Scheduling recurring work" section to `agents/_defaults/openclaw/{user,team}/AGENTS.md` steering agents to prefer cron for scheduled tasks (the `cron` tool is in the `coding` profile our agents run).

If a heartbeat is genuinely needed, do **not** rely on `lightContext` — use `isolatedSession: true` + `activeHours` + a longer `every`.

**Validation.**
- `openclaw cron list` shows the scheduled job with the expected next-run.
- Per-API-key Anthropic usage shows the `input_cache_write` floor dropping after the heartbeat is neutered.
- Agent `HEARTBEAT.md` is comments-only (or absent).

**Escape conditions.** cron is the correct tool for *scheduled* work regardless of the bug, so this is effectively permanent for scheduled tasks. If a heartbeat-based task is ever wanted again, only rely on `lightContext` once a pinned version fixes filtering.

**Note — non-declarative state.** The cron job and the emptied `HEARTBEAT.md` live only in the agent's data-dir/workspace, not in the repo. They survive `conga refresh` but are invisible to version control — see the runtime-state boundary in [agent-cost-and-runtime-config.md](./agent-cost-and-runtime-config.md).

---

## Resolved upstream

History of upstream bugs that bit us, with the fix commit/release. These
no longer need workarounds at the current pin but inform future operator
expectations.

| Issue | Symptom | Fixed in | Conga action taken |
|---|---|---|---|
| [#45311](https://github.com/openclaw/openclaw/issues/45311) | Slack socket-mode regression | v2026.3.22 (PR [#45953](https://github.com/openclaw/openclaw/pull/45953), Slack Bolt import-interop hardening) | Held image pin at v2026.3.11 until fix, then bumped. |
| [#84319](https://github.com/openclaw/openclaw/issues/84319) | Claude `thinking` blocks leaked to Slack via non-streaming delivery paths | v2026.5.20 (PR [#84322](https://github.com/openclaw/openclaw/pull/84322), `b05c6158`) | Bumped pin from v2026.5.18 (which had the leak) to v2026.5.26 on 2026-05-27. |

## Adding a new entry

When a new upstream OpenClaw bug bites us:

1. **File or find the upstream issue.** If filing, link the Conga-side reproduction. If finding, add a comment with our reproduction context.
2. **Add an "Active workaround" entry** here with: upstream link + state, symptom (concrete example), Conga workaround (code/config/prompt locations), validation steps, and escape conditions (what would let us remove the workaround).
3. **Update CLAUDE.md** with a one-paragraph entry in *OpenClaw Behavioral Issues* that points at this document for full context.
4. **Match scope to the bug.** If the bug affects only team agents, gate the workaround on `provider.AgentTypeTeam`. Don't apply a costly workaround fleetwide for a narrow upstream bug.
5. **When the bug is fixed upstream and we've pinned to the fix**, move the entry to *Resolved upstream*, remove the workaround code/config (with a separate PR — don't bundle removal with pin bumps), and update validation/tests.
