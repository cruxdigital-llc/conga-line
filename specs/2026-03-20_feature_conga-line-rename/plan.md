# Plan: Conga Line Rename

## Approach
Bottom-up rename: start with leaf files (Go code, scripts), then infrastructure (Terraform), then docs. This ensures each layer compiles/validates before moving to the next.

## Phases

### Phase 1: CLI Code (Go)
**Files**: `cli/go.mod`, `cli/main.go`, `cli/cmd/*.go`, `cli/pkg/**/*.go`, `cli/pkg/**/*_test.go`

1. Update `go.mod` module path: `github.com/cruxdigital-llc/openclaw-template/cli` → `github.com/cruxdigital-llc/conga/cli`
2. Update all import paths across every `.go` file
3. Rename CLI binary references:
   - `Use: "cruxclaw"` → `Use: "conga"` in root.go
   - `"CruxClaw"` → `"Conga Line"` in help text
   - `"cruxclaw %s"` → `"conga %s"` in version.go
   - Help text examples: `cruxclaw admin ...` → `conga admin ...`
4. Update SSM path constants: `/openclaw/` → `/conga/`
5. Update instance tag constant: `openclaw-host` → `conga-host`
6. Update Secrets Manager path patterns: `openclaw/` → `conga-line/`
7. Update Docker container/network name patterns: `openclaw-` → `conga-`
8. Update host path references: `/opt/openclaw/` → `/opt/conga/`
9. Update systemd/log references: `openclaw-` → `conga-`
10. Fix all test files with hardcoded paths

**Verify**: `cd cli && go build -o conga . && go test ./... && go vet ./...`

### Phase 2: GoReleaser + CI
**Files**: `cli/.goreleaser.yaml`, `.github/workflows/*.yml`

1. `project_name: cruxclaw` → `project_name: conga`
2. `binary: cruxclaw` → `binary: conga`
3. `name_template` with `cruxclaw_` → `conga_`
4. Ldflags module path update
5. GitHub repo `name: crux-claw` → `name: conga-line`
6. Update any CI workflow references

**Verify**: `goreleaser check` (if installed)

### Phase 3: Terraform
**Files**: `terraform/*.tf`, `terraform/*.tf.example`, `terraform/*.tfvars.example`

1. Update `variables.tf` defaults: `project_name = "openclaw"` → `"conga-line"`
2. Rename Terraform resource identifiers (local names only — these don't affect AWS):
   - `aws_launch_template.openclaw` → `aws_launch_template.conga`
   - `aws_instance.openclaw` → `aws_instance.conga`
   - `aws_security_group.openclaw_host` → `aws_security_group.conga_host`
   - `aws_iam_role.openclaw_host` → `aws_iam_role.conga_host`
   - `aws_iam_instance_profile.openclaw_host` → `aws_iam_instance_profile.conga_host`
   - `aws_ecr_repository.openclaw` → `aws_ecr_repository.conga`
   - `aws_ecr_lifecycle_policy.openclaw` → `aws_ecr_lifecycle_policy.conga`
3. Update SSM parameter paths: `/openclaw/` → `/conga/`
4. Update Secrets Manager ARN patterns: `secret:openclaw/` → `secret:conga/`
5. Update S3 key prefixes: `openclaw/` → `conga-line/`
6. Update output names: `openclaw_host_sg_id` → `conga_host_sg_id`, etc.
7. Update security group description text
8. Update `.example` files
9. Update comments referencing `cruxclaw` CLI commands

**Verify**: `cd terraform && terraform validate` (with dummy backend)

### Phase 4: Bootstrap Script
**Files**: `terraform/bootstrap.sh`, `terraform/populate-secrets.sh`

1. Update default PROJECT_NAME: `openclaw` → `conga-line`
2. Update host paths: `/opt/openclaw/` → `/opt/conga/`
3. Update Docker container/network naming: `openclaw-` → `conga-`
4. Update systemd unit naming: `openclaw-` → `conga-`
5. Update log file paths: `/var/log/openclaw-` → `/var/log/conga-`

### Phase 5: Router
**Files**: `router/package.json`, `router/src/index.js`

1. Package name: `openclaw-slack-router` → `conga-slack-router`
2. Any hardcoded container/network name references

### Phase 6: Documentation
**Files**: `CLAUDE.md`, `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `product-knowledge/*.md`

1. Brand name: "OpenClaw" (project) → "Conga Line", "CruxClaw"/"Crux Claw" → "Conga Line"
2. CLI references: `cruxclaw` → `conga`
3. SSM/Secrets/S3 path examples
4. Docker/systemd/log path examples
5. GitHub repo URL: `crux-claw` → `conga-line`
6. **Preserve** all upstream Open Claw references (Docker image, GitHub links, issue #s)
7. Update README title/tagline

### Phase 7: Spec Files (Historical)
**Files**: `specs/**/*.md`

1. Update path references in spec files so they reflect current naming
2. These are historical records — update references but keep spec dates and structure

### Phase 8: Product Knowledge
**Files**: `product-knowledge/*.md`, `product-knowledge/**/*.md`

1. Update PROJECT_STATUS.md with new naming
2. Update ROADMAP.md, TECH_STACK.md, MISSION.md references

### Phase 9: Misc Config
**Files**: `.claude/settings.local.json`, any other dotfiles

1. Update any project-name references in local config

## Risk Assessment

| Risk | Mitigation |
|------|------------|
| Accidentally rename upstream Open Claw refs | Each phase: grep for `openclaw` after rename, verify remaining hits are upstream-only |
| Terraform state drift | Resource renames are local identifiers only; `terraform state mv` needed for live deployment |
| Go import path breaks | Single `go mod edit -module` + `goimports` pass |
| Missed reference | Final full-repo grep for `openclaw`, `cruxclaw`, `crux.claw`, `CruxClaw` |

## Deployment Notes (Post-Rename)
After the code rename, deploying to an existing environment requires:
1. `terraform state mv` for all renamed resources
2. New SSM parameters at `/conga/` paths (or CLI re-setup)
3. New Secrets Manager entries at `conga-line/` paths
4. S3 objects at new prefixes
5. Host paths updated on EC2 instance (easiest: terminate + re-bootstrap)

## Verification Checklist
- [ ] `cd cli && go build -o conga . && go test ./... && go vet ./...`
- [ ] `cd terraform && terraform validate`
- [ ] `grep -ri 'cruxclaw' --include='*.go' --include='*.tf' --include='*.yaml'` returns 0 hits
- [ ] `grep -ri 'openclaw' --include='*.go' --include='*.tf' --include='*.sh'` returns only upstream refs
- [ ] README install instructions reference `conga-line` repo and `conga` binary
- [ ] CLAUDE.md reflects all new naming
