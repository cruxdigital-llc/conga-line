# Slack Router — Learnings & Future Reference

## What We Built
A Node.js Socket Mode proxy that receives all Slack events via a single outbound WSS connection, routes by channel/user ID, and forwards to per-user Conga Line containers via internal HTTP. Zero ingress preserved.

## What Worked
- **Router receives events**: `@slack/socket-mode` v2 SDK connects and receives events via the `slack_event` catch-all listener (NOT the `events_api`/`interactive` named events — SDK v2 emits differently)
- **Routing logic**: Channel-based routing (`C*` channels) and user-based routing (DMs `D*` by sender member ID) works correctly
- **Docker networking (Option B)**: Router joins all per-user networks. Containers remain isolated from each other. DNS resolves container names.
- **Signature computation**: The router correctly computes `x-slack-signature` HMAC-SHA256 headers for forwarded requests

## What Didn't Work
- **Conga Line HTTP webhook mode returns 404**: Despite correct config (`mode: "http"`, `botToken`, `signingSecret`, `webhookPath` all present) and the log saying `[slack] http mode listening at /slack/events`, POST requests to the endpoint return 404.

## Root Cause Analysis
Traced through the Conga Line source at github.com/openclaw/openclaw:

1. **`extensions/slack/src/http/registry.ts`** maintains a module-level `Map<string, Handler>` called `slackHttpRoutes`
2. **`extensions/slack/src/monitor/provider.ts`** calls `registerSlackHttpHandler()` which writes to this Map
3. **`src/gateway/server-http.ts`** calls `handleSlackHttpRequest()` which reads from this Map
4. **The Map is empty when the gateway reads it** — the monitor and gateway resolve to different module instances, each with their own Map. This is a module identity split caused by the bundling/compilation of the Docker image.
5. The `[slack] http mode listening at /slack/events` log is emitted by the monitor after it registers the handler in ITS copy of the Map. The gateway's copy remains empty.

**This is an Conga Line bug.** The HTTP webhook handler registers in a different module instance than the one the gateway HTTP server reads from. It affects the compiled Docker image (`ghcr.io/openclaw/openclaw:latest`, version 2026.3.13).

## Additional Findings

### SDK v2 Event Names
`@slack/socket-mode` v2.0.6 does NOT emit `events_api`, `interactive`, etc. as event names. It emits:
- The specific event type (e.g., `message`, `app_mention`) via `this.emit(event.payload.event.type, ...)`
- The envelope type via `this.emit(event.type, ...)` — but `event.type` here is `event_callback`, NOT `events_api`
- A catch-all `slack_event` that fires for everything

**Use `slack_event` for routing**, not the named event types from the SDK v1 docs.

### Socket Mode vs Events API Payload Format
- Socket Mode envelopes: `{ envelope_id, type: "events_api", payload: { ...actual event... } }`
- Events API HTTP: `{ token, team_id, event: { ... }, type: "event_callback" }`
- The `@slack/socket-mode` SDK unwraps the envelope and provides `body` as the inner payload (Events API format)
- No manual unwrapping needed when using the SDK

### Conga Line HTTP Mode Requirements
- `botToken` MUST be in `openclaw.json` (not just env var) for the Bolt `HTTPReceiver` to initialize
- `signingSecret` MUST be in `openclaw.json` (env var override doesn't work for this field)
- Without `botToken` in config, the route never registers (returns 404)
- With `botToken` in config, the route partially works (returns 400 from host curl with bad sig, but 404 from Docker networking — the module split issue)

### Docker Network Reconnection
When a Docker container restarts, it loses `docker network connect` connections. The systemd unit for the router needs an `ExecStartPost` to reconnect to all user networks after every restart.

### User-Data 16KB Limit
EC2 user-data is limited to 16KB. Our bootstrap script exceeded this. Solution: upload the full bootstrap script to S3 and use a thin user-data shim that downloads and executes it.

## Files We Created (to be cleaned up or preserved)
- `router/package.json` — Node.js package for the router
- `router/src/index.js` — Router source code (working, tested)
- `terraform/router.tf` — S3 upload for router source + bootstrap script
- `terraform/user-data-shim.sh.tftpl` — Thin shim that downloads bootstrap from S3

## Recommendation
1. **File an Conga Line issue** about the HTTP webhook mode module identity split bug
2. **Preserve the router code** — it works correctly; the bug is in Conga Line's HTTP mode, not our router
3. **Revisit when Conga Line fixes HTTP mode** or when we can build from source with the fix
4. **For now, use separate Slack apps per user** with Socket Mode (each container connects directly)
