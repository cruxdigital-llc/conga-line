# Credential Flow — Current vs Proposed

This document traces the complete lifecycle of a secret from user input to OpenClaw consumption, for both the current architecture and the proposed credential proxy architecture.

## Current Flow (No Credential Proxy)

### Adding a Shared Secret

```
Admin runs: conga admin setup

                                         LOCAL PROVIDER
  ┌──────────┐    ui.SecretPrompt()     ┌──────────────────────────────────────┐
  │  Admin    │ ──────────────────────▶  │  ~/.conga/secrets/shared/            │
  │ terminal  │   (hidden input)         │    anthropic-api-key     (mode 0400) │
  └──────────┘                           │    slack-bot-token       (mode 0400) │
                                         │    slack-signing-secret  (mode 0400) │
                                         │    slack-app-token       (mode 0400) │
                                         │    google-client-id      (mode 0400) │
                                         │    google-client-secret  (mode 0400) │
                                         └──────────────────────────────────────┘
                                         Atomic write: temp file → chmod 0400 → rename

                                         AWS PROVIDER
                                         ┌──────────────────────────────────────┐
                                         │  AWS Secrets Manager                 │
                                         │    conga/shared/anthropic-api-key    │
                                         │    conga/shared/slack-bot-token      │
                                         │    conga/shared/slack-signing-secret │
                                         │    conga/shared/slack-app-token      │
                                         │    conga/shared/google-client-id     │
                                         │    conga/shared/google-client-secret │
                                         └──────────────────────────────────────┘
                                         Encrypted at rest (KMS), IAM-scoped access
```

### Adding a Per-Agent Secret

```
User runs: conga secrets set myagent brave-api-key

                                         LOCAL PROVIDER
  ┌──────────┐    writeSecret()          ┌──────────────────────────────────────┐
  │   User    │ ──────────────────────▶  │  ~/.conga/secrets/agents/myagent/      │
  │ terminal  │   (prompted, hidden)     │    brave-api-key         (mode 0400) │
  └──────────┘                           │    trello-api-key        (mode 0400) │
                                         │    trello-token          (mode 0400) │
                                         └──────────────────────────────────────┘

                                         AWS PROVIDER
                                         ┌──────────────────────────────────────┐
                                         │  AWS Secrets Manager                 │
                                         │    conga/agents/myagent/brave-api-key  │
                                         │    conga/agents/myagent/trello-api-key │
                                         │    conga/agents/myagent/trello-token   │
                                         └──────────────────────────────────────┘
```

Secret is stored. **Nothing happens yet** — the running container doesn't see the new value until refresh.

### Refreshing an Agent (Secrets → Container)

The retrieval step is provider-specific, but everything after that is identical:

```
User runs: conga refresh --agent myagent

  LOCAL PROVIDER                     AWS PROVIDER
  readSharedSecrets()                Secrets Manager GetSecretValue()
  ~/.conga/secrets/shared/*          conga/shared/*
  readAgentSecrets("myagent")          conga/agents/myagent/*
  ~/.conga/secrets/agents/myagent/*
          │                                    │
          ▼                                    ▼
  ┌─────────────────────┐           ┌────────────────────────┐
  │ SharedSecrets{       │           │ SharedSecrets{         │
  │   SlackBotToken      │           │   (same struct,        │
  │   SlackSigningSecret │           │    populated from      │
  │   GoogleClientID     │           │    Secrets Manager     │
  │   GoogleClientSecret │           │    instead of files)   │
  │ }                    │           │ }                      │
  └─────────┬───────────┘           └───────────┬────────────┘
            │                                   │
            └──────────┬────────────────────────┘
                       │
          BOTH PROVIDERS (common package)
                       ▼
              GenerateEnvFile()
              SecretNameToEnvVar("brave-api-key") → "BRAVE_API_KEY"
                       │
                       ▼
        ENV FILE (local: ~/.conga/config/myagent.env)
                  (AWS:  /opt/conga/config/myagent.env)
                  (both: mode 0400, root:root)
        ┌─────────────────────────────────────┐
        │ SLACK_BOT_TOKEN=xoxb-...            │  ← shared
        │ SLACK_SIGNING_SECRET=abc...         │  ← shared
        │ GOOGLE_CLIENT_ID=123...             │  ← shared
        │ GOOGLE_CLIENT_SECRET=xyz...         │  ← shared
        │ NODE_OPTIONS=--max-old-space-size=… │  ← config (not a secret)
        │ ANTHROPIC_API_KEY=sk-ant-...        │  ← shared (via perAgent*)
        │ BRAVE_API_KEY=BSA...                │  ← per-agent
        │ TRELLO_API_KEY=abc...               │  ← per-agent
        │ TRELLO_TOKEN=def...                 │  ← per-agent
        └─────────────────────────────────────┘

        * Note: ANTHROPIC_API_KEY is currently stored as a shared secret
          but flows through the same readSharedSecrets/perAgent path.
          The distinction is storage location, not env file output.

                       │
              docker stop conga-myagent
              docker rm conga-myagent
              docker run ... --env-file {path}/myagent.env ...
                       │
                       ▼
        ┌───────────────────────────────────────────────┐
        │  conga-myagent container                        │
        │                                               │
        │  process.env.ANTHROPIC_API_KEY = "sk-ant-..." │  ← REAL KEY
        │  process.env.BRAVE_API_KEY = "BSA..."         │  ← REAL KEY
        │  process.env.TRELLO_API_KEY = "abc..."        │  ← REAL KEY
        │  process.env.SLACK_BOT_TOKEN = "xoxb-..."     │  ← REAL KEY
        │                                               │
        │  OpenClaw reads env vars at startup.          │
        │  Env vars override any config file values.    │
        │  The process has all secrets in memory.       │
        └───────────────────────────────────────────────┘
```

