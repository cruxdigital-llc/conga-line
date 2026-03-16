# Spec: Multi-User Onboarding (Epics 5+6)

## Overview
Refactor all per-user config to be driven by a `users` variable. Remove per-user secrets from Terraform — users self-serve. User-data loops over users to create isolated containers. Onboarding script for self-service secret management.

## Migration Plan (CRITICAL — run before apply)

Aaron's secrets exist in Secrets Manager and are managed by Terraform. Removing them from Terraform will trigger destruction. To preserve them:

```bash
# Remove Aaron's secrets from Terraform state (preserves them in AWS)
terraform state rm 'aws_secretsmanager_secret.openclaw["openclaw/myagent/anthropic-api-key"]'
terraform state rm 'aws_secretsmanager_secret.openclaw["openclaw/myagent/trello-api-key"]'
terraform state rm 'aws_secretsmanager_secret.openclaw["openclaw/myagent/trello-token"]'
terraform state rm 'aws_secretsmanager_secret_version.openclaw["openclaw/myagent/anthropic-api-key"]'
terraform state rm 'aws_secretsmanager_secret_version.openclaw["openclaw/myagent/trello-api-key"]'
terraform state rm 'aws_secretsmanager_secret_version.openclaw["openclaw/myagent/trello-token"]'
```

## Deliverables

### 1. Updated `terraform/variables.tf`

Add `users` variable, remove single-user references:

```hcl
variable "users" {
  description = "Map of user IDs to their config. Admin adds entries, users self-serve secrets."
  type = map(object({
    slack_channel = string
  }))
  default = {
    myagent = {
      slack_channel = "CEXAMPLE01"
    }
  }
}
```

### 2. Updated `terraform/secrets.tf`

Keep only shared secrets:

```hcl
locals {
  shared_secrets = {
    "openclaw/shared/slack-bot-token" = "Slack bot token (xoxb-)"
    "openclaw/shared/slack-app-token" = "Slack app token (xapp-)"
  }
}

resource "aws_secretsmanager_secret" "shared" {
  for_each    = local.shared_secrets
  name        = each.key
  description = each.value

  tags = {
    Name = each.key
  }
}

resource "aws_secretsmanager_secret_version" "shared" {
  for_each      = local.shared_secrets
  secret_id     = aws_secretsmanager_secret.shared[each.key].id
  secret_string = "REPLACE_ME"

  lifecycle {
    ignore_changes = [secret_string]
  }
}
```

Note: Rename resource from `aws_secretsmanager_secret.openclaw` to `aws_secretsmanager_secret.shared`. Need to move state for the shared secrets:
```bash
terraform state mv \
  'aws_secretsmanager_secret.openclaw["openclaw/shared/slack-bot-token"]' \
  'aws_secretsmanager_secret.shared["openclaw/shared/slack-bot-token"]'
terraform state mv \
  'aws_secretsmanager_secret.openclaw["openclaw/shared/slack-app-token"]' \
  'aws_secretsmanager_secret.shared["openclaw/shared/slack-app-token"]'
terraform state mv \
  'aws_secretsmanager_secret_version.openclaw["openclaw/shared/slack-bot-token"]' \
  'aws_secretsmanager_secret_version.shared["openclaw/shared/slack-bot-token"]'
terraform state mv \
  'aws_secretsmanager_secret_version.openclaw["openclaw/shared/slack-app-token"]' \
  'aws_secretsmanager_secret_version.shared["openclaw/shared/slack-app-token"]'
```

### 3. Updated `terraform/iam.tf`

Dynamic secrets read policy covering all user paths:

```hcl
resource "aws_iam_role_policy" "secrets_read" {
  name_prefix = "${var.project_name}-secrets-"
  role        = aws_iam_role.openclaw_host.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "secretsmanager:GetSecretValue",
        "secretsmanager:ListSecrets"
      ]
      Resource = concat(
        ["arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:openclaw/shared/*"],
        [for uid in keys(var.users) : "arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:openclaw/${uid}/*"]
      )
    }]
  })
}
```

Note: Added `secretsmanager:ListSecrets` — needed for the dynamic secret discovery at boot. However, `ListSecrets` doesn't support resource-level restrictions (it's a list operation). Need to add a separate statement:

```hcl
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue"]
        Resource = concat(
          ["arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:openclaw/shared/*"],
          [for uid in keys(var.users) : "arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:openclaw/${uid}/*"]
        )
      },
      {
        Effect   = "Allow"
        Action   = ["secretsmanager:ListSecrets"]
        Resource = "*"
      }
    ]
```

