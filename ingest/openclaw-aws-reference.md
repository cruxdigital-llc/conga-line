# OpenClaw Hardened AWS Deployment — Reference Document

> **Generated from Claude conversation, March 15 2026.**
> Feed this file to Claude Code as context: `claude --context openclaw-aws-reference.md`

---

## 1. What is OpenClaw

OpenClaw (formerly Clawdbot/Moltbot) is an open-source, self-hosted autonomous AI assistant created by Peter Steinberger. It runs as a Node.js gateway process that connects messaging platforms (Slack, WhatsApp, Telegram, Discord, Signal, etc.) to LLM backends (Anthropic Claude, OpenAI GPT, local models). It executes real tasks — shell commands, file management, web automation, API calls — via a skills/plugin system. It stores configuration and memory locally, runs a WebSocket control plane on port 18789, and supports Docker sandboxing for agent tool execution.

### Key technical facts

- **Runtime**: Node.js ≥22, gateway listens on `ws://127.0.0.1:18789`
- **Docker image**: `ghcr.io/openclaw/openclaw:latest`, runs as non-root `node` user (uid 1000)
- **Minimum RAM**: 2 GB (for gateway + Docker sandbox)
- **Config path**: `~/.openclaw/openclaw.json` (JSON5, hot-reloadable)
- **Slack integration**: Socket Mode (default, recommended) — uses outbound WebSocket via `xapp-` App Token + `xoxb-` Bot Token. No public URL needed.
- **Slack scopes needed**: `chat:write`, `channels:history`, `channels:read`, `im:write`, `im:history`, `im:read`, `users:read`, `reactions:read`, `reactions:write`, `files:write`
- **Security risks**: Prompt injection, malicious skills, credential exposure, unrestricted shell access. Cisco found third-party skills performing data exfiltration. One maintainer warned it's "far too dangerous" for users who can't understand command-line security.

### Slack Socket Mode architecture

```
Slack Cloud ←──WSS──→ OpenClaw Gateway (outbound-initiated)
                       ws://127.0.0.1:18789

Config:
{
  "channels": {
    "slack": {
      "enabled": true,
      "mode": "socket",
      "appToken": "xapp-...",
      "botToken": "xoxb-...",
      "groupPolicy": "allowlist",
      "dm": { "policy": "pairing" },
      "requireMention": true
    }
  }
}
```

Socket Mode means OpenClaw initiates the WebSocket to Slack's relay servers — **no inbound ports required**. This is the foundation of the zero-ingress security model.

---

## 2. Architecture Design

### Design principles

1. **Per-user isolation**: Each user gets their own VPC, IAM role, KMS key, and EC2 instance
2. **Zero inbound traffic**: Security group has literally zero ingress rules
3. **No SSH**: Port 22 doesn't exist; access is via AWS SSM Session Manager
4. **Secrets off-disk**: API keys injected at boot from Secrets Manager, never written to filesystem
5. **Encrypted everything**: EBS volumes encrypted with per-user KMS key
6. **Least-privilege IAM**: Instance role scoped to user-specific resource ARN paths
7. **Defense-in-depth**: NACLs + SGs + systemd hardening + Docker isolation + OS hardening
8. **Small & cheap**: t3.small, single-AZ, /28 subnets, minimal footprint

### Network topology

```
┌─────────────────────────────────────────────────────────────────┐
│  SLACK WORKSPACE (Socket Mode WSS — outbound only)             │
│  LLM API (Anthropic/OpenAI — outbound HTTPS only)              │
└────────────────────────┬────────────────────────────────────────┘
                         │ egress 443
┌────────────────────────▼────────────────────────────────────────┐
│  VPC  10.0.0.0/24                                              │
│                                                                 │
│  ┌──────────────────┐    ┌────────────────────────────────────┐ │
│  │ PUBLIC /28       │    │ PRIVATE /28                        │ │
│  │                  │    │                                    │ │
│  │  NAT Gateway ◄───┼────┤  EC2 t3.small (no public IP)      │ │
│  │  (Elastic IP)    │    │   └─ Docker: OpenClaw Gateway      │ │
│  │                  │    │   └─ Docker: Agent Sandbox          │ │
│  └──────────────────┘    │   └─ SSM Agent (no SSH)            │ │
│                          └────────────────────────────────────┘ │
│                                                                 │
│  VPC Endpoints: ssm, ssmmessages, ec2messages,                  │
│                 secretsmanager, logs, s3 (gateway)              │
└─────────────────────────────────────────────────────────────────┘
         │                          │
   ┌─────▼──────┐          ┌───────▼────────┐
   │ CloudWatch │          │ Secrets Manager│
   │  Logs      │          │  API keys      │
   └────────────┘          └────────────────┘
```

