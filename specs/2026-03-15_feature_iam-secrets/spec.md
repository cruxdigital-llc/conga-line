# Spec: IAM + Secrets

## Overview
Create IAM role with least-privilege + deny-dangerous policies, KMS key for EBS encryption, and Secrets Manager entries for Aaron's deployment. Placeholder values in Terraform; real credentials populated manually.

## Deliverables

### 1. `terraform/iam.tf`

```hcl
# --- Instance Role ---

resource "aws_iam_role" "openclaw_host" {
  name_prefix = "${var.project_name}-host-"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ec2.amazonaws.com"
      }
    }]
  })

  tags = {
    Name = "${var.project_name}-host-role"
  }
}

resource "aws_iam_instance_profile" "openclaw_host" {
  name_prefix = "${var.project_name}-host-"
  role        = aws_iam_role.openclaw_host.name
}

# --- SSM Access (AWS managed policy) ---

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.openclaw_host.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

# --- Secrets Manager Read ---

resource "aws_iam_role_policy" "secrets_read" {
  name_prefix = "${var.project_name}-secrets-"
  role        = aws_iam_role.openclaw_host.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "secretsmanager:GetSecretValue"
      ]
      Resource = [
        "arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:openclaw/shared/*",
        "arn:aws:secretsmanager:${var.aws_region}:${data.aws_caller_identity.current.account_id}:secret:openclaw/aaron/*"
      ]
    }]
  })
}

# --- CloudWatch Logs ---

resource "aws_iam_role_policy" "cloudwatch_logs" {
  name_prefix = "${var.project_name}-logs-"
  role        = aws_iam_role.openclaw_host.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "logs:CreateLogStream",
        "logs:PutLogEvents",
        "logs:DescribeLogStreams"
      ]
      Resource = [
        "${aws_cloudwatch_log_group.gateway.arn}:*"
      ]
    }]
  })
}

# --- Deny Dangerous Actions ---

resource "aws_iam_role_policy" "deny_dangerous" {
  name_prefix = "${var.project_name}-deny-"
  role        = aws_iam_role.openclaw_host.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Deny"
      Action = [
        "iam:*",
        "organizations:*",
        "sts:AssumeRole",
        "ec2:RunInstances",
        "ec2:CreateVpc",
        "ec2:CreateSecurityGroup",
        "ec2:AuthorizeSecurityGroupIngress",
        "lambda:*",
        "s3:DeleteBucket",
        "s3:PutBucketPolicy"
      ]
      Resource = "*"
    }]
  })
}
```

Note: Requires a `data.aws_caller_identity.current` data source. Add to `vpc.tf` or a new `data.tf`:
```hcl
data "aws_caller_identity" "current" {}
```

Also requires a `aws_cloudwatch_log_group.gateway` resource — create in this epic so the IAM policy can reference it:
```hcl
resource "aws_cloudwatch_log_group" "gateway" {
  name              = "/${var.project_name}/gateway"
  retention_in_days = 30

  tags = {
    Name = "${var.project_name}-gateway-logs"
  }
}
```

### 2. `terraform/kms.tf`

```hcl
resource "aws_kms_key" "ebs" {
  description             = "OpenClaw EBS encryption key"
  deletion_window_in_days = 7
  enable_key_rotation     = true

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowAccountRoot"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action   = "kms:*"
        Resource = "*"
      },
      {
        Sid    = "AllowInstanceRole"
        Effect = "Allow"
        Principal = {
          AWS = aws_iam_role.openclaw_host.arn
        }
        Action = [
          "kms:Decrypt",
          "kms:GenerateDataKey",
          "kms:DescribeKey"
        ]
        Resource = "*"
      },
      {
        Sid    = "AllowEBSService"
        Effect = "Allow"
        Principal = {
          AWS = "*"
        }
        Action = [
          "kms:CreateGrant"
        ]
        Resource = "*"
        Condition = {
          Bool = {
            "kms:GrantIsForAWSResource" = "true"
          }
        }
      }
    ]
  })

  tags = {
    Name = "${var.project_name}-ebs-key"
  }
}

resource "aws_kms_alias" "ebs" {
  name          = "alias/${var.project_name}-ebs"
  target_key_id = aws_kms_key.ebs.key_id
}
```

### 3. `terraform/secrets.tf`