### 4. Updated `terraform/compute.tf`

Pass users map to templatefile:

```hcl
  user_data = base64encode(templatefile("${path.module}/user-data.sh.tftpl", {
    aws_region                    = var.aws_region
    project_name                  = var.project_name
    users                         = var.users
    config_check_interval_minutes = var.config_check_interval_minutes
  }))
```

### 5. Rewritten `terraform/user-data.sh.tftpl`

Major rewrite — loops over users. Key sections:

```bash
#!/bin/bash
set -euxo pipefail

exec > >(tee /var/log/openclaw-bootstrap.log) 2>&1
echo "=== OpenClaw Bootstrap Start: $(date -u) ==="

AWS_REGION="${aws_region}"
PROJECT_NAME="${project_name}"

# Users config (from Terraform)
%{ for user_id, user_config in users ~}
USERS_${upper(user_id)}_CHANNEL="${user_config.slack_channel}"
%{ endfor ~}

# --- Sections 1-2: OS Hardening + Docker Install (unchanged) ---

# --- Section 3: Fetch Secrets (per-user dynamic) ---

get_secret() { ... }  # unchanged

# Shared secrets
SLACK_BOT_TOKEN=$(get_secret "openclaw/shared/slack-bot-token")
SLACK_APP_TOKEN=$(get_secret "openclaw/shared/slack-app-token")

# --- Section 4-8: Per-User Loop ---

%{ for user_id, user_config in users ~}
echo "=== Setting up user: ${user_id} ==="

USER_ID="${user_id}"
SLACK_CHANNEL="${user_config.slack_channel}"

# Discover and fetch this user's secrets
USER_SECRETS=$(aws secretsmanager list-secrets \
  --filter Key=name,Values=openclaw/$USER_ID/ \
  --query 'SecretList[].Name' --output text \
  --region "$AWS_REGION")

# Build env file: shared secrets + user secrets
cat > /opt/openclaw/config/$USER_ID.env << ENVFILE
SLACK_BOT_TOKEN=$SLACK_BOT_TOKEN
SLACK_APP_TOKEN=$SLACK_APP_TOKEN
ENVFILE

for SECRET_PATH in $USER_SECRETS; do
  SECRET_VALUE=$(get_secret "$SECRET_PATH")
  # Convert secret name to env var: openclaw/myagent/anthropic-api-key → ANTHROPIC_API_KEY
  SECRET_NAME=$(echo "$SECRET_PATH" | sed "s|openclaw/$USER_ID/||" | tr '[:lower:]-' '[:upper:]_')
  echo "$SECRET_NAME=$SECRET_VALUE" >> /opt/openclaw/config/$USER_ID.env
done
chmod 0400 /opt/openclaw/config/$USER_ID.env

# Generate generic config with user's channel
cat > /opt/openclaw/config/$USER_ID-openclaw.json << OCCONFIG
{
  "agents": {
    "defaults": {
      "model": { "primary": "anthropic/claude-opus-4-6" },
      "models": { "anthropic/claude-opus-4-6": {} },
      "workspace": "/home/node/.openclaw/data/workspace"
    }
  },
  "tools": { "profile": "coding" },
  "commands": { "native": "auto", "nativeSkills": "auto", "restart": true, "ownerDisplay": "raw" },
  "session": { "dmScope": "per-channel-peer" },
  "hooks": {
    "internal": {
      "enabled": true,
      "entries": {
        "command-logger": { "enabled": true },
        "session-memory": { "enabled": true }
      }
    }
  },
  "channels": {
    "slack": {
      "mode": "socket", "enabled": true, "userTokenReadOnly": true,
      "groupPolicy": "allowlist", "streaming": "partial", "nativeStreaming": true,
      "channels": { "$SLACK_CHANNEL": { "allow": true, "requireMention": false } }
    }
  },
  "gateway": { "port": 18789, "mode": "local", "bind": "loopback" },
  "skills": { "install": { "nodeManager": "pnpm" } },
  "plugins": { "entries": { "slack": { "enabled": true } } }
}
OCCONFIG

# Create persistent storage
mkdir -p /opt/openclaw/data/$USER_ID/{workspace,memory,logs,agents,canvas,cron,devices,identity,media}
chown -R 1000:1000 /opt/openclaw/data/$USER_ID
cp /opt/openclaw/config/$USER_ID-openclaw.json /opt/openclaw/data/$USER_ID/openclaw.json
chown 1000:1000 /opt/openclaw/data/$USER_ID/openclaw.json

# Docker network
docker network create --driver bridge "openclaw-$USER_ID" || true

# Systemd unit — dynamically pass all env vars from the env file
cat > /etc/systemd/system/openclaw-$USER_ID.service << UNIT
[Unit]
Description=OpenClaw Gateway ($USER_ID)
After=docker.service
Requires=docker.service

[Service]
Type=simple
EnvironmentFile=/opt/openclaw/config/$USER_ID.env
ExecStartPre=-/usr/bin/docker rm -f openclaw-$USER_ID
ExecStart=/bin/bash -c 'ENV_ARGS=""; while IFS="=" read -r key value; do ENV_ARGS="$$ENV_ARGS -e $$key"; done < /opt/openclaw/config/$USER_ID.env; /usr/bin/docker run --name openclaw-$USER_ID --network openclaw-$USER_ID --cap-drop ALL --security-opt no-new-privileges --memory 2g --cpus 1.5 -e NODE_OPTIONS="--max-old-space-size=1536" --pids-limit 256 -v /opt/openclaw/data/$USER_ID:/home/node/.openclaw:rw $$ENV_ARGS ghcr.io/openclaw/openclaw:latest'
ExecStop=/usr/bin/docker stop openclaw-$USER_ID
StandardOutput=append:/var/log/openclaw-$USER_ID.log
StandardError=append:/var/log/openclaw-$USER_ID.log
Restart=always
RestartSec=10
TimeoutStartSec=120
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
UNIT

echo "User $USER_ID configured"
%{ endfor ~}
```

