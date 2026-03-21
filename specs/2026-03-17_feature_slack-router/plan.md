# Plan: Slack Event Router

## Overview

A Node.js service that maintains a single outbound Slack Socket Mode connection, inspects incoming events, and forwards them to the correct Conga Line container via internal HTTP webhook. Preserves zero-ingress security model — all connections are outbound.

## Architecture

```
Slack Cloud
    ▲ (WSS, outbound from router)
    │
┌───┴────────────────────────────┐
│  Router Container              │
│  - Socket Mode client (outbound WSS to Slack)
│  - Receives events, extracts channel/user
│  - Routes via internal HTTP POST to containers
│  - Acks envelopes back to Slack
│  - Holds: Slack tokens only
│  - No LLM, no user data
└────────┬───────┬───────┬───────┘
    HTTP │  HTTP │  HTTP │ (internal, container-to-container)
    ┌────▼──┐ ┌──▼────┐ ┌▼──────┐
    │OC     │ │OC     │ │OC     │
    │User A │ │User B │ │Team 1 │
    │:18789 │ │:18789 │ │:18789 │
    │http   │ │http   │ │http   │
    │mode   │ │mode   │ │mode   │
    └───────┘ └───────┘ └───────┘
    (isolated Docker networks)
```

**Zero ingress preserved** — the router connects outbound to Slack via WSS. Containers receive events via internal HTTP from the router. No public endpoints needed.

## Router Service

### Components (~200 lines)

```
router/
├── package.json
├── Dockerfile
└── src/
    ├── index.js              # Entry point, load config, start
    ├── slack-connection.js   # Socket Mode client (outbound WSS)
    ├── event-router.js       # Extract channel/user, lookup target, forward
    └── config.js             # Read routing table from config file
```

### Socket Mode Protocol

The router handles the Socket Mode protocol:
1. Call `apps.connections.open` with `xapp-` token → get WSS URL
2. Connect outbound, receive `hello` message
3. For each envelope received:
   - Extract `envelope_id`
   - Extract event payload → find `channel` or `user` field
   - Look up target container in routing table
   - Forward event via HTTP POST to container's webhook path
   - Ack the envelope back to Slack (within 3 seconds)
4. Handle ping/pong keepalives
5. Reconnect on disconnect

### Routing Logic

```
Event arrives from Slack:
  1. Extract channel_id from event payload
  2. If channel_id starts with "C" → look up in channel routes
  3. If channel_id starts with "D" → extract sender's user field → look up in member routes
  4. If no match → drop event (or route to default)
  5. HTTP POST to target container's webhook path
```

### Routing Table

Generated at bootstrap from the Terraform `users` variable:

```json
{
  "channels": {
    "CEXAMPLE01": "http://conga-UEXAMPLE01:18789/slack/events",
    "CEXAMPLE02": "http://conga-UEXAMPLE02:18789/slack/events"
  },
  "members": {
    "UEXAMPLE01": "http://conga-UEXAMPLE01:18789/slack/events",
    "UEXAMPLE02": "http://conga-UEXAMPLE02:18789/slack/events"
  }
}
```

### Dependencies

- `@slack/socket-mode` — Socket Mode client
- `@slack/web-api` — for `apps.connections.open` (may be included in socket-mode)
- No other dependencies needed

## Container Changes

Each Conga Line container switches from Socket Mode to HTTP webhook mode:

```json
{
  "channels": {
    "slack": {
      "mode": "http",
      "enabled": true,
      "botToken": "...",
      "signingSecret": "...",
      "webhookPath": "/slack/events",
      "groupPolicy": "allowlist",
      "channels": { ... }
    }
  }
}
```

Key changes:
- `mode: "socket"` → `mode: "http"`
- Remove `appToken` from container env (router holds it)
- Add `signingSecret` to container env (for webhook signature verification)
- Container still needs `botToken` for sending replies via Slack Web API
- Container's `groupPolicy: "allowlist"` still enforces channel boundaries

## Networking

The router needs to reach each container's port 18789. Options:

### Option A: Shared Docker network
Create a single `conga-router` network. Router and all containers join it. Containers can't reach each other (Docker bridge isolation between containers on the same network is NOT enforced — they CAN communicate).

**Problem**: Breaks container isolation.

### Option B: Router joins all per-user networks
Router container joins every user's Docker network. Each user network is isolated — containers on different networks can't reach each other, but the router can reach all of them.

```bash
docker network connect conga-UEXAMPLE01 router
docker network connect conga-UEXAMPLE02 router
```

**This preserves isolation** — User A's container can't reach User B's container because they're on separate networks. The router is the only bridge.

**Recommended: Option B.**

## Secrets

- **Router container**: Slack `appToken` (xapp-) + `botToken` (xoxb-) + `signingSecret`
- **User containers**: Slack `botToken` (xoxb-) + `signingSecret` + per-user API keys
- Slack tokens are shared between router and containers (botToken needed by containers to send responses)

New shared secret needed: `conga/shared/slack-signing-secret`

## User-Data Changes

1. Build and start router container before user containers
2. Router joins all user Docker networks
3. User containers switch to `mode: "http"` in their openclaw.json
4. User containers no longer need `SLACK_APP_TOKEN` — router holds it
5. User containers need `SLACK_SIGNING_SECRET` env var

## Terraform Changes

- Add `slack_signing_secret` to shared secrets
- Router container definition in user-data
- Router systemd unit
- Network connection logic (router joins all user networks)

## Scaling Considerations

- Router is lightweight (~50MB memory) — single Node.js process
- Adding a user = add to routing table + connect router to new network + restart router
- 15 channels is trivial for the router — it's just HTTP forwarding
- Instance sizing: router adds minimal overhead to the t4g.medium

## Architect Review

- **Zero ingress preserved**: All Slack communication is outbound WSS from the router. Internal HTTP between router and containers is localhost-only.
- **Router as SPOF**: If the router dies, all users lose Slack. Mitigated by systemd restart. Same risk as the current single NAT instance.
- **3-second ack window**: Slack requires envelope acknowledgement within 3 seconds. The router must ack BEFORE forwarding to the container (acknowledge receipt, not processing completion). Conga Line sends responses asynchronously via the Web API.
- **Option B networking**: Router joins all networks but containers remain isolated from each other. Clean topology.
- **Bot token sharing**: Both router and containers need the bot token — router for Socket Mode connection metadata, containers for sending replies. This is the standard Slack pattern.
