# Spec: EC2 + Docker Bootstrap

## Overview
Deploy a t4g.medium instance with AL2023, bootstrap Docker rootless via user-data, run Aaron's OpenClaw container with full hardening, secrets via env vars, persistent state on encrypted EBS.

## Deliverables

### 1. `terraform/compute.tf`

```hcl
# --- AMI Lookup ---

data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

# --- Launch Template ---

resource "aws_launch_template" "openclaw" {
  name_prefix   = "${var.project_name}-"
  image_id      = data.aws_ssm_parameter.al2023_arm64.value
  instance_type = "t4g.medium"

  iam_instance_profile {
    name = aws_iam_instance_profile.openclaw_host.name
  }

  vpc_security_group_ids = [aws_security_group.openclaw_host.id]

  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_size           = 20
      volume_type           = "gp3"
      encrypted             = true
      kms_key_id            = aws_kms_key.ebs.arn
      delete_on_termination = true
    }
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  user_data = base64encode(templatefile("${path.module}/user-data.sh.tftpl", {
    aws_region    = var.aws_region
    project_name  = var.project_name
    user_id       = "aaron"
    slack_channel = "C0ALL272SV8"
  }))

  tag_specifications {
    resource_type = "instance"
    tags = {
      Name = "${var.project_name}-host"
    }
  }

  tag_specifications {
    resource_type = "volume"
    tags = {
      Name = "${var.project_name}-host-ebs"
    }
  }

  tags = {
    Name = "${var.project_name}-launch-template"
  }
}

# --- EC2 Instance ---

resource "aws_instance" "openclaw" {
  subnet_id = aws_subnet.private.id

  launch_template {
    id      = aws_launch_template.openclaw.id
    version = "$Latest"
  }

  tags = {
    Name = "${var.project_name}-host"
  }
}
```

Notes:
- Using SSM parameter for AMI lookup — always gets the latest AL2023 arm64 AMI, no hardcoded AMI ID.
- `http_put_response_hop_limit = 1` prevents containers from reaching IMDS.
- User-data passes only non-secret config (region, user_id, channel). Secrets are fetched at boot from Secrets Manager.

### 2. `terraform/user-data.sh.tftpl`

