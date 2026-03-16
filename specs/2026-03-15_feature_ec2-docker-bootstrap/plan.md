# Plan: EC2 + Docker Bootstrap

## Overview
Deploy a t4g.medium instance with AL2023, bootstrap Docker in rootless mode via user-data, generate Aaron's openclaw.json from secrets, and run the OpenClaw container with full hardening. Persistent state stored on an EBS-backed path.

## File Structure

New/modified files in `terraform/`:
```
terraform/
├── ...existing files...
├── compute.tf              # EC2 instance, launch template, AMI lookup
├── user-data.sh.tftpl      # Cloud-init bootstrap script (templatefile)
└── openclaw-config.json.tftpl  # OpenClaw config template
```

## Step 1: AMI Lookup + Launch Template (`compute.tf`)

- Data source: latest AL2023 arm64 AMI from Amazon
- Launch template:
  - Instance type: t4g.medium (4GB RAM, 2 vCPU Graviton)
  - Encrypted EBS root volume (20GB gp3, KMS key from Epic 2)
  - IMDSv2 enforced, hop limit 1
  - Instance profile from Epic 2
  - Security group from Epic 1
  - Private subnet from Epic 1
  - User-data: templatefile rendering of bootstrap script
- EC2 instance from launch template

## Step 2: Bootstrap Script (`user-data.sh.tftpl`)

Cloud-init bash script that runs on first boot. Sequence:

### 2a. OS Hardening
- Remove openssh-server
- Enable unattended-upgrades equivalent (dnf-automatic on AL2023)
- Sysctl hardening:
  - `net.ipv4.ip_forward=1` (required for Docker NAT — but restricted to Docker bridge)
  - `net.ipv4.conf.all.send_redirects=0`
  - `net.ipv4.conf.all.accept_redirects=0`
  - `net.ipv4.tcp_syncookies=1`
  - `net.ipv6.conf.all.disable_ipv6=1`

### 2b. Install Docker
- Install Docker via `dnf install docker`
- Install rootless Docker prerequisites: `uidmap`, `fuse-overlayfs`
- Create `openclaw` system user (uid 1000)
- Set up Docker rootless for the `openclaw` user:
  - `loginctl enable-linger openclaw`
  - Run `dockerd-rootless-setuptool.sh install` as openclaw user
  - Configure rootless Docker socket path

### 2c. Fetch Secrets
- Use instance role to fetch all secrets from Secrets Manager
- Store in shell variables (never write to disk):
  ```bash
  SLACK_BOT_TOKEN=$(aws secretsmanager get-secret-value --secret-id openclaw/shared/slack-bot-token --query SecretString --output text)
  SLACK_APP_TOKEN=$(aws secretsmanager get-secret-value --secret-id openclaw/shared/slack-app-token --query SecretString --output text)
  ANTHROPIC_API_KEY=$(aws secretsmanager get-secret-value --secret-id openclaw/aaron/anthropic-api-key --query SecretString --output text)
  TRELLO_API_KEY=$(aws secretsmanager get-secret-value --secret-id openclaw/aaron/trello-api-key --query SecretString --output text)
  TRELLO_TOKEN=$(aws secretsmanager get-secret-value --secret-id openclaw/aaron/trello-token --query SecretString --output text)
  ```

### 2d. Generate Config
- Write openclaw.json from template, substituting secrets for Slack tokens
- Set ownership to root:root, mode 0444 (read-only to openclaw user)
- Config placed at `/opt/openclaw/config/openclaw.json`

### 2e. Create Persistent Storage
- Create directory `/opt/openclaw/data/aaron/` for workspace and memory
- Owned by openclaw user (uid 1000)
- This lives on the root EBS volume (encrypted via KMS)

### 2f. Create Docker Network
- Isolated bridge network for Aaron: `openclaw-aaron`
- No inter-container communication (only one container for now, but sets the pattern)

### 2g. Create Systemd Unit
- Systemd user unit for the openclaw user (rootless Docker)
- Or: systemd system unit that runs docker as the openclaw user
- Unit configuration:
  ```ini
  [Service]
  ExecStart=/usr/bin/docker run \
    --name openclaw-aaron \
    --network openclaw-aaron \
    --read-only \
    --tmpfs /tmp:rw,noexec,nosuid \
    --cap-drop ALL \
    --security-opt no-new-privileges \
    --memory 1536m \
    --cpus 1.5 \
    --pids-limit 256 \
    -v /opt/openclaw/config/openclaw.json:/home/node/.openclaw/openclaw.json:ro \
    -v /opt/openclaw/data/aaron:/home/node/.openclaw/data:rw \
    -e ANTHROPIC_API_KEY \
    -e SLACK_BOT_TOKEN \
    -e SLACK_APP_TOKEN \
    -e TRELLO_API_KEY \
    -e TRELLO_TOKEN \
    ghcr.io/openclaw/openclaw:latest
  Restart=always
  RestartSec=10
  ```
- Hardening directives:
  - `NoNewPrivileges=true`
  - `ProtectSystem=strict`
  - `MemoryMax=1800M` (systemd-level cap)

### 2h. Start Service
- Enable and start the openclaw systemd unit
- Verify container is running

## Step 3: Config Template (`openclaw-config.json.tftpl`)

Based on Aaron's local config, with adjustments for AWS:
- Slack tokens injected via environment variables (OpenClaw reads from env)
- Gateway bind set to loopback
- Workspace path adjusted to container path
- Channel allowlist: C0ALL272SV8
- Tools, skills, hooks preserved from local config

Key question: Does OpenClaw read Slack tokens from environment variables, or only from the JSON config? This determines whether we put tokens in the config file or pass them as env vars to the container.

## Step 4: Outputs

- Instance ID
- Instance private IP (for SSM reference)

## Architect Review

- **Docker rootless on AL2023**: AL2023 ships Docker but rootless setup requires additional packages (uidmap, fuse-overlayfs) and user session setup (loginctl enable-linger). More complex than standard Docker but eliminates root-level container escape risk.
- **Secrets in env vars**: Docker `-e` flags pass env vars to the container. They're visible in `/proc/[pid]/environ` on the host but only to root and the process owner. With rootless Docker, the Docker daemon runs as the openclaw user, so only that user can inspect the env vars.
- **Config as templatefile**: Using Terraform's `templatefile()` to render the openclaw.json means the config content (with Slack channel IDs, model settings, etc.) is in the Terraform plan. No secrets in the template — those come from env vars.
- **Single EBS volume**: Workspace/memory lives on the root EBS volume under `/opt/openclaw/data/`. Simpler than attaching a second EBS volume. The root volume is KMS-encrypted so data at rest is protected.
- **Container restart policy**: `Restart=always` in systemd means the container recovers from crashes automatically. Combined with the fck-nat ASG self-healing, the system is resilient without manual intervention.
