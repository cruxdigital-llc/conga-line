# Specification: Behavior Management

## Overview

This feature adds a behavior file management pipeline to the OpenClaw deployment. Version-controlled markdown files in the repo define agent identity and guidelines, get uploaded to S3 via Terraform, and are automatically composed and deployed to agent workspaces on every container start.

## 1. Repo Structure

```
behavior/
  base/
    SOUL.md
    AGENTS.md
  user/
    SOUL.md
    USER.md.tmpl
  team/
    SOUL.md
    USER.md.tmpl
  overrides/
    .gitkeep
```

The `overrides/` directory is empty by default. Per-agent overrides are optional and may be committed or gitignored depending on sensitivity.

### 1.1 Template Variables

Templates use `{{.VarName}}` syntax, rendered by `sed` at deployment time.

| Variable | Available In | Source |
|----------|-------------|--------|
| `{{.AgentName}}` | `user/USER.md.tmpl`, `team/USER.md.tmpl` | SSM agent parameter name |
| `{{.SlackChannel}}` | `team/USER.md.tmpl` | SSM agent parameter `slack_channel` |
| `{{.SlackMemberID}}` | `user/USER.md.tmpl` | SSM agent parameter `slack_member_id` |

### 1.2 Composition Rules

For each target file (SOUL.md, AGENTS.md, USER.md):

1. **Override check**: If `overrides/<agent_name>/<file>` exists in the S3-synced staging area, use it verbatim.
2. **Concatenation**: Otherwise, concatenate `base/<file>` + newline + `<type>/<file>` (if the type-specific file exists). If only the base file exists, use it alone.
3. **Template rendering**: For `.tmpl` files, substitute template variables via `sed`, then write as the target filename (without `.tmpl` suffix).

**MEMORY.md is never written, read, or touched.** The deploy helper has no code path that references it.

## 2. Terraform Resources

### 2.1 New file: `terraform/behavior.tf`

```hcl
locals {
  behavior_files = fileset("${path.module}/../behavior", "**/*")
}

resource "aws_s3_object" "behavior" {
  for_each = local.behavior_files
  bucket   = local.state_bucket
  key      = "openclaw/behavior/${each.value}"
  content  = file("${path.module}/../behavior/${each.value}")
  etag     = md5(file("${path.module}/../behavior/${each.value}"))
}
```

This uses the same pattern as `router.tf` — individual S3 objects with `etag` for change detection.

### 2.2 IAM policy update: `terraform/iam.tf`

Add to the `aws_iam_role_policy.s3_read` resource's `Resource` array:

```hcl
"arn:aws:s3:::${local.state_bucket}/openclaw/behavior/*"
```

This allows the EC2 instance to read behavior files from S3 (same permission level as router and bootstrap artifacts).

## 3. Deploy Helper Script

### 3.1 File: `cli/scripts/deploy-behavior.sh.tmpl`

Installed to `/opt/openclaw/bin/deploy-behavior.sh` on the host during bootstrap.

**Signature**: `deploy-behavior.sh <agent_name> <agent_type>`

Where `agent_type` is `user` or `team`.

