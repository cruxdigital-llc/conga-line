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
read_secret "openclaw/aaron/trello-api-key" "Trello API Key"
read_secret "openclaw/aaron/trello-token" "Trello Token"

echo ""
echo "All secrets populated. Verify with:"
echo "  aws secretsmanager list-secrets --filter Key=name,Values=openclaw --profile $AWS_PROFILE --region $AWS_REGION --query 'SecretList[].Name' --output table"
