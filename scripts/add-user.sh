#!/usr/bin/env bash
set -euo pipefail

# Usage: ./add-user.sh <member_id> <slack_channel> [instance_id] [aws_profile] [aws_region]
MEMBER_ID="${1:?Usage: $0 <member_id> <slack_channel> [instance_id] [aws_profile] [aws_region]}"
SLACK_CHANNEL="${2:?Usage: $0 <member_id> <slack_channel> [instance_id] [aws_profile] [aws_region]}"
INSTANCE_ID="${3:-}"
AWS_PROFILE="${4:-openclaw}"
AWS_REGION="${5:-us-east-2}"

# Auto-detect instance ID from Terraform output if not provided
if [ -z "$INSTANCE_ID" ]; then
  echo "Detecting instance ID from Terraform..."
  INSTANCE_ID=$(cd "$(dirname "$0")/../terraform" && terraform output -raw instance_id 2>/dev/null) || {
    echo "ERROR: Could not detect instance ID. Pass it as the 3rd argument."
    exit 1
  }
fi

echo "Add User to OpenClaw"
echo "===================="
echo "  Member ID:  $MEMBER_ID"
echo "  Channel:    $SLACK_CHANNEL"
echo "  Instance:   $INSTANCE_ID"
echo ""

# Build the remote setup script
SETUP_SCRIPT=$(cat << 'REMOTESCRIPT'
set -euxo pipefail

MEMBER_ID="__MEMBER_ID__"
SLACK_CHANNEL="__SLACK_CHANNEL__"
AWS_REGION="__AWS_REGION__"

get_secret() {
  aws secretsmanager get-secret-value \
    --secret-id "$1" --query SecretString --output text --region "$AWS_REGION"
}

echo "=== Setting up user: $MEMBER_ID ==="

# Fetch shared secrets
SLACK_BOT_TOKEN=$(get_secret "openclaw/shared/slack-bot-token")
SLACK_APP_TOKEN=$(get_secret "openclaw/shared/slack-app-token")

# Discover and fetch user secrets
USER_SECRET_NAMES=$(aws secretsmanager list-secrets \
  --filter Key=name,Values="openclaw/$MEMBER_ID/" \
  --query 'SecretList[].Name' --output text \
  --region "$AWS_REGION" 2>/dev/null || echo "")

# Build env file
cat > /opt/openclaw/config/$MEMBER_ID.env << ENVFILE
SLACK_BOT_TOKEN=$SLACK_BOT_TOKEN
SLACK_APP_TOKEN=$SLACK_APP_TOKEN
ENVFILE

if [ -n "$USER_SECRET_NAMES" ]; then
  for SECRET_PATH in $USER_SECRET_NAMES; do
    SECRET_VALUE=$(get_secret "$SECRET_PATH")
    SECRET_NAME=$(echo "$SECRET_PATH" | sed "s|openclaw/$MEMBER_ID/||" | tr '[:lower:]-' '[:upper:]_')
    echo "$SECRET_NAME=$SECRET_VALUE" >> /opt/openclaw/config/$MEMBER_ID.env
  done
fi
chmod 0400 /opt/openclaw/config/$MEMBER_ID.env

# Generate config
cat > /opt/openclaw/config/$MEMBER_ID-openclaw.json << OCCONFIG
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

# Create data dir if new
if [ ! -d /opt/openclaw/data/$MEMBER_ID ]; then
  mkdir -p /opt/openclaw/data/$MEMBER_ID/{workspace,memory,logs,agents,canvas,cron,devices,identity,media}
  chown -R 1000:1000 /opt/openclaw/data/$MEMBER_ID
fi
cp /opt/openclaw/config/$MEMBER_ID-openclaw.json /opt/openclaw/data/$MEMBER_ID/openclaw.json
chown 1000:1000 /opt/openclaw/data/$MEMBER_ID/openclaw.json

# Hash baseline
sha256sum /opt/openclaw/config/$MEMBER_ID-openclaw.json | cut -d' ' -f1 > /opt/openclaw/config/$MEMBER_ID-openclaw.json.sha256
chmod 0444 /opt/openclaw/config/$MEMBER_ID-openclaw.json.sha256

# Docker network
docker network create --driver bridge "openclaw-$MEMBER_ID" || true

# Build -e flags
ENV_FLAGS=""
while IFS="=" read -r key value; do
  [ -n "$key" ] && ENV_FLAGS="$ENV_FLAGS -e $key"
done < /opt/openclaw/config/$MEMBER_ID.env

# Systemd unit
cat > /etc/systemd/system/openclaw-$MEMBER_ID.service << UNIT
[Unit]
Description=OpenClaw Gateway ($MEMBER_ID)
After=docker.service
Requires=docker.service

[Service]
Type=simple
EnvironmentFile=/opt/openclaw/config/$MEMBER_ID.env
ExecStartPre=-/usr/bin/docker rm -f openclaw-$MEMBER_ID
ExecStart=/usr/bin/docker run --name openclaw-$MEMBER_ID --network openclaw-$MEMBER_ID --cap-drop ALL --security-opt no-new-privileges --memory 2g --cpus 1.5 -e NODE_OPTIONS="--max-old-space-size=1536" --pids-limit 256 -v /opt/openclaw/data/$MEMBER_ID:/home/node/.openclaw:rw $ENV_FLAGS ghcr.io/openclaw/openclaw:latest
ExecStop=/usr/bin/docker stop openclaw-$MEMBER_ID
StandardOutput=append:/var/log/openclaw-$MEMBER_ID.log
StandardError=append:/var/log/openclaw-$MEMBER_ID.log
Restart=always
RestartSec=10
TimeoutStartSec=120
TimeoutStopSec=30

[Install]
WantedBy=multi-user.target
UNIT

# Start
systemctl daemon-reload
systemctl enable openclaw-$MEMBER_ID.service
systemctl start openclaw-$MEMBER_ID.service

echo "=== User $MEMBER_ID setup complete ==="
REMOTESCRIPT

# Substitute placeholders
SETUP_SCRIPT="${SETUP_SCRIPT//__MEMBER_ID__/$MEMBER_ID}"
SETUP_SCRIPT="${SETUP_SCRIPT//__SLACK_CHANNEL__/$SLACK_CHANNEL}"
SETUP_SCRIPT="${SETUP_SCRIPT//__AWS_REGION__/$AWS_REGION}"

echo "Sending setup command to instance..."
COMMAND_ID=$(aws ssm send-command \
  --instance-ids "$INSTANCE_ID" \
  --document-name "AWS-RunShellScript" \
  --parameters "commands=[\"$SETUP_SCRIPT\"]" \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --query 'Command.CommandId' --output text)

echo "Command ID: $COMMAND_ID"
echo "Waiting for completion..."

# Wait for command to finish
aws ssm wait command-executed \
  --command-id "$COMMAND_ID" \
  --instance-id "$INSTANCE_ID" \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" 2>/dev/null || true

sleep 5

# Get result
STATUS=$(aws ssm get-command-invocation \
  --command-id "$COMMAND_ID" \
  --instance-id "$INSTANCE_ID" \
  --profile "$AWS_PROFILE" \
  --region "$AWS_REGION" \
  --query 'Status' --output text)

if [ "$STATUS" = "Success" ]; then
  echo ""
  echo "User $MEMBER_ID deployed successfully!"
  echo ""
  echo "Next steps:"
  echo "  1. User runs: ./scripts/onboard-user.sh $MEMBER_ID <their-aws-profile> $AWS_REGION"
  echo "  2. Then refresh secrets: ./scripts/refresh-user.sh $MEMBER_ID"
  echo "  3. Don't forget to add the user to terraform/variables.tf for persistence"
else
  echo "ERROR: Command failed with status: $STATUS"
  OUTPUT=$(aws ssm get-command-invocation \
    --command-id "$COMMAND_ID" \
    --instance-id "$INSTANCE_ID" \
    --profile "$AWS_PROFILE" \
    --region "$AWS_REGION" \
    --query 'StandardOutputContent' --output text)
  echo "$OUTPUT"
fi
