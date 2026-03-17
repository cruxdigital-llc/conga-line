# Requirements: Slack Event Router

## Goal
Replace direct Slack Socket Mode connections from individual containers with a single stateless router that proxies all WebSocket communication to the correct OpenClaw container based on channel ID.

## Success Criteria
1. Single Slack app, single Socket Mode connection — no missed messages
2. Router proxies full WebSocket communication (not just events — all Socket Mode protocol) to the correct container
3. Container responses relayed back to Slack via the router
4. Per-user container isolation preserved
5. Router is stateless — holds only Slack tokens and routing config
6. Routing table dynamically derived from container config
7. Supports 15+ channels without architecture changes
8. Keep t4g.medium for 2-3 users for now; scale instance later

## Key Decisions
- Proxy full WebSocket (not translate to webhooks) — more maintainable
- Node.js implementation — Slack SDK available
- Dynamic routing table — discovered from container configs
