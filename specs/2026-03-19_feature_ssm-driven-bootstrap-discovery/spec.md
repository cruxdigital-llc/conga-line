# Spec: SSM-Driven Bootstrap Discovery

## SSM Parameter Schema

### Single agent namespace

All agents live under `/openclaw/agents/<name>`:

```
/openclaw/agents/myagent   → {"type":"user","member_id":"UEXAMPLE01","gateway_port":18789,"iam_identity":"exampleuser"}
/openclaw/agents/zach    → {"type":"user","member_id":"UEXAMPLE02","gateway_port":18790,"iam_identity":"zachhendershot"}
/openclaw/agents/devops  → {"type":"team","slack_channel":"CEXAMPLE01","gateway_port":18800}
```

### Config namespace

```
/openclaw/config/openclaw-image → "123456789012.dkr.ecr.us-east-2.amazonaws.com/openclaw:latest"
```

### Removed paths

| Old path | Replacement |
|----------|------------|
| `/openclaw/users/<member_id>` | `/openclaw/agents/<name>` with `type: "user"` |
| `/openclaw/teams/<team_name>` | `/openclaw/agents/<name>` with `type: "team"` |
| `/openclaw/users/by-iam/<sso_name>` | `iam_identity` field inside agent param value |

### Agent param value schema

**User agent:**
```json
{
  "type": "user",
  "member_id": "UXXXXXXXXXX",
  "gateway_port": 18789,
  "iam_identity": "sso-username"
}
```

**Team agent:**
```json
{
  "type": "team",
  "slack_channel": "CXXXXXXXXXX",
  "gateway_port": 18800
}
```

**Container ID derivation:** User agents use `member_id` as container ID. Team agents use the agent name (SSM param key) as container ID. This preserves existing container naming and data directory paths on the EBS volume.

### Per-agent secrets paths

Unchanged:
- User secrets: `openclaw/<member_id>/<secret_name>` (e.g., `openclaw/UEXAMPLE01/anthropic-api-key`)
- Team secrets: `openclaw/teams/<agent_name>/<secret_name>` (e.g., `openclaw/teams/devops/anthropic-api-key`)

## Bootstrap Script Changes

### Current flow (template-driven)

```
terraform apply → renders user-data.sh.tftpl with %{ for } loops → uploads to S3
instance boot → downloads bootstrap.sh → runs baked-in per-agent setup
```

### New flow (SSM-driven)

```
terraform apply → uploads static bootstrap.sh to S3 (no agent-specific content)
instance boot → downloads bootstrap.sh → queries SSM → provisions discovered agents
```

### Section-by-section changes

**Section 2 (packages):** Add `jq` to `dnf install`.

**Section 4 (image pull):** Replace `OPENCLAW_IMAGE="${openclaw_image}"` with SSM read:
```bash
OPENCLAW_IMAGE=$(aws ssm get-parameter --name "/openclaw/config/openclaw-image" \
  --query "Parameter.Value" --output text --region "$AWS_REGION")
```

**Section 5 (router setup):** Remove static `routing.json` heredoc (`${routing_json}`). Routing built after agent discovery.

**Section 6 (agent setup):** Replace entire `%{ for }` block with:

```bash
# Discover all agents from SSM
AGENT_PARAMS=$(aws ssm get-parameters-by-path \
  --path "/openclaw/agents/" \
  --query "Parameters" \
  --output json --region "$AWS_REGION" 2>/dev/null || echo '[]')

# Initialize routing
ROUTING_CHANNELS="{}"
ROUTING_MEMBERS="{}"
ALL_CONTAINER_IDS=""

# Process each agent
for PARAM in $(echo "$AGENT_PARAMS" | jq -r '.[] | @base64'); do
  PARAM_JSON=$(echo "$PARAM" | base64 -d)
  AGENT_NAME=$(echo "$PARAM_JSON" | jq -r '.Name' | xargs basename)
  PARAM_VALUE=$(echo "$PARAM_JSON" | jq -r '.Value')
  AGENT_TYPE=$(echo "$PARAM_VALUE" | jq -r '.type')
  GATEWAY_PORT=$(echo "$PARAM_VALUE" | jq -r '.gateway_port')

  if [ "$AGENT_TYPE" = "user" ]; then
    MEMBER_ID=$(echo "$PARAM_VALUE" | jq -r '.member_id')
    CONTAINER_ID="$MEMBER_ID"
    setup_user_agent "$CONTAINER_ID" "$MEMBER_ID" "$GATEWAY_PORT"
    ROUTING_MEMBERS=$(echo "$ROUTING_MEMBERS" | jq \
      --arg k "$MEMBER_ID" \
      --arg v "http://openclaw-$CONTAINER_ID:18789/slack/events" \
      '. + {($k): $v}')
  elif [ "$AGENT_TYPE" = "team" ]; then
    SLACK_CHANNEL=$(echo "$PARAM_VALUE" | jq -r '.slack_channel')
    CONTAINER_ID="$AGENT_NAME"
    setup_team_agent "$CONTAINER_ID" "$AGENT_NAME" "$SLACK_CHANNEL" "$GATEWAY_PORT"
    ROUTING_CHANNELS=$(echo "$ROUTING_CHANNELS" | jq \
      --arg k "$SLACK_CHANNEL" \
      --arg v "http://openclaw-$CONTAINER_ID:18789/slack/events" \
      '. + {($k): $v}')
  fi

  ALL_CONTAINER_IDS="$ALL_CONTAINER_IDS $CONTAINER_ID"
done

# Write routing.json
jq -n --argjson channels "$ROUTING_CHANNELS" --argjson members "$ROUTING_MEMBERS" \
  '{"channels": $channels, "members": $members}' > /opt/openclaw/config/routing.json
```

