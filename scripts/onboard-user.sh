#!/usr/bin/env bash
set -euo pipefail

# Usage: ./onboard-user.sh <user_id> [aws_profile] [aws_region]
USER_ID="${1:?Usage: $0 <user_id> [aws_profile] [aws_region]}"
AWS_PROFILE="${2:-default}"
AWS_REGION="${3:-us-east-2}"

echo "OpenClaw User Onboarding: $USER_ID"
echo "===================================="
echo ""

# Verify AWS access
aws sts get-caller-identity --profile "$AWS_PROFILE" --region "$AWS_REGION" >/dev/null 2>&1 || {
  echo "ERROR: AWS credentials not configured for profile '$AWS_PROFILE'"
  echo "Configure with: aws configure --profile $AWS_PROFILE"
  exit 1
}

add_secret() {
  local name="$1"
  local desc="$2"
  local required="$3"
  local value

  echo -n "Enter $desc: "
  read -rs value
  echo ""

  if [ -z "$value" ] && [ "$required" = "required" ]; then
    echo "  ERROR: $desc is required"
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
echo "Add secrets for any OpenClaw skills you use."
echo "Secret names become env vars (e.g., 'trello-api-key' → TRELLO_API_KEY)"
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
echo ""
echo "Next steps:"
echo "  1. Ask your admin to run 'terraform apply' to deploy your container"
echo "  2. Your container will pick up all secrets under openclaw/$USER_ID/"
echo "  3. To add more secrets later, run this script again or use:"
echo "     aws secretsmanager create-secret --name openclaw/$USER_ID/<name> --secret-string <value>"
