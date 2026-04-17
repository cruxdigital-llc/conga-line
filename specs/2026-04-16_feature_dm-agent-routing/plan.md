# Plan: DM Agent Routing

## Overview

Add LLM-classified DM routing to the Slack event router so that users with access to multiple agents can DM the bot and have the right agent respond transparently. DM access is derived automatically from Slack channel membership. The classifier defaults to Haiku but supports any OpenAI-compatible endpoint (e.g. a self-hosted model via Ollama) for privacy-sensitive deployments. When confidence is low, the system asks the user to clarify. Thread replies stay pinned to the routed agent.

## Architecture

```
User DM → Slack Socket Mode → Router
                                 ├─ User not in any bound channel? → personal agent via members map (unchanged)
                                 ├─ User in 1 channel (+ optional personal)? → 1 agent: forward directly
                                 ├─ User in 2+ channels, thread reply? → forward to cached agent
                                 └─ User in 2+ channels, new message?
                                      ├─ Classifier confident → forward to chosen agent
                                      └─ Classifier uncertain → ephemeral message asking user to pick

Membership resolution:
  Router startup → conversations.members for each bound channel → in-memory maps
  Steady state  → member_joined_channel / member_left_channel events → update maps
```

## Phases

### Phase 1: Go Data Model Extensions

**Files:**
- `pkg/provider/provider.go` — Add `Description string` to `AgentConfig`
- `pkg/common/routing.go` — Add `AgentDescriptions map[string]AgentDescription` to `RoutingConfig`
- `pkg/common/routing.go` — Extend `GenerateRoutingJSON()` to populate agent descriptions

**`AgentConfig` changes:**
```go
Description string `json:"description,omitempty"` // agent purpose — used by classifier
```

Note: `DMAccess` is NOT stored on AgentConfig. DM access is derived from Slack channel membership at runtime by the router.

**`RoutingConfig` extension:**
```go
AgentDescriptions map[string]AgentDescription `json:"agentDescriptions,omitempty"`
```
```go
type AgentDescription struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    URL         string `json:"url"`
    Type        string `json:"type"` // "user" or "team"
}
```

The router uses `agentDescriptions` combined with live channel membership to build per-user DM routing at runtime.

**Tests:** `pkg/common/routing_test.go` — new test cases:
- Agent with description: appears in `agentDescriptions`
- Agent without description: gets default `"{name} ({type} agent)"`
- Paused agent excluded from `agentDescriptions`

### Phase 2: Team Agent DM Acceptance

**Files:**
- `pkg/runtime/openclaw/config.go` — Enable DM acceptance for all team agents bound to a Slack channel

**Logic:** After `ch.OpenClawChannelConfig()` returns (line 34), if agent is team type with a Slack channel binding:
```go
if string(params.Agent.Type) == "team" {
    if slackSection, ok := channelsCfg["slack"].(map[string]any); ok {
        // Enable DMs — the router controls who can actually reach the agent
        // via channel membership resolution
        slackSection["dmPolicy"] = "allowlist"
        slackSection["allowFrom"] = []string{} // router handles access control
        slackSection["dm"] = map[string]any{"enabled": true}
    }
}
```

The `allowFrom` list is empty because the router handles access control via channel membership. The team agent just needs to accept DMs that the router forwards to it.

**Tests:** `pkg/runtime/openclaw/config_test.go` — verify team agent produces `dmPolicy: "allowlist"` with `dm.enabled: true`.

### Phase 3: Channel Membership Resolution (Router)

**Files:**
- `router/slack/src/membership.js` (new)

**Bootstrap (on router startup):**
1. For each channel in `routing.json` bindings, call `conversations.members` (Tier 4 rate limit — 100 req/min, trivial for 5-20 channels)
2. Build in-memory maps:
   - `channelId → Set<userId>` — who's in each channel
   - `userId → [{ agentName, agentUrl, description }]` — which agents a user can DM (derived from channel bindings + `agentDescriptions`)
3. Add personal agent (from `members` map) to each user's agent list if they have one

**Steady-state (event-driven):**
- Subscribe to `member_joined_channel` / `member_left_channel` events via Socket Mode
- Update both maps on each event — DM routing reflects changes within seconds

**Safety net:**
- Re-poll `conversations.members` every 30 minutes to catch missed events

**Exports:** `{ bootstrap, getUserAgents, handleMemberJoin, handleMemberLeave }`

### Phase 4: Agent Descriptions

**Files:**
- `internal/cmd/admin_provision.go` — Add `--description` flag
- New subcommand or flag on existing `conga agent` command for updating descriptions post-hoc

**Behavior:**
- Description stored in `AgentConfig.Description`
- Default if empty: `"{name} ({type} agent)"` — generated at routing config time, not stored
- Descriptions appear in `routing.json` `dmRouting.agents[].description`
- The classifier prompt quality depends directly on description quality

