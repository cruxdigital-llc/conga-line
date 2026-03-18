# Spec: CruxClaw CLI

## Overview

Cross-platform Go CLI (`cruxclaw`) enabling non-technical users to manage their OpenClaw deployment via AWS SSO + SSM. Discovers infrastructure through AWS APIs, embeds container management scripts, and handles SSM port forwarding for web UI access.

---

## 1. Terraform Changes

### 1a. `terraform/ssm-parameters.tf` (new file)

```hcl
resource "aws_ssm_parameter" "user_config" {
  for_each = var.users
  name     = "/openclaw/users/${each.key}"
  type     = "String"
  value = jsonencode({
    slack_channel = each.value.slack_channel
    gateway_port  = each.value.gateway_port
  })
  tags = { Project = var.project_name }
}

resource "aws_ssm_parameter" "user_iam_mapping" {
  for_each = var.users
  name     = "/openclaw/users/by-iam/${each.value.iam_identity}"
  type     = "String"
  value    = each.key
  tags     = { Project = var.project_name }
}
```

### 1b. `terraform/variables.tf` — extend users type

```hcl
variable "users" {
  type = map(object({
    slack_channel = string
    gateway_port  = number
    iam_identity  = string  # SSO username/email for CLI auto-resolution
  }))
  default = {
    UEXAMPLE01 = {
      slack_channel = "CEXAMPLE01"
      gateway_port  = 18789
      iam_identity  = "user@example.com"  # placeholder — replace with actual
    }
    UEXAMPLE02 = {
      slack_channel = "CEXAMPLE02"
      gateway_port  = 18790
      iam_identity  = "user2@example.com"  # placeholder — replace with actual
    }
  }
  # existing validations unchanged
}
```

### 1c. `terraform/iam.tf` — no changes expected

Instance role already has `ssm:*` and `secretsmanager:GetSecretValue`. SSM Parameter Store uses the same `ssm:` namespace. CLI users authenticate via SSO with their own credentials, not the instance role.

### 1d. `terraform/compute.tf` — verify tags

Instance already tagged `Name=openclaw-host` via launch template. No changes needed.

---

## 2. CLI Architecture

### 2a. Go Module: `cli/`

```
cli/
  main.go                              # cobra root command init + Execute()
  go.mod                               # module: github.com/cruxdigital-llc/openclaw-template/cli
  cmd/
    root.go                            # root command, persistent flags, AWS session init
    auth.go                            # auth login, auth status
    secrets.go                         # secrets set, secrets list, secrets delete
    connect.go                         # SSM tunnel + gateway token + device pairing
    refresh.go                         # Restart container with fresh secrets
    status.go                          # Container status via SSM
    logs.go                            # Tail container logs via SSM
    admin.go                           # admin add-user, admin remove-user, admin list-users, admin cycle-host
    version.go                         # version command (set via ldflags at build time)
  internal/
    config/
      config.go                        # Load/save ~/.cruxclaw/config.toml, defaults
    aws/
      session.go                       # AWS SDK v2 config, SSO credential provider, error wrapping
      ec2.go                           # DescribeInstances by tag, return instance ID
      ssm.go                           # SendCommand + poll + return stdout/stderr
      params.go                        # GetParameter, GetParametersByPath, PutParameter, DeleteParameter
      secrets.go                       # CreateSecret, PutSecretValue, ListSecrets, DeleteSecret
    discovery/
      instance.go                      # FindInstance: EC2 tag lookup, cache result for session
      user.go                          # ResolveUser: GetParameter /openclaw/users/{id}, parse JSON
      identity.go                      # WhoAmI: GetCallerIdentity → extract identity → lookup by-iam/
    tunnel/
      tunnel.go                        # PluginCheck, StartTunnel, WaitForExit — manages session-manager-plugin
    ui/
      prompt.go                        # SecretPrompt (hidden input), Confirm, Select
      spinner.go                       # Spinner with status text
      table.go                         # Formatted table output
  scripts/
    add-user.sh.tmpl                   # Go template, embedded via //go:embed
    refresh-user.sh.tmpl               # Go template, embedded via //go:embed
    remove-user.sh.tmpl               # Go template, embedded via //go:embed
```