```bash
#!/bin/bash
set -euxo pipefail

exec > >(tee /var/log/openclaw-bootstrap.log) 2>&1
echo "=== OpenClaw Bootstrap Start: $(date -u) ==="

AWS_REGION="${aws_region}"
PROJECT_NAME="${project_name}"
USER_ID="${user_id}"
SLACK_CHANNEL="${slack_channel}"

# ============================================================
# 1. OS HARDENING
# ============================================================

# Remove SSH server
dnf remove -y openssh-server || true

# Enable automatic security updates
dnf install -y dnf-automatic
sed -i 's/apply_updates = no/apply_updates = yes/' /etc/dnf/automatic.conf
systemctl enable --now dnf-automatic-install.timer

# Sysctl hardening
cat > /etc/sysctl.d/99-openclaw.conf << 'SYSCTL'
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.default.send_redirects = 0
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv4.tcp_syncookies = 1
net.ipv6.conf.all.disable_ipv6 = 1
net.ipv6.conf.default.disable_ipv6 = 1
SYSCTL
sysctl --system

# ============================================================
# 2. INSTALL DOCKER (ROOTLESS)
# ============================================================

# Install Docker and rootless prerequisites
dnf install -y docker uidmap fuse-overlayfs

# Create openclaw user
useradd -m -u 1000 -s /bin/bash openclaw || true

# Enable lingering for rootless Docker
loginctl enable-linger openclaw

# Set up rootless Docker as openclaw user
su - openclaw -c '
  # Install rootless Docker
  dockerd-rootless-setuptool.sh install

  # Set DOCKER_HOST for this user
  echo "export DOCKER_HOST=unix:///run/user/1000/docker.sock" >> ~/.bashrc
  echo "export XDG_RUNTIME_DIR=/run/user/1000" >> ~/.bashrc
'

# Wait for rootless Docker to be ready
sleep 5
DOCKER_CMD="sudo -u openclaw env XDG_RUNTIME_DIR=/run/user/1000 DOCKER_HOST=unix:///run/user/1000/docker.sock docker"

# Verify Docker rootless is running
$DOCKER_CMD info > /dev/null 2>&1 || {
  echo "ERROR: Docker rootless failed to start"
  exit 1
}
echo "Docker rootless is running"

# ============================================================
# 3. FETCH SECRETS
# ============================================================

get_secret() {
  aws secretsmanager get-secret-value \
    --secret-id "$1" \
    --query SecretString \
    --output text \
    --region "$AWS_REGION"
}

SLACK_BOT_TOKEN=$(get_secret "openclaw/shared/slack-bot-token")
SLACK_APP_TOKEN=$(get_secret "openclaw/shared/slack-app-token")
ANTHROPIC_API_KEY=$(get_secret "openclaw/$USER_ID/anthropic-api-key")
TRELLO_API_KEY=$(get_secret "openclaw/$USER_ID/trello-api-key")
TRELLO_TOKEN=$(get_secret "openclaw/$USER_ID/trello-token")

echo "All secrets fetched from Secrets Manager"

# ============================================================
# 4. GENERATE CONFIG (NO SECRETS)
# ============================================================

mkdir -p /opt/openclaw/config

cat > /opt/openclaw/config/openclaw.json << OCCONFIG
{
  "agents": {
    "defaults": {
      "model": {
        "primary": "anthropic/claude-opus-4-6"
      },
      "models": {
        "anthropic/claude-opus-4-6": {}
      },
      "workspace": "/home/node/.openclaw/data/workspace"
    }
  },
  "tools": {
    "profile": "coding"
  },
  "commands": {
    "native": "auto",
    "nativeSkills": "auto",
    "restart": true,
    "ownerDisplay": "raw"
  },
  "session": {
    "dmScope": "per-channel-peer"
  },
  "hooks": {
    "internal": {
      "enabled": true,
      "entries": {
        "command-logger": {
          "enabled": true
        },
        "session-memory": {
          "enabled": true
        }
      }
    }
  },
  "channels": {
    "slack": {
      "mode": "socket",
      "enabled": true,
      "userTokenReadOnly": true,
      "groupPolicy": "allowlist",
      "streaming": "partial",
      "nativeStreaming": true,
      "channels": {
        "$SLACK_CHANNEL": {
          "allow": true,
          "requireMention": false
        }
      }
    }
  },
  "gateway": {
    "port": 18789,
    "mode": "local",
    "bind": "loopback"
  },
  "skills": {
    "install": {
      "nodeManager": "pnpm"
    },
    "entries": {
      "trello": {
        "env": {}
      }
    }
  },
  "plugins": {
    "entries": {
      "slack": {
        "enabled": true
      }
    }
  }
}
OCCONFIG

# Set immutable ownership
chown root:root /opt/openclaw/config/openclaw.json
chmod 0444 /opt/openclaw/config/openclaw.json

echo "Config written to /opt/openclaw/config/openclaw.json (root-owned, read-only)"

# ============================================================
# 5. CREATE PERSISTENT STORAGE
# ============================================================

mkdir -p /opt/openclaw/data/$USER_ID/{workspace,memory,logs,agents,canvas,cron,devices,identity,media}
chown -R 1000:1000 /opt/openclaw/data/$USER_ID

echo "Persistent storage created at /opt/openclaw/data/$USER_ID"

# ============================================================
# 6. CREATE DOCKER NETWORK
# ============================================================

$DOCKER_CMD network create --driver bridge "openclaw-$USER_ID" || true

echo "Docker network openclaw-$USER_ID created"

# ============================================================
# 7. PULL IMAGE
# ============================================================

$DOCKER_CMD pull ghcr.io/openclaw/openclaw:latest

echo "OpenClaw image pulled"

# ============================================================
# 8. CREATE SYSTEMD SERVICE
# ============================================================

# Create env file with secrets (owned by openclaw, mode 0400)
cat > /opt/openclaw/config/$USER_ID.env << ENVFILE
ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY
SLACK_BOT_TOKEN=$SLACK_BOT_TOKEN
SLACK_APP_TOKEN=$SLACK_APP_TOKEN
TRELLO_API_KEY=$TRELLO_API_KEY
TRELLO_TOKEN=$TRELLO_TOKEN
ENVFILE
chown openclaw:openclaw /opt/openclaw/config/$USER_ID.env
chmod 0400 /opt/openclaw/config/$USER_ID.env

# Create systemd user service directory
mkdir -p /home/openclaw/.config/systemd/user
chown -R openclaw:openclaw /home/openclaw/.config

cat > /home/openclaw/.config/systemd/user/openclaw-$USER_ID.service << UNIT
[Unit]
Description=OpenClaw Gateway ($USER_ID)
After=default.target

[Service]
Type=simple
EnvironmentFile=/opt/openclaw/config/$USER_ID.env
ExecStartPre=-/usr/bin/env XDG_RUNTIME_DIR=/run/user/1000 DOCKER_HOST=unix:///run/user/1000/docker.sock docker rm -f openclaw-$USER_ID
ExecStart=/usr/bin/env XDG_RUNTIME_DIR=/run/user/1000 DOCKER_HOST=unix:///run/user/1000/docker.sock docker run \
  --name openclaw-$USER_ID \
  --network openclaw-$USER_ID \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid \
  --tmpfs /home/node/.openclaw/.cache:rw \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --memory 1536m \
  --cpus 1.5 \
  --pids-limit 256 \
  -v /opt/openclaw/config/openclaw.json:/home/node/.openclaw/openclaw.json:ro \
  -v /opt/openclaw/data/$USER_ID:/home/node/.openclaw/data:rw \
  -e ANTHROPIC_API_KEY \
  -e SLACK_BOT_TOKEN \
  -e SLACK_APP_TOKEN \
  -e TRELLO_API_KEY \
  -e TRELLO_TOKEN \
  ghcr.io/openclaw/openclaw:latest
Restart=always
RestartSec=10
TimeoutStartSec=120
TimeoutStopSec=30

[Install]
WantedBy=default.target
UNIT

chown openclaw:openclaw /home/openclaw/.config/systemd/user/openclaw-$USER_ID.service

# ============================================================
# 9. START SERVICE
# ============================================================

# Enable and start as openclaw user
su - openclaw -c "
  export XDG_RUNTIME_DIR=/run/user/1000
  export DOCKER_HOST=unix:///run/user/1000/docker.sock
  systemctl --user daemon-reload
  systemctl --user enable openclaw-$USER_ID.service
  systemctl --user start openclaw-$USER_ID.service
"

echo "=== OpenClaw Bootstrap Complete: $(date -u) ==="
echo "Service: openclaw-$USER_ID"
echo "Check status: sudo -u openclaw XDG_RUNTIME_DIR=/run/user/1000 systemctl --user status openclaw-$USER_ID"
```

