# Plan — OpenShell-Inspired Security Hardening

## Implementation Order

Features are ordered by value-to-effort ratio and dependency:

1. **Feature A: Credential Proxy Sidecar** — New sidecar container. Highest security value. Benefits from the existing Envoy-based egress policy (proxy's outbound traffic goes through egress allowlist).
2. **Feature B: Landlock Filesystem Isolation** — Container entrypoint wrapper. Independent of A but lowest priority (hash monitoring is working, and config is already root-owned 0444).

> **Note**: Feature C (Egress Allowlist Proxy) was originally part of this spec but has been superseded by the Envoy-based egress policy system implemented in Features 15-17 (see `specs/2026-03-25_feature_egress-allowlist/`, `specs/2026-03-26_feature_network-level-egress-enforcement/`, `specs/2026-03-26_feature_mcp-policy-tools/`). The egress policy system provides per-agent domain allowlisting via `conga-policy.yaml`, Envoy proxy enforcement, and MCP tools for policy management — all of which exceed what Feature C originally proposed.

---

## Feature A: Credential Proxy Sidecar

### Architecture

```
Docker network: conga-{name}

  conga-{name}            conga-proxy-{name}
  (OpenClaw)      --->    (Go reverse proxy)
                          Holds real keys:
  ANTHROPIC_              ANTHROPIC_API_KEY     ---> api.anthropic.com
  BASE_URL=               BRAVE_API_KEY         ---> api.search.brave.com
  http://conga-           TRELLO_API_KEY        ---> api.trello.com
  proxy-{name}            GOOGLE_CLIENT_SECRET  ---> www.googleapis.com
  :8080
```

The proxy is a simple Go HTTP reverse proxy (~150 lines) that:
1. Receives requests from the agent on port 8080
2. Routes to the correct upstream based on the request path prefix
3. Injects the real API key into the `Authorization` or `x-api-key` header
4. Streams the response back (no buffering — critical for SSE)

### Why Per-Agent (Not Shared)

A single shared proxy would be simpler (one container like the egress proxy), but it would hold *every* agent's API keys in one process. A compromise of the shared proxy leaks all credentials. The per-agent model preserves our existing isolation boundary: each proxy only holds its own agent's keys, so the blast radius of a compromise is identical to today's env-var model.

**Resource reality**: Each proxy is ~32MB RAM. At 10 agents, that's 320MB total — vs 20GB for the agents themselves (1.6% overhead). Container count doubles but resource cost barely moves.

### Credential Sources — Shared vs Per-Agent

The proxy env file is assembled from two sources at provisioning time, using the same merge pattern `GenerateEnvFile()` already implements:

```
{name}-proxy.env (mode 0400, mounted only by conga-proxy-{name})
├── From shared secrets (identical across all proxy sidecars):
│   ├── ANTHROPIC_API_KEY=sk-ant-...      (conga admin setup)
│   ├── GOOGLE_CLIENT_ID=...              (conga admin setup)
│   └── GOOGLE_CLIENT_SECRET=...          (conga admin setup)
└── From per-agent secrets (unique to this agent):
    ├── BRAVE_API_KEY=BSA...              (conga secrets set)
    ├── TRELLO_API_KEY=...                (conga secrets set)
    └── TRELLO_TOKEN=...                  (conga secrets set)
```

No new storage model or distribution path — shared secrets come from Secrets Manager / `~/.conga/secrets/shared/`, per-agent secrets from `~/.conga/secrets/agents/{name}/`, merged into one env file that only the proxy container mounts. The agent container's env file is stripped to non-secret config only (`NODE_OPTIONS`, `ANTHROPIC_BASE_URL`, `HTTPS_PROXY`).

Slack tokens (`SLACK_BOT_TOKEN`, `SLACK_SIGNING_SECRET`) stay in openclaw.json — they're used for inbound webhook validation, not outbound API calls, so they don't route through the proxy.

### Why a Dedicated Proxy (Not HTTPS_PROXY)

`HTTPS_PROXY` handles TLS tunneling (CONNECT method) — it can't inspect or modify HTTP headers inside the TLS tunnel. To rewrite `Authorization` headers, we need to terminate TLS at the proxy and re-establish it to the upstream. But that's TLS MITM, which we explicitly don't want.

Instead, the proxy is an **application-level reverse proxy** for specific API endpoints. OpenClaw is configured to use `http://conga-proxy-{name}:8080` as the base URL for its model provider. The proxy adds auth headers and forwards to the real API over HTTPS. This is the same pattern OpenShell uses (their "Privacy Router"), just without the K3s overhead.

### Credential Routing Map

| OpenClaw Config Field | Current | With Proxy |
|---|---|---|
| `ANTHROPIC_API_KEY` | Real key in env | Removed from env |
| `ANTHROPIC_BASE_URL` | Not set (default) | `http://conga-proxy-{name}:8080/anthropic` |
| `BRAVE_API_KEY` | Real key in env | Removed from env |
| `TRELLO_API_KEY` + `TRELLO_TOKEN` | Real keys in env | Removed from env |
| `GOOGLE_CLIENT_ID` + `GOOGLE_CLIENT_SECRET` | Real values in env | Removed from env |

The proxy maps path prefixes to upstreams:
- `/anthropic/*` → `https://api.anthropic.com/*` (injects `x-api-key` header)
- `/brave/*` → `https://api.search.brave.com/*` (injects `X-Subscription-Token` header)
- `/trello/*` → `https://api.trello.com/*` (injects `key` + `token` query params)
- `/google/*` → `https://www.googleapis.com/*` (injects `Authorization: Bearer` header)

### Plan

#### Step 1: Build the Proxy Binary
- New directory: `deploy/credential-proxy/`
- `main.go`: ~150 lines. HTTP server on `:8080`, data-driven reverse proxy with header injection, SSE-aware (disable response buffering via `FlushInterval: -1` on `httputil.ReverseProxy`)
- **Route table is data-driven**, loaded from `/etc/credential-proxy/routes.json` at startup:
  ```json
  [
    {
      "prefix": "/anthropic",
      "upstream": "https://api.anthropic.com",
      "auth": { "type": "header", "header": "x-api-key", "env": "ANTHROPIC_API_KEY" }
    },
    {
      "prefix": "/brave",
      "upstream": "https://api.search.brave.com",
      "auth": { "type": "header", "header": "X-Subscription-Token", "env": "BRAVE_API_KEY" }
    },
    {
      "prefix": "/trello",
      "upstream": "https://api.trello.com",
      "auth": { "type": "query", "params": { "key": "TRELLO_API_KEY", "token": "TRELLO_TOKEN" } }
    },
    {
      "prefix": "/google",
      "upstream": "https://www.googleapis.com",
      "auth": { "type": "header", "header": "Authorization", "prefix": "Bearer ", "env": "GOOGLE_CLIENT_SECRET" }
    }
  ]
  ```
- Adding a new service = adding a JSON entry + setting the env var via `conga secrets set`. No proxy code changes.
- **Two default route configs ship with the project:**
  - `routes.json` — generous default with all known OpenClaw-supported services (Anthropic, OpenAI, Brave, Trello, Google, GitHub). Routes without a corresponding env var return 502 — no harm if unused.
  - `routes-restricted.json` — example locked-down config (Anthropic only). For enterprise deployments that want to limit which external services agents can authenticate to.
- **Per-deployment configurable**: `conga admin setup` copies the selected route config to the deployment config directory. Admin can swap or edit it anytime:
  - Local: `~/.conga/config/routes.json`
  - AWS: `/opt/conga/config/routes.json`
- The file is mounted read-only into each proxy container. All proxies on a host share the same route table (the *credentials* differ per-agent, but the *allowed services* are a deployment-wide policy).
- This pairs with the egress allowlist (Feature C) as a two-layer control: the route table controls which services the proxy *can authenticate to*, and the egress allowlist controls which domains the agent *can reach at all*. Enterprise admins lock down both.
- Proxy reads env vars named in the route config to get actual credential values.
- Health endpoint at `/healthz` for `conga status` integration (reports which routes are configured vs missing env vars vs disabled by route table)
- `Dockerfile`: `FROM golang:1.25-alpine AS builder` then `FROM alpine:3.20` (multi-stage, ~10MB image)
- ARM64 + AMD64 multi-arch build

#### Step 2: Env File Split
- `GenerateEnvFile()` in `common/config.go` splits into two files:
  - `{name}.env` — non-secret config only (`NODE_OPTIONS`, `ANTHROPIC_BASE_URL`, `HTTPS_PROXY`, etc.)
  - `{name}-proxy.env` — real API keys (mode 0400, only mounted by proxy container)
- Secrets that are *not* outbound API credentials stay in the agent env (e.g., `SLACK_BOT_TOKEN` and `SLACK_SIGNING_SECRET` are used for inbound webhook validation and remain in openclaw.json, not env)

#### Step 3: Provider Integration — Container Lifecycle
- **Local provider** (`localprovider/docker.go`):
  - New `runProxyContainer()` function (similar to `runRouterContainer()`)
  - `ProvisionAgent()` creates proxy container on agent network before starting agent
  - `RemoveAgent()` removes proxy container
  - `RefreshAgent()` recreates proxy (fresh secrets) then restarts agent
  - `PauseAgent()` stops both containers
  - `UnpauseAgent()` starts proxy first, then agent
- **AWS provider** (`awsprovider/`):
  - Bootstrap script creates proxy container per agent (same lifecycle as agent systemd unit)
  - `ExecStartPre` in agent systemd unit ensures proxy is running
  - Secrets fetched from Secrets Manager at boot, injected into proxy env file

#### Step 4: CLI Status Integration
- `GetStatus()` includes proxy container state (running/stopped/not-found)
- `GetLogs()` supports `--component proxy` flag to tail proxy logs
- `conga status` shows proxy health alongside agent health

#### Step 5: OpenClaw Configuration Changes
- `GenerateOpenClawConfig()` sets `ANTHROPIC_BASE_URL` to proxy URL when proxy is enabled
- For Brave/Trello/Google: configure plugin base URLs to route through proxy
- Fallback: if proxy is not enabled (e.g., during migration), existing env-var injection still works

#### Step 6: Testing
- Unit test: proxy correctly rewrites `Authorization` header for each upstream
- Unit test: proxy streams SSE responses without buffering
- Unit test: proxy passes through 429/529 error responses transparently
- Integration test: `docker exec conga-{name} env | grep ANTHROPIC_API_KEY` returns empty
- Integration test: `docker exec conga-{name} env | grep ANTHROPIC_BASE_URL` returns proxy URL
- Integration test: agent can complete a Claude API conversation through the proxy
- Failure test: proxy crash causes agent API calls to fail immediately (fail-closed)
- Failure test: proxy restart causes agent to reconnect automatically (no state in proxy)

### Files Created
- `deploy/credential-proxy/main.go` — reverse proxy binary (data-driven, reads routes.json)
- `deploy/credential-proxy/routes.json` — route table config (prefix → upstream + auth injection)
- `deploy/credential-proxy/Dockerfile` — multi-stage build

### Files Modified
- `cli/internal/common/config.go` — `GenerateEnvFile()` split, `GenerateProxyEnvFile()` new
- `cli/internal/common/config.go` — `GenerateOpenClawConfig()` sets proxy base URLs
- `cli/internal/provider/localprovider/docker.go` — `runProxyContainer()`, lifecycle integration
- `cli/internal/provider/localprovider/provider.go` — proxy in ProvisionAgent/RemoveAgent/Refresh/Pause/Unpause
- `cli/internal/provider/awsprovider/` — bootstrap script updates for proxy container
- `terraform/bootstrap.sh` — proxy container creation in AWS bootstrap

### Resource Cost
- Go reverse proxy: ~32MB RAM per agent (tiny binary, minimal allocations)
- One additional container per agent on the Docker network

### Full Container Topology (Post-Hardening)

For an environment with 2 agents (`myagent`, `leadership`), the complete container set:

| Container | Purpose | Scope | Image | RAM |
|---|---|---|---|---|
| `conga-myagent` | OpenClaw agent | Per-agent | openclaw:2026.3.11 | ~2GB |
| `conga-proxy-myagent` | Credential proxy (outbound API auth) | Per-agent | credential-proxy:latest | ~32MB |
| `conga-leadership` | OpenClaw agent | Per-agent | openclaw:2026.3.11 | ~2GB |
| `conga-proxy-leadership` | Credential proxy (outbound API auth) | Per-agent | credential-proxy:latest | ~32MB |
| `conga-router` | Slack event router (inbound Socket Mode → HTTP fan-out) | Shared | node:22-alpine | ~128MB |
| `conga-egress-{name}` | Egress allowlist (Envoy-based domain filtering) | Per-agent | envoyproxy/envoy:v1.32 | ~64MB |

**Total for 2 agents**: 6 containers, ~4.3GB RAM

The credential proxy and Slack router serve opposite directions:
- **Credential proxy**: outbound — agent → proxy → external API (injects auth headers)
- **Slack router**: inbound — Slack WebSocket → router → agent webhook (fans out events)

The Slack router stays Node.js (`@slack/socket-mode` dependency) and shared (single Socket Mode connection for the whole Slack app). The credential proxy is Go and per-agent (isolation boundary).

---

## Feature B: Landlock Filesystem Isolation

### Architecture

```
Container entrypoint flow:
  1. Docker starts container, entrypoint is conga-landlock-init
  2. conga-landlock-init (runs as uid 1000):
     a. Creates Landlock ruleset
     b. Adds write rules for allowed paths only
     c. Enforces ruleset (self-restricting, irreversible)
     d. Calls the real OpenClaw entrypoint (node)
  3. OpenClaw process inherits Landlock restrictions
     - Can write: data/, memory/, /tmp, .tmp files
     - Cannot write: openclaw.json, /etc/, /usr/, anything else
```

### Landlock Rules

| Path | Access | Rationale |
|---|---|---|
| `/home/node/.openclaw/` | Read + write (dirs only, for `.tmp` file creation) | OpenClaw hot-reload creates `.tmp` files next to config |
| `/home/node/.openclaw/openclaw.json` | Read only | Config file must not be writable by agent process |
| `/home/node/.openclaw/data/` | Read + write | Agent data/memory storage |
| `/home/node/.openclaw/memory/` | Read + write | Agent conversation memory |
| `/tmp` | Read + write | Scratch space for Node.js |
| `/` (everything else) | Read only | Default: read-only filesystem |

### Plan

#### Step 1: Build the Init Binary
- New directory: `deploy/landlock-init/`
- `main.go`: ~100 lines. Uses `golang.org/x/sys/unix` for Landlock syscalls
- Detects Landlock ABI version at runtime — if unsupported (ABI 0), logs warning and proceeds into node directly (graceful degradation)
- Compiled as a static binary (`CGO_ENABLED=0`) for minimal dependencies
- Added to the OpenClaw container image via a custom Dockerfile layer

#### Step 2: Custom Container Image Layer
- New `deploy/landlock-init/Dockerfile`:
  ```
  FROM golang:1.25-alpine AS builder
  # ... build conga-landlock-init

  FROM ghcr.io/openclaw/openclaw:2026.3.11
  COPY --from=builder /conga-landlock-init /usr/local/bin/
  ENTRYPOINT ["/usr/local/bin/conga-landlock-init"]
  ```
- This wraps the upstream OpenClaw image with our init binary as the entrypoint
- The init binary calls into `node` (the original entrypoint) after applying Landlock rules
- Image built and pushed to our registry (ECR on AWS, local build for local provider)

#### Step 3: Configurable Write Paths
- Write paths read from env var `LANDLOCK_WRITE_PATHS` (colon-separated)
- Default: `/home/node/.openclaw/data:/home/node/.openclaw/memory:/tmp`
- Allows future OpenClaw versions that write to new paths to be accommodated without rebuilding the init binary

#### Step 4: Provider Integration
- **Local provider**: `runAgentContainer()` uses the custom image (configured via `admin setup`)
- **AWS provider**: bootstrap pulls the custom image from ECR
- No provider interface changes — this is a container image concern, not a provider concern

#### Step 5: Testing
- Unit test: Landlock init correctly restricts writes (test in a Linux container)
- Integration test: `docker exec conga-{name} touch /home/node/.openclaw/openclaw.json` fails with EACCES
- Integration test: `docker exec conga-{name} touch /home/node/.openclaw/data/test` succeeds
- Integration test: `docker exec conga-{name} touch /tmp/test` succeeds
- Degradation test: on a kernel without Landlock support, agent starts normally with a warning log

### Files Created
- `deploy/landlock-init/main.go` — Landlock init wrapper
- `deploy/landlock-init/Dockerfile` — custom OpenClaw image layer
- `deploy/landlock-init/go.mod` — minimal Go module

### Files Modified
- `cli/internal/common/config.go` — `GenerateEnvFile()` adds `LANDLOCK_WRITE_PATHS`
- `cli/internal/provider/localprovider/docker.go` — uses custom image reference
- `terraform/bootstrap.sh` — pulls custom image from ECR

### Resource Cost
- Zero runtime overhead — Landlock is kernel-enforced with no userspace daemon
- Init binary adds ~2MB to the container image
- One-time ~5ms at container start for rule application

---

## Feature D: Credential-in-Chat Defense

### Problem

Features A-C protect against credential leakage from the *infrastructure* layer. But a user can bypass all of it by posting a credential directly in chat: "here's my Trello key: abc123". At that point, the credential is:

1. **In conversation context** — the agent has it in working memory
2. **Persisted to disk** — OpenClaw saves conversation history to `/home/node/.openclaw/data/`
3. **Potentially usable** — the agent has coding tools and could craft a direct HTTP request with the raw key, bypassing the credential proxy (since the egress proxy allows the target domain)

The credential proxy protects against the agent *reading its own env vars*. It doesn't protect against a user *handing the agent a key directly*.

### Threat Model

| Scenario | Severity | Likelihood |
|---|---|---|
| User pastes credential in chat, agent stores in memory | Medium | High (users are lazy) |
| Agent uses pasted credential to make direct API call | Low | Low (requires intent + coding tool use) |
| Credential persists in conversation history on disk | Medium | High (automatic OpenClaw behavior) |
| Attacker prompt-injects agent to exfiltrate pasted credential | High | Low (requires successful injection + credential in context) |

### Defense Layers

#### Layer 1: Behavioral Guardrail (do first, zero cost)

Add to `behavior/base/SOUL.md` under Boundaries:

```markdown
## Credential Hygiene

If a user shares an API key, token, password, secret, or any credential in chat:
1. Do NOT store, repeat, or use the credential value
2. Do NOT acknowledge what the credential is for or confirm its format
3. Respond with: "I can't accept credentials through chat — they get stored in my
   conversation history. Please use your terminal instead:
   `conga secrets set <your-name> <secret-name> <value>`"
4. Move on — do not reference the credential in subsequent messages

This applies even if the user insists. Credentials in chat are a security risk that
the platform is designed to prevent.
```

**Limitation**: Behavioral instructions are best-effort. A sufficiently creative prompt injection or a persistent user could get the agent to acknowledge the credential. This is a soft control, not a hard boundary.

#### Layer 2: Credential Pattern Detection (medium effort)

A periodic scanner that checks conversation history for credential patterns:

- **Where**: New systemd timer (AWS) or Docker healthcheck script (local), similar to config integrity monitoring
- **What**: Regex scan of conversation logs for known credential patterns:
  - `sk-ant-[a-zA-Z0-9-_]{20,}` (Anthropic API keys)
  - `xoxb-[0-9]+-[0-9]+-[a-zA-Z0-9]+` (Slack bot tokens)
  - `xapp-[0-9]+-[a-zA-Z0-9]+` (Slack app tokens)
  - `sk-[a-zA-Z0-9]{20,}` (generic API key pattern)
  - Configurable pattern list in `deploy/credential-scanner/patterns.conf`
- **Action**: Log `CREDENTIAL_IN_CHAT` event → CloudWatch metric filter → alarm (same pattern as config integrity). Does NOT auto-delete (too risky — could corrupt OpenClaw state).
- **Alert contains**: agent name, pattern matched (not the credential itself), timestamp
- **Admin response**: manual review, optionally clear conversation history via `conga admin` command

#### Layer 3: Accepted Residual Risk

Even with layers 1 and 2, a credential that enters chat *will* persist in conversation memory on disk until the conversation is pruned or manually cleared. This is acceptable because:

- Conversation data is already protected at rest (encrypted EBS on AWS, disk encryption on local)
- The credential proxy means the agent's process doesn't have *other* credentials in env to correlate with
- Landlock restricts where the agent can write (can't exfiltrate to unexpected paths)
- The egress allowlist limits where the agent can send data (can't exfiltrate to arbitrary domains)
- The behavioral guardrail means the agent won't *use* the credential in normal operation

The residual risk is: a pasted credential sits in encrypted conversation history until pruned. This is comparable to a user pasting a credential in a Slack DM — the platform protects it at rest but can't un-see it.

### Plan

#### Step 1: Behavior File Update
- Add "Credential Hygiene" section to `behavior/base/SOUL.md`
- Applies to all agents (user and team) via base behavior composition
- Deploy via `conga admin refresh-all` (behavior files sync on container restart)

#### Step 2: Credential Pattern Scanner
- New directory: `deploy/credential-scanner/`
- `patterns.conf`: one regex per line, version-controlled
- Scanner script (~50 lines bash or Go): reads patterns, scans conversation logs, emits structured log on match
- AWS: systemd timer runs every 15 minutes (same pattern as config integrity timer)
- Local: optional — runs via `conga admin scan-credentials` command or background timer
- CloudWatch metric filter for `CREDENTIAL_IN_CHAT` → existing SNS alarm topic

#### Step 3: Admin Tooling (optional, deferred)
- `conga admin clear-history <agent>` — clears conversation memory for an agent
- Useful as a response to credential-in-chat alerts
- Deferred — can be done manually via `docker exec` for now

### Files Created
- `deploy/credential-scanner/patterns.conf` — credential regex patterns
- `deploy/credential-scanner/scan.sh` (or `scan.go`) — scanner script

### Files Modified
- `behavior/base/SOUL.md` — add Credential Hygiene section
- `terraform/bootstrap.sh` — add scanner systemd timer (AWS)
- `cli/internal/provider/localprovider/provider.go` — optional scanner integration

### Resource Cost
- Behavioral guardrail: zero
- Scanner: negligible (runs every 15 min, scans text files, exits)

---

## Implementation Timeline

| Week | Feature | Milestone |
|---|---|---|
| 1 | D: Credential-in-Chat | Behavior file update (immediate, zero-cost) |
| 1 | A: Credential Proxy | Go proxy binary + Dockerfile + unit tests |
| 2 | A: Credential Proxy | Env file split + provider lifecycle integration |
| 2 | A: Credential Proxy | OpenClaw config changes + end-to-end testing |
| 3 | B: Landlock Init | Init binary + custom image layer |
| 3 | D: Credential Scanner | Pattern scanner + systemd timer + alerting |
| 3 | B: Landlock Init | Provider integration + testing + documentation |

> **Note**: Feature C (Egress Proxy) rows removed — egress is already implemented via the Envoy-based policy system.

## Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| OpenClaw doesn't respect `ANTHROPIC_BASE_URL` | Low | Blocks Feature A | Verify with current pinned image before building proxy. Fallback: configure via openclaw.json model config. |
| Landlock init breaks on future OpenClaw image updates | Medium | Container won't start | Configurable write paths via env var. CI test that verifies container starts with Landlock enabled. |
| Credential proxy adds latency to API calls | Low | Degraded UX | Proxy is on the same Docker network (sub-millisecond). No TLS termination on the proxy side (HTTP between agent and proxy). |
| Anthropic SDK doesn't send `x-api-key` to non-Anthropic URLs | Medium | Blocks Feature A | Proxy injects the key itself — the SDK just sends requests to the base URL without auth. Verify SDK behavior. |
| User pastes credential in chat despite behavioral guardrail | High | Credential persists in conversation memory | Layer 1 (behavior) reduces frequency; Layer 2 (scanner) detects and alerts; encrypted disk protects at rest. Accepted residual risk. |
| Credential scanner produces false positives | Medium | Alert fatigue | Tune patterns conservatively. Start with known prefixes (`sk-ant-`, `xoxb-`, `xapp-`) that have low false-positive rates. |

## Handoff

This plan is ready for `/glados:spec-feature` to create detailed technical specifications for each sub-feature. Recommended spec order:

1. **Feature D** (behavior file update is immediate, scanner is simple)
2. **Feature A** (highest security value, egress already in place for full coverage)
3. **Feature B** (independent, lowest priority)
