# Trace: CLI Hardening — Design, Reliability & Test Coverage

## Session Log

### 2026-03-19 — Spec Creation
- **Session resumed from**: Deep codebase analysis of all 24 CLI source files
- **Requirements defined**: `requirements.md` — 6 requirement categories (R1-R6)
- **Plan defined**: `plan.md` — 6 phases, ordered by risk reduction
- **Spec defined**: `spec.md` — detailed implementation spec with code examples

### Files Created
- `specs/2026-03-19_feature_cli-hardening/requirements.md`
- `specs/2026-03-19_feature_cli-hardening/plan.md`
- `specs/2026-03-19_feature_cli-hardening/spec.md`
- `specs/2026-03-19_feature_cli-hardening/README.md` (this file)

### Key Decisions
1. **Bug fixes first**: Silent failures in `admin.go` are the highest-risk issues
2. **Interface-based testability**: AWS service interfaces enable mocking without real credentials
3. **No new dependencies**: Tests use Go's built-in `testing` package; no `testify` or mock frameworks required
4. **Slack ID validation tightened**: `U` + 10 chars for members, `C` + 10 chars for channels
5. **Out of scope**: Color output, `--json` flag, `auth login` auto-exec, LocalStack E2E tests

## Persona Reviews

### Product Manager
**Verdict**: Approve

The spec addresses real user-facing issues (silent failures during admin operations, confusing next-steps messages) while staying focused. Scope is well-bounded — no feature creep. The "out of scope" section explicitly defers nice-to-haves (color, --json, clipboard) that don't block reliability. Success criteria are measurable (coverage targets, zero silent failures). The ordering (bugs -> reliability -> tests -> polish) correctly prioritizes user trust over developer convenience.

One note: R6.3 (uptime display) is the lowest-priority item and could be dropped if the phase runs long. It doesn't affect reliability.

### Architect
**Verdict**: Approve

The testability refactoring (interfaces + CLIContext) is the right architectural move. Key observations:

1. **Interface design is correct**: Interfaces declare only methods actually called, not the full SDK surface. This follows Go's "accept interfaces, return structs" idiom.
2. **CLIContext migration is low-risk**: The struct encapsulates existing global state without changing behavior. The migration path (global var -> struct field) is mechanical.
3. **No new dependencies**: Using Go's built-in testing package avoids dependency sprawl. If `testify` is added later, it's additive.
4. **`admin.go` split is pure organization**: Same package, same functions, just file boundaries. Zero behavioral change.
5. **Phase 3 as single commit**: Correct — the interface migration touches many files and should be atomic to avoid half-migrated state.

Concern: The `CLIContext` struct introduces a new pattern. Ensure that *all* global state is migrated in Phase 3 — don't leave some globals alongside the struct, as that creates two patterns for the same thing.

### QA Engineer
**Verdict**: Approve with note

Test coverage plan is thorough. The progression from pure functions (no mocks) to mocked AWS tests to UI tests is the right ordering — easiest to hardest.

Observations:
1. **`parseKeyValues` edge cases are well-covered**: equals-in-value, empty input, trailing newline. Add a test for `KEY=` (key with empty value) — this is important because SSM RunCommand output can have empty values.
2. **`RunCommand` consecutive error test is critical**: This is the most complex state machine in the CLI. The 5-error threshold test should verify both the boundary (5 errors then success = ok) and the exceeded case (6 errors = fail).
3. **`secretNameToEnvVar` test should include edge case `"a-"` (trailing dash)** — currently the transform would produce `A_` which is a valid but unusual env var name.
4. **Missing test**: `resolveGatewayPort` with empty agent list (should return 18789) and with agents that have port 0 (should skip them).

Note: The spec says "50%+ on internal/, 30%+ on cmd/" — I'd recommend tracking these separately in CI rather than just running `go test ./...` which gives a single aggregate number.

## Standards Gate Report

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Security (security.md) | all | must | ✅ PASSES — No new attack surface. Tighter input validation reduces injection risk. Secret handling unchanged. |
| No secrets on disk | secrets | must | ✅ PASSES — No changes to secret storage or handling patterns. |
| Least privilege | iam | must | ✅ PASSES — No IAM changes. |
| Defense in depth | all | should | ✅ PASSES — Adding input validation at CLI layer complements existing IAM controls. |
