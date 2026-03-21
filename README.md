# OpenClaw on AWS

Hardened, per-agent-isolated AWS deployment of [OpenClaw](https://github.com/openclaw/openclaw) (autonomous AI assistant) via Slack. Single EC2 host with per-agent Docker containers in a zero-ingress VPC. Terraform manages infrastructure, the CLI manages agents.

## Architecture

- **Single EC2 host** (t4g.medium, AL2023) with per-agent Docker containers
- **Zero ingress** — no SSH, no public ports. Access via AWS SSM only
- **Per-agent isolation** — separate Docker networks, secrets, and config per agent
- **Two agent types** — user agents (DM-only) and team agents (channel-based)
- **Cost-optimized** — fck-nat (~$3/mo vs $33/mo NAT Gateway), ~$10/mo total
- **Slack event router** — single Socket Mode connection fans out to per-agent containers via HTTP
- **SSM-driven bootstrap** — the instance discovers agents from SSM Parameter Store at boot, so CLI-added agents survive instance restarts without Terraform changes

### Separation of Concerns

| Layer | Managed by | What it does |
|-------|-----------|-------------|
| **Infrastructure** | Terraform | VPC, EC2, IAM, router, SNS alerts, setup manifest |
| **Configuration** | CLI (`cruxclaw admin setup`) | Shared secrets, Docker image, deployment settings |
| **Agents** | CLI (`cruxclaw admin add-user/add-team`) | Per-agent containers, configs, routing, secrets |

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

### First-time setup (end users)

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

This section is for operators deploying or managing the AWS infrastructure.

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

### 2. Configure and deploy infrastructure

```bash
cp backend.tf.example backend.tf
# Edit backend.tf with your account ID, region, profile

cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars — set region, profile, project name

terraform init
terraform plan
terraform apply
```

Terraform creates the VPC, EC2 instance, IAM roles, router, and a **setup manifest** in SSM that describes what configuration the deployment needs.

### 3. Configure the deployment

```bash
cruxclaw admin setup
```

This reads the setup manifest from SSM and interactively prompts for:
- **Config values** (stored in SSM) — Docker image URL
- **Shared secrets** (stored in Secrets Manager) — Slack tokens, signing secret, Google OAuth credentials

### 4. Build and push the Docker image

See [Docker Image](#docker-image) for why a custom image is needed. Once pushed, set it via `cruxclaw admin setup` when prompted for the `openclaw-image` config value.

### 5. Add agents

```bash
# Add a user agent (DM-only, restricted to one Slack user)
cruxclaw admin add-user myagent UEXAMPLE01

# Add a team agent (channel-based, accessible to anyone in the channel)
cruxclaw admin add-team leadership CEXAMPLE01

# Verify
cruxclaw admin list-agents
```

### 6. Start everything

```bash
cruxclaw admin cycle-host
```

This restarts the EC2 instance. The bootstrap script discovers all agents from SSM and provisions them automatically.

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
├── internal/      # Internal packages (config, AWS clients, discovery)
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
| `cruxclaw auth status` | Show your AWS identity and agent mapping |
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
| `cruxclaw admin setup` | Configure shared secrets and settings from the deployment manifest |
| `cruxclaw admin add-user <name> <slack_member_id>` | Provision a user agent (DM-only) |
| `cruxclaw admin add-team <name> <slack_channel>` | Provision a team agent (channel-based) |
| `cruxclaw admin list-agents` | List all provisioned agents |
| `cruxclaw admin remove-agent <name>` | Remove an agent |
| `cruxclaw admin cycle-host` | Stop/start the EC2 instance |
| `cruxclaw admin refresh-all` | Restart all agent containers (picks up latest behavior, config, secrets) |

### Global Flags

| Flag | Description |
|------|-------------|
| `--profile` | AWS CLI profile (default: `AWS_PROFILE` env var) |
| `--region` | AWS region (default: from config) |
| `--agent` | Override auto-detected agent name |
| `--verbose` | Verbose output |

## How It Works

The CLI discovers infrastructure via AWS APIs — no Terraform access or repo clone needed:

- **Instance**: Found by EC2 tag `Name=openclaw-host`
- **Agent config**: Stored in SSM Parameter Store at `/openclaw/agents/{name}`
- **Identity mapping**: Your SSO username matches the `iam_identity` field in your agent's SSM config
- **Secrets**: Managed in AWS Secrets Manager under `openclaw/agents/{name}/`
- **Shared config**: Setup manifest at `/openclaw/config/setup-manifest`, image at `/openclaw/config/openclaw-image`
- **Remote operations**: Executed via SSM RunCommand (no SSH, no ingress)
- **Bootstrap discovery**: On instance boot, the bootstrap script reads all agents from `/openclaw/agents/` in SSM and provisions them — no Terraform template loops
