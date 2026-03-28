# Technical Specification — OpenShell-Inspired Security Hardening

## Overview

Three remaining features that layer to close security gaps identified in the NVIDIA OpenShell comparison. Each feature is independently deployable. Implementation order: D → A → B.

> **Note**: Feature C (Egress Allowlist Proxy) was originally part of this spec but has been superseded by the Envoy-based egress policy system implemented in Features 15-17. See `specs/2026-03-25_feature_egress-allowlist/`, `specs/2026-03-26_feature_network-level-egress-enforcement/`, and `specs/2026-03-26_feature_mcp-policy-tools/`.

---

## Feature D: Credential-in-Chat Defense

### D.1 Behavior File Change

**File**: `behavior/base/SOUL.md`

Add after the existing `## Boundaries` section:

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

**Deployment**: `conga admin refresh-all` syncs behavior files to containers on restart.

### D.2 Credential Pattern Scanner

**New file**: `deploy/credential-scanner/patterns.conf`

```
# Credential patterns — one regex per line. Lines starting with # are comments.
# Matches are logged as CREDENTIAL_IN_CHAT events. No auto-deletion.
sk-ant-[a-zA-Z0-9\-_]{20,}
xoxb-[0-9]+-[0-9]+-[a-zA-Z0-9]+
xapp-[0-9]+-[a-zA-Z0-9]+
sk-[a-zA-Z0-9]{32,}
ghp_[a-zA-Z0-9]{36,}
gho_[a-zA-Z0-9]{36,}
AIza[a-zA-Z0-9\-_]{35}
```

**New file**: `deploy/credential-scanner/scan.sh`

```bash
#!/bin/bash
# Scans OpenClaw conversation data for credential patterns.
# Emits structured log lines for CloudWatch metric filtering.
# Exit 0 always (monitoring tool, not a gating check).

PATTERNS_FILE="${PATTERNS_FILE:-/opt/conga/config/patterns.conf}"
DATA_DIR="${DATA_DIR:-/home/node/.openclaw/data}"
AGENT_NAME="${AGENT_NAME:-unknown}"

if [ ! -f "$PATTERNS_FILE" ]; then
    echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) CREDENTIAL_SCAN agent=$AGENT_NAME status=SKIPPED reason=no_patterns_file"
    exit 0
fi

FOUND=0
while IFS= read -r pattern; do
    [[ "$pattern" =~ ^#.*$ || -z "$pattern" ]] && continue
    if grep -rqE "$pattern" "$DATA_DIR" 2>/dev/null; then
        echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) CREDENTIAL_IN_CHAT agent=$AGENT_NAME pattern_prefix=${pattern:0:10}..."
        FOUND=1
    fi
done < "$PATTERNS_FILE"

if [ "$FOUND" -eq 0 ]; then
    echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) CREDENTIAL_SCAN agent=$AGENT_NAME status=CLEAN"
fi
```

**AWS deployment**: Systemd timer per agent, runs every 15 minutes:

```ini
# /etc/systemd/system/conga-credscan-{name}.timer
[Unit]
Description=Credential scan for conga-%i

[Timer]
OnCalendar=*:0/15
RandomizedDelaySec=60

[Install]
WantedBy=timers.target
```

```ini
# /etc/systemd/system/conga-credscan-{name}.service
[Unit]
Description=Credential scan for conga-%i

[Service]
Type=oneshot
ExecStart=/usr/bin/docker exec conga-%i /opt/conga/config/scan.sh
Environment=AGENT_NAME=%i
```

**Local deployment**: `conga admin scan-credentials` CLI command runs the scanner on-demand. Optional background timer deferred.

**CloudWatch integration**: Metric filter on `/conga/gateway` log group:

```
{ $.message = "CREDENTIAL_IN_CHAT" }
```

Maps to existing SNS alarm topic (same as config integrity violations).

### D.3 Edge Cases

| Scenario | Handling |
|---|---|
| User pastes credential split across multiple messages | Scanner catches the concatenated value in conversation history, not individual messages |
| Credential in a code block or file attachment | Scanner searches all text content in data directory recursively |
| Scanner false positive on random strings | Patterns use known prefixes with high specificity (`sk-ant-`, `xoxb-`). Generic `sk-` pattern requires 32+ chars to reduce noise. |
| Agent ignores behavioral guardrail (prompt injection) | Scanner detects after the fact. Feature A (credential proxy) limits damage — agent can't use the credential to authenticate directly. |

