# OpenClaw AWS Deployment

Hardened AWS infrastructure for running [OpenClaw](https://github.com/openclaw/openclaw) — an open-source, self-hosted autonomous AI assistant — securely for a small team.

## Architecture

```
Slack Cloud ←──WSS──→ OpenClaw Container (outbound-initiated, Socket Mode)
                       ↕
                    EC2 t4g.medium (AL2023, private subnet)
                       │ Docker: per-user isolated containers
                       │ Secrets: env vars from Secrets Manager
                       │ Config: read-only openclaw.json (no secrets)
                       │ Access: SSM only (no SSH, no public IP)
                       ↕
                    fck-nat (t4g.nano, ASG) → Internet (443 only)
```

- **Single EC2 host** with per-user Docker containers
- **Zero ingress** — security group has no inbound rules
- **No SSH** — access via AWS SSM Session Manager only
- **Secrets off-disk** — API keys from Secrets Manager, injected as container env vars
- **Encrypted EBS** — KMS-encrypted root volume
- **Defense-in-depth** — NACLs + security groups + container hardening (cap-drop ALL, no-new-privileges, resource limits)
- **~$10/mo total** for 2 users

## Quick Start

### Prerequisites

- AWS CLI configured with profile `123456789012_AdministratorAccess`
- Terraform >= 1.5
- An OpenClaw Slack app (Socket Mode) with bot and app tokens

### 1. Bootstrap State Backend

```bash
cd terraform
./bootstrap.sh
```

Creates the S3 bucket and DynamoDB table for Terraform state.

### 2. Deploy Infrastructure

```bash
terraform init
terraform plan
terraform apply
```

### 3. Populate Secrets

```bash
./populate-secrets.sh
```

Prompts for Slack tokens, Anthropic API key, and skill credentials.

### 4. Connect via SSM

```bash
aws ssm start-session --target <instance-id> --region us-east-2 --profile 123456789012_AdministratorAccess
```

### 5. Verify

```bash
# On the instance via SSM:
docker ps
systemctl status openclaw-myagent
docker logs openclaw-myagent --tail 20
```

## Project Structure

```
├── README.md
├── CLAUDE.md                    # Instructions for Claude Code
├── product-knowledge/           # GLaDOS planning docs
│   ├── MISSION.md
│   ├── ROADMAP.md
│   ├── TECH_STACK.md
│   ├── PROJECT_STATUS.md
│   ├── standards/security.md
│   └── ...
├── specs/                       # Feature specs and trace logs
└── terraform/                   # All infrastructure code
    ├── bootstrap.sh             # One-time state backend setup
    ├── populate-secrets.sh      # Interactive secret population
    ├── backend.tf               # S3 state backend
    ├── providers.tf             # AWS provider config
    ├── variables.tf             # Input variables
    ├── outputs.tf               # Output values
    ├── vpc.tf                   # VPC, subnets, IGW, route tables
    ├── nat.tf                   # fck-nat module
    ├── security.tf              # Security group + NACLs
    ├── flow-logs.tf             # VPC Flow Logs
    ├── iam.tf                   # Instance role + policies
    ├── kms.tf                   # EBS encryption key
    ├── secrets.tf               # Secrets Manager entries
    ├── compute.tf               # EC2 instance + launch template
    ├── user-data.sh.tftpl       # Cloud-init bootstrap script
    └── data.tf                  # Data sources
```

## Security

See [product-knowledge/standards/security.md](product-knowledge/standards/security.md) for the full security standards including:

- Network controls (zero ingress, HTTPS-only egress, NACLs)
- Container hardening (cap-drop ALL, no-new-privileges, resource limits, isolated networks)
- Configuration integrity (hash-check monitoring)
- IAM least-privilege with explicit deny-dangerous policy
- Isolation upgrade path (standard Docker → gVisor → per-user subnets → per-user VPCs)

## Cost

| Resource | Monthly |
|---|---|
| EC2 t4g.medium | ~$6 |
| fck-nat t4g.nano | ~$3 |
| EBS 20GB gp3 | ~$1.60 |
| Secrets Manager (5) | ~$2 |
| CloudWatch | ~$1 |
| **Total** | **~$14** |
