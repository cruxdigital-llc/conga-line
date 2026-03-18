# Plan: Open-Source Sanitization

## Approach

Three workstreams executed in order: (1) Terraform genericization, (2) CLI genericization, (3) documentation scrub. Each workstream makes the code company-agnostic while keeping the existing deployment functional via gitignored config files.

## Workstream 1: Terraform

### 1a. Gitignore + example files
- Add `terraform/backend.tf` and `terraform/terraform.tfvars` to `.gitignore`
- Create `terraform/backend.tf.example` with placeholder values and a comment explaining how to use it
- Update `terraform/terraform.tfvars.example` — replace real Slack IDs with `UXXXXXXXXXX`/`CXXXXXXXXXX` placeholders, add `openclaw_image` variable

### 1b. New variable: `openclaw_image`
- Add to `terraform/variables.tf` — no default (required), with description explaining ECR or public registry
- Pass through `terraform/router.tf` templatefile call → `user-data.sh.tftpl`

### 1c. Remove hardcoded user defaults
- `terraform/variables.tf`: Change `users` default from real user map to `{}`

### 1d. Fix template variable plumbing
- `terraform/data.tf`: Add `local.lock_table` derived from `project_name`
- `terraform/outputs.tf`: Use `local.state_bucket` and `local.lock_table` instead of hardcoded strings
- `terraform/router.tf`: Pass `openclaw_image` and `state_bucket` to the `user-data.sh.tftpl` templatefile call
- `terraform/compute.tf`: Pass `state_bucket` to the `user-data-shim.sh.tftpl` templatefile call
- `terraform/user-data-shim.sh.tftpl`: Use `${state_bucket}` instead of hardcoded S3 bucket name
- `terraform/user-data.sh.tftpl`:
  - Replace hardcoded ECR repo (`123456789012.dkr.ecr...`) with `${openclaw_image}` template variable
  - Auto-detect ECR vs non-ECR image for docker login
  - Replace hardcoded S3 bucket in router download commands with `${state_bucket}`
  - Replace hardcoded region fallback with `${aws_region}`

### 1e. Genericize shell scripts
- `terraform/bootstrap.sh`: Derive bucket/table names from project name + dynamically-fetched account ID (matches `data.tf` pattern). Accept project name as `$1` arg, profile/region from env vars with defaults
- `terraform/populate-secrets.sh`: Read profile/region from env vars with defaults

## Workstream 2: CLI

### 2a. Remove org-specific defaults
- `cli/internal/config/config.go`: Empty defaults for `Region`, `SSOStartURL`, `SSOAccountID`, `SSORoleName`. Keep `InstanceTag` default (`openclaw-host`)
- Add `OpenClawImage` field to `Config` struct with `CRUXCLAW_OPENCLAW_IMAGE` env var override
- Add validation in `Load()` — return clear error when required fields are unconfigured

### 2b. Configurable Docker image in templates
- `cli/scripts/add-user.sh.tmpl`: Replace `ghcr.io/openclaw/openclaw:latest` with `{{.OpenClawImage}}`
- `cli/scripts/refresh-user.sh.tmpl`: Replace `ghcr.io/openclaw/openclaw:latest` with `{{.OpenClawImage}}`
- `cli/cmd/admin.go`: Add `OpenClawImage` to template execution struct, sourced from `cfg.OpenClawImage`
- `cli/cmd/refresh.go`: Add `OpenClawImage` to template execution struct

### 2c. Scrub example values from error messages
- `cli/cmd/root.go`: Replace `UEXAMPLE01` → `UXXXXXXXXXX`, `CEXAMPLE01` → `CXXXXXXXXXX` in validation error examples

## Workstream 3: Documentation

### 3a. READMEs
- `README.md` (root): Replace org-specific SSO URL, account ID, username, Slack IDs with placeholders. Reframe SSO setup section as generic "configure your AWS credentials". Add note about Docker image and PR #49514 bugfix
- `cli/README.md`: Same treatment. Add section about `~/.cruxclaw/config.toml` setup with example config

### 3b. CLAUDE.md
- Replace all real values (account ID, Slack IDs, channel IDs, username, service names) with generic placeholders or description patterns

### 3c. ROADMAP.md
- Replace deployed resource IDs (VPC, subnet, SG), Slack channel IDs, S3 bucket names with placeholders
- Replace "Aaron" references with generic "user" language

### 3d. Spec files
- **Leave as-is** — these are historical design documents. They contain point-in-time values that were accurate when the specs were written. Not worth the churn to sanitize.

## File Change Summary

| File | Change |
|------|--------|
| `.gitignore` | Add `terraform/backend.tf`, `terraform/terraform.tfvars` |
| `terraform/variables.tf` | Empty users default, add `openclaw_image` var |
| `terraform/data.tf` | Add `local.lock_table` |
| `terraform/outputs.tf` | Use locals instead of hardcoded strings |
| `terraform/backend.tf.example` | New file — placeholder backend config |
| `terraform/terraform.tfvars.example` | Placeholder values |
| `terraform/user-data-shim.sh.tftpl` | Template var for S3 bucket |
| `terraform/compute.tf` | Pass `state_bucket` to shim templatefile |
| `terraform/user-data.sh.tftpl` | Template vars for image, bucket, region fallback |
| `terraform/router.tf` | Pass `openclaw_image`, `state_bucket` to templatefile |
| `terraform/bootstrap.sh` | Dynamic bucket/table derivation |
| `terraform/populate-secrets.sh` | Env var defaults for profile/region |
| `cli/internal/config/config.go` | Empty defaults, add `OpenClawImage` field |
| `cli/cmd/root.go` | Generic example IDs in error messages |
| `cli/cmd/admin.go` | Pass `OpenClawImage` to template |
| `cli/cmd/refresh.go` | Pass `OpenClawImage` to template |
| `cli/scripts/add-user.sh.tmpl` | Use `{{.OpenClawImage}}` |
| `cli/scripts/refresh-user.sh.tmpl` | Use `{{.OpenClawImage}}` |
| `CLAUDE.md` | Replace all real IDs with generic values |
| `README.md` | Replace real IDs, SSO URL, add Docker image note |
| `cli/README.md` | Replace real IDs, SSO URL, add config.toml docs |
| `product-knowledge/ROADMAP.md` | Replace deployed resource IDs |

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Terraform plan breaks after variable changes | Run `terraform plan` with real tfvars to verify |
| CLI won't compile with config struct changes | Run `go build` to verify |
| Missed hardcoded values | Post-implementation grep audit for account ID, Slack IDs, SSO URL |
| `openclaw_image` empty at deploy time | Terraform validation block — require non-empty |
| New users hit cryptic errors with unconfigured CLI | Add clear error message in `config.Load()` when required fields are empty |