---

## Feature A: Credential Proxy Sidecar

### A.1 Route Configuration Schema

**New file**: `deploy/credential-proxy/routes.json` (generous default)

```json
[
    {
        "prefix": "/anthropic",
        "upstream": "https://api.anthropic.com",
        "auth": {
            "type": "header",
            "header": "x-api-key",
            "env": "ANTHROPIC_API_KEY"
        }
    },
    {
        "prefix": "/openai",
        "upstream": "https://api.openai.com",
        "auth": {
            "type": "header",
            "header": "Authorization",
            "prefix": "Bearer ",
            "env": "OPENAI_API_KEY"
        }
    },
    {
        "prefix": "/brave",
        "upstream": "https://api.search.brave.com",
        "auth": {
            "type": "header",
            "header": "X-Subscription-Token",
            "env": "BRAVE_API_KEY"
        }
    },
    {
        "prefix": "/trello",
        "upstream": "https://api.trello.com",
        "auth": {
            "type": "query",
            "params": {
                "key": "TRELLO_API_KEY",
                "token": "TRELLO_TOKEN"
            }
        }
    },
    {
        "prefix": "/google",
        "upstream": "https://www.googleapis.com",
        "auth": {
            "type": "header",
            "header": "Authorization",
            "prefix": "Bearer ",
            "env": "GOOGLE_CLIENT_SECRET"
        }
    },
    {
        "prefix": "/github",
        "upstream": "https://api.github.com",
        "auth": {
            "type": "header",
            "header": "Authorization",
            "prefix": "Bearer ",
            "env": "GITHUB_TOKEN"
        }
    }
]
```

**New file**: `deploy/credential-proxy/routes-restricted.json` (enterprise example)

```json
[
    {
        "prefix": "/anthropic",
        "upstream": "https://api.anthropic.com",
        "auth": {
            "type": "header",
            "header": "x-api-key",
            "env": "ANTHROPIC_API_KEY"
        }
    }
]
```

**Deployed path**:
- Local: `~/.conga/config/routes.json`
- AWS: `/opt/conga/config/routes.json`

Mounted read-only into each proxy container at `/etc/credential-proxy/routes.json`. Shared across all proxies on a host (deployment-wide policy).

### A.2 Go Types

```go
// Route defines a proxy route entry loaded from routes.json.
type Route struct {
    Prefix   string    `json:"prefix"`
    Upstream string    `json:"upstream"`
    Auth     AuthSpec  `json:"auth"`
}

// AuthSpec defines how credentials are injected per upstream.
type AuthSpec struct {
    Type   string            `json:"type"`    // "header" or "query"
    Header string            `json:"header"`  // for type=header: header name
    Prefix string            `json:"prefix"`  // for type=header: value prefix (e.g., "Bearer ")
    Env    string            `json:"env"`     // for type=header: env var name holding the value
    Params map[string]string `json:"params"`  // for type=query: param_name → env_var_name
}

// RouteStatus reports the state of a single route (for /healthz).
type RouteStatus struct {
    Prefix   string `json:"prefix"`
    Upstream string `json:"upstream"`
    Active   bool   `json:"active"`   // true if all required env vars are set
    Missing  string `json:"missing"`  // comma-separated missing env var names
}
```

### A.3 Proxy Binary — `deploy/credential-proxy/main.go`

Pseudocode structure (~150 lines):

