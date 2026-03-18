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
- **Web UI via SSM port forwarding** — each user's gateway port is bound to `127.0.0.1` on the host, accessed via SSM tunnel. No ingress rules needed.
- **Secrets off-disk** — API keys from Secrets Manager, injected as container env vars.
- **Encrypted EBS** — KMS-encrypted volumes with `prevent_destroy` on the key.
- **Defense-in-depth** — NACLs + security groups + container hardening (cap-drop ALL, no-new-privileges, resource limits, isolated Docker networks).
- **~$14/mo total** for 2 users.

## For Users — CruxClaw CLI

If you're a user (not managing the infrastructure), you don't need this repo. Install the CLI and get started:

```bash
# Install (macOS Apple Silicon)
curl -sL https://github.com/cruxdigital-llc/crux-claw/releases/latest/download/cruxclaw_0.0.1_darwin_arm64.tar.gz | tar xz
sudo mv cruxclaw /usr/local/bin/

# Also install prerequisites
brew install awscli
brew install --cask session-manager-plugin

# Set up AWS SSO (one time)
aws configure sso --profile openclaw
export AWS_PROFILE=openclaw  # add to ~/.zshrc

# Log in and go
aws sso login
cruxclaw secrets set anthropic-api-key
cruxclaw refresh
cruxclaw connect
# Open http://localhost:18789
```

See [`cli/README.md`](cli/README.md) for full documentation including all commands and SSO setup details.

---

## For Admins — Infrastructure Setup

Everything below is for infrastructure maintainers who manage the Terraform deployment.

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
# Add your user via CLI
cruxclaw admin add-user <your-slack-member-id> <your-slack-channel>

# Set your API key
cruxclaw secrets set anthropic-api-key

# Restart to pick up secrets
cruxclaw refresh
```

See [Adding a New User](#adding-a-new-user) below for full details.

### 5. Verify

```bash
cruxclaw status
cruxclaw logs --lines 20
cruxclaw connect
# Open http://localhost:18789
```

## Adding a New User

### Via CruxClaw CLI (recommended)

```bash
# Admin provisions the user (auto-assigns port, prompts for SSO identity)
cruxclaw admin add-user <SLACK_MEMBER_ID> <SLACK_CHANNEL_ID>

# Then share the user onboarding instructions from cli/README.md
```

The user installs the CLI and follows the [Quick Start](cli/README.md#quick-start) — no repo access needed.

### Via Terraform + shell scripts (alternative)

For admins who prefer to work directly with Terraform:

1. **Get their Slack member ID** — In Slack: click their profile → three dots (⋯) → Copy member ID

2. **Add them to `terraform/variables.tf`**:
   ```hcl
   users = {
     UA13HEGTS = {
       slack_channel = "C0ALL272SV8"
       gateway_port  = 18789
       iam_identity  = "aaronstone"
     }
     NEW_MEMBER_ID = {
       slack_channel = "C0NEWCHANNEL"
       gateway_port  = 18791
       iam_identity  = "newuser"
     }
   }
   ```

3. **Apply**:
   ```bash
   cd terraform
   terraform apply
   terraform apply -replace=aws_instance.openclaw
   ```

4. **User onboards** via CLI: `cruxclaw secrets set anthropic-api-key && cruxclaw refresh`

## Web UI Access

Each user can access the OpenClaw web UI via SSM port forwarding — no VPC ingress changes required.

### Via CruxClaw CLI (recommended)

```bash
cruxclaw connect
```

Opens a tunnel, displays the gateway token, and auto-approves device pairing. Open http://localhost:18789 in your browser.

### Via SSM directly

```bash
aws ssm start-session \
  --target <instance-id> \
  --region us-east-2 \
  --profile openclaw \
  --document-name AWS-StartPortForwardingSession \
  --parameters '{"portNumber":["<your-port>"],"localPortNumber":["18789"]}'
```

### Adding More Secrets Later

```bash
cruxclaw secrets set <secret-name>
cruxclaw refresh
```

## Project Structure

```
├── README.md
├── CLAUDE.md                    # Instructions for Claude Code
├── cli/                         # CruxClaw CLI (Go)
│   ├── README.md                # User-facing install + usage docs
│   ├── cmd/                     # Cobra commands
│   ├── internal/                # AWS clients, discovery, tunnel, UI
│   └── scripts/                 # Embedded bash templates
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
| Secrets Manager (7) | ~$3 |
| CloudWatch | ~$1 |
| **Total** | **~$15** |
