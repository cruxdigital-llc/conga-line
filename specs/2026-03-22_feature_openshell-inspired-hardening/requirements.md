# Requirements — OpenShell-Inspired Security Hardening

## Background

A comparison of our Conga Line deployment platform with NVIDIA's OpenShell project identified three security areas where OpenShell's approach is materially stronger than ours:

1. **Credential isolation** — OpenShell's agent process never sees real API keys (proxy rewrites auth headers). Our agents read real keys from env vars.
2. **Filesystem enforcement** — OpenShell uses Linux Landlock LSM for kernel-enforced filesystem boundaries. We use hash-based monitoring (detection, not prevention).
3. ~~**Egress control**~~ — *Closed.* Now implemented via the Envoy-based egress policy system (`conga-policy.yaml`, `cli/internal/policy/`). See Features 15-17.

This spec plans lightweight implementations of the remaining gaps, without adopting OpenShell as a dependency or introducing its K3s/Rust/OPA overhead.

## Goal

Close the remaining security gaps (credential isolation, filesystem enforcement, credential-in-chat defense) by implementing native solutions within our existing Docker + Go CLI architecture, maintaining compatibility with both AWS and local providers. Egress control is already addressed by the Envoy-based policy system.

## Success Criteria

### Feature A: Credential Proxy Sidecar
- [ ] Agent container env file contains zero real API keys (Anthropic, Google, Brave, Trello, etc.)
- [ ] Agent container receives only a proxy base URL (`http://conga-proxy-{name}:8080`)
- [ ] Proxy container holds real keys and rewrites `Authorization` headers on outbound requests
- [ ] SSE streaming responses pass through correctly (no buffering, no truncation)
- [ ] Works identically on AWS and local providers
- [ ] `conga secrets set` workflow unchanged — secrets still stored in Secrets Manager / filesystem
- [ ] `conga refresh` recreates proxy with updated secrets
- [ ] Proxy crash is observable via `conga status` and `conga logs`
- [ ] Slack tokens (botToken, signingSecret) remain in openclaw.json — they are used for inbound webhook validation, not outbound API calls, and OpenClaw reads them from config not env