**Preconditions**:
- `/opt/openclaw/behavior/` staging area is populated (caller's responsibility)
- `/opt/openclaw/data/<agent_name>/workspace/` directory exists

**Behavior**:

```bash
#!/bin/bash
set -euo pipefail

AGENT_NAME="$1"
AGENT_TYPE="$2"
STAGING="/opt/openclaw/behavior"
WORKSPACE="/opt/openclaw/data/$AGENT_NAME/workspace"

# Compose SOUL.md
if [ -f "$STAGING/overrides/$AGENT_NAME/SOUL.md" ]; then
  cp "$STAGING/overrides/$AGENT_NAME/SOUL.md" "$WORKSPACE/SOUL.md"
elif [ -f "$STAGING/base/SOUL.md" ]; then
  cp "$STAGING/base/SOUL.md" "$WORKSPACE/SOUL.md"
  if [ -f "$STAGING/$AGENT_TYPE/SOUL.md" ]; then
    echo "" >> "$WORKSPACE/SOUL.md"
    cat "$STAGING/$AGENT_TYPE/SOUL.md" >> "$WORKSPACE/SOUL.md"
  fi
fi

# Compose AGENTS.md
if [ -f "$STAGING/overrides/$AGENT_NAME/AGENTS.md" ]; then
  cp "$STAGING/overrides/$AGENT_NAME/AGENTS.md" "$WORKSPACE/AGENTS.md"
elif [ -f "$STAGING/base/AGENTS.md" ]; then
  cp "$STAGING/base/AGENTS.md" "$WORKSPACE/AGENTS.md"
  if [ -f "$STAGING/$AGENT_TYPE/AGENTS.md" ]; then
    echo "" >> "$WORKSPACE/AGENTS.md"
    cat "$STAGING/$AGENT_TYPE/AGENTS.md" >> "$WORKSPACE/AGENTS.md"
  fi
fi

# Compose USER.md
if [ -f "$STAGING/overrides/$AGENT_NAME/USER.md" ]; then
  cp "$STAGING/overrides/$AGENT_NAME/USER.md" "$WORKSPACE/USER.md"
elif [ -f "$STAGING/$AGENT_TYPE/USER.md.tmpl" ]; then
  sed -e "s/{{.AgentName}}/$AGENT_NAME/g" \
      "$STAGING/$AGENT_TYPE/USER.md.tmpl" > "$WORKSPACE/USER.md"
fi

# Fix ownership — container runs as uid 1000
chown 1000:1000 "$WORKSPACE/SOUL.md" "$WORKSPACE/AGENTS.md" "$WORKSPACE/USER.md" 2>/dev/null || true

echo "Behavior files deployed for $AGENT_NAME ($AGENT_TYPE)"
```

**Note on SlackChannel/SlackMemberID substitution**: The deploy helper receives only agent name and type. For USER.md templates that reference `{{.SlackChannel}}` or `{{.SlackMemberID}}`, the provisioning scripts (add-user.sh.tmpl, add-team.sh.tmpl) already have these values available. The ExecStartPre path needs them too — they can be read from the SSM agent parameter JSON at `/openclaw/agents/<name>`. To avoid adding an AWS CLI call to every container start, store the Slack identifier alongside the type file:

- `/opt/openclaw/config/<agent>.type` — contains "user" or "team"
- `/opt/openclaw/config/<agent>.slack-id` — contains the Slack member ID or channel ID

The deploy helper reads `<agent>.slack-id` and substitutes both `{{.SlackChannel}}` and `{{.SlackMemberID}}` (only one will match per template, the other is a no-op sed).

Updated sed line:
```bash
SLACK_ID=$(cat "/opt/openclaw/config/$AGENT_NAME.slack-id" 2>/dev/null || echo "")
sed -e "s/{{.AgentName}}/$AGENT_NAME/g" \
    -e "s/{{.SlackChannel}}/$SLACK_ID/g" \
    -e "s/{{.SlackMemberID}}/$SLACK_ID/g" \
    "$STAGING/$AGENT_TYPE/USER.md.tmpl" > "$WORKSPACE/USER.md"
```

## 4. Bootstrap Changes (`terraform/user-data.sh.tftpl`)

### 4.1 S3 sync (after router download, ~line 131)

```bash
# Download behavior files from S3
aws s3 sync s3://${state_bucket}/openclaw/behavior/ /opt/openclaw/behavior/ --region "$AWS_REGION"
```

### 4.2 Install deploy helper (after bin directory setup)

```bash
cat > /opt/openclaw/bin/deploy-behavior.sh << 'DEPLOY_BEHAVIOR'
<contents of deploy-behavior.sh>
DEPLOY_BEHAVIOR
chmod +x /opt/openclaw/bin/deploy-behavior.sh
```

### 4.3 Update `setup_agent_common()` signature

Current: `setup_agent_common "$AGENT_NAME" "$GATEWAY_PORT"`
New: `setup_agent_common "$AGENT_NAME" "$GATEWAY_PORT" "$AGENT_TYPE"`

Add to `setup_agent_common()`, after the workspace mkdir block (line ~362):

```bash
local AGENT_TYPE="$3"

# Store agent type and Slack ID for ExecStartPre
echo "$AGENT_TYPE" > /opt/openclaw/config/$AGENT_NAME.type
```

And the deploy call:
```bash
/opt/openclaw/bin/deploy-behavior.sh "$AGENT_NAME" "$AGENT_TYPE"
```

### 4.4 Store Slack ID during agent setup

In `setup_user_agent()`, after calling `setup_agent_common`:
```bash
echo "$SLACK_MEMBER_ID" > /opt/openclaw/config/$AGENT_NAME.slack-id
```

In `setup_team_agent()`:
```bash
echo "$SLACK_CHANNEL" > /opt/openclaw/config/$AGENT_NAME.slack-id
```

Note: These writes must happen **before** the `setup_agent_common` call (which calls deploy-behavior.sh), or be moved into `setup_agent_common` using the SSM parameter data that's already parsed. The simplest approach: write the slack-id and type files in the type-specific functions, before calling `setup_agent_common`, and have `setup_agent_common` read them.

### 4.5 Systemd ExecStartPre

Add to the systemd unit template in `setup_agent_common()`, before the existing `ExecStartPre`:

```
ExecStartPre=/bin/bash -c 'aws s3 sync s3://${state_bucket}/openclaw/behavior/ /opt/openclaw/behavior/ --region ${aws_region} 2>/dev/null || true; /opt/openclaw/bin/deploy-behavior.sh %N $(cat /opt/openclaw/config/%N.type 2>/dev/null || echo user)'
```

Wait — `%N` is the systemd unit name (e.g. `openclaw-myagent`), but we need the agent name (e.g. `myagent`). Use a variable substitution:

```
ExecStartPre=/bin/bash -c 'AGENT=${state_bucket:+$AGENT_NAME}; aws s3 sync s3://${state_bucket}/openclaw/behavior/ /opt/openclaw/behavior/ --region ${aws_region} 2>/dev/null || true; /opt/openclaw/bin/deploy-behavior.sh $AGENT_NAME $(cat /opt/openclaw/config/$AGENT_NAME.type 2>/dev/null || echo user)'
```

Since the systemd unit is generated with `$AGENT_NAME` already expanded in the heredoc, this is straightforward — `$AGENT_NAME` becomes a literal string in the unit file:

```
ExecStartPre=/bin/bash -c 'aws s3 sync s3://${state_bucket}/openclaw/behavior/ /opt/openclaw/behavior/ --region ${aws_region} 2>/dev/null || true; /opt/openclaw/bin/deploy-behavior.sh $AGENT_NAME $(cat /opt/openclaw/config/$AGENT_NAME.type 2>/dev/null || echo user)'
```

In the generated unit file, `$AGENT_NAME`, `${state_bucket}`, and `${aws_region}` are all expanded at generation time (they're shell variables in the bootstrap heredoc). The resulting unit file contains literal values like:

```
ExecStartPre=/bin/bash -c 'aws s3 sync s3://openclaw-terraform-state-123456/openclaw/behavior/ /opt/openclaw/behavior/ --region us-east-1 2>/dev/null || true; /opt/openclaw/bin/deploy-behavior.sh myagent $(cat /opt/openclaw/config/myagent.type 2>/dev/null || echo user)'
```

**Error handling**: `2>/dev/null || true` on the S3 sync means that if S3 is unreachable (network blip, IAM issue), the container still starts with whatever behavior files were last synced. Stale behavior is better than failing to start.

### 4.6 Update callers

In `setup_user_agent()`, change:
```bash
setup_agent_common "$AGENT_NAME" "$GATEWAY_PORT"
```
to:
```bash
echo "$SLACK_MEMBER_ID" > /opt/openclaw/config/$AGENT_NAME.slack-id
setup_agent_common "$AGENT_NAME" "$GATEWAY_PORT" "user"
```

In `setup_team_agent()`, change:
```bash
setup_agent_common "$AGENT_NAME" "$GATEWAY_PORT"
```
to:
```bash
echo "$SLACK_CHANNEL" > /opt/openclaw/config/$AGENT_NAME.slack-id
setup_agent_common "$AGENT_NAME" "$GATEWAY_PORT" "team"
```

## 5. CLI Provisioning Script Changes

### 5.1 `cli/scripts/add-user.sh.tmpl`

Add after workspace mkdir block (line ~104), before container start:

```bash
# Store agent metadata for behavior deployment
echo "user" > /opt/openclaw/config/$AGENT_NAME.type
echo "$SLACK_MEMBER_ID" > /opt/openclaw/config/$AGENT_NAME.slack-id

# Sync and deploy behavior files
aws s3 sync "s3://{{.StateBucket}}/openclaw/behavior/" /opt/openclaw/behavior/ --region "$AWS_REGION" 2>/dev/null || true
/opt/openclaw/bin/deploy-behavior.sh "$AGENT_NAME" "user"
```

**New template variable**: `{{.StateBucket}}` must be passed when rendering. Resolved in the CLI from SSM parameter `/openclaw/config/state-bucket`.

### 5.2 `cli/scripts/add-team.sh.tmpl`

Same pattern, with `"team"` and `$SLACK_CHANNEL`:

```bash
echo "team" > /opt/openclaw/config/$AGENT_NAME.type
echo "$SLACK_CHANNEL" > /opt/openclaw/config/$AGENT_NAME.slack-id

aws s3 sync "s3://{{.StateBucket}}/openclaw/behavior/" /opt/openclaw/behavior/ --region "$AWS_REGION" 2>/dev/null || true
/opt/openclaw/bin/deploy-behavior.sh "$AGENT_NAME" "team"
```

### 5.3 CLI template rendering updates

In `cli/cmd/admin_provision.go`, update the template data structs:

For `adminAddUserRun`:
```go
err = tmpl.Execute(&buf, struct {
    AgentName     string
    SlackMemberID string
    AWSRegion     string
    GatewayPort   int
    StateBucket   string   // NEW
}{
    AgentName:     agentName,
    SlackMemberID: slackMemberID,
    AWSRegion:     resolvedRegion,
    GatewayPort:   gatewayPort,
    StateBucket:   stateBucket,  // NEW
})
```

`stateBucket` is resolved by reading SSM parameter `/openclaw/config/state-bucket`.

For `adminAddTeamRun`, same addition of `StateBucket`.

## 6. State Bucket SSM Parameter

### 6.1 Store during `admin setup`

Add to the setup flow: after the setup manifest is processed, write the state bucket name to SSM:

```
/openclaw/config/state-bucket = <project_name>-terraform-state-<account_id>
```

The account ID is available from `sts:GetCallerIdentity` (already used by the CLI). The project name comes from the setup manifest or is hardcoded as the SSM namespace prefix.

### 6.2 Read in provisioning commands

Add a helper in the CLI (e.g. in `cli/internal/aws/params.go` or similar):

```go
func GetStateBucket(ctx context.Context, ssmClient SSMClient) (string, error) {
    return GetParameter(ctx, ssmClient, "/openclaw/config/state-bucket")
}
```

Call this in `adminAddUserRun` and `adminAddTeamRun` before template rendering.

## 7. CLI Command: `admin refresh-all`

### 7.1 File: `cli/cmd/admin_refresh_all.go`

```go
func adminRefreshAllRun(cmd *cobra.Command, args []string) error {
    ctx, cancel := commandContext()
    defer cancel()
    if err := ensureClients(ctx); err != nil {
        return err
    }

    agents, err := discovery.ListAgents(ctx, clients.SSM)
    if err != nil {
        return err
    }
    if len(agents) == 0 {
        fmt.Println("No agents found.")
        return nil
    }

    instanceID, err := findInstance(ctx)
    if err != nil {
        return err
    }

    tmpl, err := template.New("refresh-all").Parse(scripts.RefreshAllScript)
    if err != nil {
        return fmt.Errorf("failed to parse refresh-all template: %w", err)
    }

    var buf bytes.Buffer
    err = tmpl.Execute(&buf, struct {
        Agents    []discovery.AgentConfig
        AWSRegion string
    }{
        Agents:    agents,
        AWSRegion: resolvedRegion,
    })
    if err != nil {
        return fmt.Errorf("failed to render refresh-all script: %w", err)
    }

    spin := ui.NewSpinner("Refreshing all agents...")
    result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 300*time.Second)
    spin.Stop()
    if err != nil {
        return err
    }

    if result.Status != "Success" {
        fmt.Fprintf(os.Stderr, "Output:\n%s\n%s\n", result.Stdout, result.Stderr)
        return fmt.Errorf("refresh-all failed on instance")
    }

    fmt.Printf("All %d agents refreshed.\n", len(agents))
    return nil
}
```

### 7.2 File: `cli/scripts/refresh-all.sh.tmpl`

```bash
set -euo pipefail

{{range .Agents}}
echo "Restarting openclaw-{{.Name}}..."
systemctl restart openclaw-{{.Name}}
sleep 2
docker network connect "openclaw-{{.Name}}" openclaw-router 2>/dev/null || true
{{end}}

echo "All agents restarted"
```

The systemd `ExecStartPre` handles S3 sync and behavior composition automatically on each restart.

### 7.3 Registration in `cli/cmd/admin.go`

Add to the `init()` function:

```go
refreshAllCmd := &cobra.Command{
    Use:   "refresh-all",
    Short: "Restart all agent containers (picks up latest behavior, config, secrets)",
    RunE:  adminRefreshAllRun,
}
refreshAllCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

adminCmd.AddCommand(setupCmd, addUserCmd, addTeamCmd, listAgentsCmd, removeAgentCmd, cycleHostCmd, refreshAllCmd)
```

### 7.4 Embed updates in `cli/scripts/embed.go`

```go
//go:embed deploy-behavior.sh.tmpl
var DeployBehaviorScript string

//go:embed refresh-all.sh.tmpl
var RefreshAllScript string
```

## 8. Edge Cases & Error Handling

| Scenario | Behavior |
|----------|----------|
| S3 sync fails during ExecStartPre | Suppressed (`|| true`). Container starts with last-synced behavior files. |
| No behavior files in S3 (first deploy before `terraform apply`) | Deploy helper finds no source files, writes nothing. Agent starts with OpenClaw defaults. |
| Agent workspace doesn't exist yet | `setup_agent_common` creates it before calling deploy helper. |
| Override directory exists but is empty | No override files found, falls through to concatenation. |
| Template variable not available (e.g. missing `.slack-id` file) | `sed` substitution is a no-op for unmatched patterns. Template literal `{{.SlackChannel}}` remains in output. Acceptable — better than failing. |
| MEMORY.md exists in workspace | Never referenced by deploy helper. Untouched. |
| Concurrent `refresh-all` while provisioning | `systemctl restart` is idempotent. S3 sync uses `--delete` implicitly via `sync`. No race condition on file writes since agents restart sequentially. |
| `behavior/overrides/<agent>/` committed for wrong agent name | Override has no effect — deploy helper checks for exact name match. |

## 9. Security Considerations

- **No secrets in behavior files**: Behavior files contain only markdown text (identity, guidelines, philosophy). No API keys, tokens, or credentials. Enforced by code review — no automated check needed.
- **S3 read-only from host**: The IAM policy grants only `s3:GetObject` — the host cannot modify behavior files in S3.
- **File ownership**: Behavior files are owned by uid 1000 (node user) so the container can read them. They are not executable.
- **ExecStartPre runs as root**: The systemd unit runs ExecStartPre as root (before dropping to the container user). This is necessary for `chown 1000:1000` and consistent with how openclaw.json is already deployed.
- **No new network access**: S3 access uses the existing VPC endpoint or HTTPS egress — no new network paths.

## 10. File Manifest

| File | Action | Description |
|------|--------|-------------|
| `behavior/base/SOUL.md` | Create | Shared identity, philosophy, boundaries |
| `behavior/base/AGENTS.md` | Create | Shared session guidelines, red lines |
| `behavior/user/SOUL.md` | Create | DM-only behavioral additions |
| `behavior/user/USER.md.tmpl` | Create | Per-user USER.md template |
| `behavior/team/SOUL.md` | Create | Team channel behavioral additions |
| `behavior/team/USER.md.tmpl` | Create | Per-team USER.md template |
| `behavior/overrides/.gitkeep` | Create | Placeholder for per-agent overrides |
| `terraform/behavior.tf` | Create | S3 upload resources for behavior files |
| `terraform/iam.tf` | Modify | Add `openclaw/behavior/*` to S3 read policy |
| `terraform/user-data.sh.tftpl` | Modify | S3 sync, helper install, ExecStartPre, setup_agent_common signature |
| `cli/scripts/deploy-behavior.sh.tmpl` | Create | Host-side composition helper script |
| `cli/scripts/refresh-all.sh.tmpl` | Create | SSM RunCommand script to restart all agents |
| `cli/scripts/embed.go` | Modify | Embed new script templates |
| `cli/scripts/add-user.sh.tmpl` | Modify | Add behavior sync + deploy after workspace setup |
| `cli/scripts/add-team.sh.tmpl` | Modify | Add behavior sync + deploy after workspace setup |
| `cli/cmd/admin_refresh_all.go` | Create | `refresh-all` command implementation |
| `cli/cmd/admin_provision.go` | Modify | Add `StateBucket` to template data structs |
| `cli/cmd/admin.go` | Modify | Register `refresh-all` subcommand |