### Phase 5: Router Classifier Module

**Files:**
- `router/slack/src/classifier.js` (new)

**Configurable endpoint:**
```javascript
// Default: Anthropic Haiku
// Self-hosted: set CLASSIFIER_URL to any OpenAI-compatible endpoint (e.g. Ollama)
const classifierUrl = process.env.CLASSIFIER_URL || 'https://api.anthropic.com';
const classifierKey = process.env.ANTHROPIC_API_KEY || null;
const useCustomEndpoint = !!process.env.CLASSIFIER_URL;
```

When `CLASSIFIER_URL` is set:
- Uses OpenAI-compatible `/v1/chat/completions` format (works with Ollama, vLLM, LiteLLM, etc.)
- No API key required (custom endpoints typically don't need one)
- Same prompt, same JSON response parsing

When `CLASSIFIER_URL` is not set:
- Uses Anthropic Messages API with Haiku (requires `ANTHROPIC_API_KEY`)
- This is the default for most CongaLine deployments

**Classifier:**
- `createClassifier(config)` — returns `{ classify, getCachedThread, cacheThread }` or `null` if neither key nor URL configured
- `classify(messageText, agents)` — calls the configured LLM endpoint:
  - System prompt: list agents with descriptions, instruct to return JSON `{"agent": "name", "confident": true/false}`
  - User message: the DM text
  - 3-second timeout via `AbortController`
  - Validate returned agent name matches a known agent
  - Return `{ agent, confident: boolean }` or `null` on failure

**Clarification flow (low confidence):**
- When `confident: false`, router sends an ephemeral Slack message to the user via `chat.postEphemeral` (requires bot token + `chat:write` scope — already in recommended scopes)
- Ephemeral message includes Block Kit buttons, one per agent: "Which assistant can help? [Personal] [Project1] [Project2]"
- Router listens for `block_actions` interactive events matching a known action ID prefix
- On button click: forward the original message to the chosen agent, cache the thread
- Pending messages held in memory with 60-second TTL (if user ignores, forward to default)

**Thread cache:**
- `Map<thread_ts, { agentUrl, expiry }>`
- 4-hour TTL, max 2000 entries, lazy eviction
- On DM thread reply: check cache before classifying

**No new npm dependencies** — uses native `fetch` for both Anthropic and OpenAI-compatible APIs.

### Phase 6: Router Integration

**Files:**
- `router/slack/src/index.js` — Modified `resolveTarget` and event handler

**Modified `resolveTarget`:**
```javascript
function resolveTarget(payload) {
  const channel = extractChannel(payload);

  if (channel && channel.startsWith('D')) {
    const userId = extractUser(payload);
    const agents = membership.getUserAgents(userId);

    if (agents && agents.length > 0) {
      // Single agent: direct forward, no classification
      if (agents.length === 1) {
        return { target: agents[0].url, reason: `dm-direct:${userId}` };
      }
      // Multi-agent: needs classification (async)
      return { target: null, agents, userId, reason: `dm-classify:${userId}` };
    }

    // Fallback: single personal agent (existing behavior)
    if (userId && config.members[userId]) {
      return { target: config.members[userId], reason: `dm:${userId}` };
    }
  }

  // ... rest unchanged ...
}
```

**Modified event handler** — async classification path:
1. If `route.target` is set, forward immediately (unchanged fast path)
2. If `route.agents` exists (needs classification):
   a. Check thread cache first (thread reply → cached agent)
   b. Call `classifier.classify(text, route.agents)`
   c. If confident → forward, cache thread
   d. If not confident → send ephemeral picker, hold message in pending map
   e. On failure → forward to first agent (default), cache thread

**Interactive handler** — button clicks from clarification:
- Match action ID prefix `dm-route-pick:`
- Look up pending message
- Forward to chosen agent
- Cache thread, clear pending entry

### Phase 7: Router Secrets & Configuration

**Files:**
- `pkg/channels/slack/slack.go` — Add to `SharedSecrets()`:
  ```go
  {Name: "anthropic-api-key", EnvVar: "ANTHROPIC_API_KEY",
   Prompt: "Anthropic API key for DM routing classifier (optional, sk-ant-...)",
   Required: false, RouterOnly: true}
  {Name: "classifier-url", EnvVar: "CLASSIFIER_URL",
   Prompt: "Custom classifier URL (optional, OpenAI-compatible endpoint for self-hosted models)",
   Required: false, RouterOnly: true}
  ```
- `pkg/channels/slack/slack.go` — Add to `RouterEnvVars()`:
  ```go
  if v := sv["anthropic-api-key"]; v != "" {
      vars["ANTHROPIC_API_KEY"] = v
  }
  if v := sv["classifier-url"]; v != "" {
      vars["CLASSIFIER_URL"] = v
  }
  ```
- Also pass `SLACK_BOT_TOKEN` to router env (needed for ephemeral messages and `conversations.members`):
  ```go
  if v := sv["slack-bot-token"]; v != "" {
      vars["SLACK_BOT_TOKEN"] = v
  }
  ```

**Router initialization:**
```javascript
const classifier = (process.env.CLASSIFIER_URL || process.env.ANTHROPIC_API_KEY)
  ? createClassifier({
      classifierUrl: process.env.CLASSIFIER_URL,
      anthropicKey: process.env.ANTHROPIC_API_KEY,
      botToken: process.env.SLACK_BOT_TOKEN,
    })
  : null;

// Bootstrap channel membership (requires SLACK_BOT_TOKEN)
await membership.bootstrap(config, process.env.SLACK_BOT_TOKEN);
```

**Classifier priority:**
1. `CLASSIFIER_URL` set → use custom OpenAI-compatible endpoint (no key needed)
2. `ANTHROPIC_API_KEY` set → use Anthropic Haiku
3. Neither set → no classifier. Single-agent DMs still route directly. Multi-agent DMs fall back to first agent.

### Phase 8: Tests

**Unit tests:**
- `pkg/common/routing_test.go` — `agentDescriptions` generation (Phase 1)
- `pkg/runtime/openclaw/config_test.go` — Team agent `dmPolicy` enablement (Phase 2)

**Router tests:**
- `router/slack/src/membership.test.js` — Membership map build, join/leave event handling
- `router/slack/src/classifier.test.js` — Anthropic path, custom URL path, timeout, fallback

**Integration tests:**
- Provision personal + team agent, verify `routing.json` has `agentDescriptions`
- Verify team agent's `openclaw.json` has `dmPolicy: "allowlist"`

**E2E verification:**
- DM the Slack app → verify correct agent responds
- Reply in thread → verify same agent responds
- Ambiguous message → verify clarification ephemeral appears
- No classifier configured → verify fallback to default agent
- Team-only user → verify DM reaches team agent
- User joins bound channel → DM routing to that agent starts
- User leaves bound channel → DM routing to that agent stops
- `CLASSIFIER_URL` set → verify custom endpoint is called instead of Anthropic

## Persona Review Checklist

### Architect
- [ ] `RoutingConfig` extension is additive and backward compatible
- [ ] `Channel` interface is NOT changed — DM policy override is in runtime config generator
- [ ] No circular import introduced (`provider` ← `channels` boundary preserved)
- [ ] Router membership resolution is resilient to Slack API failures (bootstrap retries, event drops handled by periodic re-poll)
- [ ] Router async path doesn't block the Slack ack (ack happens before classification)
- [ ] Thread cache doesn't leak memory (TTL + max size + lazy eviction)
- [ ] Classifier endpoint is pluggable — Anthropic and OpenAI-compatible paths share the same prompt/response format

### QA
- [ ] Classifier timeout (3s) prevents hung requests
- [ ] Ephemeral clarification has 60s TTL — pending messages don't leak
- [ ] Thread cache eviction works correctly at boundary (2000 entries)
- [ ] Duplicate events handled: dedup fires before classification (existing dedup is sufficient)
- [ ] Membership re-poll catches missed join/leave events
- [ ] Bot not in channel → skip gracefully, log warning

### PM
- [ ] Zero-syntax UX for end users — no prefixes, no commands
- [ ] Zero admin overhead — DM access follows channel membership automatically
- [ ] Clarification flow is non-blocking — user can ignore and default fires after 60s
- [ ] Feature is fully opt-in (no classifier configured = no change)
- [ ] Self-hosted classifier option for privacy-sensitive deployments
- [ ] Success metrics: classify accuracy (log chosen vs. clarification rate)

## Terraform Provider Impact

Changes to `pkg/provider/provider.go` (`Description` field) and `pkg/common/routing.go` (new types) require a Terraform provider release. These are additive optional fields — backward compatible. Follow release flow in CLAUDE.md.

Note: `DMAccess` is NOT added to `AgentConfig` — DM access is resolved at runtime from Slack channel membership.

## Implementation Order

| Phase | Depends on | Risk | Effort |
|-------|-----------|------|--------|
| 1. Go data model | — | Low | S |
| 2. DM acceptance | Phase 1 | Low | S |
| 3. Membership resolution | — | Medium | M |
| 4. Agent descriptions | Phase 1 | Low | S |
| 5. Router classifier | — | Medium | M |
| 6. Router integration | Phase 3, 5 | Medium | L |
| 7. Router secrets/config | — | Low | S |
| 8. Tests | All | — | M |

Phases 1-2 (Go) and Phases 3, 5 (router modules) can be developed in parallel.
Phase 6 (router integration) depends on Phases 3 and 5.
Phase 7 (secrets) is independent — just env var wiring.
