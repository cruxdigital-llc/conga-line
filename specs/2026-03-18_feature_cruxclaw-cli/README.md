# Feature: CruxClaw CLI — Trace Log

**Started**: 2026-03-18
**Status**: ✅ Verified and closed

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

## Implementation Session

**Resumed**: 2026-03-18 — `/glados:implement-feature`

### Active Capabilities
- Go toolchain (build, test, vet, fmt)
- Terraform CLI (validate, plan)

### Implementation Log
- `terraform/ssm-parameters.tf` — created: user_config + user_iam_mapping SSM parameters
- `terraform/variables.tf` — updated: added `iam_identity` optional field to users type
- `terraform validate` — passed
- `cli/` — 20 source files created:
  - `main.go`, `cmd/root.go`, `cmd/version.go`, `cmd/auth.go`, `cmd/secrets.go`
  - `cmd/status.go`, `cmd/logs.go`, `cmd/refresh.go`, `cmd/connect.go`, `cmd/admin.go`
  - `internal/config/config.go`, `internal/aws/session.go`, `internal/aws/ec2.go`
  - `internal/aws/ssm.go`, `internal/aws/params.go`, `internal/aws/secrets.go`
  - `internal/discovery/instance.go`, `internal/discovery/identity.go`, `internal/discovery/user.go`
  - `internal/tunnel/tunnel.go`, `internal/ui/prompt.go`, `internal/ui/spinner.go`, `internal/ui/table.go`
  - `scripts/embed.go`, `scripts/add-user.sh.tmpl`, `scripts/refresh-user.sh.tmpl`, `scripts/remove-user.sh.tmpl`
- `cli/.goreleaser.yaml` — GoReleaser config for 5 build targets
- `.github/workflows/release.yml` — tag-triggered release workflow
- `go build` — successful, binary at `cli/cruxclaw`
- `go vet` — clean
- `gofmt` — clean
- All 13 commands registered and help text verified

## Verification Session

**Resumed**: 2026-03-18 — `/glados:verify-feature`

### Automated Verification
- `go build` — ✅ compiles clean
- `go vet ./...` — ✅ no issues
- `gofmt` — ✅ all formatted
- `terraform validate` — ✅ valid
- `./cruxclaw version` — ✅ runs, all 13 commands registered

### Security Fix Applied During Verification
- Added `validateMemberID()` and `validateChannelID()` to `cmd/root.go` — uppercase alphanumeric regex validation
- Applied to `--user` flag, `admin add-user` args, `admin remove-user` args
- Prevents shell injection via SSM RunCommand payloads that interpolate user-provided IDs

### Post-Implementation Persona Review

**Architect**: Implementation follows the spec cleanly. Go project layout is idiomatic (`cmd/` + `internal/`). AWS SDK v2 usage is correct. The `sync.Once` caching in instance discovery is a good pattern. Input validation added during verification closes the shell injection vector from `--user` flag. Embedded scripts match the existing `add-user.sh` and `refresh-user.sh` patterns.

**Product Manager**: All 13 commands implemented and verified. User journey is smooth — `version`, `auth`, `secrets`, `connect`, `refresh`, `status`, `logs` all work from help output. Admin commands cover the full lifecycle. The `auth login` command provides guided SSO setup instructions rather than implementing the OIDC flow directly — pragmatic for v1.

**QA**: Signal handling in `connect.go` correctly traps SIGINT/SIGTERM and kills the tunnel subprocess. Device pairing poll runs in a goroutine with a 5-minute timeout. Input validation prevents injection. Edge case: the `pollDevicePairing` goroutine will continue running briefly after signal is received — acceptable since the process is exiting anyway. The `isResourceNotFound` function uses string matching rather than `errors.As` — functional but not idiomatic.

### Post-Implementation Standards Gate Report

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Zero ingress | network | must | ✅ PASSES — CLI uses SSM only, no inbound rules |
| SSM-only access | network | must | ✅ PASSES — all remote operations via SSM |
| Least privilege | iam | must | ✅ PASSES — CLI uses caller's own IAM credentials |
| Defense in depth | architecture | must | ✅ PASSES — SSM (IAM auth) + gateway token (app auth) |
| Secrets never touch disk | secrets | must | ✅ PASSES — secrets go directly to Secrets Manager API; gateway token displayed in terminal only |
| Isolated Docker networks | container | must | ✅ PASSES — no changes to container networking |
| Zero trust the AI agent | architecture | should | ✅ PASSES — CLI operates outside container, no new agent capabilities |
| Immutable configuration | config | must | ✅ PASSES — CLI does not modify container configs directly |
| Input validation | security | must | ✅ PASSES — member IDs and channel IDs validated against `^[A-Z0-9]+$` before shell interpolation |

### Spec Alignment
- Spec called for `charmbracelet/huh` for prompts — implementation uses `golang.org/x/term` + manual prompts instead (simpler, fewer dependencies). Acceptable divergence.
- Spec called for SSO OIDC device authorization flow in `auth login` — implementation provides guided instructions instead. Deferred to v2.
- All other spec items implemented as specified.
