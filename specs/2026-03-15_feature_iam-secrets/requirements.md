# Requirements: IAM + Secrets

## Goal
Create the IAM role, KMS key, and Secrets Manager entries needed for the EC2 host to bootstrap Aaron's Conga Line container securely.

## Secrets Inventory

### Shared (reused by user 2)
- `conga/shared/slack-bot-token` — Slack botToken (xoxb-)
- `conga/shared/slack-app-token` — Slack appToken (xapp-)

### Per-user (Aaron)
- `conga/myagent/anthropic-api-key`
- `conga/myagent/gateway-token`
- `conga/myagent/trello-api-key`
- `conga/myagent/trello-token`

## Success Criteria
1. Instance IAM role with SSM access, Secrets Manager read, CloudWatch Logs write
2. Deny-dangerous policy blocks privilege escalation
3. KMS key for EBS encryption
4. All secrets stored in Secrets Manager under structured paths
5. IMDSv2 hop limit 1 blocks containers from accessing the instance role
6. Terraform creates secrets with placeholder values; real values populated manually