```go
func main() {
    routes := loadRoutes("/etc/credential-proxy/routes.json")

    mux := http.NewServeMux()
    for _, route := range routes {
        mux.Handle(route.Prefix+"/", newRouteHandler(route))
    }
    mux.HandleFunc("/healthz", healthHandler(routes))

    log.Printf("credential-proxy starting on :8080 with %d routes", len(routes))
    log.Fatal(http.ListenAndServe(":8080", mux))
}

func newRouteHandler(route Route) http.Handler {
    upstream, _ := url.Parse(route.Upstream)
    proxy := &httputil.ReverseProxy{
        Rewrite: func(r *httputil.ProxyRequest) {
            r.SetURL(upstream)
            // Strip the route prefix from the path
            r.Out.URL.Path = strings.TrimPrefix(r.Out.URL.Path, route.Prefix)
            r.Out.Host = upstream.Host

            switch route.Auth.Type {
            case "header":
                value := os.Getenv(route.Auth.Env)
                if value != "" {
                    r.Out.Header.Set(route.Auth.Header, route.Auth.Prefix+value)
                }
            case "query":
                q := r.Out.URL.Query()
                for param, envVar := range route.Auth.Params {
                    value := os.Getenv(envVar)
                    if value != "" {
                        q.Set(param, value)
                    }
                }
                r.Out.URL.RawQuery = q.Encode()
            }
        },
        FlushInterval: -1, // Stream SSE without buffering
    }
    return proxy
}

func healthHandler(routes []Route) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        statuses := make([]RouteStatus, 0, len(routes))
        for _, route := range routes {
            status := RouteStatus{Prefix: route.Prefix, Upstream: route.Upstream}
            missing := missingEnvVars(route)
            status.Active = len(missing) == 0
            status.Missing = strings.Join(missing, ",")
            statuses = append(statuses, status)
        }
        json.NewEncoder(w).Encode(statuses)
    }
}
```

**Key behaviors**:
- `FlushInterval: -1` disables response buffering — critical for SSE streaming
- 429/529 and all other upstream responses pass through transparently
- No retry logic in the proxy — OpenClaw handles its own retries
- Route with missing env var returns 502 with body `{"error": "route /brave configured but BRAVE_API_KEY not set"}`
- Structured JSON logging: `{"time":"...","route":"/anthropic","upstream":"api.anthropic.com","method":"POST","path":"/v1/messages","status":200,"duration_ms":1234}`

### A.4 Dockerfile

**New file**: `deploy/credential-proxy/Dockerfile`

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o credential-proxy .

FROM alpine:3.20
RUN adduser -D -u 1000 proxy
COPY --from=builder /build/credential-proxy /usr/local/bin/
USER proxy
EXPOSE 8080
ENTRYPOINT ["credential-proxy"]
```

Multi-arch build (ARM64 + AMD64) via `docker buildx`.

### A.5 Env File Split

**Changes to `cli/internal/common/config.go`**:

Replace `GenerateEnvFile()` with two functions:

```go
// GenerateAgentEnvFile produces the env file for the agent container.
// Contains ZERO real credentials — only config pointing to the credential proxy.
// Note: HTTPS_PROXY and egress-related env vars are managed by the egress policy
// system (cli/internal/policy/), not by this function.
func GenerateAgentEnvFile(agent provider.AgentConfig, proxyEnabled bool) []byte {
    var buf []byte
    appendEnv := func(key, val string) {
        if val != "" {
            buf = append(buf, []byte(fmt.Sprintf("%s=%s\n", key, val))...)
        }
    }

    appendEnv("NODE_OPTIONS", "--max-old-space-size=1536")

    if proxyEnabled {
        proxyHost := fmt.Sprintf("conga-proxy-%s", agent.Name)
        appendEnv("ANTHROPIC_BASE_URL", fmt.Sprintf("http://%s:8080/anthropic", proxyHost))
    }

    return buf
}

// GenerateProxyEnvFile produces the env file for the credential proxy container.
// Contains ALL real credentials — mounted only by the proxy sidecar.
func GenerateProxyEnvFile(shared SharedSecrets, perAgent map[string]string) []byte {
    var buf []byte
    appendEnv := func(key, val string) {
        if val != "" {
            buf = append(buf, []byte(fmt.Sprintf("%s=%s\n", key, val))...)
        }
    }

    // Shared credentials
    appendEnv("ANTHROPIC_API_KEY", shared.AnthropicAPIKey)
    appendEnv("GOOGLE_CLIENT_ID", shared.GoogleClientID)
    appendEnv("GOOGLE_CLIENT_SECRET", shared.GoogleClientSecret)

    // Per-agent credentials
    for name, value := range perAgent {
        appendEnv(SecretNameToEnvVar(name), value)
    }

    return buf
}
```

**Note**: `SharedSecrets` struct gains an `AnthropicAPIKey` field. Currently Anthropic API key is stored as a per-agent secret even though it's shared — this should be normalized to the shared secrets path during this work.

### A.6 OpenClaw Config Changes

**Changes to `GenerateOpenClawConfig()`**:

When the credential proxy is enabled, the generated `openclaw.json` sets `ANTHROPIC_BASE_URL` via env var (already handled by the agent env file). No changes needed to openclaw.json itself for the Anthropic route — the SDK reads `ANTHROPIC_BASE_URL` from env.

For OpenClaw skills (Brave, Trello, Google), the plugin configuration in openclaw.json must point to the proxy:

```go
if proxyEnabled {
    proxyHost := fmt.Sprintf("conga-proxy-%s", agent.Name)
    config["skills"] = map[string]interface{}{
        "brave": map[string]interface{}{
            "baseUrl": fmt.Sprintf("http://%s:8080/brave", proxyHost),
        },
        "trello": map[string]interface{}{
            "baseUrl": fmt.Sprintf("http://%s:8080/trello", proxyHost),
        },
    }
}
```

**Verification needed**: Confirm each OpenClaw skill supports a `baseUrl` config override. If not, the skill's outbound requests go direct (through the egress proxy but without credential injection) and the credential must remain in env. This is a per-skill decision.

### A.7 Container Lifecycle

**New function in `localprovider/docker.go`**:

```go
// proxyContainerName returns the Docker container name for an agent's credential proxy.
func proxyContainerName(agentName string) string {
    return "conga-proxy-" + agentName
}

