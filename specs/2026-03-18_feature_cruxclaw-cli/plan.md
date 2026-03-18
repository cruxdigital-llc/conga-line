# Plan: CruxClaw CLI

## Overview

Cross-platform Go CLI replacing shell scripts for non-technical users. Discovers infrastructure via AWS APIs (EC2 tags, SSM Parameter Store), authenticates via AWS SSO, and manages OpenClaw containers via SSM commands.

## Phase 1: Terraform — Infrastructure Discovery Layer

Add SSM Parameter Store resources so the CLI can discover users and config without Terraform state.

### 1a. New file: `terraform/ssm-parameters.tf`

- `aws_ssm_parameter.user_config` — per-user config at `/openclaw/users/{member_id}` containing `{slack_channel, gateway_port}`
- `aws_ssm_parameter.user_iam_mapping` — IAM identity mapping at `/openclaw/users/by-iam/{iam_identity}` → member ID

### 1b. Update `terraform/variables.tf`

- Add `iam_identity` (string) to users variable type — SSO username/email for CLI auto-resolution

### 1c. Verify `terraform/iam.tf`

- Confirm instance role has `ssm:GetParameter` / `ssm:PutParameter` for `/openclaw/*`
- CLI users need: `ssm:GetParameter`, `ssm:StartSession`, `ssm:SendCommand`, `ec2:DescribeInstances`, `secretsmanager:*` (scoped), `sts:GetCallerIdentity`
- Admin CLI users additionally need: `ssm:PutParameter`, `secretsmanager:*` on shared path, `ec2:StopInstances`, `ec2:StartInstances`

### 1d. Verify `terraform/compute.tf`

- Instance already tagged `Name=openclaw-host` — sufficient for discovery

## Phase 2: Go CLI Scaffold

### Project structure

```
cli/
  main.go                          # Entry point
  go.mod
  cmd/
    root.go                        # Cobra root, global flags (--profile, --region, --user, --verbose)
    auth.go                        # auth login, auth status
    secrets.go                     # secrets set/list/delete
    connect.go                     # SSM tunnel + token + device pairing
    refresh.go                     # Restart container with fresh secrets
    status.go                      # Container status
    logs.go                        # Tail container logs
    admin.go                       # admin add-user/remove-user/list-users/cycle-host
    version.go                     # Version info
  internal/
    aws/
      session.go                   # AWS SDK config, SSO credential provider
      ec2.go                       # Instance discovery by tag
      ssm.go                       # RunCommand wrapper (send + poll + return)
      params.go                    # SSM Parameter Store CRUD
      secrets.go                   # Secrets Manager operations
    discovery/
      instance.go                  # Find instance ID by tag, cache
      user.go                      # Resolve user config from Parameter Store
      identity.go                  # Map IAM identity → member ID
    tunnel/
      tunnel.go                    # session-manager-plugin subprocess
    ui/
      prompt.go                    # Interactive prompts (hidden input, confirmations)
      spinner.go                   # Progress indicators
      output.go                    # Formatted output (tables, status)
  scripts/
    add-user.sh.tmpl               # Embedded via //go:embed
    refresh-user.sh.tmpl           # Embedded via //go:embed
```

### Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/aws/aws-sdk-go-v2` + service clients
- `github.com/charmbracelet/huh` — interactive prompts
- `github.com/charmbracelet/lipgloss` — styled output

### Config

`~/.cruxclaw/config.toml` with baked-in defaults:

```toml
region = "us-east-2"
sso_start_url = "https://crux-login.awsapps.com/start/"
sso_account_id = "167595588574"
sso_role_name = "OpenClawUser"
instance_tag = "openclaw-host"
```

## Phase 3: Auth Commands

- `cruxclaw auth login` — SSO OIDC device authorization, open browser, cache credentials
- `cruxclaw auth status` — `sts:GetCallerIdentity`, display identity + resolved OpenClaw user + session expiry
- Shared session init in `internal/aws/session.go` reused by all commands

## Phase 4: Infrastructure Discovery

- `internal/aws/ec2.go` — `DescribeInstances` with tag filter `Name=openclaw-host`
- `internal/aws/params.go` — `GetParameter` / `GetParametersByPath` / `PutParameter`
- `internal/discovery/instance.go` — find + cache instance ID
- `internal/discovery/user.go` — resolve user config from `/openclaw/users/{id}`
- `internal/discovery/identity.go` — `sts:GetCallerIdentity` → extract identity → lookup `/openclaw/users/by-iam/{identity}`

