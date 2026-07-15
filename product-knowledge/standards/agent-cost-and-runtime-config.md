# Agent Cost & Runtime Configuration

Operational standard for keeping per-agent LLM cost sane, and for reasoning about
configuration that agents create **at runtime**. Written from a 2026-07 cost
investigation in which a single team agent had grown to ~96% of the org's
Anthropic bill.

Related: [config-taxonomy.md](./config-taxonomy.md) · [upstream-openclaw-issues.md](./upstream-openclaw-issues.md) · [egress-controls.md](./egress-controls.md)

## The rules (TL;DR)

1. **Scheduled work is a cron job, never a heartbeat.** A fixed time/recurrence → `cron`. An open-ended "is anything up?" check with no set time → heartbeat. Using the heartbeat as a clock is the single most expensive mistake we've hit.
2. **A team agent's cost is dominated by what *wakes* it, not by what it *says*.** Output tokens are tiny; the bill is input/cache-write on every wake. Cut the number of full-context wakes first.
3. **Cache writes with no reads are worse than no cache.** A high write:read ratio means the cached prefix is re-written and never reused — you pay the write premium for nothing.
4. **Agents can self-configure at runtime, but that state is invisible to the repo.** Cron jobs and self-written workspace files live only in the container. Know the durable-vs-regenerated boundary before relying on it.

## 1. Scheduling: heartbeat vs cron

| Trigger | Mechanism |
|---|---|
| A specific time / recurrence (a weekday-morning post, a weekly report) | **cron job** — fires only at the scheduled time |
| An open-ended condition to keep checking, no fixed time | heartbeat — wakes on an interval and evaluates |

**Why it matters (the incident):** a team agent's recurring standup post (a few mornings a week) was implemented as a **55-minute heartbeat**. It woke ~26×/day, 24/7, and — worsened by OpenClaw bug [#43767](https://github.com/openclaw/openclaw/issues/43767) (`lightContext: true` is ignored; the full ~140K context loads anyway) — re-cached the whole prompt on every wake. Result: **~$24/day (~$700/mo) even on days with zero channel activity**, to post a message a few times a week. Moving it to a cron job (`0 9 * * <days> @ <tz>`, announce to the channel) took it to **~$1/mo** with identical behavior.

**Cron payload — command vs agent-turn.** For a *fixed* scheduled output (a templated post with exact mentions/formatting), use a **command** payload that emits the text deterministically and compute only the dynamic bits (e.g. the date). An **agent-turn** payload makes the model transcribe your template and can corrupt it — in this incident it dropped the `@` on one mention, so a teammate wasn't pinged. Reserve agent-turn payloads for scheduled work that genuinely needs generation or reasoning.

**Heartbeat cost knobs** (when a heartbeat is genuinely warranted):
- `every` — interval; longer = fewer wakes.
- `isolatedSession: true` — fresh session per run, no conversation history (~100K → ~2–5K tokens).
- `activeHours` — only wake in a window (e.g. business hours) instead of 24/7.
- `model` — point the heartbeat at a cheaper/secondary model.
- Empty / comments-only `HEARTBEAT.md` → the heartbeat **skips its API call** (`reason=empty-heartbeat-file`).
- Do **not** rely on `lightContext` for cost control — it's bugged ([#43767](https://github.com/openclaw/openclaw/issues/43767), [#61395](https://github.com/openclaw/openclaw/issues/61395)). Use `isolatedSession` + `activeHours` + frequency instead.

**Steering:** `agents/_defaults/openclaw/{user,team}/AGENTS.md` carries a "Scheduling recurring work" section telling agents to prefer cron. This only works because the `cron` tool is in the `coding` profile — the profile our agents run. **Steering a tool the agent doesn't have does nothing**: confirm the tool is available before relying on prose.

## 2. Diagnosing a costly agent

