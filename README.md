# Conga Line 🦞🦞🦞 - Run an OpenClaw "cluster" on AWS
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-%3E%3D1.25.0-00ADD8.svg)](cli/)
[![Terraform](https://img.shields.io/badge/Terraform-%3E%3D1.5-7B42BC.svg)](terraform/)

<p align="center">
  <img src="assets/congaline.png" alt="OpenClaw agents" width="300">
</p>

Deploy and manage "clusters" of OpenClaw instances on hardened AWS infrastructure behind a single Slack App. Each agent runs in its own isolated Docker container with dedicated secrets, networking, and access controls — giving teams and enterprises granular permission management over their AI workforce.

## Key Features

- **Zero-ingress networking** — no SSH, no public ports; all access through AWS SSM
- **Per-agent isolation** — separate Docker containers, networks, secrets, and config
- **Two agent types** — user agents (DM-only) for individuals, team agents (channel-based) for groups
- **SSM-driven discovery** — agents are registered in Parameter Store and provisioned automatically at boot, no Terraform changes needed to add or remove agents
- **Slack event router** — single Socket Mode connection fans out to per-agent containers via HTTP webhook
- **Cost-optimized** — fck-nat (~$3/mo vs $33/mo NAT Gateway); instance sized to ~2GB per agent (e.g. r6g.medium for 3 agents)
- **CLI for everything** — operators and end users manage agents, secrets, and infrastructure through the `conga` CLI

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
| **Configuration** | CLI (`conga admin setup`) | Shared secrets, Docker image, deployment settings |
| **Agents** | CLI (`conga admin add-user/add-team`) | Per-agent containers, configs, routing, secrets |

## Quick Start (Operators)

### Prerequisites

- **AWS account** with [AWS SSO (Identity Center)](https://aws.amazon.com/iam/identity-center/) configured
- **AWS CLI v2** with **session-manager-plugin** installed
- **Terraform** >= 1.5
- **Slack app** configured for OpenClaw — follow the [OpenClaw Slack setup guide](https://github.com/openclaw/openclaw/blob/main/docs/slack.md) to create an app with Socket Mode enabled and the required bot scopes. You'll need the bot token, app token, and signing secret for `conga admin setup`
- **OpenClaw Docker image** — pinned to `v2026.3.11` (see [Docker Image](#docker-image))

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
conga admin setup
```

This reads the setup manifest from SSM and prompts for shared secrets (Slack tokens, signing secret, Google OAuth) and config values (Docker image URL).

### 4. Add agents and start

```bash
conga admin add-user boblobclaw UEXAMPLE01
conga admin add-team bluthcompany CEXAMPLE01
conga admin list-agents

conga admin cycle-host   # restarts EC2; bootstrap discovers and provisions all agents
```

## Install the CLI (End Users)

No Terraform, Go, or repo clone required. This is how users manage their agents and secrets as well as access the web UI securely.

### Prerequisites

- **AWS CLI v2** — [Install guide](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- **session-manager-plugin** — macOS: `brew install --cask session-manager-plugin` | [Other platforms](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
- **AWS SSO access** — your admin will provide the SSO URL and account ID

### Install

**macOS (Apple Silicon)** — tested:
```bash
curl -fsSL https://github.com/cruxdigital-llc/conga-line/releases/latest/download/conga_darwin_arm64.tar.gz | tar xz -C /usr/local/bin conga
```

**macOS (Intel)**:
```bash
curl -fsSL https://github.com/cruxdigital-llc/conga-line/releases/latest/download/conga_darwin_amd64.tar.gz | tar xz -C /usr/local/bin conga
```

**Linux (amd64)** — untested:
```bash
curl -fsSL https://github.com/cruxdigital-llc/conga-line/releases/latest/download/conga_linux_amd64.tar.gz | tar xz -C /usr/local/bin conga
```

**Linux (arm64)** — untested:
```bash
curl -fsSL https://github.com/cruxdigital-llc/conga-line/releases/latest/download/conga_linux_arm64.tar.gz | tar xz -C /usr/local/bin conga
```

### First-time setup

```bash
aws configure sso --profile your-profile
export AWS_PROFILE=your-profile
aws sso login

conga auth status        # triggers interactive CLI setup
conga secrets set anthropic-api-key
conga refresh
conga connect            # opens SSM tunnel to web UI
```

Open http://localhost:18789 in your browser.

> NOTE: For ease of use in the future, you may want to add the `export AWS_PROFILE=your-profile` line to your shell profile (~/.bashrc, ~/.zshrc, etc.) to avoid having to export it or pass `--profile` every time.

## CLI Reference

### User Commands

| Command | Description |
|---------|-------------|
| `conga auth login` | Authenticate via AWS SSO |
| `conga auth status` | Show your AWS identity and agent mapping |
| `conga secrets set <name>` | Create or update a secret |
| `conga secrets list` | List your secrets |
| `conga secrets delete <name>` | Delete a secret |
| `conga connect` | Open SSM tunnel to web UI |
| `conga refresh` | Restart container with fresh secrets |
| `conga status` | Show container status and resource usage |
| `conga logs` | Tail container logs |
| `conga version` | Show CLI version |

### Admin Commands

| Command | Description |
|---------|-------------|
| `conga admin setup` | Configure shared secrets and settings from the deployment manifest |
| `conga admin add-user <name> <slack_member_id>` | Provision a user agent (DM-only) |
| `conga admin add-team <name> <slack_channel>` | Provision a team agent (channel-based) |
| `conga admin list-agents` | List all provisioned agents |
| `conga admin remove-agent <name>` | Remove an agent |
| `conga admin cycle-host` | Stop/start the EC2 instance |
| `conga admin refresh-all` | Restart all agent containers (picks up latest behavior, config, secrets) |

### Global Flags

| Flag | Description                                                  |
|------|--------------------------------------------------------------|
| `--profile` | AWS CLI profile (default: `AWS_PROFILE` env var)             |
| `--region` | AWS region (default: from config)                            |
| `--agent` | Override auto-detected agent name or target a specific agent |
| `--verbose` | Verbose output                                               |

## How It Works

The CLI discovers infrastructure via AWS APIs — no Terraform access or repo clone needed:

- **Instance**: Found by EC2 tag `Name=conga-line-host`
- **Agent config**: Stored in SSM Parameter Store at `/conga/agents/{name}`
- **Identity mapping**: Your SSO username matches the `iam_identity` field in your agent's SSM config
- **Secrets**: Managed in AWS Secrets Manager under `conga/agents/{name}/`
- **Shared config**: Setup manifest at `/conga/config/setup-manifest`, image at `/conga/config/image`
- **Remote operations**: Executed via SSM RunCommand (no SSH, no ingress)
- **Bootstrap discovery**: On instance boot, the bootstrap script reads all agents from `/conga/agents/` in SSM and provisions them automatically

## Docker Image

This project uses the official OpenClaw image pinned to **v2026.3.11** (`29dc654`), the last stable release before a [Slack socket mode regression](https://github.com/openclaw/openclaw/issues/45311) was introduced in v2026.3.12.

```
ghcr.io/openclaw/openclaw:2026.3.11
```

Set this as the image URL when prompted by `conga admin setup`.

> NOTE: Once the bug introduced in v2026.3.12 is fixed, we'll update this to reference the latest stable release.

## Development

For developers building and testing the `conga` CLI locally.

### Prerequisites

- **Go** >= 1.25
- AWS access configured (see [Install the CLI](#install-the-cli-end-users))

### Build and run

```bash
cd cli
go build -o conga .
./conga auth status
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
