# OpenClaw on AWS

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-%3E%3D1.25-00ADD8.svg)](cli/)
[![Terraform](https://img.shields.io/badge/Terraform-%3E%3D1.5-7B42BC.svg)](terraform/)

Deploy and manage clusters of autonomous AI agents on hardened AWS infrastructure. Each agent runs in its own isolated Docker container with dedicated secrets, networking, and access controls — giving teams and enterprises granular permission management over their AI workforce.

## Key Features

- **Zero-ingress networking** — no SSH, no public ports; all access through AWS SSM
- **Per-agent isolation** — separate Docker containers, networks, secrets, and config
- **Two agent types** — user agents (DM-only) for individuals, team agents (channel-based) for groups
- **SSM-driven discovery** — agents are registered in Parameter Store and provisioned automatically at boot, no Terraform changes needed to add or remove agents
- **Slack event router** — single Socket Mode connection fans out to per-agent containers via HTTP webhook
- **Cost-optimized** — fck-nat (~$3/mo vs $33/mo NAT Gateway) on a single t4g.medium host; ~$10/mo total
- **CLI for everything** — operators and end users manage agents, secrets, and infrastructure through the `cruxclaw` CLI

## Architecture

```
                        +---------------------+
                        |     AWS Cloud        |
                        |  (zero-ingress VPC)  |
                        |                      |
  Slack API <-----------+----> Router          |
  (Socket Mode)         |       |              |
                        |       +-> Agent A    |
                        |       |   (container)|
                        |       +-> Agent B    |
                        |       |   (container)|
                        |       +-> Agent N    |
                        |           (container)|
                        |                      |
  Operator/User <--SSM--+----> EC2 Host        |
                        +---------------------+
```

### Separation of Concerns

| Layer | Managed by | What it does |
|-------|-----------|-------------|
| **Infrastructure** | Terraform | VPC, EC2, IAM, router, SNS alerts, setup manifest |
| **Configuration** | CLI (`cruxclaw admin setup`) | Shared secrets, Docker image, deployment settings |
| **Agents** | CLI (`cruxclaw admin add-user/add-team`) | Per-agent containers, configs, routing, secrets |

## Quick Start (Operators)

### Prerequisites

- **AWS account** with [AWS SSO (Identity Center)](https://aws.amazon.com/iam/identity-center/) configured
- **AWS CLI v2** with **session-manager-plugin** installed
- **Terraform** >= 1.5
- **A patched OpenClaw Docker image** (see [Docker Image](#docker-image))

### 1. Bootstrap Terraform state

```bash
export AWS_PROFILE=your-aws-profile
export AWS_REGION=us-east-2

cd terraform
./bootstrap.sh
```

### 2. Deploy infrastructure

```bash
cp backend.tf.example backend.tf    # edit with your account ID, region, profile
cp terraform.tfvars.example terraform.tfvars  # edit with your settings

terraform init
terraform plan
terraform apply
```

### 3. Configure the deployment

```bash
cruxclaw admin setup
```

This reads the setup manifest from SSM and prompts for shared secrets (Slack tokens, signing secret, Google OAuth) and config values (Docker image URL).

### 4. Build and push the Docker image

See [Docker Image](#docker-image) for details. Set the image URL when prompted by `cruxclaw admin setup`.

### 5. Add agents and start

```bash
cruxclaw admin add-user myagent UEXAMPLE01
cruxclaw admin add-team leadership CEXAMPLE01
cruxclaw admin list-agents

cruxclaw admin cycle-host   # restarts EC2; bootstrap discovers and provisions all agents
```

## Install the CLI (End Users)

No Terraform, Go, or repo clone required.

### Prerequisites

- **AWS CLI v2** — [Install guide](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- **session-manager-plugin** — macOS: `brew install --cask session-manager-plugin` | [Other platforms](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
- **AWS SSO access** — your admin will provide the SSO URL and account ID

### Install

**macOS (Apple Silicon)** — tested:
```bash
curl -fsSL https://github.com/cruxdigital-llc/crux-claw/releases/latest/download/cruxclaw_darwin_arm64.tar.gz | tar xz -C /usr/local/bin cruxclaw
```

**macOS (Intel)**:
```bash
curl -fsSL https://github.com/cruxdigital-llc/crux-claw/releases/latest/download/cruxclaw_darwin_amd64.tar.gz | tar xz -C /usr/local/bin cruxclaw
```

**Linux (amd64)** — untested:
```bash
curl -fsSL https://github.com/cruxdigital-llc/crux-claw/releases/latest/download/cruxclaw_linux_amd64.tar.gz | tar xz -C /usr/local/bin cruxclaw
```

**Linux (arm64)** — untested:
```bash
curl -fsSL https://github.com/cruxdigital-llc/crux-claw/releases/latest/download/cruxclaw_linux_arm64.tar.gz | tar xz -C /usr/local/bin cruxclaw
```

### First-time setup

```bash
aws configure sso --profile your-profile
export AWS_PROFILE=your-profile
aws sso login

cruxclaw auth status        # triggers interactive CLI setup
cruxclaw secrets set anthropic-api-key
cruxclaw refresh
cruxclaw connect            # opens SSM tunnel to web UI
```

Open http://localhost:18789 in your browser.

## CLI Reference

### User Commands

| Command | Description |
|---------|-------------|
| `cruxclaw auth login` | Authenticate via AWS SSO |
| `cruxclaw auth status` | Show your AWS identity and agent mapping |
| `cruxclaw secrets set <name>` | Create or update a secret |
| `cruxclaw secrets list` | List your secrets |
| `cruxclaw secrets delete <name>` | Delete a secret |
| `cruxclaw connect` | Open SSM tunnel to web UI |
| `cruxclaw refresh` | Restart container with fresh secrets |
| `cruxclaw status` | Show container status and resource usage |
| `cruxclaw logs` | Tail container logs |
| `cruxclaw version` | Show CLI version |

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
- **Bootstrap discovery**: On instance boot, the bootstrap script reads all agents from `/openclaw/agents/` in SSM and provisions them automatically

## Docker Image

The upstream OpenClaw image does not support Slack HTTP webhook mode without the fix from [PR #49514](https://github.com/openclaw/openclaw/pull/49514). Until merged upstream, build a custom image:

```bash
git clone https://github.com/openclaw/openclaw.git
cd openclaw
# Cherry-pick or apply the fix from PR #49514

docker build -t <account_id>.dkr.ecr.<region>.amazonaws.com/openclaw:latest .
aws ecr get-login-password --region <region> | docker login --username AWS --password-stdin <account_id>.dkr.ecr.<region>.amazonaws.com
docker push <account_id>.dkr.ecr.<region>.amazonaws.com/openclaw:latest
```

## Development

For developers building and testing the `cruxclaw` CLI locally.

### Prerequisites

- **Go** >= 1.21
- AWS access configured (see [Install the CLI](#install-the-cli-end-users))

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

See [CONTRIBUTING.md](CONTRIBUTING.md) for code style, testing, and PR guidelines.

## License

This project is licensed under the Apache License 2.0 — see [LICENSE](LICENSE) for details.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to get involved.

## Security

See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.