### How OpenClaw Uses the Secrets

| Secret | How OpenClaw reads it | What it does with it |
|---|---|---|
| `ANTHROPIC_API_KEY` | `process.env` → SDK client | Sent as `x-api-key` header to `api.anthropic.com` on every LLM call |
| `SLACK_BOT_TOKEN` | `process.env` + openclaw.json `channels.slack.botToken` | Used for Slack Web API calls (post messages, reactions, etc.) |
| `SLACK_SIGNING_SECRET` | openclaw.json `channels.slack.signingSecret` | Validates inbound webhook signatures from the router |
| `BRAVE_API_KEY` | `process.env` → Brave skill | Sent as `X-Subscription-Token` header to `api.search.brave.com` |
| `TRELLO_API_KEY` + `TRELLO_TOKEN` | `process.env` → Trello skill | Sent as query params to `api.trello.com` |
| `GOOGLE_CLIENT_*` | `process.env` → Google OAuth | Used for Google Calendar/Gmail OAuth flow |

### Current Security Properties

- Secrets at rest: files on disk, mode 0400 (owner-read only)
- Secrets in transit: env file → Docker `--env-file` → process environment
- Secrets in memory: **all secrets live in the OpenClaw process memory**
- Exposure: `docker exec conga-myagent env` reveals all real keys
- Exposure: a prompt-injected agent could read its own `process.env` and exfiltrate keys

---

## Proposed Flow (With Credential Proxy)

### Secret Storage — Unchanged

Storage is identical on both providers. No changes to Secrets Manager paths, IAM policies, file paths, or the `conga secrets set/get/delete` commands.

| | Local Provider | AWS Provider |
|---|---|---|
| **Shared secrets** | `~/.conga/secrets/shared/*` (files, mode 0400) | `conga/shared/*` (Secrets Manager, KMS-encrypted) |
| **Per-agent secrets** | `~/.conga/secrets/agents/{name}/*` (files, mode 0400) | `conga/agents/{name}/*` (Secrets Manager, KMS-encrypted) |
| **CLI commands** | Unchanged | Unchanged |
| **IAM policies** | N/A | Unchanged (instance role reads `conga/*`) |

### Refreshing an Agent — Split Env Files

The only change is in the last mile: `GenerateEnvFile()` splits into two functions, producing two env files instead of one. The secret retrieval step (files or Secrets Manager) is untouched.

