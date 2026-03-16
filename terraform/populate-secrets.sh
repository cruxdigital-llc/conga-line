#!/usr/bin/env bash
set -euo pipefail

AWS_PROFILE="167595588574_AdministratorAccess"
AWS_REGION="us-east-2"

echo "Populate shared OpenClaw secrets"
echo "================================"
echo ""

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