### Feature B: Landlock Filesystem Isolation
- [ ] OpenClaw process (uid 1000) cannot write to `openclaw.json` (kernel-enforced, not just permissions)
- [ ] OpenClaw process can write `.tmp` files in the config directory (hot-reload requirement)
- [ ] OpenClaw process can write to its data/memory directories (`/home/node/.openclaw/data/`, `/home/node/.openclaw/memory/`)
- [ ] OpenClaw process can write to `/tmp`
- [ ] All other filesystem paths are write-blocked by Landlock
- [ ] Works on AL2023 kernel 6.1+ (Landlock ABI v1-v3)
- [ ] Local provider on macOS: Landlock is a no-op (Docker runs a Linux VM, so it actually works — but graceful degradation if kernel doesn't support it)
- [ ] Container restarts and systemd lifecycle unaffected
- [ ] Replaces hash-based config integrity monitoring as the primary control (monitoring can remain as defense-in-depth)

### ~~Feature C: Egress Allowlist Proxy~~ — Superseded
> Implemented by the Envoy-based egress policy system (Features 15-17). Per-agent domain allowlisting via `conga-policy.yaml` with MCP tools for policy management. All original success criteria met or exceeded.

### Feature D: Credential-in-Chat Defense
- [ ] Agent behavioral guardrail: refuses credentials posted in chat, directs user to `conga secrets set`
- [ ] Credential pattern scanner detects known key formats in conversation history (`sk-ant-`, `xoxb-`, `xapp-`, etc.)
- [ ] Scanner alerts via structured log event (`CREDENTIAL_IN_CHAT`) — same pattern as config integrity monitoring
- [ ] Scanner does NOT auto-delete or modify conversation history (too risky)
- [ ] Pattern list is configurable and version-controlled (not hardcoded)
- [ ] Behavior file update deploys via existing `conga admin refresh-all` workflow
- [ ] Works on both AWS (systemd timer) and local (CLI command or optional timer)

## Non-Goals

- No OPA/Rego policy engine — egress is handled by the existing Envoy-based policy system
- No TLS MITM — egress filtering uses Envoy CONNECT passthrough, not certificate interception
- No GPU passthrough or inference routing — not relevant to our always-on assistant use case
- No OpenShell as a runtime dependency
- No changes to the Provider interface — these are container-level enhancements, not new provider methods
- Slack tokens stay in openclaw.json — the credential proxy only handles outbound API credentials

## Constraints

- **Resource budget**: Combined overhead of credential proxy + Landlock must stay under 64MB RAM per agent (proxy ~32MB, Landlock wrapper ~0). Egress proxy overhead is already accounted for in the policy system.
- **ARM64**: All components must work on ARM64 (r6g instances on AWS, Apple Silicon locally)
- **No Docker-in-Docker**: Solutions must work within standard Docker, not nested containers
- **Provider parity**: Both AWS and local providers must support all three features
- **Backwards compatibility**: Existing `conga` CLI commands (secrets, status, logs, refresh, connect) must work without modification from the user's perspective
- **Existing egress proxy**: The Envoy-based egress proxy is already deployed and enforced via `conga-policy.yaml`. The credential proxy sidecar should integrate with it (proxy's outbound traffic routes through the egress proxy).

## Architect Review

**Architecture fit**: All remaining features are additive container-level enhancements. They don't change the Provider interface, agent lifecycle, or routing architecture. The credential proxy is a new sidecar container per agent (similar pattern to the router). Landlock is an entrypoint wrapper inside the existing container. Egress is already handled by the Envoy-based policy system.

**New dependencies**: Feature A introduces a small Go HTTP reverse proxy binary (compiled into a container image). Feature B introduces a small Go init binary. No new egress dependencies (already implemented).

**Concern — credential proxy failure mode**: If the proxy sidecar crashes, the agent loses all outbound API access. This is actually *better* than the current model (fail-closed vs fail-open), but we need clear observability. The proxy should be a simple, low-surface-area binary with minimal crash risk.

**Concern — Landlock granularity**: Landlock v1 (kernel 5.13) supports filesystem access control. Landlock v2 (kernel 5.19) adds the `refer` right. Landlock v3 (kernel 6.2) adds file truncation control. AL2023 kernel 6.1 gives us v1+v2 but not v3. This is sufficient for our needs (we only need write-path control).

## QA Review

**Edge case — proxy and SSE**: OpenClaw uses Server-Sent Events for streaming Claude API responses. The proxy must not buffer the response body — it must stream chunks as they arrive. Test with a long-running conversation that generates multi-minute streaming responses.

**Edge case — proxy and retries**: If the Anthropic API returns 429 (rate limit) or 529 (overloaded), OpenClaw retries with backoff. The proxy must pass through error responses transparently, not add its own retry logic.

**Edge case — Landlock and OpenClaw upgrades**: Future OpenClaw versions may write to new directories. The Landlock allowlist must be configurable (not hardcoded) so it can be updated when upgrading the image.

**Edge case — egress proxy and MCP tools**: OpenClaw's MCP tool servers (if enabled) may need to reach additional domains. The egress allowlist in `conga-policy.yaml` supports per-agent overrides for this.

**Failure mode — cascading restart**: When `conga refresh` restarts an agent, it must restart the credential proxy first (to have fresh secrets), then the agent container. Order matters.

**Testability**: All three features should have clear pass/fail tests:
- Feature A: `docker exec conga-{name} env | grep ANTHROPIC` should show proxy URL, not real key
- Feature B: `docker exec conga-{name} touch /home/node/.openclaw/openclaw.json` should fail with EACCES
- Feature C: `docker exec conga-{name} curl https://evil.example.com` should fail; `curl https://api.anthropic.com` should succeed (via proxy)