## Phase 5: Secrets Commands

- `cruxclaw secrets set <name>` — hidden input prompt, `CreateSecret` or `PutSecretValue` at `openclaw/{user_id}/{name}`
- `cruxclaw secrets list` — `ListSecrets` filtered by prefix, table output
- `cruxclaw secrets delete <name>` — confirmation prompt, `DeleteSecret`

## Phase 6: SSM RunCommand Wrapper

- `internal/aws/ssm.go` — send command, poll status (3s intervals, 120s timeout), return stdout/stderr
- Reused by: refresh, status, logs, admin add-user, admin remove-user, connect (token fetch + device pairing)

## Phase 7: Status + Logs Commands

- `cruxclaw status` — SSM RunCommand → `systemctl status openclaw-{user_id}` + `docker inspect`, format output
- `cruxclaw logs --lines N` — SSM RunCommand → `docker logs openclaw-{user_id} --tail N`

## Phase 8: Refresh Command

- SSM RunCommand with embedded `refresh-user.sh.tmpl` (templated with user ID)
- Script: fetch fresh secrets, regenerate env file, update systemd ExecStart, daemon-reload, restart

## Phase 9: Connect Command (most complex)

1. Discover instance, resolve user + gateway port
2. Fetch gateway token via SSM RunCommand (read config JSON on instance)
3. Display token (copy to clipboard if `pbcopy`/`xclip` available)
4. Call `ssm:StartSession` API → get session token/URL
5. Spawn `session-manager-plugin` subprocess for port forwarding
6. Poll for device pairing (every 10s, 5min timeout), auto-approve
7. Signal handling: trap SIGINT/SIGTERM, kill plugin on exit

Prerequisite check: verify `session-manager-plugin` on PATH, print install instructions if missing.

## Phase 10: Admin Commands

- `cruxclaw admin add-user <member_id> <slack_channel>` — auto-assign port, prompt for IAM identity, create SSM params, send setup script via SSM
- `cruxclaw admin list-users` — `GetParametersByPath` on `/openclaw/users/` (exclude `by-iam/`), table output
- `cruxclaw admin remove-user <member_id>` — confirmation, SSM stop/disable/remove, delete SSM params, optionally delete secrets
- `cruxclaw admin cycle-host` — confirmation, `StopInstances` → wait stopped → `StartInstances` → wait running + SSM online

## Phase 11: Distribution

- `.goreleaser.yaml` — build targets: darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64
- GitHub Actions workflow triggered on tag push

## Edge Cases

| Scenario | Handling |
|----------|----------|
| SSO session expired | Catch `ExpiredTokenException`, prompt `cruxclaw auth login` |
| Instance stopped/terminated | `DescribeInstances` returns no results → clear error |
| Port 18789 already in use | Detect bind failure, suggest `--local-port` |
| `session-manager-plugin` missing | Check PATH, print platform-specific install instructions |
| User not provisioned | SSM param not found → "Ask admin to run `cruxclaw admin add-user`" |
| IAM identity not mapped | `by-iam/` lookup fails → prompt for `--user`, suggest admin update |
| SSM RunCommand timeout | 120s timeout, show progress, suggest retry |
| cycle-host: instance fails to start | Report error, suggest AWS console |
| cycle-host: SSM agent slow | Poll with backoff up to 5 min, then timeout |

## Verification

1. `terraform validate && terraform plan` — SSM parameters as additions, no destructive changes
2. `cd cli && go build -o cruxclaw .` — compiles without errors
3. `./cruxclaw auth login` → SSO flow completes
4. `./cruxclaw auth status` → shows identity + resolved user
5. `./cruxclaw admin list-users` → returns users from Parameter Store
6. `./cruxclaw secrets list` → shows secrets (auto-resolved user)
7. `./cruxclaw connect` → tunnel established, web UI at `localhost:18789`
8. `./cruxclaw refresh` → container restarts
9. `./cruxclaw admin add-user UTESTUSER C0TESTCHAN` → provisions container
10. Cross-platform build: darwin/arm64 + linux/amd64 both run