```hcl
locals {
  shared_secrets = {
    "openclaw/shared/slack-bot-token" = "Slack bot token (xoxb-)"
    "openclaw/shared/slack-app-token" = "Slack app token (xapp-)"
  }

  aaron_secrets = {
    "openclaw/aaron/anthropic-api-key" = "Anthropic API key"
    "openclaw/aaron/gateway-token"     = "OpenClaw gateway auth token"
    "openclaw/aaron/trello-api-key"    = "Trello API key"
    "openclaw/aaron/trello-token"      = "Trello token"
  }

  all_secrets = merge(local.shared_secrets, local.aaron_secrets)
}

resource "aws_secretsmanager_secret" "openclaw" {
  for_each    = local.all_secrets
  name        = each.key
  description = each.value

  tags = {
    Name = each.key
  }
}

resource "aws_secretsmanager_secret_version" "openclaw" {
  for_each      = local.all_secrets
  secret_id     = aws_secretsmanager_secret.openclaw[each.key].id
  secret_string = "REPLACE_ME"

  lifecycle {
    ignore_changes = [secret_string]
  }
}
```

### 4. Populate Secrets Script (`terraform/populate-secrets.sh`)

Helper script for manually setting real values after `terraform apply`:

```bash
#!/usr/bin/env bash
set -euo pipefail

AWS_PROFILE="167595588574_AdministratorAccess"
AWS_REGION="us-east-2"

echo "Populate OpenClaw secrets in AWS Secrets Manager"
echo "================================================"
echo ""

read_secret() {
  local name="$1"
  local desc="$2"
  local value
  echo -n "Enter $desc ($name): "
  read -rs value
  echo ""
  aws secretsmanager put-secret-value \
    --secret-id "$name" \
    --secret-string "$value" \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" >/dev/null
  echo "  ✓ $name updated"
}

echo "--- Shared Secrets ---"
read_secret "openclaw/shared/slack-bot-token" "Slack Bot Token (xoxb-...)"
read_secret "openclaw/shared/slack-app-token" "Slack App Token (xapp-...)"

echo ""
echo "--- Aaron's Secrets ---"
read_secret "openclaw/aaron/anthropic-api-key" "Anthropic API Key"
read_secret "openclaw/aaron/gateway-token" "Gateway Auth Token"
read_secret "openclaw/aaron/trello-api-key" "Trello API Key"
read_secret "openclaw/aaron/trello-token" "Trello Token"

echo ""
echo "All secrets populated. Verify with:"
echo "  aws secretsmanager list-secrets --filter Key=name,Values=openclaw --profile $AWS_PROFILE --region $AWS_REGION --query 'SecretList[].Name' --output table"
```

### 5. Updated Outputs in `terraform/outputs.tf`

Append:
```hcl
output "instance_profile_arn" {
  description = "IAM instance profile ARN for OpenClaw host"
  value       = aws_iam_instance_profile.openclaw_host.arn
}

output "instance_profile_name" {
  description = "IAM instance profile name for OpenClaw host"
  value       = aws_iam_instance_profile.openclaw_host.name
}

output "kms_key_arn" {
  description = "KMS key ARN for EBS encryption"
  value       = aws_kms_key.ebs.arn
}

output "gateway_log_group" {
  description = "CloudWatch log group for gateway logs"
  value       = aws_cloudwatch_log_group.gateway.name
}
```

## Edge Cases

| Scenario | Handling |
|---|---|
| Secret name already exists | Terraform import or delete existing; `for_each` will error on collision |
| `terraform apply` after manual secret update | `ignore_changes` prevents overwrite |
| `terraform destroy` removes secrets | Expected; secrets are recreatable. Real credential values should be stored securely outside AWS (e.g., 1Password) |
| KMS key deletion | 7-day deletion window provides recovery period |
| Instance role has no EC2 instance yet | Fine — role exists but is unused until Epic 3 |

## Validation Steps

1. `terraform plan` — should show IAM role, profile, 4 policies, KMS key + alias, 6 secrets + versions, gateway log group
2. `terraform apply` — creates all resources
3. Verify IAM role policies via AWS CLI
4. Verify secrets exist with placeholder values
5. Run `populate-secrets.sh` to set real values
6. Verify secrets can be read with the instance role (will test fully in Epic 3)