### Security controls summary

| Control                    | Implementation                                      |
|----------------------------|------------------------------------------------------|
| No inbound traffic         | Security group has zero ingress rules                 |
| No SSH                     | Port 22 closed; access via SSM Session Manager only   |
| No public IP               | Instance in private subnet, NAT for egress            |
| Secrets off-disk           | Injected at boot from Secrets Manager via user-data   |
| Encrypted storage          | EBS volumes encrypted with per-user KMS key           |
| Non-root container         | OpenClaw Docker runs as uid 1000                      |
| Tight NACLs                | Only 443 outbound + ephemeral return                  |
| VPC Flow Logs              | All traffic logged to CloudWatch                      |
| Least-privilege IAM        | Instance role scoped to user-specific secret paths    |
| Per-user isolation         | Separate VPC, SG, IAM role per user_id                |
| IMDSv2 enforced            | Hop limit 1 prevents container SSRF to metadata       |
| Explicit IAM denies        | Role denies iam:*, ec2:RunInstances, lambda:*, etc.   |
| Systemd hardening          | NoNewPrivileges, ProtectSystem=strict, MemoryMax      |
| Auto security updates      | unattended-upgrades enabled                           |
| OS hardening               | sysctl: no IP forward, no ICMP redirect, no IPv6      |

### Estimated monthly cost (us-east-1)

| Resource       | Est. Cost |
|----------------|-----------|
| t3.small       | ~$15      |
| NAT Gateway    | ~$32      |
| EBS 20 GB gp3  | ~$1.60    |
| Secrets (4)    | ~$1.60    |
| CloudWatch     | ~$3       |
| VPC Endpoints  | ~$7–14    |
| **Total**      | **~$60–67/mo** |

For multi-user, a shared NAT via Transit Gateway hub VPC saves ~$30/user.

---

## 3. Terraform Configuration

### File structure

```
terraform-openclaw/
├── main.tf                    # All resources (~710 lines)
├── variables.tf               # Input variables with validation
├── outputs.tf                 # SSM connect command, IPs, ARNs
├── user-data.sh.tftpl         # Cloud-init bootstrap script
├── terraform.tfvars.example   # Example variable values
├── multi-user.tf.example      # Workspace-based multi-user pattern
└── .gitignore
```

### Quick start

```bash
cd terraform-openclaw
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars OR set env vars:
export TF_VAR_anthropic_api_key="sk-ant-..."
export TF_VAR_slack_app_token="xapp-..."
export TF_VAR_slack_bot_token="xoxb-..."

terraform init
terraform plan
terraform apply
```

### Multi-user via workspaces

```bash
terraform workspace new user-alice
terraform apply -var-file=users/alice.tfvars

terraform workspace new user-bob
terraform apply -var-file=users/bob.tfvars
```

### Resources created (per user)

- 1 VPC + IGW
- 1 public subnet (/28) + route table
- 1 private subnet (/28) + route table
- 1 NAT Gateway + Elastic IP
- 1 Network ACL (private subnet, stateless 443-only)
- 1 Security Group (zero inbound, 443 outbound)
- 6 VPC Endpoints (ssm, ssmmessages, ec2messages, secretsmanager, logs, s3)
- 1 VPC Endpoint security group
- 1 KMS key + alias
- 1 IAM role + instance profile + 4 policies (SSM, secrets, logs, deny-dangerous)
- 4 Secrets Manager secrets (anthropic key, slack app token, slack bot token, gateway token)
- 1 EC2 instance (t3.small, private subnet, encrypted EBS, IMDSv2)
- 1 CloudWatch log group (gateway logs)
- Optional: VPC flow logs (log group + IAM role)
- Optional: idle-shutdown CloudWatch alarm

---

## 4. Key Implementation Details

### Security Group (zero inbound)

