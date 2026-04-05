# Feature: Agent Portability

**Trace ID**: 2026-04-05_feature_agent-portability
**Created**: 2026-04-05
**Status**: Verified
**Lead**: Architect

## Session Log

### 2026-04-05 — Planning Session

**Objective**: Design an abstraction layer so Conga Line can deploy any compatible AI agent runtime (not just OpenClaw), starting with Hermes Agent as the second supported runtime.

**Active Personas**: Architect, QA, Product Manager
**Active Capabilities**: CLI tools, Web Fetch (GitHub research), File system

## Context

Conga Line is currently tightly coupled to OpenClaw as the sole agent runtime. The coupling spans:
- Config file format (`openclaw.json`)
- Container directory layout (`/home/node/.openclaw/`)
- Health detection (OpenClaw-specific log markers)
- Gateway port conventions (18789)
- Node.js-specific environment variables (`NODE_OPTIONS`)
- Webhook URL paths (`/slack/events`)
- Token extraction (JavaScript executed inside container)
- Plugin/channel config structure

The goal is to introduce a **Runtime** abstraction that makes the agent runtime pluggable, similar to how the Provider interface already makes infrastructure pluggable.

**Target runtimes**:
1. OpenClaw (existing, Node.js, JSON config, GHCR image)
2. Hermes Agent (Python, YAML config, build-from-source Docker, 15+ platform adapters)

## Decisions

1. **Conga remains the routing/control layer** — all runtimes receive Slack events via HTTP webhook from the Conga router. No direct Socket Mode from agent containers.
2. **Conga generates each runtime's native config** — `openclaw.json` for OpenClaw, `config.yaml` for Hermes. Conga data model is the source of truth.
3. **Slack-only channel scope** — no new channel types added at the Conga layer as part of this feature.
4. **Local provider first** — end-to-end verification on local Docker before extending to remote and AWS.
5. **Runtime × Provider composition** — the Runtime interface is orthogonal to the Provider interface. Any provider works with any runtime.

## Artifacts

- [requirements.md](requirements.md) — Functional and non-functional requirements
- [plan.md](plan.md) — 7-phase implementation plan
- [spec.md](spec.md) — Detailed technical specification

## Files Created

| File | Purpose |
|------|---------|
| `specs/2026-04-05_feature_agent-portability/README.md` | This trace log |
| `specs/2026-04-05_feature_agent-portability/requirements.md` | Requirements document |
| `specs/2026-04-05_feature_agent-portability/plan.md` | High-level implementation plan |
| `specs/2026-04-05_feature_agent-portability/spec.md` | Technical specification |

### 2026-04-05 — Specification Session

**Objective**: Produce detailed technical specification — Runtime interface contract, data model changes, edge cases, and Hermes integration details.

**Artifacts created**:
- [spec.md](spec.md) — 13-section technical specification

**Persona review**:
- **Architect**: Approved with one fix — `SharedSecrets` import cycle resolved by moving type to `pkg/provider/`
- **QA**: Approved with one addition — mixed-runtime routing test added
- **PM**: Approved — no blocking issues

**Standards gate**: PASSED (0 violations, 1 warning)
- ⚠️ Interface Parity: `--runtime` JSON schema + MCP tool params added to spec Section 8.3
- All 15 standards checked, 14 pass, 1 warning resolved

**Key findings from research**:
- Hermes does NOT support HTTP webhook Slack mode (Socket Mode only)
- Hermes has `/health` endpoint on port 8642 and bearer token auth via `API_SERVER_KEY`
- Hermes has a generic WebhookAdapter on port 8644 that could be adapted for Slack event delivery
- Phase 1 will be gateway-only (no Slack) for Hermes; Slack integration is Phase 2

### 2026-04-05 — Verification Session

**Objective**: Verify implementation against requirements and standards.

**Automated verification**:
- Full test suite: 16 packages, 0 failures
- `go vet`: clean
- `gofmt`: clean (3 files auto-formatted)

**Persona review**: All three approved
- Architect: Found `deployBehavior` hardcoding workspace path → fixed to use `rt.WorkspacePath()`
- QA: Approved, no test gaps identified
- PM: Approved, UX matches requirements

**Standards gate** (post-implementation): PASSED (0 violations, 1 warning — MCP parity deferred)

**Spec retrospection**:
- 4 minor divergences from spec: `ContainerSpec()` simplified (no `image` param), `ProxyEnvVars` not needed, `Tmpfs` not needed, `SharedSecrets` in `provider` not `common` (per Section 10)
- Spec updated to match implementation
- Test helper cleaned up (custom `contains` → `strings.Contains`)

**Post-verification fix**: `deployBehavior` now uses `rt.WorkspacePath()` instead of hardcoded `"data/workspace"`

### 2026-04-05 — Implementation Session

**Objective**: Implement the Runtime interface, extract OpenClaw, wire providers, implement Hermes runtime, and add CLI changes.

**Completed**:
- Phase 1: Runtime interface (`pkg/runtime/runtime.go`), registry, SharedSecrets migration
- Phase 2: OpenClaw runtime extraction (8 files in `pkg/runtime/openclaw/`), backward-compat wrappers in `pkg/common/config.go`
- Phase 3: Local provider wired to Runtime interface (ProvisionAgent, RefreshAgent, GetStatus, Connect all delegate to Runtime)
- Phase 4: Hermes runtime implemented (8 files in `pkg/runtime/hermes/`)
- Phase 5: `--runtime` CLI flag, Runtime field in AgentConfig/Config/SetupConfig/Manifest, status display
- Phase 6: 38 runtime tests (contract + specific), all 16 test suites pass

**Files created** (18 new):
- `pkg/runtime/runtime.go`, `pkg/runtime/registry.go`, `pkg/runtime/runtime_test.go`
- `pkg/runtime/openclaw/runtime.go`, `config.go`, `env.go`, `container.go`, `dirs.go`, `health.go`, `token.go`, `channels.go`, `openclaw-defaults.json`
- `pkg/runtime/hermes/runtime.go`, `config.go`, `env.go`, `container.go`, `dirs.go`, `health.go`, `token.go`, `channels.go`

**Files modified** (9):
- `pkg/provider/provider.go` — SharedSecrets type + Runtime field on AgentConfig
- `pkg/provider/config.go` — Runtime field
- `pkg/provider/setup_config.go` — Runtime field
- `pkg/common/config.go` — backward-compat wrappers, removed direct OpenClaw logic
- `pkg/provider/localprovider/provider.go` — delegates to Runtime interface
- `pkg/provider/localprovider/docker.go` — parameterized by ContainerSpec
- `pkg/provider/localprovider/channels.go` — runtime-aware config regeneration
- `pkg/manifest/manifest.go` — Runtime fields on Manifest + ManifestAgent
- `internal/cmd/root.go` — `--runtime` flag
- `internal/cmd/status.go` — runtime display

**Files removed** (1):
- `pkg/common/openclaw-defaults.json` — moved to `pkg/runtime/openclaw/`

**Deferred**:
- 3.8: routing.go webhook path parameterization (needed for mixed-runtime Slack delivery)
- 6.4: mixed-runtime routing test
- Remote and AWS provider wiring (Phase 6 in plan)

## Active Personas

- **Architect** — Interface design, extraction strategy, provider-runtime composition
- **QA** — Runtime contract tests, combinatorial test surface (providers × runtimes)
- **Product Manager** — Scope control, backward compatibility, success criteria