// runProxyContainer starts the credential proxy sidecar for an agent.
func runProxyContainer(ctx context.Context, opts proxyContainerOpts) error {
    args := []string{
        "run", "-d",
        "--name", opts.Name,
        "--network", opts.Network,
        "--env-file", opts.EnvFile,
        "--cap-drop", "ALL",
        "--security-opt", "no-new-privileges",
        "--memory", "64m",
        "--read-only",
        "--tmpfs", "/tmp:rw,noexec,nosuid",
        "-v", fmt.Sprintf("%s:/etc/credential-proxy/routes.json:ro", opts.RoutesJSON),
    }
    args = append(args, opts.Image)

    _, err := dockerRun(ctx, args...)
    return err
}

type proxyContainerOpts struct {
    Name       string
    Network    string
    EnvFile    string // {name}-proxy.env (mode 0400, contains real keys)
    RoutesJSON string // deployment-wide routes.json
    Image      string // credential-proxy:latest
}
```

**Lifecycle integration in `localprovider/provider.go`**:

| Provider method | Proxy behavior |
|---|---|
| `ProvisionAgent()` | Write `{name}-proxy.env` → start proxy container → then start agent container |
| `RemoveAgent()` | Stop + remove proxy container → then stop + remove agent container → delete `{name}-proxy.env` |
| `RefreshAgent()` | Regenerate `{name}-proxy.env` → restart proxy → then restart agent |
| `PauseAgent()` | Stop agent container → then stop proxy container |
| `UnpauseAgent()` | Start proxy container → then start agent container |
| `GetStatus()` | Include proxy container state in `AgentStatus.Errors` if proxy is down |
| `GetLogs()` | Support `--component proxy` flag (deferred — tail agent logs by default) |

**Startup order matters**: proxy must be running before agent starts, because the agent's first action is often an API call (heartbeat, Slack connection). If proxy isn't ready, the call fails and OpenClaw may cache the error.

### A.8 AWS Bootstrap Integration

Each agent's systemd unit adds a `ExecStartPre` to ensure the proxy is running:

```ini
[Service]
ExecStartPre=/usr/bin/docker start conga-proxy-%i || /usr/bin/docker run -d \
    --name conga-proxy-%i \
    --network conga-%i \
    --env-file /opt/conga/config/%i-proxy.env \
    --cap-drop ALL \
    --security-opt no-new-privileges \
    --memory 64m \
    --read-only \
    --tmpfs /tmp:rw,noexec,nosuid \
    -v /opt/conga/config/routes.json:/etc/credential-proxy/routes.json:ro \
    conga-credential-proxy