```hcl
resource "aws_security_group" "openclaw" {
  # ZERO INBOUND RULES — no SSH, no HTTP, no WebSocket ingress
  # Access exclusively via SSM Session Manager

  egress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]  # Slack WSS, LLM APIs, npm
  }

  egress {
    from_port   = 53
    to_port     = 53
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]  # DNS within VPC
  }

  egress {
    from_port   = 53
    to_port     = 53
    protocol    = "udp"
    cidr_blocks = [var.vpc_cidr]
  }
}
```

### IAM deny-dangerous policy

```hcl
resource "aws_iam_role_policy" "deny_dangerous" {
  policy = jsonencode({
    Statement = [{
      Effect = "Deny"
      Action = [
        "iam:*",
        "organizations:*",
        "sts:AssumeRole",
        "ec2:RunInstances",
        "ec2:CreateVpc",
        "ec2:CreateSecurityGroup",
        "ec2:AuthorizeSecurityGroupIngress",
        "lambda:*",
        "s3:DeleteBucket",
        "s3:PutBucketPolicy",
      ]
      Resource = "*"
    }]
  })
}
```

### Secrets injection (user-data pattern)

```bash
# Fetched at boot, injected as systemd env vars, never written to config files
ANTHROPIC_API_KEY=$(aws secretsmanager get-secret-value \
  --secret-id "openclaw/${USER_ID}/anthropic-api-key" \
  --query SecretString --output text)

# Systemd unit uses Environment= directives (in-memory only)
```

### IMDSv2 enforcement

```hcl
metadata_options {
  http_endpoint               = "enabled"
  http_tokens                 = "required"  # IMDSv2 only
  http_put_response_hop_limit = 1           # Prevent container escape to IMDS
}
```

### OS hardening (user-data)

- SSH server removed entirely (`apt-get remove openssh-server`)
- sysctl: IP forwarding disabled, ICMP redirects ignored, SYN cookies enabled, IPv6 disabled
- Automatic security updates via `unattended-upgrades`
- Systemd: `NoNewPrivileges=true`, `ProtectSystem=strict`, `MemoryMax=1800M`, `TasksMax=256`

---

## 5. Slack Setup Checklist

1. Go to api.slack.com/apps → Create New App → From scratch
2. Enable Socket Mode → generate App-Level Token (`xapp-...`) with `connections:write`
3. Subscribe to bot events: `app_mention`, `message.channels`, `message.groups`, `message.im`, `message.mpim`
4. OAuth & Permissions → add scopes: `chat:write`, `channels:history`, `channels:read`, `im:write`, `im:history`, `im:read`, `users:read`
5. Install to workspace → copy Bot Token (`xoxb-...`)
6. Pass both tokens as `TF_VAR_slack_app_token` and `TF_VAR_slack_bot_token`
7. After deploy, connect via SSM and run: `sudo -u openclaw openclaw pairing approve slack <CODE>`

---

## 6. Operational Runbook

### Connect to instance
```bash
aws ssm start-session --target <instance-id> --region us-east-1
```

### Check OpenClaw status
```bash
sudo -u openclaw openclaw status
sudo -u openclaw openclaw doctor
journalctl -u openclaw.service -f
```

### View logs
```bash
aws logs tail /openclaw/<user-id>/gateway --follow
```

### Rotate secrets
```bash
# Update in Secrets Manager, then restart the service
aws secretsmanager put-secret-value --secret-id openclaw/<user-id>/anthropic-api-key --secret-string "new-key"
# SSM into instance, then:
sudo systemctl restart openclaw
```

### Tear down a user
```bash
terraform workspace select user-alice
terraform destroy
```

---

## 7. Future Improvements to Consider

- **Shared NAT Gateway** via Transit Gateway hub VPC for multi-user cost reduction
- **Graviton instances** (t4g.small) for ~20% cost savings
- **WAF on NAT** or explicit egress proxy (e.g., Squid) to domain-allowlist outbound traffic
- **AWS Config rules** to detect security group drift
- **GuardDuty** for anomaly detection on VPC flow logs
- **Backup/restore** of OpenClaw memory/config via scheduled EBS snapshots
- **Blue/green deploys** via launch template versioning
- **Terraform remote state** in S3 + DynamoDB lock table for team use
