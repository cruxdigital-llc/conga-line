# Spec: SSM-Driven Bootstrap Discovery

## Design Principle

The **agent name** is the universal identifier. All infrastructure — containers, data directories, networks, systemd units, SSM parameters, secrets — is keyed by agent name. Engagement-system-specific identifiers (Slack member IDs, channel IDs) are configuration attributes, not identity.

## SSM Parameter Schema

### Single agent namespace

All agents live under `/conga/agents/<name>`:

```
/conga/agents/myagent   → {"type":"user","slack_member_id":"UEXAMPLE01","gateway_port":18789,"iam_identity":"exampleuser"}
/conga/agents/zach    → {"type":"user","slack_member_id":"UEXAMPLE02","gateway_port":18790,"iam_identity":"zachhendershot"}
/conga/agents/devops  → {"type":"team","slack_channel":"CEXAMPLE01","gateway_port":18800}
```

### Config namespace

```
/conga/config/conga-image → "123456789012.dkr.ecr.us-east-2.amazonaws.com/conga:latest"
```

### Removed paths

| Old path | Replacement |
|----------|------------|
| `/conga/users/<member_id>` | `/conga/agents/<name>` with `type: "user"` |
| `/conga/teams/<team_name>` | `/conga/agents/<name>` with `type: "team"` |
| `/conga/users/by-iam/<sso_name>` | `iam_identity` field inside agent param value |

### Agent param value schema

**User agent:**
```json
{
  "type": "user",
  "slack_member_id": "UXXXXXXXXXX",
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

### Container ID = agent name (always)

The container ID is the agent name for all agent types. No derivation logic needed.

| Agent | Name | Container | Data dir | Docker network | Systemd unit |
|-------|------|-----------|----------|---------------|--------------|
| myagent | `myagent` | `conga-myagent` | `/opt/conga/data/myagent/` | `conga-myagent` | `conga-myagent.service` |
| devops | `devops` | `conga-devops` | `/opt/conga/data/devops/` | `conga-devops` | `conga-devops.service` |

### Per-agent secrets

Unified path for all agent types:
- `conga/agents/<name>/<secret_name>` (e.g., `conga/agents/myagent/anthropic-api-key`)

This replaces the previous split:
- ~~`conga/<member_id>/`~~ (user secrets)
- ~~`conga/teams/<team_name>/`~~ (team secrets)

## Terraform Changes

### variables.tf

Rename `member_id` → `slack_member_id`:
```hcl
variable "agents" {
  type = map(object({
    type            = string
    slack_member_id = optional(string, "")
    slack_channel   = optional(string, "")
    gateway_port    = number
    iam_identity    = optional(string, "")
  }))
}
```

Update all validations to reference `slack_member_id`.

Remove locals `user_agents`, `team_agents`, `agent_container_id` — replaced by:
```hcl
locals {
  user_agents = { for k, v in var.agents : k => v if v.type == "user" }
}
```
Only `user_agents` is needed (for `secrets.tf` cleanup). `team_agents` and `agent_container_id` are eliminated.

### ssm-parameters.tf

Replace three resources with two:
```hcl
resource "aws_ssm_parameter" "agent_config" {
  for_each = var.agents
  name     = "/conga/agents/${each.key}"
  type     = "String"
  value = jsonencode(merge(
    { type = each.value.type, gateway_port = each.value.gateway_port },
    each.value.type == "user" ? {
      slack_member_id = each.value.slack_member_id
      iam_identity    = each.value.iam_identity
    } : {},
    each.value.type == "team" ? {
      slack_channel = each.value.slack_channel
    } : {}
  ))
  tags = { Project = var.project_name }
}