```
User runs: conga refresh --agent myagent

  SECRET RETRIEVAL (unchanged, provider-specific)

  Local: readSharedSecrets() + readAgentSecrets()
  AWS:   GetSecretValue() for each conga/shared/* and conga/agents/myagent/*
          │                                    │
          ▼                                    ▼
  ┌─────────────────────┐           ┌────────────────────────┐
  │ SharedSecrets{...}   │           │ map[string]string{...} │
  └─────────┬───────────┘           └───────────┬────────────┘
            │                                   │
            └──────────┬────────────────────────┘
                       │
          ENV FILE GENERATION (common package — the ONLY change)
                       │
            ┌──────────┴──────────┐
            ▼                     ▼
   GenerateAgentEnvFile()   GenerateProxyEnvFile()
            │                     │
            ▼                     ▼

   myagent.env (mode 0644)    myagent-proxy.env (mode 0400)
   ┌──────────────────────┐ ┌──────────────────────────────┐
   │ NODE_OPTIONS=...     │ │ ANTHROPIC_API_KEY=sk-ant-... │
   │ ANTHROPIC_BASE_URL=  │ │ BRAVE_API_KEY=BSA...        │
   │  http://conga-proxy- │ │ TRELLO_API_KEY=abc...       │
   │  myagent:8080          │ │ TRELLO_TOKEN=def...         │
   │                      │ │ GOOGLE_CLIENT_ID=123...     │
   │                      │ │ GOOGLE_CLIENT_SECRET=xyz... │
   │                      │ └──────────────────────────────┘
   └──────────────────────┘     mounted ONLY by proxy container

   mounted by agent container      Local: ~/.conga/config/myagent-proxy.env
   (contains ZERO real keys)       AWS:   /opt/conga/config/myagent-proxy.env

   Local: ~/.conga/config/myagent.env
   AWS:   /opt/conga/config/myagent.env
```

### Container Startup Order

```
1. Start proxy first (needs secrets before agent can make API calls):

   docker run -d \
     --name conga-proxy-myagent \
     --network conga-myagent \
     --env-file ~/.conga/config/myagent-proxy.env \    ← REAL KEYS
     --cap-drop ALL \
     --security-opt no-new-privileges \
     --memory 64m \
     credential-proxy:latest

2. Then start agent (proxy is already available on the network):

   docker run -d \
     --name conga-myagent \
     --network conga-myagent \
     --env-file ~/.conga/config/myagent.env \          ← NO REAL KEYS
     --cap-drop ALL \
     --security-opt no-new-privileges \
     --memory 2g \
     -v ~/.conga/data/myagent:/home/node/.openclaw:rw \
     -p 127.0.0.1:18789:18789 \
     openclaw:2026.3.11
```

### How the Credential Proxy Works

```
  conga-myagent (OpenClaw)                 conga-proxy-myagent
  ┌─────────────────────────────────┐    ┌──────────────────────────────────┐
  │                                 │    │                                  │
  │  SDK calls ANTHROPIC_BASE_URL:  │    │  Receives HTTP request on :8080  │
  │  POST http://conga-proxy-       │───▶│                                  │
  │    myagent:8080/anthropic         │    │  Route: /anthropic/* →           │
  │    /v1/messages                 │    │    https://api.anthropic.com/*   │
  │                                 │    │                                  │
  │  Request has NO auth header     │    │  Injects header:                │
  │  (agent doesn't have the key)   │    │    x-api-key: sk-ant-...        │
  │                                 │    │    (from ANTHROPIC_API_KEY env)  │
  │                                 │    │                                  │
  │                                 │    │  Forwards to upstream over HTTPS │
  │                                 │    │                                  │
  │  Receives SSE stream ◀─────────│────│  Streams response back           │
  │  (FlushInterval: -1,           │    │  (no buffering)                  │
  │   no buffering)                 │    │                                  │
  └─────────────────────────────────┘    └──────────────────────────────────┘

  HTTP (plaintext, same Docker network)    HTTPS (TLS to upstream)
  No auth header leaves the agent          Auth injected at proxy
```

### Proxy Routing Table

The route table is **data-driven** — loaded from `routes.json` at startup, not hardcoded. Adding a new service is a config change (new JSON entry + env var), not a code change.

Config mounted read-only at `/etc/credential-proxy/routes.json` inside the proxy container. Deployed copy lives at `~/.conga/config/routes.json` (local) or `/opt/conga/config/routes.json` (AWS).

**Two defaults ship with the project:**
- `routes.json` — generous, all known OpenClaw-supported services. Routes with no env var set return 502 (safe no-op).
- `routes-restricted.json` — locked down (e.g., Anthropic only). For enterprise deployments.