### 2b. Dependencies

| Package | Purpose | Version |
|---------|---------|---------|
| `github.com/spf13/cobra` | CLI framework | latest |
| `github.com/aws/aws-sdk-go-v2` | AWS SDK core | latest |
| `github.com/aws/aws-sdk-go-v2/config` | Config loading, SSO | latest |
| `github.com/aws/aws-sdk-go-v2/service/ec2` | Instance discovery | latest |
| `github.com/aws/aws-sdk-go-v2/service/ssm` | RunCommand, StartSession, Parameters | latest |
| `github.com/aws/aws-sdk-go-v2/service/secretsmanager` | Secrets CRUD | latest |
| `github.com/aws/aws-sdk-go-v2/service/sts` | GetCallerIdentity | latest |
| `github.com/aws/aws-sdk-go-v2/service/ssooidc` | SSO device auth flow | latest |
| `github.com/charmbracelet/huh` | Interactive prompts | latest |
| `github.com/charmbracelet/lipgloss` | Styled output | latest |
| `github.com/BurntSushi/toml` | Config file parsing | latest |

### 2c. Build Configuration

Version injected via ldflags:
```go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)
```

Build: `go build -ldflags "-X main.version=v1.0.0 -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o cruxclaw .`

---

## 3. Config Model

### 3a. `~/.cruxclaw/config.toml`

```toml
# AWS SSO settings
region         = "us-east-2"
sso_start_url  = "https://example-sso.awsapps.com/start/"
sso_account_id = "123456789012"
sso_role_name  = "OpenClawUser"

# Infrastructure discovery
instance_tag   = "openclaw-host"
```

### 3b. `internal/config/config.go`

```go
type Config struct {
    Region       string `toml:"region"`
    SSOStartURL  string `toml:"sso_start_url"`
    SSOAccountID string `toml:"sso_account_id"`
    SSORoleName  string `toml:"sso_role_name"`
    InstanceTag  string `toml:"instance_tag"`
}
```

Defaults baked in as struct literal. Config file loaded from `~/.cruxclaw/config.toml` if exists. CLI flags override all.

### 3c. Resolution Order

1. Baked-in defaults (compile-time)
2. `~/.cruxclaw/config.toml` (user-level)
3. Environment variables: `CRUXCLAW_REGION`, `CRUXCLAW_SSO_START_URL`, etc.
4. CLI flags: `--region`, `--profile`

---

## 4. AWS Session Management

### 4a. `internal/aws/session.go`

**SSO Credential Flow:**

1. Check for existing SSO token cache at `~/.aws/sso/cache/`
2. If valid token exists, use it
3. If expired or missing:
   - If running `auth login`: initiate OIDC device authorization
   - If running any other command: return error "Session expired. Run `cruxclaw auth login`"
4. Create `aws.Config` with SSO credential provider
5. Return typed clients (EC2, SSM, SecretsManager, STS)

**OIDC Device Authorization Flow (`auth login`):**

1. Call `ssooidc:RegisterClient` → get `clientId`, `clientSecret`
2. Call `ssooidc:StartDeviceAuthorization` → get `verificationUri`, `userCode`, `deviceCode`
3. Display: "Open {verificationUri} and enter code: {userCode}"
4. Open browser automatically (`open` on macOS, `xdg-open` on Linux, `start` on Windows)
5. Poll `ssooidc:CreateToken` every `interval` seconds until success
6. Cache the SSO token at `~/.aws/sso/cache/`

**Error Handling:**