**`setup_user_agent` function** — defined before section 6, takes `(container_id, member_id, gateway_port)`:
- Discover secrets under `openclaw/<member_id>/`
- Build env file (shared + per-agent secrets)
- Generate user-type `openclaw.json` (`groupPolicy: "disabled"`, `dmPolicy: "allowlist"`, `allowFrom: [member_id]`)
- Create data dir, config hash, Docker network, systemd unit
- Identical logic to `cli/scripts/add-user.sh.tmpl` lines 11-137

**`setup_team_agent` function** — takes `(container_id, team_name, slack_channel, gateway_port)`:
- Discover secrets under `openclaw/teams/<team_name>/`
- Build env file (shared + per-agent secrets)
- Generate team-type `openclaw.json` (`groupPolicy: "allowlist"`, `dmPolicy: "disabled"`, channel allowlist)
- Create data dir, config hash, Docker network, systemd unit
- Identical logic to `cli/scripts/add-team.sh.tmpl` lines 12-124

**Sections 7-9 (integrity, metrics, CloudWatch):** No changes — already use filesystem globs, no template loops.

**Section 10 (service startup):** Replace `%{ for }` loops with:
```bash
for CONTAINER_ID in $ALL_CONTAINER_IDS; do
  docker network connect "openclaw-$CONTAINER_ID" openclaw-router 2>/dev/null || true
done
for CONTAINER_ID in $ALL_CONTAINER_IDS; do
  systemctl enable "openclaw-$CONTAINER_ID.service"
  systemctl start "openclaw-$CONTAINER_ID.service"
done
```

**Final status output:** Replace `%{ for }` loop with bash loop over `ALL_CONTAINER_IDS`.

### Template variables removed from router.tf

| Removed | Why |
|---------|-----|
| `agents` | Read from SSM at boot |
| `agent_container_id` | Derived from SSM values at boot |
| `openclaw_image` | Read from SSM at boot |
| `routing_json` | Built dynamically at boot |

Remaining: `aws_region`, `project_name`, `config_check_interval_minutes`, `state_bucket`.

## Terraform Changes

### ssm-parameters.tf

**Remove:**
- `aws_ssm_parameter.user_config` (was keyed by `/openclaw/users/<member_id>`)
- `aws_ssm_parameter.team_config` (was keyed by `/openclaw/teams/<team_name>`)
- `aws_ssm_parameter.user_iam_mapping` (was keyed by `/openclaw/users/by-iam/<sso_name>`)

**Add:**
```hcl
resource "aws_ssm_parameter" "agent_config" {
  for_each = var.agents
  name     = "/openclaw/agents/${each.key}"
  type     = "String"
  value = jsonencode(merge(
    { type = each.value.type, gateway_port = each.value.gateway_port },
    each.value.type == "user" ? {
      member_id    = each.value.member_id
      iam_identity = each.value.iam_identity
    } : {},
    each.value.type == "team" ? {
      slack_channel = each.value.slack_channel
    } : {}
  ))
  tags = { Project = var.project_name }
}

resource "aws_ssm_parameter" "openclaw_image" {
  name  = "/openclaw/config/openclaw-image"
  type  = "String"
  value = var.openclaw_image
  tags  = { Project = var.project_name }
}
```

### iam.tf

Replace per-user secret ARN enumeration with wildcards:
```hcl
Resource = [
  "arn:aws:secretsmanager:...:secret:openclaw/shared/*",
  "arn:aws:secretsmanager:...:secret:openclaw/U*",
  "arn:aws:secretsmanager:...:secret:openclaw/teams/*"
]
```

### secrets.tf

Update `for_each` from `local.user_agents` to filter `var.agents` for user type. Update `member_id` reference to use new structure. Path stays `openclaw/<member_id>/` for secret cleanup.

### variables.tf

`var.agents`, validations, and locals stay. Remove `agent_container_id` local if no longer needed by any Terraform resource (check: `monitoring.tf` — if it references `agent_container_id`, keep it; otherwise remove).

