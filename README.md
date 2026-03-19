# OpenClaw on AWS

Hardened, per-user-isolated AWS deployment of [OpenClaw](https://github.com/openclaw/openclaw) (autonomous AI assistant) via Slack. Single EC2 host with per-user Docker containers in a zero-ingress VPC. Infrastructure-as-code — the deliverable is Terraform configuration + bootstrap scripts.

## Architecture

- **Single EC2 host** (t4g.medium, AL2023) with per-user Docker containers
- **Zero ingress** — no SSH, no public ports. Access via AWS SSM only
- **Per-user isolation** — separate Docker networks, secrets, and config per user
- **Cost-optimized** — fck-nat (~$3/mo vs $33/mo NAT Gateway), ~$10/mo total for 2 users
- **Slack event router** — single Socket Mode connection fans out to per-user containers via HTTP

---

## Install the CLI

This is all you need to use OpenClaw as an end user. No Terraform, Go, or repo clone required.

### Prerequisites

- **AWS CLI v2** — [Install guide](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- **session-manager-plugin** — required for `cruxclaw connect`
  - macOS: `brew install --cask session-manager-plugin`
  - Linux/Windows: [AWS install guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
- **AWS SSO access** — your admin will provide the SSO URL and account ID

### Install

```bash
gh release download --repo cruxdigital-llc/crux-claw --pattern "cruxclaw_*_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/').tar.gz" --output - | tar xz -C /usr/local/bin cruxclaw
```

### First-time setup

```bash
# 1. Configure AWS SSO (one time)
aws configure sso --profile your-profile
export AWS_PROFILE=your-profile
aws sso login

# 2. First run — triggers interactive CLI setup
cruxclaw auth status

# 3. Add your API key and connect
cruxclaw secrets set anthropic-api-key
cruxclaw refresh
cruxclaw connect
```

Open http://localhost:18789 in your browser.

---

## Deploy the Infrastructure

This section is for operators deploying or managing the AWS infrastructure. You'll need Terraform and access to the AWS account.

### Prerequisites

- Everything from [Install the CLI](#install-the-cli) above
- **AWS account** with [AWS SSO (Identity Center)](https://aws.amazon.com/iam/identity-center/) configured
- **Terraform** >= 1.5
- **A patched OpenClaw Docker image** — see [Docker Image](#docker-image) below

### 1. Bootstrap Terraform state backend

```bash
export AWS_PROFILE=your-aws-profile
export AWS_REGION=us-east-2

cd terraform
./bootstrap.sh
```

This creates the S3 bucket and DynamoDB table for Terraform state.

### 2. Configure Terraform

```bash
cp backend.tf.example backend.tf
# Edit backend.tf with your account ID, region, profile

cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars — set openclaw_image, add users, etc.
```

### 3. Build and push the Docker image

See [Docker Image](#docker-image) for why this is needed. Once you have your image pushed to ECR (or another registry), set `openclaw_image` in `terraform.tfvars`.

### 4. Deploy

```bash
cd terraform
terraform init
terraform plan
terraform apply
```

### Docker Image

The upstream `ghcr.io/openclaw/openclaw:latest` does **not** work with Slack in HTTP webhook mode without a bugfix from [PR #49514](https://github.com/openclaw/openclaw/pull/49514). Until that PR is merged, you need to build your own image with the fix applied:

```bash
# Clone OpenClaw and apply the fix
git clone https://github.com/openclaw/openclaw.git
cd openclaw
# Cherry-pick or apply the fix from PR #49514

# Build and push to your ECR
docker build -t <account_id>.dkr.ecr.<region>.amazonaws.com/openclaw:latest .
aws ecr get-login-password --region <region> | docker login --username AWS --password-stdin <account_id>.dkr.ecr.<region>.amazonaws.com
docker push <account_id>.dkr.ecr.<region>.amazonaws.com/openclaw:latest
```

Set the image in `terraform.tfvars`:
```hcl
openclaw_image = "<account_id>.dkr.ecr.<region>.amazonaws.com/openclaw:latest"
```

---

## Develop the CLI

This section is for developers building and testing the `cruxclaw` CLI locally.

### Prerequisites

- **Go** >= 1.21
- Everything from [Install the CLI](#install-the-cli) above (for AWS access during testing)

### Build and run

```bash
cd cli
go build -o cruxclaw .
./cruxclaw auth status
```

### Project structure

```
cli/
├── cmd/           # Cobra command definitions
├── internal/      # Internal packages (config, AWS clients, etc.)
├── scripts/       # Embedded shell script templates for remote execution
├── main.go        # Entrypoint
├── go.mod
└── go.sum
```

---

## CLI Commands

### User Commands

| Command | Description |
|---------|-------------|
| `cruxclaw init` | Configure CruxClaw for first use |
| `cruxclaw auth login` | Show SSO setup instructions |
| `cruxclaw auth status` | Show your AWS identity and OpenClaw user |
| `cruxclaw secrets set <name>` | Create or update a secret |
| `cruxclaw secrets list` | List your secrets |
| `cruxclaw secrets delete <name>` | Delete a secret |
| `cruxclaw connect` | Open SSM tunnel to web UI |
| `cruxclaw refresh` | Restart container with fresh secrets |
| `cruxclaw status` | Show container status and resource usage |
| `cruxclaw logs` | Tail container logs |

### Admin Commands

| Command | Description |
|---------|-------------|
| `cruxclaw admin add-user <id> <channel>` | Provision a new user |
| `cruxclaw admin list-users` | Show all provisioned users |
| `cruxclaw admin remove-user <id>` | Remove a user |
| `cruxclaw admin map-user <id> <iam>` | Map IAM identity to Slack member ID |
| `cruxclaw admin cycle-host` | Stop/start the EC2 instance |

### Global Flags

| Flag | Description |
|------|-------------|
| `--profile` | AWS CLI profile (default: `AWS_PROFILE` env var) |
| `--region` | AWS region (default: from config) |
| `--user` | Override auto-detected user |
| `--verbose` | Verbose output |

## How It Works

The CLI discovers infrastructure via AWS APIs — no Terraform access or repo clone needed:

- **Instance**: Found by EC2 tag `Name=openclaw-host`
- **User config**: Stored in SSM Parameter Store at `/openclaw/users/{member_id}`
- **Identity mapping**: Your SSO username maps to your member ID via `/openclaw/users/by-iam/{sso_name}`
- **Secrets**: Managed in AWS Secrets Manager under `openclaw/{member_id}/`
- **Remote operations**: Executed via SSM RunCommand (no SSH, no ingress)
