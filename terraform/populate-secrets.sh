#!/usr/bin/env bash
set -euo pipefail

AWS_PROFILE="${AWS_PROFILE:?Set AWS_PROFILE to your AWS CLI profile name}"
AWS_REGION="${AWS_REGION:-us-east-2}"

echo "Populate shared Conga Line secrets"
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

read_secret "conga/shared/slack-bot-token" "Slack Bot Token (xoxb-...)"
read_secret "conga/shared/slack-app-token" "Slack App Token (xapp-...)"
read_secret "conga/shared/slack-signing-secret" "Slack Signing Secret"

echo ""
echo "Shared secrets populated."
echo "Users should run scripts/onboard-user.sh to add their own secrets."