ExecStart=/usr/bin/docker start conga-%i
```

The bootstrap script builds the credential proxy image and creates proxy env files during the per-agent loop.

### A.9 Edge Cases

| Scenario | Handling |
|---|---|
| Proxy crashes mid-request | Client (OpenClaw) sees connection reset. OpenClaw retries automatically. Proxy is stateless — restart recovers. |
| Agent tries to reach api.anthropic.com directly | Blocked by egress proxy (Feature C) unless HTTPS_PROXY is bypassed. Even if bypassed, agent doesn't have the API key. |
| SSE stream lasts 10+ minutes | `FlushInterval: -1` streams continuously. No timeouts in the proxy. HTTP keep-alive maintained. |
| Upstream returns 401 (bad key) | Passed through to agent. OpenClaw logs the error. Admin updates secret via `conga secrets set` + `conga refresh`. |
| Route configured but env var missing | Proxy returns HTTP 502 with error body: `{"error": "route /brave configured but BRAVE_API_KEY not set"}`. Agent skill sees the error and reports it to the user. |
| Multiple agents share same network name | Impossible — network names are `conga-{agent_name}` and agent names are unique. |
| Proxy container name collision | Impossible — proxy names are `conga-proxy-{agent_name}` and agent names are unique. |
| Agent sends request with its own auth header | Proxy overwrites it. The `Rewrite` function unconditionally sets the header from the env var. |

---

## Feature B: Landlock Filesystem Isolation

### B.1 Init Binary — `deploy/landlock-init/main.go`

```go
package main

import (
    "fmt"
    "log"
    "os"
    "strings"
    "syscall"

    "golang.org/x/sys/unix"
)

const (
    defaultWritePaths = "/home/node/.openclaw/data:/home/node/.openclaw/memory:/tmp"
    configDir         = "/home/node/.openclaw"
)

func main() {
    if len(os.Args) < 2 {
        log.Fatal("usage: conga-landlock-init <command> [args...]")
    }

    // Check Landlock support
    abiVersion, err := unix.LandlockGetABI()
    if err != nil || abiVersion < 1 {
        log.Printf("WARNING: Landlock not supported (ABI=%d), proceeding without filesystem isolation", abiVersion)
        execInto(os.Args[1], os.Args[1:])
    }

    // Parse write paths from env (configurable for future OpenClaw versions)
    writePaths := strings.Split(getEnvOrDefault("LANDLOCK_WRITE_PATHS", defaultWritePaths), ":")

    // Create ruleset: all filesystem access types as handled
    ruleset, err := unix.LandlockCreateRuleset(&unix.LandlockRulesetAttr{
        HandledAccessFS: unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
            unix.LANDLOCK_ACCESS_FS_MAKE_REG |
            unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
            unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
            unix.LANDLOCK_ACCESS_FS_REMOVE_DIR,
    }, 0)
    if err != nil {
        log.Printf("WARNING: Landlock ruleset creation failed: %v, proceeding without isolation", err)
        execInto(os.Args[1], os.Args[1:])
    }

    // Add write rules for allowed paths
    for _, path := range writePaths {
        addWriteRule(ruleset, path)
    }

    // Allow .tmp file creation in config dir (OpenClaw hot-reload)
    // But NOT writes to openclaw.json itself (Landlock is path-based,
    // so we allow the directory but the file's existing permissions +
    // the fact that Landlock restricts at the handle level protects it)
    addWriteRule(ruleset, configDir)

    // Enforce — irreversible, applies to this process and all children
    if err := unix.LandlockRestrictSelf(ruleset, 0); err != nil {
        log.Printf("WARNING: Landlock enforcement failed: %v, proceeding without isolation", err)
        execInto(os.Args[1], os.Args[1:])
    }

    log.Printf("Landlock enforced: write allowed in %v", writePaths)
    execInto(os.Args[1], os.Args[1:])
}

func addWriteRule(ruleset int, path string) {
    fd, err := syscall.Open(path, syscall.O_PATH|syscall.O_DIRECTORY, 0)
    if err != nil {
        log.Printf("WARNING: cannot open %s for Landlock rule: %v", path, err)
        return
    }
    defer syscall.Close(fd)
    unix.LandlockAddPathBeneathRule(ruleset, &unix.LandlockPathBeneathAttr{
        AllowedAccess: unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
            unix.LANDLOCK_ACCESS_FS_MAKE_REG |
            unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
            unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
            unix.LANDLOCK_ACCESS_FS_REMOVE_DIR,
        ParentFd: fd,
    })
}

func execInto(command string, args []string) {
    // Replace this process with the target command
    syscall.Exec(command, args, os.Environ())
}

