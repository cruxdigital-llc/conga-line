# Feature: Slack Event Router — Trace Log

**Started**: 2026-03-17
**Status**: Planning

## Active Personas
- Architect — routing design, container networking, security boundaries

## Active Capabilities
- AWS CLI (profile: `openclaw`)
- Terraform CLI
- Node.js / Go (router implementation)

## Status
**Blocked** — OpenClaw HTTP webhook mode has a module identity split bug (route registers in monitor's module instance but gateway reads from a separate empty instance). Router works correctly; the bug is in OpenClaw. Reverting to separate Slack apps per user with Socket Mode.

See [LEARNINGS.md](LEARNINGS.md) for full analysis.

## Research Findings
- No off-the-shelf Socket Mode multiplexer exists
- OpenClaw natively supports `mode: "http"` (webhook) — containers don't need Socket Mode
- Slack recommends HTTP for production; Socket Mode is for dev/behind-firewall
- OpenShell (NVIDIA) solves a different problem (agent sandboxing, not message routing) but has patterns worth borrowing later (credential injection, policy engine)
- Router joins all per-user Docker networks (Option B) — preserves container isolation

## Files Created
- [requirements.md](requirements.md)
- [plan.md](plan.md) — architecture with Socket Mode proxy + HTTP webhook containers
- [spec.md](spec.md) — full implementation: router source, container config changes, networking, user-data

## Persona Review
**Architect**: ✅ Approved. SDK handles protocol complexity (~60 lines of routing logic). Option B networking preserves isolation. Ack-then-forward pattern satisfies Slack's 3-second window.

## Standards Gate
| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Zero ingress | network | must | ✅ PASSES |
| Container isolation | container | must | ✅ PASSES |
| Least privilege | container | must | ✅ PASSES |
| Secrets handling | secrets | must | ✅ PASSES |
