# Requirements: Open-Source Sanitization

## Goal

Make the repository publishable as an early-preview open-source project. Anyone should be able to clone it, configure it for their own AWS account and Slack workspace, and deploy OpenClaw. Speed over completeness — rough edges are acceptable, but the project must be company-agnostic and fully configurable.

## Success Criteria

1. **No leaked environment-specific values**: No real AWS account IDs, Slack member/channel IDs, SSO URLs, usernames, or deployed resource IDs in any committed file
2. **Configurable via gitignored files**: `terraform.tfvars` and `backend.tf` are gitignored; `.example` versions are committed with placeholder values and clear instructions
3. **Your deployment still works**: Terraform plans/applies using your real values in the gitignored files with zero functional changes
4. **Company-agnostic CLI**: No hardcoded org-specific defaults (SSO URL, account ID). Users configure via `~/.cruxclaw/config.toml` or env vars
5. **Configurable Docker image**: The OpenClaw image name is a variable, not hardcoded — users can point to their own ECR, GHCR, or Docker Hub image (upstream `ghcr.io/openclaw/openclaw:latest` requires PR #49514 bugfix to work with Slack)
6. **Usable README**: A new user can clone the repo, read the README, and understand what to configure and in what order

## Non-Goals (for this pass)

- Perfect onboarding experience — rough edges are fine
- Comprehensive contribution guide / LICENSE / templates
- Removing historical spec documents that reference real values (point-in-time records)
- Automated setup wizard or interactive configuration tooling

## Personas

### Architect
- Ensure variable/template plumbing is structurally sound
- `bootstrap.sh` should derive bucket/table names dynamically (matching `data.tf` pattern)
- Template variables flow correctly through `router.tf` → `user-data.sh.tftpl` and `compute.tf` → `user-data-shim.sh.tftpl`
- CLI config struct cleanly supports new `OpenClawImage` field

### Product Manager
- Open-source consumption experience: Can a developer clone this and figure out what to do?
- README should orient the reader: what is this, what do I need, what do I configure, what order
- `terraform.tfvars.example` should be self-documenting with comments
- Error messages should guide users when config is missing (not cryptic empty-string failures)

### QA
- Verify no environment-specific values remain in committed files (grep audit)
- Terraform `plan` succeeds with example-derived values
- CLI compiles with the config changes
- Edge case: what happens if `openclaw_image` is empty? If `users = {}`? If CLI config.toml doesn't exist?