func getEnvOrDefault(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}
```

**Note on openclaw.json protection**: Landlock v1 operates at the directory level, not individual file level. We allow writes to `/home/node/.openclaw/` (for `.tmp` files) but `openclaw.json` is additionally protected by root ownership (mode 0444) and Docker read-only volume mount where possible. Landlock adds a third layer — even if the file permissions were somehow changed, the process cannot escalate beyond what Landlock allows. On Landlock v3+ (kernel 6.2+), we could add file-level truncation control for stronger protection.

### B.2 Custom Container Image

**New file**: `deploy/landlock-init/Dockerfile`

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o conga-landlock-init .

FROM ghcr.io/openclaw/openclaw:2026.3.11
COPY --from=builder /build/conga-landlock-init /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/conga-landlock-init", "node"]
```

The original OpenClaw entrypoint is `node`. Our init wraps it: `conga-landlock-init` applies Landlock rules, then `exec`s into `node` with the original arguments.

**New file**: `deploy/landlock-init/go.mod`

```
module github.com/cruxdigital-llc/conga-line/landlock-init

go 1.25

require golang.org/x/sys v0.30.0
```

### B.3 Provider Integration

**Image configuration**: `conga admin setup` stores the image reference. When Landlock is enabled, the image is the custom-built one (e.g., `conga-openclaw:2026.3.11-landlock`). The base image tag is preserved in `local-config.json` for upgrade tracking.

**Changes to `localprovider/docker.go` — `runAgentContainer()`**:

No code change needed — the function already uses `opts.Image`. The image reference just changes from `ghcr.io/openclaw/openclaw:2026.3.11` to the custom image.

**Changes to `cli/internal/common/config.go` — `GenerateAgentEnvFile()`**:

Add:
```
LANDLOCK_WRITE_PATHS=/home/node/.openclaw/data:/home/node/.openclaw/memory:/tmp
```

### B.4 Edge Cases

