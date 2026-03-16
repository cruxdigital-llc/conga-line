# Plan: IAM + Secrets

## Overview
Create IAM role with least-privilege policies, KMS key for EBS encryption, and Secrets Manager entries for Aaron's deployment. Terraform creates the secret resources with placeholder values — real credentials populated manually afterward.

## File Structure

New files added to `terraform/`:
```
terraform/
├── ...existing files...
├── iam.tf          # Instance role, profile, policies
├── kms.tf          # KMS key for EBS encryption
└── secrets.tf      # Secrets Manager secrets (placeholders)
```

## Step 1: IAM Role + Policies (`iam.tf`)

**Instance role** with 4 policy attachments:

1. **SSM managed policy** — `AmazonSSMManagedInstanceCore` (AWS managed, required for Session Manager)

2. **Secrets read policy** (inline) — allows `secretsmanager:GetSecretValue` on:
   - `arn:aws:secretsmanager:us-east-2:167595588574:secret:openclaw/shared/*`
   - `arn:aws:secretsmanager:us-east-2:167595588574:secret:openclaw/aaron/*`
   - (Future users will get their own path added)

3. **CloudWatch Logs policy** (inline) — allows `logs:CreateLogStream`, `logs:PutLogEvents` on the gateway log group

4. **Deny-dangerous policy** (inline) — explicit denies on:
   - `iam:*`, `organizations:*`, `sts:AssumeRole`
   - `ec2:RunInstances`, `ec2:CreateVpc`, `ec2:CreateSecurityGroup`, `ec2:AuthorizeSecurityGroupIngress`
   - `lambda:*`, `s3:DeleteBucket`, `s3:PutBucketPolicy`

**Instance profile** wrapping the role.

## Step 2: KMS Key (`kms.tf`)

- Single KMS key for EBS encryption
- Key policy: allows the AWS account root + the instance role to use the key
- Alias: `alias/openclaw-ebs`

## Step 3: Secrets Manager (`secrets.tf`)

Create 6 secrets with placeholder values (`REPLACE_ME`):

| Secret Path | Description |
|---|---|
| `openclaw/shared/slack-bot-token` | Slack xoxb- token (shared) |
| `openclaw/shared/slack-app-token` | Slack xapp- token (shared) |
| `openclaw/aaron/anthropic-api-key` | Anthropic API key |
| `openclaw/aaron/gateway-token` | OpenClaw gateway auth token |
| `openclaw/aaron/trello-api-key` | Trello API key |
| `openclaw/aaron/trello-token` | Trello token |

After `terraform apply`, populate real values:
```bash
aws secretsmanager put-secret-value \
  --secret-id openclaw/shared/slack-bot-token \
  --secret-string "xoxb-actual-token" \
  --profile 167595588574_AdministratorAccess \
  --region us-east-2
```

Use `lifecycle { ignore_changes = [secret_string] }` so Terraform doesn't overwrite manually-set values on subsequent applies.

## Step 4: New Outputs

- Instance profile ARN (needed by Epic 3 launch template)
- KMS key ARN (needed by Epic 3 EBS encryption)
- Secret ARNs (for reference)

## Architect Review

- **Secret path structure**: `openclaw/{scope}/{name}` where scope is `shared` or a user ID. Clean, extensible for user 2.
- **Placeholder approach**: Secrets created by Terraform with dummy values, real values set manually. This keeps real credentials out of Terraform state. The `ignore_changes` lifecycle rule prevents Terraform from reverting manual updates.
- **Deny-dangerous policy**: Uses explicit Deny which overrides any Allow. Even if someone attaches AdministratorAccess to the role, the denied actions remain blocked.
- **Single KMS key**: One key for all EBS volumes. Per-user keys would be needed at Isolation Level 4 (per-user VPCs) but are unnecessary for shared-host model.
- **SSM managed policy**: Using the AWS-managed `AmazonSSMManagedInstanceCore` rather than crafting a custom SSM policy. This is standard practice and maintained by AWS.
