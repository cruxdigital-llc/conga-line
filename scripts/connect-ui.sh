#!/usr/bin/env bash
set -euo pipefail

# Usage: ./connect-ui.sh <user_id> [aws_profile] [aws_region]
#
# One-command onboarding for OpenClaw web UI access:
# 1. Looks up instance ID and port from Terraform output
# 2. Fetches the gateway auth token via SSM
# 3. Starts an SSM port forwarding tunnel
# 4. After user connects in browser, approves the device pairing request

USER_ID="${1:?Usage: $0 <user_id> [aws_profile] [aws_region]}"
AWS_PROFILE="${2:-openclaw}"
AWS_REGION="${3:-us-east-2}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="$SCRIPT_DIR/../terraform"

if [ ! -d "$TF_DIR" ]; then
  echo "ERROR: terraform/ directory not found at $TF_DIR"
  exit 1
fi

# --- Helper: run a command on the instance and return stdout ---
run_remote() {
  local cmd_id
  cmd_id=$(aws ssm send-command \
    --instance-ids "$INSTANCE_ID" \
    --document-name "AWS-RunShellScript" \
    --parameters "commands=[\"$1\"]" \
    --region "$AWS_REGION" \
    --profile "$AWS_PROFILE" \
    --output text \
    --query "Command.CommandId" 2>/dev/null) || return 1

  for _ in $(seq 1 15); do
    sleep 3
    local status
    status=$(aws ssm get-command-invocation \
      --command-id "$cmd_id" \
      --instance-id "$INSTANCE_ID" \
      --region "$AWS_REGION" \
      --profile "$AWS_PROFILE" \
      --output text \
      --query "Status" 2>/dev/null) || true

    if [ "$status" = "Success" ]; then
      aws ssm get-command-invocation \
        --command-id "$cmd_id" \
        --instance-id "$INSTANCE_ID" \
        --region "$AWS_REGION" \
        --profile "$AWS_PROFILE" \
        --output text \
        --query "StandardOutputContent" 2>/dev/null
      return 0
    elif [ "$status" = "Failed" ]; then
      return 1
    fi
  done
  return 1
}

# --- Step 1: Look up instance and port ---
echo "Looking up instance and port for $USER_ID..."

INSTANCE_ID=$(cd "$TF_DIR" && terraform output -raw instance_id 2>/dev/null) || {
  echo "ERROR: Could not read instance_id from terraform output"
  exit 1
}

REMOTE_PORT=$(cd "$TF_DIR" && terraform output -json ssm_port_forward_commands 2>/dev/null | python3 -c "
import sys, json, re
cmds = json.load(sys.stdin)
if '$USER_ID' not in cmds:
    print('ERROR: User $USER_ID not found in terraform outputs', file=sys.stderr)
    sys.exit(1)
m = re.search(r'portNumber.*?\"(\d+)\"', cmds['$USER_ID'])
if m:
    print(m.group(1))
else:
    print('ERROR: Could not parse port', file=sys.stderr)
    sys.exit(1)
") || {
  echo "ERROR: Could not determine gateway port for $USER_ID"
  exit 1
}

# Local port is always 18789 so OAuth redirect URIs work for all users
LOCAL_PORT=18789

echo "Instance:    $INSTANCE_ID"
echo "Remote port: $REMOTE_PORT"
echo "Local port:  $LOCAL_PORT"
echo ""

# --- Step 2: Fetch gateway token ---
echo "Fetching gateway token..."

TOKEN=$(run_remote "python3 -c \\\"import json; c=json.load(open('/opt/openclaw/data/$USER_ID/openclaw.json')); print(c.get('gateway',{}).get('auth',{}).get('token','NOT_FOUND'))\\\"" | tr -d '[:space:]') || {
  echo "ERROR: Failed to fetch token. Is the instance running?"
  exit 1
}

if [ -z "$TOKEN" ] || [ "$TOKEN" = "NOT_FOUND" ]; then
  echo "ERROR: Gateway token not found. The container may not have started yet."
  echo "Check: docker logs openclaw-$USER_ID"
  exit 1
fi

echo ""
echo "========================================"
echo "  Gateway Token (paste into browser):"
echo "  $TOKEN"
echo "========================================"
echo ""

# --- Step 3: Start tunnel in background ---
echo "Starting SSM port forward on localhost:$LOCAL_PORT..."
echo "Open http://localhost:$LOCAL_PORT in your browser"
echo ""

aws ssm start-session \
  --target "$INSTANCE_ID" \
  --region "$AWS_REGION" \
  --profile "$AWS_PROFILE" \
  --document-name AWS-StartPortForwardingSession \
  --parameters "{\"portNumber\":[\"$REMOTE_PORT\"],\"localPortNumber\":[\"$LOCAL_PORT\"]}" &
TUNNEL_PID=$!

# Give the tunnel a moment to establish
sleep 3

# --- Step 4: Wait for user to connect, then approve pairing ---
# --- Step 4: Check if device pairing is needed ---
echo ""
echo "Checking for pending pairing requests..."

PENDING=$(run_remote "docker exec openclaw-$USER_ID npx openclaw devices list 2>&1") || true
HAS_PENDING=$(echo "$PENDING" | grep -c "Pending" || true)

if [ "$HAS_PENDING" -gt 0 ]; then
  REQUEST_ID=$(echo "$PENDING" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1) || true
  if [ -n "${REQUEST_ID:-}" ]; then
    echo "Approving device: $REQUEST_ID"
    run_remote "docker exec openclaw-$USER_ID npx openclaw devices approve $REQUEST_ID 2>&1" >/dev/null || true
    echo "Device approved! Refresh your browser if needed."
  fi
else
  echo "No pending pairing requests. If this is a new device, open the browser"
  echo "and paste the token first, then re-run this script to approve pairing."
fi

echo ""
echo "Tunnel is running. Press Ctrl+C to disconnect."
wait $TUNNEL_PID