| Scenario | Handling |
|---|---|
| Kernel doesn't support Landlock | Init logs `WARNING`, proceeds without isolation. Agent works normally. |
| OpenClaw upgrade writes to new directory | Admin updates `LANDLOCK_WRITE_PATHS` env var via config change + `conga refresh`. No image rebuild needed. |
| `docker exec` into container to debug | Commands run as uid 1000 inherit Landlock restrictions. `docker exec -u root` bypasses Landlock (root can override). |
| Container restart | Landlock is applied fresh on each start (it's in the entrypoint, not persistent state). |
| Init binary crashes | Container fails to start. `conga status` shows container exited. `conga logs` shows the error. |

---

## Provider Interface

**No changes to the Provider interface.** All three features are implemented within existing provider methods:

| Feature | Integration point | How |
|---|---|---|
| D: Behavior | Existing behavior file sync in `RefreshAgent()` | New content in SOUL.md, same deployment path |
| D: Scanner | New systemd timer (AWS) / CLI command (local) | Added in bootstrap, not in Provider interface |
| A: Credential Proxy | `ProvisionAgent()`, `RemoveAgent()`, `RefreshAgent()`, `PauseAgent()`, `UnpauseAgent()` | New container managed alongside agent container |
| B: Landlock | Image reference change | Different image in `runAgentContainer()` |

---

## Container Interface Contracts

### Credential Proxy Container (`conga-proxy-{name}`)

| Property | Value |
|---|---|
| **Image** | `conga-credential-proxy:latest` (locally built) |
| **Port** | 8080 (HTTP, no TLS) |
| **Network** | Same as agent (`conga-{name}`) |
| **Env file** | `{name}-proxy.env` (mode 0400) — real API keys |
| **Volume** | `routes.json` mounted read-only at `/etc/credential-proxy/routes.json` |
| **Memory** | 64m |
| **Capabilities** | `--cap-drop ALL` |
| **Filesystem** | `--read-only` + tmpfs `/tmp` |
| **Health** | `GET /healthz` returns JSON array of route statuses |
| **Logging** | JSON to stdout (docker logs) |

> **Note**: The egress proxy container (`conga-egress-{name}`) is managed by the egress policy system (`cli/internal/policy/`). See the egress enforcement specs for its container contract.

### Agent Container (`conga-{name}`) — Changes

| Property | Current | After hardening |
|---|---|---|
| **Image** | `ghcr.io/openclaw/openclaw:2026.3.11` | `conga-openclaw:2026.3.11-landlock` (custom layer) |
| **Env file** | `{name}.env` (all secrets + config) | `{name}.env` (config only, zero secrets) |
| **New env vars** | — | `ANTHROPIC_BASE_URL`, `LANDLOCK_WRITE_PATHS` |
| **Entrypoint** | `node` | `conga-landlock-init node` |

---

## Migration Strategy

Features can be enabled incrementally. No big-bang cutover required.

### Phase 1 (Week 1): D
1. Update `behavior/base/SOUL.md` with credential hygiene section
2. `conga admin refresh-all` deploys the change
3. **Verify**: agent refuses credentials posted in chat
4. **Rollback**: revert SOUL.md change

> **Note**: Egress proxy is already deployed and enforced via the policy system. No Phase 1 egress work needed.

### Phase 2 (Week 1-2): A
1. Build credential proxy image
2. Split env files
3. Deploy proxy sidecar per agent
4. **Verify**: `docker exec conga-{name} env | grep ANTHROPIC_API_KEY` returns empty
5. **Rollback**: merge env files back, remove proxy containers, unset `ANTHROPIC_BASE_URL`

### Phase 3 (Week 3-4): B
1. Build custom OpenClaw image with Landlock init
2. Switch agent containers to custom image
3. **Verify**: `docker exec conga-{name} touch /home/node/.openclaw/openclaw.json` fails
4. **Rollback**: switch back to upstream image

### Phase 4 (Week 3): D scanner
1. Deploy patterns.conf + scan.sh
2. Add systemd timer (AWS) or CLI command (local)
3. **Verify**: scanner detects test credential planted in conversation data
4. **Rollback**: disable timer / remove scanner files

---

## Testing Strategy

### Unit Tests

| Test | Feature | File |
|---|---|---|
| Route config parsing (valid JSON, missing fields, empty file) | A | `deploy/credential-proxy/main_test.go` |
| Header injection per auth type (header, query, prefix) | A | `deploy/credential-proxy/main_test.go` |
| SSE response streaming (no buffering) | A | `deploy/credential-proxy/main_test.go` |
| 429/529 passthrough (no retry) | A | `deploy/credential-proxy/main_test.go` |
| Missing env var returns 502 with error message | A | `deploy/credential-proxy/main_test.go` |
| `GenerateAgentEnvFile()` contains zero secrets | A | `cli/internal/common/config_test.go` |
| `GenerateProxyEnvFile()` contains all secrets | A | `cli/internal/common/config_test.go` |
| Scanner pattern matching (true positive, false positive) | D | `deploy/credential-scanner/scan_test.sh` |
| Landlock ABI detection + graceful degradation | B | `deploy/landlock-init/main_test.go` |

### Integration Tests

| Test | Feature | Command |
|---|---|---|
| Agent env has no real API keys | A | `docker exec conga-{name} env \| grep -c "sk-ant-"` → 0 |
| Agent env has proxy base URL | A | `docker exec conga-{name} printenv ANTHROPIC_BASE_URL` → `http://conga-proxy-...` |
| Proxy /healthz returns route status | A | `docker exec conga-{name} wget -qO- http://conga-proxy-{name}:8080/healthz` |
| Agent can complete Claude conversation | A | End-to-end Slack/web UI test |
| Config file not writable | B | `docker exec conga-{name} touch /home/node/.openclaw/openclaw.json` → EACCES |
| Data directory writable | B | `docker exec conga-{name} touch /home/node/.openclaw/data/test` → success |
| Scanner detects planted credential | D | Plant `sk-ant-test123456789012345678901` in data dir, run scan.sh, check output |

---

## Security Summary

Post-implementation, the defense-in-depth stack for a single agent:

| Layer | Control | Type | What it prevents |
|---|---|---|---|
| 1 | Egress allowlist (existing — Envoy policy) | Prevention | Agent reaching unauthorized domains |
| 2 | Credential proxy (A) | Prevention | Agent reading/leaking API keys from env |
| 3 | Landlock (B) | Prevention | Agent modifying its own config |
| 4 | Behavioral guardrail (D) | Guidance | Users posting credentials in chat |
| 5 | Credential scanner (D) | Detection | Credentials that enter chat despite guardrail |
| 6 | Docker isolation (existing) | Prevention | Container escape, inter-container access |
| 7 | Config integrity monitor (existing) | Detection | Config tampering that bypasses other controls |
| 8 | Encrypted disk (existing) | At-rest | Credential exposure from disk theft |
