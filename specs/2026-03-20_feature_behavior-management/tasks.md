# Implementation Tasks: Behavior Management

## Task 1: Create `behavior/` directory with content files
- [x] `behavior/base/SOUL.md` — shared identity and philosophy
- [x] `behavior/base/AGENTS.md` — shared session guidelines
- [x] `behavior/user/SOUL.md` — individual agent additions
- [x] `behavior/user/USER.md.tmpl` — per-user template
- [x] `behavior/team/SOUL.md` — team channel additions
- [x] `behavior/team/USER.md.tmpl` — per-team template
- [x] `behavior/overrides/.gitkeep` — placeholder

## Task 2: Terraform — S3 upload + IAM
- [x] Create `terraform/behavior.tf` — S3 object resources via `fileset()`
- [x] Modify `terraform/iam.tf` — add `openclaw/behavior/*` to S3 read policy + s3:ListBucket for sync
- [x] Add `state-bucket` SSM parameter in `terraform/ssm-parameters.tf`

## Task 3: Deploy helper script
- [x] Create `cli/scripts/deploy-behavior.sh.tmpl` — composition logic
- [x] Update `cli/scripts/embed.go` — embed new templates

## Task 4: Bootstrap integration (`terraform/user-data.sh.tftpl`)
- [x] Add S3 sync for behavior files after router download
- [x] Install deploy-behavior.sh helper during bootstrap
- [x] Write `.slack-id` files before `setup_agent_common` in user/team functions
- [x] Update `setup_agent_common()` signature to accept `AGENT_TYPE`
- [x] Add behavior deploy call in `setup_agent_common()`
- [x] Add `ExecStartPre` for S3 sync + behavior deploy to systemd unit template

## Task 5: CLI provisioning script updates
- [x] Modify `cli/scripts/add-user.sh.tmpl` — add type/slack-id files, S3 sync, deploy call
- [x] Modify `cli/scripts/add-team.sh.tmpl` — same
- [x] Update template data structs in `cli/cmd/admin_provision.go` — add `StateBucket`

## Task 6: CLI `admin refresh-all` command
- [x] Create `cli/scripts/refresh-all.sh.tmpl` — restart all agents script
- [x] Update `cli/scripts/embed.go` — embed refresh-all template (done in Task 3)
- [x] Create `cli/cmd/admin_refresh_all.go` — command implementation
- [x] Modify `cli/cmd/admin.go` — register subcommand

## Task 7: Build verification
- [x] `go build` CLI compiles without errors
- [x] `terraform validate` passes
