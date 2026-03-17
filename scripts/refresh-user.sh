#!/usr/bin/env bash
set -euo pipefail

# Usage: ./refresh-user.sh <member_id> [instance_id] [aws_profile] [aws_region]
# Refreshes a user's secrets and restarts their container (no instance replacement needed)
MEMBER_ID="${1:?Usage: $0 <member_id> [instance_id] [aws_profile] [aws_region]}"
INSTANCE_ID="${2:-}"
AWS_PROFILE="${3:-openclaw}"
AWS_REGION="${4:-us-east-2}"

if [ -z "$INSTANCE_ID" ]; then
  INSTANCE_ID=$(cd "$(dirname "$0")/../terraform" && terraform output -raw instance_id 2>/dev/null) || {
    echo "ERROR: Could not detect instance ID. Pass it as the 2nd argument."
    exit 1
  }
fi

echo "Refreshing secrets for $MEMBER_ID on $INSTANCE_ID..."

COMMAND_ID=$(aws ssm send-command \
  --instance-ids "$INSTANCE_ID" \
  --document-name "AWS-RunShellScript" \
  --parameters "commands=[\"set -eux; AWS_REGION=$AWS_REGION; get_secret() { aws secretsmanager get-secret-value --secret-id \\\"\$1\\\" --query SecretString --output text --region \\\"\$AWS_REGION\\\"; }; SLACK_BOT_TOKEN=\$(get_secret openclaw/shared/slack-bot-token); SLACK_APP_TOKEN=\$(get_secret openclaw/shared/slack-app-token); cat > /opt/openclaw/config/$MEMBER_ID.env << EOF\nSLACK_BOT_TOKEN=\$SLACK_BOT_TOKEN\nSLACK_APP_TOKEN=\$SLACK_APP_TOKEN\nEOF\n; USER_SECRETS=\$(aws secretsmanager list-secrets --filter Key=name,Values=openclaw/$MEMBER_ID/ --query 'SecretList[].Name' --output text --region \$AWS_REGION 2>/dev/null || echo ''); for SP in \$USER_SECRETS; do SV=\$(get_secret \$SP); SN=\$(echo \$SP | sed 's|openclaw/$MEMBER_ID/||' | tr '[:lower:]-' '[:upper:]_'); echo \\\"\$SN=\$SV\\\" >> /opt/openclaw/config/$MEMBER_ID.env; done; chmod 0400 /opt/openclaw/config/$MEMBER_ID.env; ENV_FLAGS=''; while IFS='=' read -r key value; do [ -n \\\"\$key\\\" ] && ENV_FLAGS=\\\"\$ENV_FLAGS -e \$key\\\"; done < /opt/openclaw/config/$MEMBER_ID.env; sed -i \\\"s|^ExecStart=.*|ExecStart=/usr/bin/docker run --name openclaw-$MEMBER_ID --network openclaw-$MEMBER_ID --cap-drop ALL --security-opt no-new-privileges --memory 2g --cpus 1.5 -e NODE_OPTIONS=\\\\\\\"--max-old-space-size=1536\\\\\\\" --pids-limit 256 -v /opt/openclaw/data/$MEMBER_ID:/home/node/.openclaw:rw \$ENV_FLAGS ghcr.io/openclaw/openclaw:latest|\\\" /etc/systemd/system/openclaw-$MEMBER_ID.service; systemctl daemon-reload; systemctl restart openclaw-$MEMBER_ID\"]" \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --query 'Command.CommandId' --output text)

echo "Command: $COMMAND_ID — waiting..."
sleep 15

STATUS=$(aws ssm get-command-invocation \
  --command-id "$COMMAND_ID" \
  --instance-id "$INSTANCE_ID" \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --query 'Status' --output text)

if [ "$STATUS" = "Success" ]; then
  echo "Secrets refreshed and container restarted for $MEMBER_ID"
else
  echo "Status: $STATUS"
  aws ssm get-command-invocation \
    --command-id "$COMMAND_ID" \
    --instance-id "$INSTANCE_ID" \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" \
    --query 'StandardOutputContent' --output text
fi