Admin selects during `conga admin setup` or swaps the file later. All proxies on a host share the same route table — credentials differ per-agent, but allowed services are a deployment-wide policy.

**Default route table (generous):**

| Agent request path | Upstream | Auth injection | Auth type |
|---|---|---|---|
| `/anthropic/*` | `https://api.anthropic.com/*` | `x-api-key: {ANTHROPIC_API_KEY}` | Header |
| `/openai/*` | `https://api.openai.com/*` | `Authorization: Bearer {OPENAI_API_KEY}` | Header w/ prefix |
| `/brave/*` | `https://api.search.brave.com/*` | `X-Subscription-Token: {BRAVE_API_KEY}` | Header |
| `/trello/*` | `https://api.trello.com/*` | `?key={TRELLO_API_KEY}&token={TRELLO_TOKEN}` | Query param |
| `/google/*` | `https://www.googleapis.com/*` | `Authorization: Bearer {GOOGLE_CLIENT_SECRET}` | Header w/ prefix |
| `/github/*` | `https://api.github.com/*` | `Authorization: Bearer {GITHUB_TOKEN}` | Header w/ prefix |
| `/healthz` | (local) | Returns 200 + per-route status | N/A |

A route with no corresponding env var is **inactive** — the proxy returns 502 with a clear error message ("route configured but BRAVE_API_KEY not set"). The user fixes it with `conga secrets set <agent> brave-api-key` + `conga refresh`.

**Restricted example (`routes-restricted.json`):**

| Agent request path | Upstream | Auth injection |
|---|---|---|
| `/anthropic/*` | `https://api.anthropic.com/*` | `x-api-key: {ANTHROPIC_API_KEY}` |
| `/healthz` | (local) | Returns 200 |

All other requests return 404 — the proxy doesn't even know about the service.

### Network Isolation Between Proxies

Each proxy is only reachable from its own agent's Docker network. Cross-agent credential access is impossible at the network level:

```
Network: conga-myagent              Network: conga-dave
┌─────────────────────┐           ┌─────────────────────┐
│ conga-myagent         │           │ conga-dave          │
│ conga-proxy-myagent   │           │ conga-proxy-dave    │
└─────────────────────┘           └─────────────────────┘
       No route between networks — Docker bridge isolation
       conga-myagent cannot resolve conga-proxy-dave
```

This inherits the same per-agent network isolation we already enforce for agent containers.

### Slack Credentials — NOT Proxied

Slack credentials follow a different path and are **not** part of the credential proxy:

```
  ┌─────────────────────────────────────────────────┐
  │  openclaw.json (generated by GenerateOpenClawConfig) │
  │                                                 │
  │  "channels": {                                  │
  │    "slack": {                                   │
  │      "mode": "http",                            │
  │      "botToken": "xoxb-...",    ← REAL TOKEN    │
  │      "signingSecret": "abc...", ← REAL SECRET   │
  │      ...                                        │
  │    }                                            │
  │  }                                              │
  └─────────────────────────────────────────────────┘
```

