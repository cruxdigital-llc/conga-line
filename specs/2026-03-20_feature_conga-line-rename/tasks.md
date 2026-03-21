# Implementation Tasks: Conga Line Rename

## Phase 1: CLI Go Code
- [x] 1a. Update `cli/go.mod` module path
- [x] 1b. Update import paths in all 18 `.go` files (`openclaw-template` → `conga-line`)
- [x] 1c. Update CLI binary name, help text, instance tag in `root.go`, `version.go`
- [x] 1d. Update SSM/Secrets path constants in `admin_setup.go`, `admin_provision.go`, `agent.go`
- [x] 1e. Update Docker/systemd/log refs in `status.go`, `logs.go`, `connect.go`, `secrets.go`, `admin_remove.go`, `admin_cycle.go`, `auth.go`
- [x] 1f. Update test files (`ssm_test.go`, `identity_test.go`)
- [x] 1g. Update CLI script templates (`cli/scripts/*.sh.tmpl` — 6 files)
- [x] 1-verify. `go build -o conga . && go test ./... && go vet ./...` — all pass

## Phase 2: GoReleaser + CI
- [x] 2a. Update `cli/.goreleaser.yaml` (project name, binary, ldflags, archive names, repo)

## Phase 3: Terraform
- [x] 3a. Update `variables.tf` (defaults to `conga-line`, config key `image`, secrets paths)
- [x] 3b. Rename resource identifiers in `compute.tf`, `security.tf`, `iam.tf`, `ecr.tf`, `kms.tf`
- [x] 3c. Update SSM paths in `ssm-parameters.tf`
- [x] 3d. Update IAM policy paths in `iam.tf` (secrets, SSM, S3, CloudWatch namespace)
- [x] 3e. Update S3 keys in `behavior.tf`, `router.tf`
- [x] 3f. Update output names in `outputs.tf`
- [x] 3g. Update `security.tf` description
- [x] 3h. Update `secrets.tf` comments
- [x] 3i. Update `.example` files
- [x] 3j. Update `monitoring.tf` (CloudWatch namespace, alarm description)
- [x] 3k. Update `kms.tf` (description)
- [x] 3l. Update `vpc.tf` (comment)

## Phase 4: Bootstrap Templates
- [x] 4a. Update `terraform/user-data.sh.tftpl` (100+ substitutions)
- [x] 4b. Update `terraform/user-data-shim.sh.tftpl`
- [x] 4c. Update `terraform/bootstrap.sh` (PROJECT_NAME default)
- [x] 4d. Update `terraform/populate-secrets.sh` (secrets paths, description)

## Phase 5: Router
- [x] 5a. Update `router/package.json` and `router/src/index.js`

## Phase 6: Documentation
- [x] 6a. Update `CLAUDE.md`
- [x] 6b. Update `README.md`
- [x] 6c. Update `CONTRIBUTING.md` and `SECURITY.md`

## Phase 7: Spec Files
- [x] 7a. Bulk rename in 53 files across 13 `specs/` directories

## Phase 8: Product Knowledge
- [x] 8a. Update `PROJECT_STATUS.md`, `ROADMAP.md`, `standards/security.md`, `observations/observed-standards.md`

## Phase 9: Misc
- [x] 9a. `.claude/settings.local.json` — no changes needed (only upstream domain ref)
- [x] 9b. Final verification grep — zero hits for cruxclaw/openclaw-template/crux-claw in code files
- [x] 9c. Remaining `openclaw` refs verified as upstream-only (openclaw.json, /home/node/.openclaw/, npx openclaw, ghcr.io/openclaw/openclaw, backend.tf)
