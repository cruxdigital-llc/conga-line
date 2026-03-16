# Plan: Multi-User Onboarding (Epics 5+6)

## Overview
Refactor from single-user hardcoded config to a data-driven multi-user model. Admin adds users via Terraform variable. Users self-serve their own secrets via an onboarding script. OpenClaw config is generic (no per-user skills). User-data loops over all users to create containers.

## Changes

### 1. New `users` Variable

```hcl
variable "users" {
  description = "Map of user IDs to their config"
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

Minimal config per user — just the Slack channel. Everything else is self-served.

### 2. Refactor `terraform/secrets.tf`

- Keep shared secrets (Slack tokens) in Terraform
- Remove all per-user secrets (`myagent_secrets` map)
- Aaron's existing secrets in Secrets Manager are unaffected (they were created with `ignore_changes` and won't be destroyed since we're just removing them from Terraform state)

Actually — removing them from Terraform WILL destroy them unless we `terraform state rm` first. Plan:
1. `terraform state rm` Aaron's secret resources before applying
2. Then apply the new config which has no per-user secrets

### 3. Refactor `terraform/iam.tf`

Secrets read policy scoped to all user paths dynamically:
```hcl
Resource = concat(
  ["arn:aws:secretsmanager:...:secret:openclaw/shared/*"],
  [for uid in keys(var.users) : "arn:aws:secretsmanager:...:secret:openclaw/${uid}/*"]
)
```

### 4. Refactor `terraform/user-data.sh.tftpl`

Major change — user-data becomes a loop:
- Pass `users` map to the template
- For each user: fetch their secrets (list + get all under their path), generate generic config with their channel, create data dir, create Docker network, create systemd unit, start service
- CloudWatch agent and config integrity check also need to handle multiple users

Template receives:
```hcl
users = var.users  # map of user_id => {slack_channel}
```

User-data pseudocode per user:
```bash
for user_id, channel in users:
  # List all secrets under openclaw/{user_id}/*
  SECRETS=$(aws secretsmanager list-secrets --filter Key=name,Values=openclaw/$user_id)
  # Fetch each and write to env file
  for secret in SECRETS:
    VALUE=$(aws secretsmanager get-secret-value ...)
    echo "SECRET_NAME=$VALUE" >> /opt/openclaw/config/$user_id.env
  # Generate generic openclaw.json with this user's channel
  # Create data dir, Docker network, systemd unit
  # Start service
```

### 5. Refactor `terraform/compute.tf`

Update templatefile to pass users map instead of single user:
```hcl
user_data = base64encode(templatefile("${path.module}/user-data.sh.tftpl", {
  aws_region                    = var.aws_region
  project_name                  = var.project_name
  users                         = var.users
  config_check_interval_minutes = var.config_check_interval_minutes
}))
```

### 6. Onboarding Script (`scripts/onboard-user.sh`)

Self-service script for users. Requires their own AWS CLI profile.

```bash
#!/bin/bash
# Usage: ./onboard-user.sh <user_id> <aws_profile> <aws_region>

# Required: add Anthropic API key
add_secret "openclaw/$USER_ID/anthropic-api-key" "Anthropic API Key (required)"

# Optional: add more
while true:
  read "Add another secret? (name or 'done')"
  if done: break
  add_secret "openclaw/$USER_ID/$name" "$description"

# Verify
list_secrets "openclaw/$USER_ID"
echo "Done. Ask your admin to run terraform apply to deploy your container."
```

### 7. Updated `terraform.tfvars.example`

```hcl
users = {
  myagent = {
    slack_channel = "CEXAMPLE01"
  }
  # To add a new user:
  # bob = {
  #   slack_channel = "CXXXXXXXXXX"
  # }
}
```

## Onboarding Flow (End-to-End)

### Admin:
1. Create Slack channel for new user
2. Add user to `terraform.tfvars`:
   ```hcl
   users = {
     myagent = { slack_channel = "CEXAMPLE01" }
     bob   = { slack_channel = "C0XXXXXXXX" }
   }
   ```
3. `terraform apply` — instance replaces, both containers deploy
4. Share onboarding instructions + `scripts/onboard-user.sh` with new user

### User:
1. Configure AWS CLI with their IAM user credentials
2. Run: `./scripts/onboard-user.sh bob 123456789012_BobAccess us-east-2`
3. Enter their Anthropic API key + any optional skill secrets
4. Notify admin → admin runs `terraform apply` again (or instance auto-picks up new secrets on next restart)

### Post-Onboard:
- User configures OpenClaw skills/plugins via Slack commands or container config
- User can add more secrets anytime by re-running the script

## Architect Review

- **Secret discovery at boot**: User-data lists all secrets under each user's path and fetches them dynamically. No Terraform-managed per-user secrets. This means a user can add a secret and have it picked up on next container restart without any Terraform changes.
- **Instance replacement on user add**: Adding a user changes user-data, which triggers instance replacement. This is acceptable — both users' containers restart but come back automatically.
- **Generic config**: No per-user skill config in openclaw.json. Users configure through OpenClaw itself. This trades some initial setup time for much simpler Terraform.
- **Secret naming convention**: Users must follow `openclaw/{user_id}/{secret-name}` pattern. The onboarding script enforces this. OpenClaw reads from env vars, so the secret name maps to an env var name (e.g., `anthropic-api-key` → `ANTHROPIC_API_KEY`).
- **Env var naming**: Need a convention for mapping secret names to env var names. Simplest: uppercase the secret name, replace hyphens with underscores. `anthropic-api-key` → `ANTHROPIC_API_KEY`.