Key change in the systemd unit: instead of hardcoding `-e ANTHROPIC_API_KEY -e TRELLO_API_KEY ...`, the ExecStart reads the env file and dynamically builds `-e` flags for every variable. This means any secret the user adds gets passed to their container.

### 6. Updated config integrity check

The check script needs to handle multiple users:

```bash
cat > /opt/openclaw/scripts/check-config-integrity.sh << 'CHECKSCRIPT'
#!/bin/bash
LOGFILE="/var/log/openclaw-integrity.log"
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
for HASHFILE in /opt/openclaw/config/*-openclaw.json.sha256; do
  [ -f "$HASHFILE" ] || continue
  USER_ID=$(basename "$HASHFILE" | sed 's/-openclaw.json.sha256//')
  EXPECTED_HASH=$(cat "$HASHFILE")
  CURRENT_HASH=$(sha256sum "/opt/openclaw/data/$USER_ID/openclaw.json" | cut -d' ' -f1)
  if [ "$EXPECTED_HASH" != "$CURRENT_HASH" ]; then
    MSG="$TIMESTAMP CONFIG_INTEGRITY_VIOLATION user=$USER_ID expected=$EXPECTED_HASH actual=$CURRENT_HASH"
    echo "$MSG" >> "$LOGFILE"
    echo "$MSG" | systemd-cat -t openclaw-integrity -p warning
  else
    MSG="$TIMESTAMP Config integrity OK user=$USER_ID hash=$CURRENT_HASH"
    echo "$MSG" >> "$LOGFILE"
  fi
done
CHECKSCRIPT
```

And the hash baseline stored per-user:
```bash
sha256sum /opt/openclaw/config/$USER_ID-openclaw.json | cut -d' ' -f1 > /opt/openclaw/config/$USER_ID-openclaw.json.sha256
chmod 0444 /opt/openclaw/config/$USER_ID-openclaw.json.sha256
```

### 7. `scripts/onboard-user.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

# Usage: ./onboard-user.sh <user_id> [aws_profile] [aws_region]
USER_ID="${1:?Usage: $0 <user_id> [aws_profile] [aws_region]}"
AWS_PROFILE="${2:-default}"
AWS_REGION="${3:-us-east-2}"

echo "OpenClaw User Onboarding: $USER_ID"
echo "===================================="
echo ""