### Security Notes on Env File

The env file at `/opt/openclaw/config/$USER_ID.env` is:
- Owned by `openclaw:openclaw`, mode `0400` (only readable by the openclaw user)
- Read by systemd's `EnvironmentFile` directive and passed to the Docker container
- On encrypted EBS (KMS)
- This is a compromise: ideally secrets would only exist in memory, but systemd needs a way to re-inject env vars on container restart. The env file is the standard systemd pattern for this.
- The file is NOT the openclaw.json config — it's a separate, minimal file with just the secret values.

### 3. Updated Outputs in `terraform/outputs.tf`

Append:
```hcl
output "instance_id" {
  description = "OpenClaw host EC2 instance ID"
  value       = aws_instance.openclaw.id
}

output "ssm_connect_command" {
  description = "Command to connect via SSM"
  value       = "aws ssm start-session --target ${aws_instance.openclaw.id} --region ${var.aws_region} --profile ${var.aws_profile}"
}
```

## Edge Cases

| Scenario | Handling |
|---|---|
| Docker rootless setup fails | Bootstrap script exits with error; check `/var/log/openclaw-bootstrap.log` via SSM |
| Secrets Manager fetch fails | `set -e` causes immediate exit; check bootstrap log |
| OpenClaw image pull fails | Network must be up (NAT instance running); script retries via systemd restart |
| Container crashes on start | systemd `Restart=always` with `RestartSec=10` retries indefinitely |
| Instance reboot | systemd user service enabled + loginctl linger = auto-starts on boot |
| Config needs updating | SSM in, update config, restart systemd unit |
| IMDS access from container | Blocked by `http_put_response_hop_limit = 1` |
| Env file readable by root | Accepted — root on the host can read everything anyway; rootless Docker means Docker daemon runs as openclaw, not root |

## Validation Steps

1. `terraform plan` — should show launch template + EC2 instance
2. `terraform apply` — creates instance
3. Wait ~3-5 minutes for bootstrap to complete
4. Connect via SSM: `aws ssm start-session --target <instance-id>`
5. Check bootstrap log: `cat /var/log/openclaw-bootstrap.log`
6. Check service status: `sudo -u openclaw XDG_RUNTIME_DIR=/run/user/1000 systemctl --user status openclaw-aaron`
7. Check container: `sudo -u openclaw XDG_RUNTIME_DIR=/run/user/1000 DOCKER_HOST=unix:///run/user/1000/docker.sock docker ps`
8. Check Slack: send a message in channel `C0ALL272SV8` and verify response