### monitoring.tf, outputs.tf

Update references from `local.agent_container_id[name]` to inline derivation (`cfg.type == "user" ? cfg.member_id : name`) if `agent_container_id` local is removed. Otherwise no change.

## CLI Changes

### admin.go

**`adminAddUserRun`:**
- SSM path changes from `/openclaw/users/<member_id>` to `/openclaw/agents/<member_id>`
  - Wait — the key should be human-readable name, but `add-user` only takes `member_id`. Need to also take a name, or derive one.
  - **Decision needed:** Either `add-user` takes `<name> <member_id>` (2 args), or we use member_id as the agent name for CLI-added users. The latter is simpler and consistent with current behavior — the Terraform path uses human-readable names, but CLI-added agents can use member_id as name.
  - **Recommendation:** Change `add-user` to `add-user <name> <member_id>` for consistency with Terraform. The name becomes the SSM key.
- Write enriched JSON: `{"type":"user","member_id":"...","gateway_port":...,"iam_identity":"..."}`
- Remove the separate `by-iam` parameter write

**`adminAddTeamRun`:**
- SSM path changes from `/openclaw/teams/<team_name>` to `/openclaw/agents/<team_name>`
- Write enriched JSON: `{"type":"team","slack_channel":"...","gateway_port":...}`

**`adminListAgentsRun`:**
- Single `GetParametersByPath` on `/openclaw/agents/` instead of two calls
- Parse `type` from value to display correctly
- No more partial failure handling needed (single call)

**`adminRemoveUserRun`:**
- SSM path changes from `/openclaw/users/<member_id>` to `/openclaw/agents/<name>`
- Need to accept name instead of member_id, or look up by member_id
- **Recommendation:** Change to `remove-agent <name>` (unified command for both types)

**`adminRemoveTeamRun`:**
- Merged into `remove-agent <name>` above

**`resolveGatewayPort`:**
- Single `GetParametersByPath` on `/openclaw/agents/` instead of two calls

### discovery/identity.go

**`ResolveIdentity`:**
- Remove the direct `/openclaw/users/by-iam/` lookup
- Instead: `GetParametersByPath` on `/openclaw/agents/`, iterate values, find the one where `iam_identity` matches the caller's session name
- Extract `member_id` from the matching agent's value

### discovery/user.go

**`ResolveUser`:**
- Path changes from `/openclaw/users/<member_id>` to scanning `/openclaw/agents/` for a param with matching `member_id`
- Or: if we know the agent name (from identity resolution), direct lookup `/openclaw/agents/<name>`

**`ResolveTeam`:**
- Path changes from `/openclaw/teams/<team_name>` to `/openclaw/agents/<team_name>`

## Edge Cases

| Case | Handling |
|------|----------|
| No agents in SSM (fresh deploy) | Log warning, write empty routing.json, router starts with no routes |
| SSM unreachable at boot | `set -e` fails the script. Consistent with existing behavior (shared secrets fetch would fail first) |
| `GetParametersByPath` pagination (>10 agents) | AWS CLI default page size is 10. Add `--max-items` or note for future. Not a concern at current scale (<10) |
| Agent param with missing/invalid `type` field | Skip with warning in bootstrap. CLI should validate on write. |
| Duplicate `member_id` across user agents | Prevented by Terraform validation (already exists). CLI should also check. |
| Port collision between Terraform and CLI agents | CLI's `resolveGatewayPort` scans all agents. Terraform validation checks within `var.agents`. Cross-path collision possible but unlikely — admin would see the port in `list-agents`. |
| Old-format SSM params after migration | Bootstrap should handle gracefully — if `type` field is missing, skip with warning. One-time migration deletes old params. |
| Agent name contains special characters | Terraform validation enforces lowercase alphanumeric + hyphens for team names. User agent names (if using member_id as key from CLI) are uppercase alphanumeric. SSM param names support both. |

## Migration

One-time migration to move from old SSM paths to new:

1. `terraform apply` step 1 creates new `/openclaw/agents/*` params
2. Old `/openclaw/users/*`, `/openclaw/teams/*`, `/openclaw/users/by-iam/*` params become orphaned
3. Clean up old params manually or via a one-time script
4. Since we're OK destroying and reprovisioning, this is not risky

## CLI Command Changes Summary

| Old | New | Args |
|-----|-----|------|
| `add-user <member_id>` | `add-user <name> <member_id>` | Name + member ID |
| `add-team <team_name> <channel>` | `add-team <name> <channel>` | No change (name was already first arg) |
| `remove-user <member_id>` | `remove-agent <name>` | Unified remove by name |
| `remove-team <team_name>` | `remove-agent <name>` | Unified remove by name |
| `list-agents` | `list-agents` | No change (single SSM call now) |
