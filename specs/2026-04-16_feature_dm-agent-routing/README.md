# Trace Log: DM Agent Routing

**Feature**: DM Agent Routing
**Spec Directory**: `specs/2026-04-16_feature_dm-agent-routing/`
**Started**: 2026-04-16
**Status**: Planning

## Active Personas
- **Architect** — System design review, data model impact, pattern consistency
- **QA** — Edge cases, failure modes, test strategy
- **PM** — User value, enrollment UX, scope control

## Active Capabilities
- **MCP Tools**: `conga_*` tools for runtime verification (status, policy, logs)
- **Playwright**: Available for gateway UI testing if needed
- **GitHub**: PR creation and review

## Session Log

### 2026-04-16 — Planning Session
- **Context**: User requested ability for Slack DM messages to route to the correct agent when a user has access to multiple agents (personal + team, or team-only)
- **Key constraint identified**: Not every team member has a personal agent. Some users only have team agent access.
- **Decision**: LLM classifier (Haiku) in the router for transparent routing. Team agent responds directly (no mediation).
- **Decision**: Explicit admin enrollment via CLI (not inferred from Slack channel membership)
- **Artifacts**:
  - [requirements.md](requirements.md)
  - [plan.md](plan.md)
- **Prior design work**: `/Users/aaronstone/.claude/plans/serialized-swimming-narwhal.md` (detailed architectural exploration)
- **Decision**: Low-confidence classifier results trigger ephemeral Slack message asking user to pick agent (60s TTL, then default)
- **Decision**: Team-only users (no personal agent) supported — `dmRouting` with 1 agent = direct forward, 2+ = classify
- **Decision**: Per-user enrollment via CLI for v1; batch/auto-infer from channel membership deferred
- **Files created**:
  - [requirements.md](requirements.md) — 5 user scenarios, 8 success criteria, non-goals
  - [plan.md](plan.md) — 8-phase implementation plan with persona review checklist

### 2026-04-16 — Plan Revision (Spark infrastructure session)
- **Decision reversal**: Replace manual enrollment with automatic channel membership resolution
  - DM access derived from Slack channel membership via `conversations.members` API
  - Router maintains membership maps via `member_joined_channel` / `member_left_channel` events
  - Eliminates `conga channels enroll/unenroll` CLI commands and `DMAccess` field
  - Rationale: manual enrollment drifts from reality, channel membership is the source of truth
- **Decision**: Configurable classifier endpoint — defaults to Anthropic Haiku, supports any OpenAI-compatible endpoint via `CLASSIFIER_URL` env var
  - Enables self-hosted models (e.g. Ollama on DGX Spark) for privacy-sensitive deployments
  - DM content stays on controlled infrastructure when using self-hosted classifier
  - CongaLine users get Haiku by default — no extra infrastructure required
- **Files updated**:
  - [requirements.md](requirements.md) — revised enrollment model, classifier model, success criteria
  - [plan.md](plan.md) — Phase 3 replaced (enrollment CLI → membership resolution), Phase 5/7 updated for configurable endpoint
