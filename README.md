# OpenClaw AWS Deployment

Hardened AWS infrastructure for running [OpenClaw](https://github.com/openclaw/openclaw) — an open-source, self-hosted autonomous AI assistant — securely for a small team.

## Architecture

```
                         ┌─────────────────────────────────────────────────────────────┐
                         │  EC2 t4g.medium  (AL2023, private subnet, SSM-only access)  │
                         │                                                             │
  Slack Cloud ──WSS──▶   │  ┌──────────────────────────────────┐                       │
                         │  │  Router Container (Socket Mode)  │                       │
                         │  │  • Receives all Slack events      │                       │
                         │  │  • Filters bot echo / subtypes   │                       │
                         │  │  • Routes by channel or member ID │                       │
                         │  └──────┬───────────────┬───────────┘                       │
                         │         │               │                                   │
                         │    HTTP POST       HTTP POST                                │
                         │    (signed)        (signed)                                 │
                         │         ▼               ▼                                   │
                         │  ┌─────────────┐ ┌─────────────┐                            │
                         │  │  OpenClaw    │ │  OpenClaw    │  Per-user containers:     │
                         │  │  User A      │ │  User B      │  • Isolated Docker net   │
                         │  │  :18789      │ │  :18789      │  • cap-drop ALL          │
                         │  └─────────────┘ └─────────────┘  • Secrets via env vars   │
                         │                                   • 2GB mem / 1.5 CPU      │
                         │                                                             │
                         │  Encrypted EBS (KMS) ◄── data volumes                       │
                         └───────────────────────────┬─────────────────────────────────┘
                                                     │
                                                     ▼
                                          fck-nat (t4g.nano, ASG)
                                                     │
                                                     ▼
                                             Internet (443 only)
                                          ┌──────────┴──────────┐
                                          │                     │
                                     Anthropic API      Slack API / ECR
```

### Key design decisions

- **Router + per-user containers** — Slack Socket Mode load-balances across connections to the same app, so each event must enter through a single router that fans out via HTTP to the correct user container.
- **Zero ingress** — the security group has no inbound rules. All connections are outbound-initiated (WSS, HTTPS).
- **No SSH** — access via AWS SSM Session Manager only.
- **Secrets off-disk** — API keys from Secrets Manager, injected as container env vars.
- **Encrypted EBS** — KMS-encrypted volumes with `prevent_destroy` on the key.
- **Defense-in-depth** — NACLs + security groups + container hardening (cap-drop ALL, no-new-privileges, resource limits, isolated Docker networks).
- **~$14/mo total** for 2 users.

## Quick Start

### Prerequisites

- AWS CLI configured with profile `openclaw`
- Terraform >= 1.5
- A Slack app with bot and app-level tokens

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
├── router/                      # Slack event router
│   ├── package.json
│   └── src/index.js             # Socket Mode → HTTP event fan-out
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
    ├── data.tf                  # Data sources + shared locals
    ├── vpc.tf                   # VPC, subnets, IGW, route tables
    ├── nat.tf                   # fck-nat module
    ├── security.tf              # Security group + NACLs
    ├── flow-logs.tf             # VPC Flow Logs
    ├── iam.tf                   # Instance role + scoped policies
    ├── kms.tf                   # EBS encryption key (prevent_destroy)
    ├── ecr.tf                   # Container registry (prevent_destroy)
    ├── secrets.tf               # Secrets Manager entries
    ├── compute.tf               # EC2 instance + launch template
    ├── router.tf                # S3 objects for router + bootstrap
    ├── monitoring.tf            # CloudWatch dashboard + alarms
    └── user-data.sh.tftpl       # Cloud-init bootstrap script
```

## Security

See [product-knowledge/standards/security.md](product-knowledge/standards/security.md) for the full security standards including:

- Network controls (zero ingress, HTTPS-only egress, NACLs)
- Container hardening (cap-drop ALL, no-new-privileges, resource limits, isolated Docker networks)
- Configuration integrity (hash-check monitoring)
- IAM least-privilege with scoped ECR/S3 policies and explicit deny-dangerous policy
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
