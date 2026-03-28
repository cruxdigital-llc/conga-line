# Trace Log — OpenShell-Inspired Security Hardening

## Session Start
- **Date**: 2026-03-22
- **Workflow**: plan-feature
- **Feature**: OpenShell-Inspired Security Hardening (3 sub-features)
- **Trigger**: Comparison analysis of NVIDIA OpenShell vs Conga Line identified three security gaps where OpenShell's approach is stronger. This spec plans lightweight implementations that capture the value without adopting OpenShell's K3s/Rust overhead.

## Active Personas
- **Architect** — system integrity, architecture fit, dependency analysis
- **QA** — edge cases, failure modes, test coverage

## Active Capabilities
- Standard file/code tools (no browser, database, or external project management tools)
- Git for version control

## Decisions
1. **Four sub-features originally identified** (three from OpenShell comparison, one from credential lifecycle analysis):
   - Feature A: Credential Proxy Sidecar (highest security value, per-agent)
   - Feature B: Landlock Filesystem Isolation (medium effort, Linux-only)
   - ~~Feature C: Egress Allowlist Proxy~~ — **Superseded** by Envoy-based egress policy system (Features 15-17)
   - Feature D: Credential-in-Chat Defense (behavioral guardrail + pattern scanner)
2. **No OpenShell dependency** — cherry-pick the ideas, implement natively in our stack
3. **Per-agent proxy, not shared** — preserves isolation boundary (shared proxy = single point of credential leakage for all agents)
4. **Feature D added** during planning when we identified that the credential proxy only protects against env-var leakage, not against users posting credentials directly in chat. Defense-in-depth: behavioral guardrail (soft) + pattern scanner (detection) + encrypted disk (at-rest).
5. **Personas selected**: Architect + QA (security-focused features need architecture review and failure mode analysis; PM scope-guard less critical since these are roadmap items already)

## Spec Session (2026-03-22)

### Persona Review
- **Architect**: Passes. Architecture is additive, no interface changes, naming conventions consistent. Two items to verify before implementation: `ANTHROPIC_BASE_URL` support in pinned image, skill `baseUrl` config overrides.
- **QA**: Passes with notes. SSE streaming test critical. Two items to track: (1) verify Landlock `O_PATH` works on tmpfs mounts, (2) add proxy-crash-mid-conversation integration test.

### Standards Gate
| Standard | Verdict |
|---|---|
| Zero trust the AI agent | ✅ PASSES |
| Immutable configuration | ✅ PASSES |
| Least privilege everywhere | ✅ PASSES |
| Defense in depth | ✅ PASSES |
| Secrets never touch disk | ⚠️ WARNING (proxy env file on disk — same as current model, accepted deviation) |
| Detect what you can't prevent | ✅ PASSES |
| Isolated Docker networks | ✅ PASSES |
| Container isolation controls | ✅ PASSES |

**Gate decision**: Proceed (1 warning, 0 violations).

## Files Created
- [requirements.md](requirements.md) — Goals, success criteria, constraints
- [plan.md](plan.md) — High-level implementation plan (Features A-D, timeline, risk register)
- [credential-flow.md](credential-flow.md) — End-to-end credential lifecycle: current vs proposed, with flow diagrams, routing tables, failure modes, and what changes vs what doesn't
- [spec.md](spec.md) — Detailed technical specification: config schemas, Go types, container contracts, nginx config, Landlock init binary, migration strategy, testing strategy
