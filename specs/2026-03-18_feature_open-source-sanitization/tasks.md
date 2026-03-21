# Implementation Tasks: Open-Source Sanitization

## Workstream 1: Terraform

- [x] **T1**: `.gitignore` — add `terraform/backend.tf` and `terraform/terraform.tfvars`
- [x] **T2**: `terraform/backend.tf.example` — create new file with placeholder values
- [x] **T3**: `terraform/terraform.tfvars.example` — replace real IDs with placeholders, add `conga_image`
- [x] **T4**: `terraform/variables.tf` — empty `users` default, add `conga_image` variable with validation
- [x] **T5**: `terraform/data.tf` — add `local.lock_table`
- [x] **T6**: `terraform/outputs.tf` — use `local.state_bucket` and `local.lock_table`
- [x] **T7**: `terraform/router.tf` — pass `conga_image` and `state_bucket` to templatefile
- [x] **T8**: `terraform/compute.tf` — pass `state_bucket` to shim templatefile
- [x] **T9**: `terraform/user-data-shim.sh.tftpl` — use `${state_bucket}` template var
- [x] **T10**: `terraform/user-data.sh.tftpl` — configurable image + ECR auto-detect, S3 bucket var, region fallback var
- [x] **T11**: `terraform/bootstrap.sh` — dynamic bucket/table derivation from project name + account ID
- [x] **T12**: `terraform/populate-secrets.sh` — env var defaults for profile/region
- [x] **T13**: `git rm --cached terraform/backend.tf` (was tracked, terraform.tfvars was not)

## Workstream 2: CLI

- [x] **T14**: `cli/internal/config/config.go` — empty defaults, add `Conga LineImage` field, add `RequiredFieldsMissing()`, add `Save()`, add env var override
- [x] **T15**: `cli/cmd/init.go` — new `conga init` command with interactive prompts
- [x] **T16**: `cli/cmd/root.go` — auto-trigger init when config missing, scrub example IDs
- [x] **T17**: `cli/scripts/add-user.sh.tmpl` — replace hardcoded image with `{{.Conga LineImage}}`
- [x] **T18**: `cli/scripts/refresh-user.sh.tmpl` — replace hardcoded image with `{{.Conga LineImage}}`
- [x] **T19**: `cli/cmd/admin.go` — add `Conga LineImage` to template struct
- [x] **T20**: `cli/cmd/refresh.go` — add `Conga LineImage` to template struct
- [x] **T21**: Verify CLI compiles: `cd cli && go build -o conga .` — PASS

## Workstream 3: Documentation

- [x] **T22**: `README.md` — rewrite as consolidated project README
- [x] **T23**: Delete `cli/README.md`
- [x] **T24**: `CLAUDE.md` — replace all real IDs with generic placeholders
- [x] **T25**: `product-knowledge/ROADMAP.md` — replace deployed resource IDs and real Slack IDs

## Verification

- [x] **T26**: Grep audit — CLEAN. No matches for `123456789012`, `UEXAMPLE01`, `UEXAMPLE02`, `CEXAMPLE01`, `CEXAMPLE02`, `example-sso.awsapps.com`, `exampleuser` in committed files (excluding specs). Only hits on disk: `terraform/backend.tf` (gitignored) and `.claude/settings.local.json` (untracked).