**Why they stay in config, not the proxy:**
- `signingSecret` is used for **inbound** webhook validation (verifying the router's signature on incoming Slack events). It's never sent outbound.
- `botToken` is used for **Slack Web API calls** (posting messages, reactions). These go to `api.slack.com`, which is a different domain and auth pattern (Bearer token, not x-api-key).
- OpenClaw reads these from `openclaw.json` at startup, not from env vars. Removing them from config would require OpenClaw code changes.

**Future consideration:** `botToken` could be moved to the proxy in a later iteration (add a `/slack/*` route). This would remove the last real credential from the agent's accessible config. However, `signingSecret` must stay — it's used server-side for request validation, not for outbound calls.

### What the Agent Container Can See — Before vs After

| Check | Current (no proxy) | Proposed (with proxy) |
|---|---|---|
| `docker exec conga-myagent env \| grep ANTHROPIC_API_KEY` | `sk-ant-...` (real key) | (empty — not set) |
| `docker exec conga-myagent env \| grep ANTHROPIC_BASE_URL` | (empty — default) | `http://conga-proxy-myagent:8080/anthropic` |
| `docker exec conga-myagent env \| grep BRAVE_API_KEY` | `BSA...` (real key) | (empty — not set) |
| `docker exec conga-myagent cat /home/node/.openclaw/openclaw.json \| jq .channels.slack.botToken` | `xoxb-...` (real token) | `xoxb-...` (real token — unchanged) |
| Agent reads `process.env.ANTHROPIC_API_KEY` | `sk-ant-...` | `undefined` |
| Agent makes Claude API call | Direct to `api.anthropic.com` with real key | Via `http://conga-proxy-myagent:8080/anthropic`, proxy injects key |

### Lifecycle Operations — Updated

| Operation | Proxy behavior | Agent behavior |
|---|---|---|
| `conga admin add-user myagent` | Create `myagent-proxy.env`, start `conga-proxy-myagent` first | Create `myagent.env`, start `conga-myagent` after proxy is up |
| `conga secrets set myagent brave-api-key` | Secret written to disk | No container change (needs refresh) |
| `conga refresh --agent myagent` | Regenerate `myagent-proxy.env`, restart `conga-proxy-myagent` first | Regenerate `myagent.env`, restart `conga-myagent` after proxy is up |
| `conga admin pause myagent` | Stop `conga-proxy-myagent` | Stop `conga-myagent` |
| `conga admin unpause myagent` | Start `conga-proxy-myagent` first | Start `conga-myagent` after proxy is up |
| `conga admin remove-agent myagent` | Remove `conga-proxy-myagent`, delete `myagent-proxy.env` | Remove `conga-myagent`, delete `myagent.env` |
| `conga status --agent myagent` | Show proxy container state | Show agent container state |
| `conga logs --agent myagent` | Available via `--component proxy` | Default log target |

### Failure Modes

| Failure | Result | Detection | Recovery |
|---|---|---|---|
| Proxy crashes | Agent loses all outbound API access (fail-closed) | `conga status` shows proxy stopped; agent logs show connection refused | `conga refresh` restarts both |
| Proxy env file missing | Proxy won't start → agent starts but can't make API calls | `conga status` shows proxy not-found | `conga refresh` regenerates env files |
| Wrong secret value | Proxy sends bad auth → upstream returns 401/403 | Agent logs show API errors | `conga secrets set` + `conga refresh` |
| Agent tries to bypass proxy | HTTP to arbitrary host blocked by Envoy egress proxy (egress policy system) | Egress proxy logs blocked connection | Behavioral guardrail (Feature D) |
| Secret file deleted from disk | Next refresh generates env file without that secret | Proxy starts but missing route returns 502 | `conga secrets set` to re-add |

---

## Summary: What Changes and What Doesn't

### Unchanged (both providers)

| Component | Detail |
|---|---|
| Secrets Manager paths (AWS) | `conga/shared/*`, `conga/agents/{name}/*` — no changes |
| Secrets Manager IAM policies (AWS) | Instance role `conga/*` read access — no changes |
| Secret file paths (local) | `~/.conga/secrets/shared/*`, `~/.conga/secrets/agents/{name}/*` — no changes |
| `conga secrets set/get/delete` | User workflow identical on both providers |
| `conga admin setup` | Shared secret prompts identical on both providers |
| Secret retrieval logic | `readSharedSecrets()` / `GetSecretValue()` — no changes |
| `SecretNameToEnvVar()` | Kebab-case → SCREAMING_SNAKE_CASE — no changes |

### Changed (both providers, in common package)

| Component | Detail |
|---|---|
| `GenerateEnvFile()` | Splits into `GenerateAgentEnvFile()` (no secrets) + `GenerateProxyEnvFile()` (secrets only) |
| `GenerateOpenClawConfig()` | Sets `ANTHROPIC_BASE_URL` to proxy URL; Slack tokens unchanged |
| Container startup order | Proxy starts first, agent starts second |
| `conga refresh` | Restarts proxy first (fresh secrets), then agent |
| `conga status` | Shows proxy health alongside agent health |
| openclaw.json | Slack tokens stay; no other secret values present (already true today) |
| Docker network topology | One additional container (`conga-proxy-{name}`) per agent on same network |
| Bootstrap script (AWS) | Creates proxy container per agent alongside agent systemd unit |
| `ProvisionAgent` / `RemoveAgent` / `PauseAgent` / `UnpauseAgent` | All manage proxy container lifecycle alongside agent container |
