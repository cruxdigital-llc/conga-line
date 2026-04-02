# Tasks: Conga Line CLI Implementation

## Phase 1: Terraform — Infrastructure Discovery Layer
- [x] **Task 1.1**: Create `terraform/ssm-parameters.tf` — user_config + user_iam_mapping resources
- [x] **Task 1.2**: Update `terraform/variables.tf` — add `iam_identity` field to users type
- [x] **Task 1.3**: Verify `terraform/compute.tf` tags and `terraform/iam.tf` permissions
- [x] **Task 1.4**: `terraform validate` — Success

## Phase 2: Go CLI Scaffold
- [x] **Task 2.1**: Initialize Go module, install dependencies (cobra, aws-sdk-go-v2, toml, x/term)
- [x] **Task 2.2**: Create `cli/main.go` + `cli/cmd/root.go` — root command with persistent flags
- [x] **Task 2.3**: Create `cli/pkg/config/config.go` — config loading (defaults → toml → env → flags)
- [x] **Task 2.4**: Create `cli/cmd/version.go` — version command with ldflags

## Phase 3: AWS Session + Identity
- [x] **Task 3.1**: Create `cli/pkg/aws/session.go` — AWS SDK config with SSO credential provider
- [x] **Task 3.2**: Create `cli/cmd/auth.go` — `auth login` (SSO instructions) + `auth status` (GetCallerIdentity)

## Phase 4: Infrastructure Discovery
- [x] **Task 4.1**: Create `cli/pkg/aws/ec2.go` — DescribeInstances by tag + Stop/Start/WaitForState
- [x] **Task 4.2**: Create `cli/pkg/aws/params.go` — SSM Parameter Store CRUD
- [x] **Task 4.3**: Create `cli/pkg/discovery/instance.go` — FindInstance with sync.Once caching
- [x] **Task 4.4**: Create `cli/pkg/discovery/identity.go` — IAM identity → member ID resolution
- [x] **Task 4.5**: Create `cli/pkg/discovery/user.go` — user config from Parameter Store

## Phase 5: UI Helpers
- [x] **Task 5.1**: Create `cli/pkg/ui/prompt.go` — SecretPrompt, Confirm, TextPrompt
- [x] **Task 5.2**: Create `cli/pkg/ui/spinner.go` — Spinner with status text
- [x] **Task 5.3**: Create `cli/pkg/ui/table.go` — formatted table output

## Phase 6: SSM RunCommand Wrapper
- [x] **Task 6.1**: Create `cli/pkg/aws/ssm.go` — SendCommand + poll + return result
- [x] **Task 6.2**: Create `cli/pkg/aws/secrets.go` — Secrets Manager CRUD

## Phase 7: User Commands
- [x] **Task 7.1**: Create `cli/cmd/secrets.go` — secrets set/list/delete
- [x] **Task 7.2**: Create `cli/cmd/status.go` — container status via SSM
- [x] **Task 7.3**: Create `cli/cmd/logs.go` — container logs via SSM
- [x] **Task 7.4**: Create `cli/cmd/refresh.go` + `cli/scripts/refresh-user.sh.tmpl`

## Phase 8: Connect Command
- [x] **Task 8.1**: Create `cli/pkg/tunnel/tunnel.go` — plugin check, StartTunnel, Stop
- [x] **Task 8.2**: Create `cli/cmd/connect.go` — tunnel + token + device pairing flow

## Phase 9: Admin Commands
- [x] **Task 9.1**: Create `cli/scripts/add-user.sh.tmpl` — embedded add-user script template
- [x] **Task 9.2**: Create `cli/scripts/remove-user.sh.tmpl` — embedded remove-user script template
- [x] **Task 9.3**: Create `cli/cmd/admin.go` — add-user, list-users, remove-user, cycle-host

## Phase 10: Distribution
- [x] **Task 10.1**: Create `cli/.goreleaser.yaml` — build config for 5 targets
- [x] **Task 10.2**: Create `.github/workflows/release.yml` — tag-triggered release

## Phase 11: Build + Smoke Test
- [x] **Task 11.1**: `go build` — compilation successful
- [x] **Task 11.2**: `go vet` + `gofmt` — all clean
- [x] **Task 11.3**: `./conga version` — binary works, all 13 commands registered
