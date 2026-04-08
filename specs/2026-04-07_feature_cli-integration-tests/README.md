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
- 2026-04-07: Implementation started via `/glados/implement-feature`.
- 2026-04-07: Implementation complete. All 4 test functions pass (48 subtests,
  ~90s total). Key fixes during implementation: os.Stdout/Stderr capture for
  CLI output, cobra pflag.Changed reset between tests, docker exec 10s timeout
  for egress tests, node setTimeout hard kill for proxy hangs.

## Modified Files
- `internal/cmd/integration_test.go` — NEW: 4 test functions (lifecycle, behavior, policy, egress)
- `internal/cmd/integration_helpers_test.go` — NEW: test infrastructure (~230 lines)
- `.github/workflows/ci.yml` — added integration job
- 2026-04-07: Verification complete via `/glados/verify-feature`. All automated
  checks pass (17 unit test packages, 48 integration subtests, gofmt, go vet).
  Persona review: QA + Architect both approve. Standards gate: 0 violations.
  Spec updated to reflect AGENTS.md override/revert instead of TEST.md.

## Active Capabilities
- Bash (go build, go test, docker)
- context7 MCP (library docs)

## Active Personas
- QA (test architecture, coverage gaps, edge cases)
- Architect (isolation strategy, CI integration, test infrastructure design)