| AWS Error | User-Facing Message |
|-----------|-------------------|
| `ExpiredTokenException` | "Session expired. Run `cruxclaw auth login` to re-authenticate." |
| `UnrecognizedClientException` | "SSO client not recognized. Run `cruxclaw auth login` again." |
| `AccessDeniedException` | "Permission denied. Contact your admin to verify your SSO role." |
| `AuthorizationPendingException` | (internal — continue polling during login) |
| `SlowDownException` | (internal — increase poll interval during login) |

---

## 5. Infrastructure Discovery

### 5a. `internal/discovery/instance.go`

```go
func FindInstance(ctx context.Context, ec2Client *ec2.Client, tag string) (string, error)
```

- `DescribeInstances` with filter `tag:Name={tag}`, state `running`
- Return first matching instance ID
- Error if zero or multiple matches
- Cache result in memory for the session (instance ID doesn't change mid-command)

### 5b. `internal/discovery/identity.go`

```go
type ResolvedIdentity struct {
    IAMArn      string  // e.g., arn:aws:sts::123456789012:assumed-role/OpenClawUser/user@example.com
    AccountID   string
    UserID      string
    SessionName string  // e.g., user@example.com (extracted from ARN)
    MemberID    string  // e.g., UEXAMPLE01 (from SSM Parameter lookup)
}

func ResolveIdentity(ctx context.Context, stsClient *sts.Client, ssmClient *ssm.Client) (*ResolvedIdentity, error)
```

1. `sts:GetCallerIdentity` → get ARN
2. Parse ARN to extract role session name (the part after the last `/` in assumed-role ARNs)
3. `ssm:GetParameter` at `/openclaw/users/by-iam/{sessionName}`
4. If found → set `MemberID`
5. If not found → return error with guidance to use `--user` or ask admin

### 5c. `internal/discovery/user.go`

```go
type UserConfig struct {
    MemberID     string
    SlackChannel string `json:"slack_channel"`
    GatewayPort  int    `json:"gateway_port"`
}

func ResolveUser(ctx context.Context, ssmClient *ssm.Client, memberID string) (*UserConfig, error)
```

- `ssm:GetParameter` at `/openclaw/users/{memberID}`
- Parse JSON value → `UserConfig`

---

## 6. SSM RunCommand Wrapper

### 6a. `internal/aws/ssm.go`

```go
type RunCommandResult struct {
    Status string
    Stdout string
    Stderr string
}

func RunCommand(ctx context.Context, client *ssm.Client, instanceID string, script string, timeout time.Duration) (*RunCommandResult, error)
```

1. `ssm:SendCommand` with `AWS-RunShellScript` document, `commands` parameter
2. Poll `ssm:GetCommandInvocation` every 3 seconds
3. Return stdout/stderr on `Success` or `Failed`
4. Return error on timeout (default 120s)
5. Show spinner during poll via `ui.Spinner`

---

## 7. Command Specifications

### 7a. `cruxclaw auth login`

**Flow:**
1. Load config (baked defaults → config file → flags)
2. Run OIDC device authorization flow (see Section 4a)
3. After successful token, call `sts:GetCallerIdentity` to verify
4. Resolve identity to OpenClaw user (if mapping exists)
5. Print: "Logged in as {email}. OpenClaw user: {memberID}. Session expires: {expiry}"

**Flags:** `--region`, `--sso-start-url` (override config)

### 7b. `cruxclaw auth status`

**Flow:**
1. Load AWS session (fail if no cached token)
2. `sts:GetCallerIdentity`
3. Resolve identity → member ID
4. Print: identity, account, member ID, session expiry

### 7c. `cruxclaw secrets set <name>`

**Flow:**
1. Resolve identity → member ID (or use `--user`)
2. Prompt for secret value (hidden input via `huh.NewInput().EchoMode(huh.EchoModePassword)`)
3. Try `secretsmanager:PutSecretValue` at `openclaw/{memberID}/{name}`
4. If `ResourceNotFoundException`, use `secretsmanager:CreateSecret` instead
5. Print confirmation

**Flags:** `--user` (override), `--value` (non-interactive, for scripting)

### 7d. `cruxclaw secrets list`

**Flow:**
1. Resolve identity → member ID
2. `secretsmanager:ListSecrets` with filter `Key=name,Values=openclaw/{memberID}/`
3. Display as table: Name (strip prefix), Last Changed Date

### 7e. `cruxclaw secrets delete <name>`

**Flow:**
1. Resolve identity → member ID
2. Confirm: "Delete secret '{name}' for user {memberID}?"
3. `secretsmanager:DeleteSecret` at `openclaw/{memberID}/{name}`
4. Print confirmation

**Flags:** `--force` (skip confirmation)

### 7f. `cruxclaw connect`

**Flow:**
1. Check `session-manager-plugin` on PATH:
   - macOS: "Install with: brew install --cask session-manager-plugin"
   - Linux: "Install from: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html"
   - Windows: "Download MSI from: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html"
2. Resolve identity → member ID → user config (gateway_port)
3. Find instance by tag
4. Fetch gateway token via SSM RunCommand:
   ```bash
   python3 -c "import json; c=json.load(open('/opt/openclaw/data/{{.MemberID}}/openclaw.json')); print(c.get('gateway',{}).get('auth',{}).get('token','NOT_FOUND'))"
   ```
5. Display token (attempt clipboard copy via `pbcopy`/`xclip`/`clip.exe`)
6. Call `ssm:StartSession` API with parameters:
   ```json
   {
     "DocumentName": "AWS-StartPortForwardingSession",
     "Parameters": {
       "portNumber": ["{{gatewayPort}}"],
       "localPortNumber": ["18789"]
     }
   }
   ```
7. Spawn `session-manager-plugin` subprocess with session response JSON:
   ```
   session-manager-plugin <session-json> <region> StartSession <profile> <session-json> <endpoint>
   ```
8. Print: "Tunnel open. Visit http://localhost:18789"
9. Start device pairing goroutine:
   - Every 10 seconds, SSM RunCommand: `docker exec openclaw-{memberID} npx openclaw devices list 2>&1`
   - If "Pending" found, extract UUID, SSM RunCommand: `docker exec openclaw-{memberID} npx openclaw devices approve {uuid} 2>&1`
   - Print: "Device approved! Refresh your browser."
   - Stop polling after approval or 5 minutes
10. `signal.Notify` on `SIGINT`, `SIGTERM` → kill plugin subprocess
11. `cmd.Wait()` on plugin process — block until user Ctrl+C

**Flags:** `--local-port` (default 18789), `--user` (override), `--no-pairing` (skip device pairing poll)

### 7g. `cruxclaw refresh`

**Flow:**
1. Resolve identity → member ID
2. Find instance by tag
3. Render `refresh-user.sh.tmpl` with `{MemberID, AWSRegion}`
4. SSM RunCommand with rendered script
5. Show spinner during execution
6. Print result

**Embedded template `scripts/refresh-user.sh.tmpl`** — extracted from `scripts/refresh-user.sh` lines 23-26, cleaned up into readable multi-line bash:

```bash
set -euxo pipefail
AWS_REGION="{{.AWSRegion}}"

get_secret() {
  aws secretsmanager get-secret-value \
    --secret-id "$1" --query SecretString --output text --region "$AWS_REGION"
}

MEMBER_ID="{{.MemberID}}"
SLACK_BOT_TOKEN=$(get_secret "openclaw/shared/slack-bot-token")
SLACK_APP_TOKEN=$(get_secret "openclaw/shared/slack-app-token")
SLACK_SIGNING_SECRET=$(get_secret "openclaw/shared/slack-signing-secret")
GOOGLE_CLIENT_ID=$(get_secret "openclaw/shared/google-client-id")
GOOGLE_CLIENT_SECRET=$(get_secret "openclaw/shared/google-client-secret")

cat > /opt/openclaw/config/$MEMBER_ID.env << EOF
SLACK_BOT_TOKEN=$SLACK_BOT_TOKEN
SLACK_SIGNING_SECRET=$SLACK_SIGNING_SECRET
GOOGLE_CLIENT_ID=$GOOGLE_CLIENT_ID
GOOGLE_CLIENT_SECRET=$GOOGLE_CLIENT_SECRET
EOF

USER_SECRETS=$(aws secretsmanager list-secrets \
  --filter Key=name,Values="openclaw/$MEMBER_ID/" \
  --query 'SecretList[].Name' --output text \
  --region "$AWS_REGION" 2>/dev/null || echo "")

for SP in $USER_SECRETS; do
  SV=$(get_secret "$SP")
  SN=$(echo "$SP" | sed "s|openclaw/$MEMBER_ID/||" | tr '[:lower:]-' '[:upper:]_')
  echo "$SN=$SV" >> /opt/openclaw/config/$MEMBER_ID.env
done

chmod 0400 /opt/openclaw/config/$MEMBER_ID.env

# Rebuild -e flags from env file
ENV_FLAGS=""
while IFS="=" read -r key value; do
  [ -n "$key" ] && ENV_FLAGS="$ENV_FLAGS -e $key"
done < /opt/openclaw/config/$MEMBER_ID.env

# Update systemd ExecStart with new env flags
sed -i "s|^ExecStart=.*|ExecStart=/usr/bin/docker run --name openclaw-$MEMBER_ID --network openclaw-$MEMBER_ID -p 127.0.0.1:$(cat /opt/openclaw/users/$MEMBER_ID.port 2>/dev/null || echo 18789):18789 --cap-drop ALL --security-opt no-new-privileges --memory 2g --cpus 1.5 -e NODE_OPTIONS=\"--max-old-space-size=1536\" --pids-limit 256 -v /opt/openclaw/data/$MEMBER_ID:/home/node/.openclaw:rw $ENV_FLAGS ghcr.io/openclaw/openclaw:latest|" /etc/systemd/system/openclaw-$MEMBER_ID.service

systemctl daemon-reload
systemctl restart openclaw-$MEMBER_ID
echo "Secrets refreshed and container restarted for $MEMBER_ID"
```

### 7h. `cruxclaw status`

**Flow:**
1. Resolve identity → member ID
2. Find instance by tag
3. SSM RunCommand:
   ```bash
   echo "=== Service Status ==="
   systemctl is-active openclaw-{{.MemberID}} 2>/dev/null || echo "inactive"
   echo "=== Container Info ==="
   docker inspect --format '{{`{{.State.Status}}`}} | Up since {{`{{.State.StartedAt}}`}} | Restarts: {{`{{.RestartCount}}`}}' openclaw-{{.MemberID}} 2>/dev/null || echo "Container not found"
   echo "=== Resource Usage ==="
   docker stats --no-stream --format 'CPU: {{`{{.CPUPerc}}`}} | Mem: {{`{{.MemUsage}}`}} | PIDs: {{`{{.PIDs}}`}}' openclaw-{{.MemberID}} 2>/dev/null || echo "N/A"
   ```
4. Parse output, display formatted

### 7i. `cruxclaw logs`

**Flow:**
1. Resolve identity → member ID
2. Find instance by tag
3. SSM RunCommand: `docker logs openclaw-{memberID} --tail {lines} 2>&1`
4. Print raw output

**Flags:** `--lines N` (default 50)

### 7j. `cruxclaw admin add-user <member_id> <slack_channel>`

**Flow:**
1. Read all existing user configs via `ssm:GetParametersByPath` on `/openclaw/users/`
2. Auto-assign gateway port: find max existing port + 1 (start at 18789 if none)
3. Prompt for IAM identity (SSO username/email) unless `--iam-identity` provided
4. Create SSM parameters:
   - `/openclaw/users/{memberID}` → `{"slack_channel": "...", "gateway_port": N}`
   - `/openclaw/users/by-iam/{iamIdentity}` → `{memberID}`
5. Find instance by tag
6. Render `add-user.sh.tmpl` with `{MemberID, SlackChannel, AWSRegion, GatewayPort}`
7. SSM RunCommand with rendered script
8. Show spinner, print result
9. Print next steps: "User {memberID} provisioned. They should run: cruxclaw secrets set anthropic-api-key && cruxclaw refresh"

**Flags:** `--gateway-port N`, `--iam-identity`

**Embedded template `scripts/add-user.sh.tmpl`** — extracted from `scripts/add-user.sh` lines 29-153, with template variables:
- `{{.MemberID}}`, `{{.SlackChannel}}`, `{{.AWSRegion}}`, `{{.GatewayPort}}`

### 7k. `cruxclaw admin list-users`

**Flow:**
1. `ssm:GetParametersByPath` on `/openclaw/users/` (non-recursive to avoid `by-iam/`)
2. Parse each parameter: extract member ID from name, parse JSON value
3. Display as table:

```
MEMBER ID     SLACK CHANNEL   GATEWAY PORT
UEXAMPLE01     CEXAMPLE01     18789
UEXAMPLE02   CEXAMPLE02     18790
```

### 7l. `cruxclaw admin remove-user <member_id>`

**Flow:**
1. Confirm: "Remove user {memberID}? This will stop their container and delete config."
2. Find instance by tag
3. SSM RunCommand with embedded `remove-user.sh.tmpl`:
   ```bash
   systemctl stop openclaw-{{.MemberID}} || true
   systemctl disable openclaw-{{.MemberID}} || true
   rm -f /etc/systemd/system/openclaw-{{.MemberID}}.service
   systemctl daemon-reload
   docker network rm openclaw-{{.MemberID}} || true
   echo "User {{.MemberID}} removed from instance"
   ```
4. Delete SSM parameters: `/openclaw/users/{memberID}`, `/openclaw/users/by-iam/*` (find matching)
5. If `--delete-secrets`: `secretsmanager:DeleteSecret` for all `openclaw/{memberID}/*`
6. Print confirmation

**Flags:** `--force` (skip confirmation), `--delete-secrets` (also remove Secrets Manager entries)

### 7m. `cruxclaw admin cycle-host`

**Flow:**
1. Confirm: "This will restart the EC2 instance and ALL user containers. Continue?"
2. Find instance by tag
3. `ec2:StopInstances` → print "Stopping instance {id}..."
4. Poll `ec2:DescribeInstances` for state `stopped` (every 5s, timeout 2 min)
5. `ec2:StartInstances` → print "Starting instance {id}..."
6. Poll `ec2:DescribeInstances` for state `running` (every 5s, timeout 2 min)
7. Poll `ssm:DescribeInstanceInformation` for status `Online` (every 10s, timeout 5 min)
8. Print "Instance {id} is running and SSM is connected."

**Flags:** `--force` (skip confirmation)

---

## 8. Embedded Script Templates

All templates use Go `text/template` syntax with `{{.FieldName}}` placeholders. Embedded via:

```go
//go:embed scripts/add-user.sh.tmpl
var addUserScript string

//go:embed scripts/refresh-user.sh.tmpl
var refreshUserScript string

//go:embed scripts/remove-user.sh.tmpl
var removeUserScript string
```

Template data structs:

```go
type AddUserData struct {
    MemberID     string
    SlackChannel string
    AWSRegion    string
    GatewayPort  int
}

type RefreshUserData struct {
    MemberID  string
    AWSRegion string
}

type RemoveUserData struct {
    MemberID string
}
```

---

## 9. Tunnel Management

### 9a. `internal/tunnel/tunnel.go`

```go
func CheckPlugin() error           // Verify session-manager-plugin on PATH
func InstallHint() string           // Platform-specific install instructions

type Tunnel struct {
    cmd        *exec.Cmd
    sessionID  string
}

func StartTunnel(ctx context.Context, ssmClient *ssm.Client, instanceID string, remotePort int, localPort int) (*Tunnel, error)
func (t *Tunnel) Wait() error       // Block until process exits
func (t *Tunnel) Stop() error       // Send SIGTERM/kill to plugin
```

**StartTunnel implementation:**

1. Call `ssm:StartSession` API:
   ```go
   input := &ssm.StartSessionInput{
       Target:       &instanceID,
       DocumentName: aws.String("AWS-StartPortForwardingSession"),
       Parameters: map[string][]string{
           "portNumber":      {strconv.Itoa(remotePort)},
           "localPortNumber": {strconv.Itoa(localPort)},
       },
   }
   ```
2. Marshal response to JSON
3. Build `session-manager-plugin` command:
   ```
   session-manager-plugin <response-json> <region> StartSession <profile> <request-json> <ssm-endpoint>
   ```
4. `exec.Command` with stdout/stderr piped
5. `cmd.Start()`
6. Return `*Tunnel`

---

## 10. Distribution

### `.goreleaser.yaml`

```yaml
version: 2
project_name: cruxclaw

builds:
  - main: ./cli
    binary: cruxclaw
    env:
      - CGO_ENABLED=0
    goos:
      - darwin
      - linux
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}

archives:
  - format: tar.gz
    format_overrides:
      - goos: windows
        format: zip

checksum:
  name_template: checksums.txt

release:
  github:
    owner: cruxdigital-llc
    name: openclaw-template
```

### GitHub Actions: `.github/workflows/release.yml`

Triggered on tag push (`v*`). Runs GoReleaser to build and publish to GitHub Releases.

---

## 11. Edge Cases & Error Handling

| Scenario | Detection | User Message |
|----------|-----------|-------------|
| SSO session expired | `ExpiredTokenException` from any AWS call | "Session expired. Run `cruxclaw auth login`." |
| Instance not found | `DescribeInstances` returns 0 results | "No OpenClaw instance found (tag: openclaw-host). Is the infrastructure deployed?" |
| Instance stopped | `DescribeInstances` returns instance in `stopped` state | "Instance is stopped. Run `cruxclaw admin cycle-host` or start it via AWS console." |
| User not provisioned | `ssm:GetParameter` returns `ParameterNotFound` | "User {id} not found. Ask admin to run `cruxclaw admin add-user`." |
| IAM identity not mapped | `by-iam/` lookup returns `ParameterNotFound` | "Your IAM identity is not mapped to an OpenClaw user. Use `--user <member_id>` or ask admin." |
| `session-manager-plugin` missing | `exec.LookPath` fails | Platform-specific install instructions |
| Port already in use | Plugin exits with non-zero immediately | "Port 18789 already in use. Use `--local-port <port>` or close the existing tunnel." |
| SSM RunCommand timeout | Poll exceeds 120s | "Command timed out after 2 minutes. The instance may be under heavy load." |
| Gateway token not found | SSM RunCommand returns "NOT_FOUND" | "Gateway token not found. Container may not have started yet. Try `cruxclaw status`." |
| Multiple instances with same tag | `DescribeInstances` returns >1 | "Multiple instances found with tag openclaw-host. Use `--instance-id` to specify." |

---

## No Changes Needed

| File | Reason |
|------|--------|
| `terraform/security.tf` | CLI uses SSM (existing outbound HTTPS), no new ingress |
| `terraform/monitoring.tf` | No new metrics or alarms needed |
| `terraform/vpc.tf` | No network changes |
| `terraform/nat.tf` | No NAT changes |
| `terraform/kms.tf` | No encryption changes |
| `terraform/ecr.tf` | CLI doesn't interact with ECR |
| `terraform/router.tf` | Router unchanged |
| `scripts/*.sh` | Shell scripts remain as-is for power users |
