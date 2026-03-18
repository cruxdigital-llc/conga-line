# Feature: CruxClaw CLI — Trace Log

**Started**: 2026-03-18
**Status**: Planning

## Active Personas
- Architect — system integrity, dependencies, pattern consistency
- Product Manager — user value, scope, success criteria
- QA — edge cases, failure modes, cross-platform concerns

## Active Capabilities
- Terraform CLI (validate, plan)
- Go toolchain (build, test, vet)

## Decisions
- **Language**: Go (Cobra CLI, AWS SDK v2) — mature SDK, single static binary, easy cross-compilation
- **Name**: `cruxclaw` — distinct from upstream `openclaw`
- **Location**: `cli/` directory in this repo, shell scripts coexist for power users
- **Auth**: AWS IAM Identity Center (SSO), start URL `https://crux-login.awsapps.com/start/`
- **Discovery**: EC2 tags (`Name=openclaw-host`) + SSM Parameter Store (`/openclaw/users/*`) — no Terraform state access required
- **User resolution**: Automatic via IAM identity → SSM Parameter mapping (`/openclaw/users/by-iam/{identity}`); `--user` is optional override
- **Script embedding**: Bash scripts embedded in Go binary via `//go:embed`, accepting duplication with Terraform user-data as trade-off for self-contained binary
- **Distribution**: GoReleaser only (GitHub Releases)

## Files to Create
- [requirements.md](requirements.md) — Requirements and success criteria
- [plan.md](plan.md) — High-level implementation plan

## Files to Modify (Implementation)
- `terraform/ssm-parameters.tf` (new) — SSM Parameter Store resources for CLI discovery
- `terraform/variables.tf` — add `iam_identity` field to `users` variable
- `terraform/iam.tf` — verify/add SSM Parameter permissions
- `terraform/compute.tf` — verify EC2 tags
- `cli/` (new directory) — entire Go CLI project

## Persona Review

**Product Manager**: Clear user value — non-technical users go from "impossible" to a 5-step onboarding flow. Name `cruxclaw` is distinct and memorable. Scope confirmed: full CLI including admin commands. Success metric: new team member at web UI with only AWS SSO + `brew install` + 3 commands.

**Architect**: Go + AWS SDK v2 is appropriate for CLI tooling. SSM Parameter Store for discovery is clean — no new infrastructure, just metadata. Embedded bash scripts duplicate logic from `user-data.sh.tftpl` but the trade-off (self-contained binary vs network dependency) is correct. `session-manager-plugin` is an unavoidable external dependency for port forwarding. No breaking changes to existing infrastructure — all Terraform additions are purely additive.

**QA**: Key edge cases identified — SSO expiry mid-operation, missing `session-manager-plugin`, port conflicts, unmapped IAM identities, instance not running. All have defined handling (error messages, fallback prompts, platform-specific install instructions). Windows support via Go's cross-platform signal handling.

## Spec Session

**Resumed**: 2026-03-18 — `/glados:spec-feature`

### Files Created
- [spec.md](spec.md) — Detailed technical specification

### Spec Persona Review

**Architect**: Spec is well-structured. Go project layout follows standard conventions (`cmd/` + `internal/`). AWS SDK usage is correct — SSO OIDC flow, SSM StartSession for tunneling, Parameter Store for discovery. The `session-manager-plugin` subprocess approach matches how the AWS CLI itself works. Template embedding via `//go:embed` keeps the binary self-contained. No unnecessary dependencies. One note: the `refresh-user.sh.tmpl` builds the gateway port from a file on disk — should instead read from SSM Parameter Store for consistency with the CLI's discovery model.

**Product Manager**: All 13 commands specified with clear flows, flags, and error messages. The user journey is smooth: `auth login` → `secrets set` → `connect`. Admin flow is complete: `add-user` auto-assigns ports and creates mappings. The `--user` flag as optional override is good UX — users don't need to know implementation details. Non-goals are clearly defined (no Homebrew, no TUI, no self-update).

**QA**: Edge cases are comprehensive — 10 failure scenarios with specific error messages. The connect command's device pairing poll (goroutine, 10s interval, 5min timeout) is well-defined. Signal handling for tunnel cleanup is specified. Cross-platform concerns addressed (browser open, clipboard copy, signal handling). Missing edge case: what if the user's container is in a restart loop? `cruxclaw status` should detect and surface this.

### Pre-Implementation Standards Gate Report

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Zero ingress | network | must | ✅ PASSES — CLI uses SSM (outbound HTTPS), no inbound rules added |
| SSM-only access | network | must | ✅ PASSES — all remote operations via SSM RunCommand or StartSession |
| Least privilege | iam | must | ✅ PASSES — CLI users scoped to their own secrets path, admin has broader but defined permissions |
| Defense in depth | architecture | must | ✅ PASSES — SSM provides IAM auth for tunnel, gateway token provides app-layer auth |
| Secrets never touch disk | secrets | must | ✅ PASSES — CLI prompts for values and sends directly to Secrets Manager API; gateway token displayed in terminal only |
| Isolated Docker networks | container | must | ✅ PASSES — no changes to container networking |
| Zero trust the AI agent | architecture | should | ✅ PASSES — CLI operates outside the container; no new capabilities exposed to the AI agent |
| Immutable configuration | config | must | ✅ PASSES — CLI does not modify container config files; admin operations go through systemd |
| Config integrity monitoring | monitoring | must | ✅ PASSES — no changes to config integrity system |
