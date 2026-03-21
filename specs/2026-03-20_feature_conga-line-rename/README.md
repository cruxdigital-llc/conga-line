# Trace Log: Conga Line Rename

**Feature**: Rename app from "OpenClaw"/"CruxClaw" to "Conga Line" / CLI from `cruxclaw` to `conga`
**Date**: 2026-03-20
**Status**: Verified complete

## Session Log

### 2026-03-20 — Planning Session
- Feature requested: comprehensive rename of app branding
- "Conga Line" — tagline: "a line of lobsters"
- CLI binary: `cruxclaw` → `conga`
- All SSM, Secrets Manager, S3 paths included in rename
- Upstream Open Claw references preserved

### 2026-03-20 — Spec Session
- Detailed spec written with file-by-file change list
- Config key `openclaw-image` renamed to `image`
- Intermediate config files renamed: `$AGENT_NAME-openclaw.json` → `$AGENT_NAME-config.json`
- CloudWatch namespace: `OpenClaw` → `CongaLine`

### 2026-03-21 — Implementation Session
- All 9 phases implemented
- Go build + tests + vet: all pass
- Final grep verification: zero stale references in code files
- Additional files caught during verification: `monitoring.tf`, `kms.tf`, `vpc.tf`, `bootstrap.sh`, `populate-secrets.sh`

### 2026-03-21 — Verification Session
- Go build + test + vet: all pass
- gofmt: clean
- Grep `cruxclaw|CruxClaw|openclaw-template|crux-claw` in code files: 0 hits
- Grep `openclaw` in code files: all remaining hits verified as upstream references
- Architect review: ✅ Approved
- QA review: ✅ Approved
- Standards gate (post-implementation): PASS — all 6 security standards ✅
- Spec retrospection: 5 additional files beyond spec caught and fixed during implementation
- No test synchronization issues (pure rename)

### Artifacts
- [requirements.md](requirements.md) — naming convention table, do-not-rename list, success criteria
- [plan.md](plan.md) — 9-phase bottom-up rename plan
- [spec.md](spec.md) — file-by-file change specification
- [tasks.md](tasks.md) — implementation checklist (all complete)

### Files Modified
**CLI Go Code (Phase 1)**: `go.mod`, `main.go`, 14 `cmd/*.go` files, 4 `internal/**/*.go` files, 3 test files, 6 `scripts/*.sh.tmpl` files
**GoReleaser (Phase 2)**: `.goreleaser.yaml`
**Terraform (Phase 3)**: `variables.tf`, `compute.tf`, `security.tf`, `iam.tf`, `ecr.tf`, `ssm-parameters.tf`, `behavior.tf`, `router.tf`, `outputs.tf`, `secrets.tf`, `monitoring.tf`, `kms.tf`, `vpc.tf`, `terraform.tfvars.example`
**Bootstrap (Phase 4)**: `user-data.sh.tftpl`, `user-data-shim.sh.tftpl`, `bootstrap.sh`, `populate-secrets.sh`
**Router (Phase 5)**: `package.json`, `src/index.js`
**Docs (Phase 6)**: `CLAUDE.md`, `README.md`, `CONTRIBUTING.md`, `SECURITY.md`
**Specs (Phase 7)**: 53 files across 13 spec directories
**Product Knowledge (Phase 8)**: `PROJECT_STATUS.md`, `ROADMAP.md`, `standards/security.md`, `observations/observed-standards.md`

## Active Personas
- **Architect** — structural integrity, infrastructure path consistency
- **QA** — verification checklist, missed-reference detection

## Key Decisions
1. **Naming**: `conga` for CLI/Docker/SSM/S3/Secrets prefix; `conga-line` for Terraform `project_name` and GitHub repo
2. **Full path rename**: SSM `/openclaw/` → `/conga/`, Secrets Manager, S3 prefixes all change
3. **Upstream preserved**: `ghcr.io/openclaw/openclaw:*`, `openclaw.json`, `/home/node/.openclaw/`, `npx openclaw`
4. **Config key rename**: `openclaw-image` → `image`
5. **Intermediate config**: `$AGENT_NAME-openclaw.json` → `$AGENT_NAME-config.json`
6. **CloudWatch namespace**: `OpenClaw` → `CongaLine`

## Standards Gate Report (Pre-Implementation)

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Zero trust the AI agent | all | must | ✅ PASSES |
| Immutable configuration | all | must | ✅ PASSES |
| Least privilege | iam | must | ✅ PASSES |
| Secrets never touch disk | secrets | must | ✅ PASSES |
| Isolated Docker networks | containers | must | ✅ PASSES |
| IMDSv2 enforced | compute | must | ✅ PASSES |

**Gate: PASS** — pure rename, no security implications.