resource "aws_ssm_parameter" "conga_image" {
  name  = "/conga/config/conga-image"
  type  = "String"
  value = var.conga_image
  tags  = { Project = var.project_name }
}
```

### router.tf

Remove `agents`, `agent_container_id`, `conga_image`, `routing_json` from `templatefile()`. Remaining: `aws_region`, `project_name`, `config_check_interval_minutes`, `state_bucket`.

### iam.tf

Replace per-user secret ARN enumeration with:
```hcl
Resource = [
  "arn:aws:secretsmanager:...:secret:conga/shared/*",
  "arn:aws:secretsmanager:...:secret:conga/agents/*"
]
```

Simpler than the previous `conga/U*` + `conga/teams/*` approach — unified secrets path means a single wildcard.

### secrets.tf

Update for_each and path:
```hcl
resource "terraform_data" "user_secrets_cleanup" {
  for_each = local.user_agents
  input = {
    agent_name = each.key
    region     = var.aws_region
    profile    = var.aws_profile
  }
  # Cleanup path: conga/agents/<name>/
}
```

### monitoring.tf

Replace `local.agent_container_id[name]` with just `name` (agent name = container ID):
```hcl
metrics = [
  for name, cfg in var.agents : [
    "Conga Line", "SessionSizeKB", "UserId", name,
    { label = name, stat = "Maximum", period = 300 }
  ]
]
```

### outputs.tf

Replace `agent_container_id` references with agent name directly:
```hcl
for name, cfg in var.agents : name => join(" ", [
  "aws ssm start-session",
  "--target ${aws_instance.conga.id}",
  "--document-name AWS-StartPortForwardingSession",
  "--parameters portNumber=${cfg.gateway_port},localPortNumber=18789",
  ...
])
```

### terraform.tfvars

```hcl
agents = {
  myagent = {
    type            = "user"
    slack_member_id = "UEXAMPLE01"
    gateway_port    = 18789
    iam_identity    = "exampleuser"
  }
  zach = {
    type            = "user"
    slack_member_id = "UEXAMPLE02"
    gateway_port    = 18790
    iam_identity    = "zachhendershot"
  }
}
```

### terraform.tfvars.example

```hcl
agents = {
  # myname = {
  #   type            = "user"
  #   slack_member_id = "UXXXXXXXXXX"
  #   gateway_port    = 18789
  #   iam_identity    = "myname"
  # }
  # devops = {
  #   type          = "team"
  #   slack_channel = "CXXXXXXXXXX"
  #   gateway_port  = 18800
  # }
}
```

## Bootstrap Script Changes

### Flow

```
terraform apply → uploads static bootstrap.sh to S3 (no agent-specific content)
instance boot → downloads bootstrap.sh → queries SSM → provisions discovered agents
```

### Section-by-section

**Section 2 (packages):** Add `jq`:
```bash
dnf install -y docker nodejs npm jq
```

**Section 4 (image pull):** Read from SSM:
```bash
CONGA_IMAGE=$(aws ssm get-parameter --name "/conga/config/conga-image" \
  --query "Parameter.Value" --output text --region "$AWS_REGION")
```

**Section 5 (router setup):** Remove `${routing_json}` heredoc. Routing built after discovery.

**Section 6 (agent setup):** Define functions + discovery loop:

```bash
setup_user_agent() {
  local AGENT_NAME="$1" SLACK_MEMBER_ID="$2" GATEWAY_PORT="$3"
  # Secrets under conga/agents/<name>/
  # Env file: shared + per-agent secrets
  # openclaw.json: groupPolicy=disabled, dmPolicy=allowlist, allowFrom=[slack_member_id]
  # Data dir, config hash, Docker network, systemd unit
  # All paths use AGENT_NAME (not slack_member_id)
}

setup_team_agent() {
  local AGENT_NAME="$1" SLACK_CHANNEL="$2" GATEWAY_PORT="$3"
  # Secrets under conga/agents/<name>/
  # Env file: shared + per-agent secrets
  # openclaw.json: groupPolicy=allowlist, dmPolicy=disabled, channels={slack_channel}
  # Data dir, config hash, Docker network, systemd unit
}

# Discover all agents
AGENT_PARAMS=$(aws ssm get-parameters-by-path \
  --path "/conga/agents/" \
  --query "Parameters" \
  --output json --region "$AWS_REGION" 2>/dev/null || echo '[]')

ROUTING_CHANNELS="{}"
ROUTING_MEMBERS="{}"
ALL_AGENT_NAMES=""

for PARAM in $(echo "$AGENT_PARAMS" | jq -r '.[] | @base64'); do
  PARAM_JSON=$(echo "$PARAM" | base64 -d)
  AGENT_NAME=$(echo "$PARAM_JSON" | jq -r '.Name' | xargs basename)
  PARAM_VALUE=$(echo "$PARAM_JSON" | jq -r '.Value')
  AGENT_TYPE=$(echo "$PARAM_VALUE" | jq -r '.type')
  GATEWAY_PORT=$(echo "$PARAM_VALUE" | jq -r '.gateway_port')

  if [ "$AGENT_TYPE" = "user" ]; then
    SLACK_MEMBER_ID=$(echo "$PARAM_VALUE" | jq -r '.slack_member_id')
    setup_user_agent "$AGENT_NAME" "$SLACK_MEMBER_ID" "$GATEWAY_PORT"
    ROUTING_MEMBERS=$(echo "$ROUTING_MEMBERS" | jq \
      --arg k "$SLACK_MEMBER_ID" \
      --arg v "http://conga-$AGENT_NAME:18789/slack/events" \
      '. + {($k): $v}')
  elif [ "$AGENT_TYPE" = "team" ]; then
    SLACK_CHANNEL=$(echo "$PARAM_VALUE" | jq -r '.slack_channel')
    setup_team_agent "$AGENT_NAME" "$SLACK_CHANNEL" "$GATEWAY_PORT"
    ROUTING_CHANNELS=$(echo "$ROUTING_CHANNELS" | jq \
      --arg k "$SLACK_CHANNEL" \
      --arg v "http://conga-$AGENT_NAME:18789/slack/events" \
      '. + {($k): $v}')
  else
    echo "WARNING: Unknown agent type '$AGENT_TYPE' for $AGENT_NAME, skipping"
    continue
  fi

  ALL_AGENT_NAMES="$ALL_AGENT_NAMES $AGENT_NAME"
done

# Write routing.json
jq -n --argjson channels "$ROUTING_CHANNELS" --argjson members "$ROUTING_MEMBERS" \
  '{"channels": $channels, "members": $members}' > /opt/conga/config/routing.json

if [ -z "$(echo "$ALL_AGENT_NAMES" | tr -d ' ')" ]; then
  echo "WARNING: No agents found in SSM. Router will start with empty routing config."
fi
```

**Sections 7-9:** No changes (filesystem globs, no template dependencies).

**Section 10 (service startup):**
```bash
for AGENT_NAME in $ALL_AGENT_NAMES; do
  docker network connect "conga-$AGENT_NAME" conga-router 2>/dev/null || true
done
for AGENT_NAME in $ALL_AGENT_NAMES; do
  systemctl enable "conga-$AGENT_NAME.service"
  systemctl start "conga-$AGENT_NAME.service"
done
```

**Final status:**
```bash
echo "=== Conga Line Bootstrap Complete: $(date -u) ==="
echo "Router: conga-router (Socket Mode -> HTTP forwarding)"
for AGENT_NAME in $ALL_AGENT_NAMES; do
  echo "Agent: $AGENT_NAME (container: conga-$AGENT_NAME)"
done
```

## CLI Changes

### Command changes

| Old | New | Args |
|-----|-----|------|
| `add-user <member_id>` | `add-user <name> <slack_member_id>` | Name + Slack member ID |
| `add-team <name> <channel>` | `add-team <name> <slack_channel>` | No change |
| `remove-user <member_id>` | `remove-agent <name>` | Unified remove by name |
| `remove-team <name>` | `remove-agent <name>` | Unified remove by name |
| `list-agents` | `list-agents` | Single SSM call |

### admin.go

**`adminAddUserRun`:**
- 2 args: `<name>` and `<slack_member_id>`
- Validate name with `validateAgentName` (lowercase alphanumeric + hyphens)
- Validate slack_member_id starts with `U`
- Write to `/conga/agents/<name>` with `{"type":"user","slack_member_id":"...","gateway_port":...,"iam_identity":"..."}`
- Remove `by-iam` parameter write

**`adminAddTeamRun`:**
- 2 args: `<name>` and `<slack_channel>`
- Validate name, validate channel starts with `C`
- Write to `/conga/agents/<name>` with `{"type":"team","slack_channel":"...","gateway_port":...}`

**`adminRemoveAgentRun` (replaces removeUser + removeTeam):**
- 1 arg: `<name>`
- Read `/conga/agents/<name>` to determine type (for user: offer `--delete-secrets`)
- Run `RemoveAgentScript` with `ContainerID = name`
- Delete SSM parameter `/conga/agents/<name>`
- If `--delete-secrets` and type=user: delete secrets under `conga/agents/<name>/`

**`adminListAgentsRun`:**
- Single `GetParametersByPath` on `/conga/agents/`
- Parse each value for `type`, display accordingly
- Headers: `NAME`, `TYPE`, `IDENTIFIER`, `GATEWAY PORT`
- User rows: name, "user", slack_member_id, port
- Team rows: name, "team", slack_channel, port

**`resolveGatewayPort`:**
- Single `GetParametersByPath` on `/conga/agents/`

### discovery/identity.go

**`ResolveIdentity`:**
- Remove `/conga/users/by-iam/` lookup
- `GetParametersByPath` on `/conga/agents/`, iterate, find matching `iam_identity`
- Return agent name + slack_member_id from matching entry

### discovery/user.go

Rename to `discovery/agent.go`. Restructure types:

```go
type AgentConfig struct {
    Name          string
    Type          string `json:"type"`
    SlackMemberID string `json:"slack_member_id,omitempty"`
    SlackChannel  string `json:"slack_channel,omitempty"`
    GatewayPort   int    `json:"gateway_port"`
    IAMIdentity   string `json:"iam_identity,omitempty"`
}
```

**`ResolveAgent(ctx, ssmClient, name)`** — direct lookup at `/conga/agents/<name>`.

**`ResolveAgentByIAM(ctx, ssmClient, iamIdentity)`** — scan `/conga/agents/`, match `iam_identity`.

Remove `ResolveUser`, `ResolveTeam`, `UserConfig`, `TeamConfig`.

### CLI setup scripts

**`add-user.sh.tmpl`:**
- Template var `MemberID` → `AgentName` + `SlackMemberID`
- All paths use `AgentName` (container, data dir, systemd, network, config files)
- `SlackMemberID` used only in `openclaw.json` (`allowFrom`) and routing entry
- Secrets path: `conga/agents/$AGENT_NAME/`

**`add-team.sh.tmpl`:**
- Template var `TeamName` → `AgentName` (already human-readable, just rename for consistency)
- `SlackChannel` stays as-is
- Secrets path: `conga/agents/$AGENT_NAME/`

**`remove-agent.sh.tmpl`:** Already uses `ContainerID` which will be set to agent name. No change needed.

## Edge Cases

| Case | Handling |
|------|----------|
| No agents in SSM (fresh deploy) | Log warning, write empty routing.json, router starts with no routes |
| SSM unreachable at boot | `set -e` fails the script. Consistent — shared secrets fetch would fail first |
| `GetParametersByPath` pagination (>10 agents) | Not a concern at current scale. Note for future. |
| Agent param with missing/invalid `type` | Skip with warning in bootstrap. CLI validates on write. |
| Duplicate `slack_member_id` across user agents | Terraform validation. CLI should also check. |
| Unknown `type` value | Bootstrap logs warning and skips. Future-proofs for new agent types. |
| Old-format SSM params after migration | Bootstrap skips params without `type` field. One-time cleanup of old paths. |

## Migration

1. `terraform apply` creates new `/conga/agents/*` params and `/conga/config/conga-image`
2. Deploy new bootstrap (static, SSM-driven) + updated CLI
3. Cycle host — new bootstrap discovers agents from `/conga/agents/`
4. Clean up old SSM params (`/conga/users/*`, `/conga/teams/*`) manually or via script
5. Clean up old secrets paths (`conga/<member_id>/`) → migrate to `conga/agents/<name>/` if needed, or leave as-is and update on next `conga secrets set`