**Where cost is (and isn't) visible:**
- **Authoritative:** the Anthropic usage dashboard, *per API key*. Each agent has its own key, so cost breaks down by agent there — the only source of true $ + token + cache split.
- **Not** in container logs — OpenClaw doesn't log per-request token usage at INFO, and its egress is TLS through the Envoy proxy (encrypted bytes, not tokens).
- Config/behavior are readable in-container: `openclaw.json` (model, heartbeat, contextPruning), `HEARTBEAT.md`, `cron/jobs.json`.

**Signals to read from the cost export:**
- **write:read ratio** — `input_cache_write` ≫ `input_cache_read` means the cache is written and never reused (churn). As configured that is *worse* than not caching: you pay the ~1.25×/2× write premium for ~0.1× reads you never collect.
- **input:output ratio** — huge input, tiny output = the agent is paying to *load context and stay quiet*. Look at what wakes it, not at its replies.
- **cost floor on zero-activity days** — a near-constant daily write cost on days with no channel messages ⇒ a timer (heartbeat/cron), not user traffic. Fastest way to separate "wake churn" from "real work."

**Cache-TTL interaction:** `contextPruning: { mode: "cache-ttl", ttl }` aligns pruning to the Anthropic prompt-cache TTL. On a low-traffic channel a 5-minute TTL expires between wakes, so every wake is a cold write. A longer TTL helps *only* if prefix-sharing requests arrive within the window **and** the prefix is byte-stable; it does nothing if the prefix mutates each turn (growing history). Confirm with a write→read flip in the next cost export before trusting a TTL change.

## 3. Runtime self-configuration & the state boundary

We *want* teammates to shape agents in conversation — the standup existed because a teammate asked the agent to set it up. That capability is real: the `cron` tool supports agent-created **agent-turn** jobs (only *command* cron — shell on the host — is operator-admin-only). But it has a sharp edge.

**What survives a `conga refresh`:**
| State | Home | On refresh/redeploy |
|---|---|---|
| Cron jobs | `~/.openclaw/cron/jobs.json` (data dir) | **Persists** — conga doesn't regenerate it |
| Agent memory, session state | data dir | **Persists** |
| `openclaw.json` | generated by conga | **Regenerated** — agent edits clobbered |
| Overlay-sourced behavior files (SOUL/AGENTS/USER) | conga overlay → S3 → workspace | **Rebuilt** from the overlay |
| Agent-authored workspace files *not* in the overlay (e.g. a self-written `HEARTBEAT.md`) | data-dir workspace only | Persist unless a workspace rebuild removes them; conga can't *restore* them (no source copy) |

**Guidance:**
- Steer runtime personalization toward **durable, conga-unmanaged surfaces** (cron, memory) — not toward files conga regenerates.
- Agent-authored runtime state is **invisible to the repo and to git.** In the incident, the standup config lived only in the container; we found it by shelling in. For a fleet where runtime personalization is expected, the missing capability is **observability** — a way to inventory what each agent has self-configured (cron jobs, workspace files, memory). Build that before leaning on the pattern.
- Durable ≠ immortal: agent state survives refresh/restart but not a fresh provision / data-dir wipe, and the agent won't know to recreate it.

"Agent-authored runtime state" is a new, non-declarative layer that sits outside the [config-taxonomy.md](./config-taxonomy.md) layers (infra→tfvars, policy→conga-policy.yaml, runtime overlay→agent.yaml, persistence→JSON/SSM, secrets). Treat it as such.

## Playbook: "an agent's Anthropic bill looks high"

1. Pull the per-API-key usage export; find the dominant agent (it's usually one).
2. Read its cost split — write:read and input:output. High write / low read / tiny output ⇒ wake churn, not real work.
3. Check a zero-activity day. A constant floor ⇒ a timer.
4. Inspect in-container: `openclaw.json` (heartbeat, contextPruning, model), `HEARTBEAT.md`, `openclaw cron list`.
5. If a scheduled task runs on the heartbeat → move it to cron and empty the `HEARTBEAT.md` task. If the heartbeat is genuinely needed → `isolatedSession` + `activeHours` + frequency.
6. Verify with the next cost export (the write floor should crater).

## References
- Incident (2026-07). Fix: a standup cron job; heartbeat neutered via comments-only `HEARTBEAT.md`; `contextPruning.ttl` reverted 1h→5m (the 1h TTL had only helped the now-removed heartbeat; with it gone, the 5m default is correct); scheduling steering added to `agents/_defaults/openclaw/{user,team}/AGENTS.md`.
- OpenClaw bugs: [#43767](https://github.com/openclaw/openclaw/issues/43767) (heartbeat ignores `lightContext`), [#61395](https://github.com/openclaw/openclaw/issues/61395) (`lightContext` workspace files) — logged in [upstream-openclaw-issues.md](./upstream-openclaw-issues.md).
- OpenClaw docs: `docs/automation/cron-jobs.md`, `docs/cli/cron.md`, `docs/gateway/heartbeat.md`.
