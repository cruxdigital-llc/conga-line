# CLI Integration Tests

Add a build-tagged integration test phase that exercises all primary CLI use
cases against a real local Docker environment. Tests verify actual effects
(secrets in container env, behavior files in workspace, egress proxy
blocking/allowing traffic) — not just CLI exit codes.

## Session log
- 2026-04-07: Feature planning started via `/glados/plan-feature`.
  Extensive prior research completed — see plan file for test infrastructure
  analysis, command inventory, and isolation strategy.
- 2026-04-07: Requirements and plan finalized. 4 test functions defined:
  agent lifecycle, behavior management, policy validation, egress enforcement.
  PROJECT_STATUS.md updated with feature #24.
- 2026-04-07: Spec session started via `/glados/spec-feature`.
- 2026-04-07: Spec written. Persona review: QA approved (edge cases covered,
  egress tests cover all 3 modes + deny-all). Architect approved (consistent
  patterns, no new deps, test isolation correct). Note: image version is
  hardcoded in both CI and test — use env var `CONGA_INTEG_IMAGE` for CI
  override.

## Active Capabilities
- Bash (go build, go test, docker)
- context7 MCP (library docs)

## Active Personas
- QA (test architecture, coverage gaps, edge cases)
- Architect (isolation strategy, CI integration, test infrastructure design)
