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

- AWS CLI configured with profile `openclaw`
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

### 3. Populate Shared Secrets

```bash
./populate-secrets.sh
```

Prompts for the shared Slack bot and app tokens.

### 4. Onboard Yourself

```bash
../scripts/onboard-user.sh <your-slack-member-id> <your-aws-profile> us-east-2
```

See [Adding a New User](#adding-a-new-user) below for full details.

### 5. Verify

```bash
# Connect via SSM:
aws ssm start-session --target <instance-id> --region us-east-2 --profile openclaw

# On the instance:
docker ps
systemctl status openclaw-<member-id>
docker logs openclaw-<member-id> --tail 20
```

## Adding a New User

Users are identified by their Slack member ID (e.g., `UEXAMPLE01`). Adding a user is a two-step process split between admin and user.

### Admin Steps

1. **Create a Slack channel** for the new user

2. **Get their Slack member ID** — In Slack: click their profile → three dots (⋯) → Copy member ID

3. **Add them to `terraform/variables.tf`** (or a `.tfvars` file):
   ```hcl
   users = {
     UEXAMPLE01 = {
       slack_channel = "CEXAMPLE01"
     }
     UEXAMPLE02 = {
       slack_channel = "CEXAMPLE02"
     }
   }
   ```

4. **Apply**:
   ```bash
   cd terraform
   terraform apply
   # Then replace the instance to pick up the new user-data:
   terraform apply -replace=aws_instance.openclaw
   ```
   This will briefly restart all containers.

5. **Share onboarding instructions** with the new user (see below).

### User Steps

The new user needs an IAM user in the AWS account with permissions to create secrets under their path.

1. **Configure AWS CLI** with your credentials:
   ```bash
   aws configure --profile my-profile
   ```

2. **Run the onboarding script**:
   ```bash
   ./scripts/onboard-user.sh <your-member-id> <your-aws-profile> us-east-2
   ```
   The script will prompt for:
   - **Anthropic API key** (required)
   - **Any additional secrets** for skills you use (e.g., `trello-api-key`, `trello-token`)

   Secret names are converted to env vars: `trello-api-key` → `TRELLO_API_KEY`

3. **Ask your admin to restart your container**:
   ```bash
   # Admin runs via SSM:
   systemctl restart openclaw-<member-id>
   ```

4. **Test** — send a message in your Slack channel

### Adding More Secrets Later

Users can add secrets at any time by re-running the onboarding script or directly:
```bash
aws secretsmanager create-secret \
  --name openclaw/<member-id>/<secret-name> \
  --secret-string "<value>" \
  --profile <your-profile> \
  --region us-east-2
```
Then ask an admin to restart your container to pick up the new secret.

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
├── scripts/
│   └── onboard-user.sh          # Self-service user onboarding
├── specs/                       # Feature specs and trace logs
└── terraform/                   # All infrastructure code
    ├── bootstrap.sh             # One-time state backend setup
    ├── populate-secrets.sh      # Shared secret population (admin)
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