add_secret() {
  local name="$1"
  local desc="$2"
  local required="$3"
  local value

  echo -n "Enter $desc: "
  read -rs value
  echo ""

  if [ -z "$value" ] && [ "$required" = "required" ]; then
    echo "ERROR: $desc is required"
    exit 1
  elif [ -z "$value" ]; then
    echo "  Skipped (optional)"
    return
  fi

  # Create or update the secret
  if aws secretsmanager describe-secret --secret-id "$name" --profile "$AWS_PROFILE" --region "$AWS_REGION" >/dev/null 2>&1; then
    aws secretsmanager put-secret-value \
      --secret-id "$name" \
      --secret-string "$value" \
      --profile "$AWS_PROFILE" \
      --region "$AWS_REGION" >/dev/null
  else
    aws secretsmanager create-secret \
      --name "$name" \
      --secret-string "$value" \
      --profile "$AWS_PROFILE" \
      --region "$AWS_REGION" >/dev/null
  fi
  echo "  ✓ $name"
}

# Required secret
echo "--- Required ---"
add_secret "openclaw/$USER_ID/anthropic-api-key" "Anthropic API Key (sk-ant-...)" "required"

# Optional secrets
echo ""
echo "--- Optional Secrets ---"
echo "Add any additional secrets your OpenClaw skills need."
echo "Secret names will be converted to env vars (e.g., 'trello-api-key' → TRELLO_API_KEY)"
echo "Enter 'done' when finished."
echo ""

while true; do
  echo -n "Secret name (or 'done'): "
  read -r secret_name
  [ "$secret_name" = "done" ] && break
  [ -z "$secret_name" ] && continue
  add_secret "openclaw/$USER_ID/$secret_name" "$secret_name" "optional"
done

# List all secrets
echo ""
echo "--- Your Secrets ---"
aws secretsmanager list-secrets \
  --filter Key=name,Values="openclaw/$USER_ID/" \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --query 'SecretList[].Name' --output table

echo ""
echo "Onboarding complete!"
echo "Ask your admin to run 'terraform apply' to deploy your container."
echo "Your container will automatically pick up all secrets under openclaw/$USER_ID/."
```

### 8. Updated `terraform/populate-secrets.sh`

Simplify to shared-only (users self-serve):

```bash
#!/usr/bin/env bash
set -euo pipefail

AWS_PROFILE="123456789012_AdministratorAccess"
AWS_REGION="us-east-2"

echo "Populate shared OpenClaw secrets"
echo "================================"

read_secret() {
  local name="$1"
  local desc="$2"
  echo -n "Enter $desc ($name): "
  read -rs value
  echo ""
  aws secretsmanager put-secret-value \
    --secret-id "$name" --secret-string "$value" \
    --profile "$AWS_PROFILE" --region "$AWS_REGION" >/dev/null
  echo "  ✓ $name updated"
}

read_secret "openclaw/shared/slack-bot-token" "Slack Bot Token (xoxb-...)"
read_secret "openclaw/shared/slack-app-token" "Slack App Token (xapp-...)"

echo ""
echo "Shared secrets populated."
echo "Users should run scripts/onboard-user.sh to add their own secrets."
```

### 9. Updated `terraform/terraform.tfvars.example`

```hcl
# AWS Configuration
aws_region  = "us-east-2"
aws_profile = "123456789012_AdministratorAccess"
project_name = "openclaw"

# Monitoring
config_check_interval_minutes = 5
alert_email = ""  # Set to receive alert emails

# Users — add entries to onboard new users
users = {
  myagent = {
    slack_channel = "CEXAMPLE01"
  }
  # bob = {
  #   slack_channel = "CXXXXXXXXXX"
  # }
}
```

## Edge Cases

| Scenario | Handling |
|---|---|
| User has no secrets yet | Container starts but OpenClaw fails (no Anthropic key). Systemd restarts it. Once user adds secrets and container restarts, it works. |
| User adds a secret after deploy | Secret picked up on next container restart. Admin can trigger: `systemctl restart openclaw-{user_id}` via SSM, or redeploy. |
| Secret name with special chars | `sed` conversion: lowercase + hyphens → uppercase + underscores. Other chars would break. Onboarding script should validate. |
| Admin removes a user from tfvars | `terraform apply` replaces instance; removed user's container no longer created. Their secrets remain in Secrets Manager (not managed by TF). |
| Two users on same channel | Both containers respond — misconfiguration. Admin responsibility to assign unique channels. |

## Validation Steps

1. Run migration state commands (preserve Aaron's secrets)
2. `terraform plan` — should show secrets refactoring + instance replacement
3. `terraform apply`
4. Verify Aaron's container still works (Slack connected, secrets loaded)
5. Add a test user to tfvars, apply, run onboard script, verify second container
