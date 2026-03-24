# Conga Line 🦞🦞🦞
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-%3E%3D1.25.0-00ADD8.svg)](cli/)
[![Terraform](https://img.shields.io/badge/Terraform-%3E%3D1.5-7B42BC.svg)](terraform/)

<p align="center">
  <img src="assets/congaline.png" alt="OpenClaw agents" width="300">
</p>

Self-host a fleet of isolated [OpenClaw](https://github.com/openclaw/openclaw) AI agents — each with its own container, network, secrets, and identity — managed through a single CLI. Deploy anywhere: your laptop, a $5 VPS, a Raspberry Pi, or a hardened AWS account.

> **conga line** *n.* A single-file procession of spiny lobsters that travel in physical contact during seasonal migration, reducing hydrodynamic drag and offering collective protection from predators.
## Key Features

- **Three deployment modes** — local Docker, remote (any SSH host), or hardened AWS
- **Per-agent isolation** — separate Docker containers, networks, secrets, and config
- **Slack optional** — use via web UI (gateway) only, or connect to Slack for team chat
- **Two agent types** — user agents (DM-only) for individuals, team agents (channel-based) for groups
- **CLI for everything** — operators and end users manage agents, secrets, and infrastructure through the `conga` CLI
- **Modular provider system** — pluggable deployment targets (AWS, local, remote, future: Kubernetes, ECS)

## Architecture

```
┌─────────────────────────────────────────────────┐
│                 CLI Commands                     │
│  (setup, add-user, status, logs, connect, ...)   │
└────────────────────┬────────────────────────────┘
                     │ Provider interface
         ┌───────────┼───────────┐
         ▼           ▼           ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ AWS Provider │ │Remote Provider│ │Local Provider│
│              │ │              │ │              │
│ EC2 + SSM    │ │ SSH + Docker │ │ Docker CLI   │
│ Secrets Mgr  │ │ File secrets │ │ File secrets │
│ Zero-ingress │ │ SSH tunnel   │ │ localhost    │
└──────────────┘ └──────────────┘ └──────────────┘
```

### Separation of Concerns

| Layer | Managed by | What it does |
|-------|-----------|-------------|
| **Infrastructure** | Terraform (AWS), `conga admin setup` (remote/local) | VPC/EC2, remote host, or local Docker environment |
| **Configuration** | CLI (`conga admin setup`) | Shared secrets, Docker image, deployment settings |
| **Agents** | CLI (`conga admin add-user/add-team`) | Per-agent containers, configs, routing, secrets |

## Quick Start (Local Docker)

The fastest way to get running — no AWS account needed.

### Prerequisites

- **Docker Desktop** installed and running
- **Go** >= 1.25 (to build the CLI)
- **Anthropic API key**

### 1. Build the CLI

```bash
cd cli
go build -o /usr/local/bin/conga .
```

### 2. Setup local environment

```bash
conga admin setup --provider local
```

This will prompt for the repo path (auto-detected), Docker image, and optionally Slack tokens. Skip Slack tokens for gateway-only mode (web UI).

### 3. Add an agent

```bash
conga admin add-user myagent
```

No Slack member ID needed for gateway-only mode. With Slack:

```bash
conga admin add-user myagent U0123456789
```

### 4. Set your API key and start

```bash
conga secrets set anthropic-api-key --agent myagent
conga refresh --agent myagent
conga status --agent myagent
```

### 5. Connect

```bash
conga connect --agent myagent
```

Open the URL in your browser. Device pairing is auto-approved.

### 6. Teardown (when done)

```bash
conga admin teardown
```

Removes all containers, networks, and local config.

## Quick Start (Remote — VPS, Bare Metal, Any SSH Host)

Deploy to any Linux machine you can SSH into — a cloud VPS (Hetzner, DigitalOcean, Linode, Hostinger), a Raspberry Pi, a Mac Mini, a colocated server, or anything else running Linux with 4GB+ RAM.

### Prerequisites

- **Any SSH-accessible Linux host** with 4GB+ RAM and 10GB+ free disk
- **SSH key authentication** configured (password auth not supported)
- **Go** >= 1.25 (to build the CLI)
- **Anthropic API key**

Docker is installed automatically during setup if not already present.

### 1. Build the CLI

```bash
cd cli
go build -o /usr/local/bin/conga .
```

### 2. Setup remote environment

```bash
conga admin setup --provider remote
```

This will prompt for:
- SSH connection details (host, port, user, key path)
- Docker image (auto-installs Docker if needed)
- Slack tokens (optional — skip for gateway-only web UI)

### 3. Add an agent

```bash
conga admin add-user myagent
```

With Slack:
```bash
conga admin add-user myagent U0123456789
```

### 4. Set your API key and start

```bash
conga secrets set anthropic-api-key --agent myagent
conga refresh --agent myagent
conga status --agent myagent
```

### 5. Connect

```bash
conga connect --agent myagent
```

Opens an SSH tunnel to the remote host's gateway. Open the URL in your browser — no ports are exposed to the internet.

### 6. Teardown (when done)

```bash
conga admin teardown
```

Removes all containers, networks, and data from the remote host.

## Quick Start (AWS)

For teams and production — hardened, zero-ingress deployment.

### Prerequisites

- **AWS account** with [AWS SSO (Identity Center)](https://aws.amazon.com/iam/identity-center/) configured
- **AWS CLI v2** with **session-manager-plugin** installed
- **Terraform** >= 1.5
- **Slack app** configured for OpenClaw (required for AWS deployment)
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

### 4. Add agents and start

```bash
conga admin add-user boblobclaw UEXAMPLE01
conga admin add-team bluthcompany CEXAMPLE01
conga admin list-agents

conga admin cycle-host   # restarts EC2; bootstrap discovers and provisions all agents
```

## Install the CLI (End Users)

No Terraform, Go, or repo clone required. This is how users manage their agents and secrets as well as access the web UI securely.

### Prerequisites (AWS provider)

- **AWS CLI v2** — [Install guide](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html)
- **session-manager-plugin** — macOS: `brew install --cask session-manager-plugin` | [Other platforms](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
- **AWS SSO access** — your admin will provide the SSO URL and account ID

### Prerequisites (Remote provider)

- **SSH access** to the remote host (key-based auth)

### Prerequisites (Local provider)

- **Docker Desktop** installed and running

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

### First-time setup (AWS)

```bash
aws configure sso --profile your-profile
export AWS_PROFILE=your-profile
aws sso login

conga auth status
conga secrets set anthropic-api-key
conga refresh
conga connect            # opens SSM tunnel to web UI
```

### First-time setup (Remote — VPS, Raspberry Pi, Mac Mini, etc.)

```bash
conga admin setup --provider remote   # prompts for SSH host, installs Docker
conga admin add-user myagent
conga secrets set anthropic-api-key --agent myagent
conga refresh --agent myagent
conga connect --agent myagent          # opens SSH tunnel to web UI
```

### First-time setup (Local)

```bash
conga admin setup --provider local
conga admin add-user myagent
conga secrets set anthropic-api-key --agent myagent
conga refresh --agent myagent
conga connect --agent myagent
```

## CLI Reference

### User Commands

| Command | Description |
|---------|-------------|
| `conga auth login` | Authenticate (AWS: SSO login; local: not required) |
| `conga auth status` | Show identity, provider, and agent mapping |
| `conga secrets set <name>` | Create or update a secret |
| `conga secrets list` | List your secrets |
| `conga secrets delete <name>` | Delete a secret |
| `conga connect` | Connect to web UI (AWS: SSM tunnel; remote: SSH tunnel; local: direct localhost) |
| `conga refresh` | Restart container with fresh secrets |
| `conga status` | Show container status and resource usage |
| `conga logs` | Tail container logs |
| `conga version` | Show CLI version |

### Admin Commands

| Command | Description |
|---------|-------------|
| `conga admin setup` | Configure shared secrets and settings |
| `conga admin add-user <name> [slack_member_id]` | Provision a user agent (Slack ID optional for gateway-only) |
| `conga admin add-team <name> [slack_channel]` | Provision a team agent (Slack channel optional for gateway-only) |
| `conga admin list-agents` | List all provisioned agents (shows status: active/paused) |
| `conga admin remove-agent <name>` | Remove an agent |
| `conga admin pause <name>` | Temporarily stop an agent (preserves all data) |
| `conga admin unpause <name>` | Resume a paused agent |
| `conga admin cycle-host` | Restart the deployment environment |
| `conga admin refresh-all` | Restart all agent containers (skips paused) |
| `conga admin teardown` | Remove the entire deployment (local only; AWS: use `terraform destroy`) |

### Global Flags

| Flag | Description |
|------|-------------|
| `--provider` | Deployment provider: `aws`, `local`, `remote` (default: `local`) |
| `--data-dir` | Data directory for local provider (default: `~/.conga/`) |
| `--profile` | AWS CLI profile (default: `AWS_PROFILE` env var) |
| `--region` | AWS region (default: from config) |
| `--agent` | Override auto-detected agent name |
| `--verbose` | Verbose output |
| `--timeout` | Global timeout for operations (default: 5m) |

## How It Works

### Provider Auto-Detection

The CLI selects a provider using:
1. `--provider` flag if specified
2. `~/.conga/config.json` `provider` field if set (persisted by `conga admin setup`)
3. Default: `local`

Use `--provider aws`, `--provider remote`, or `--provider local`, or run `conga admin setup` to persist the choice.

### AWS Provider

Discovers infrastructure via AWS APIs — no Terraform access or repo clone needed:
- **Instance**: Found by EC2 tag `Name=conga-line-host`
- **Agent config**: SSM Parameter Store at `/conga/agents/{name}`
- **Secrets**: AWS Secrets Manager under `conga/agents/{name}/`
- **Remote operations**: SSM RunCommand (no SSH, no ingress)

### Remote Provider

For any SSH-accessible Linux host — VPS instances (Hetzner, DigitalOcean, Linode, Hostinger), bare metal servers (Raspberry Pi, Mac Mini), colocated servers, or any machine you can SSH into:
- **Agent config**: `/opt/conga/agents/{name}.json` on remote host
- **Secrets**: `/opt/conga/secrets/agents/{name}/` on remote host (file per secret, mode 0400)
- **Container data**: `/opt/conga/data/{name}/` on remote host
- **Container operations**: Docker CLI over SSH
- **Network isolation**: Per-agent Docker bridge networks, localhost-only port binding on remote
- **Gateway access**: SSH tunnel (no inbound ports exposed beyond SSH)
- **Slack routing**: Router container auto-started when Slack tokens are configured
- **Docker auto-install**: Setup detects the OS and installs Docker if not present

### Local Provider

All state lives under `~/.conga/`:
- **Agent config**: `~/.conga/agents/{name}.json`
- **Secrets**: `~/.conga/secrets/agents/{name}/` (file per secret, mode 0400)
- **Container data**: `~/.conga/data/{name}/` (mounted to `/home/node/.openclaw`)
- **Container operations**: Docker CLI (`docker run`, `docker logs`, etc.)
- **Network isolation**: Per-agent Docker bridge networks, localhost-only port binding
- **Slack routing**: Router container auto-started when Slack tokens are configured

## MCP Server (AI Agent Integration)

The CLI includes an MCP server that exposes agent management as tools for AI coding assistants like Claude Code. This lets an AI manage your Conga Line deployment conversationally — listing agents, checking status, setting secrets, refreshing containers, etc.

### Setup

1. Install the `conga` CLI (see above)
2. Copy the example config:

```bash
cp .mcp.json.example .mcp.json
```

3. Edit `.mcp.json` with your provider and credentials:

**AWS:**
```json
{
  "mcpServers": {
    "conga": {
      "command": "conga",
      "args": ["mcp", "serve"],
      "env": {
        "CONGA_PROVIDER": "aws",
        "AWS_PROFILE": "your-profile"
      }
    }
  }
}
```

**Remote (SSH):**
```json
{
  "mcpServers": {
    "conga": {
      "command": "conga",
      "args": ["mcp", "serve"],
      "env": {
        "CONGA_PROVIDER": "remote"
      }
    }
  }
}
```

**Local Docker:**
```json
{
  "mcpServers": {
    "conga": {
      "command": "conga",
      "args": ["mcp", "serve"]
    }
  }
}
```

4. Restart Claude Code — the conga tools will appear automatically.

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
- **Docker** (for local provider testing)

### Build and run

```bash
cd cli
go build -o conga .
./conga auth status --provider local
```

### Project structure

```
cli/
├── cmd/                        # Cobra command definitions
├── internal/
│   ├── aws/                    # AWS SDK wrappers
│   ├── common/                 # Shared logic (config gen, routing, validation)
│   ├── discovery/              # Agent & identity resolution (AWS)
│   ├── mcpserver/              # MCP server (AI agent integration)
│   ├── provider/               # Provider interface & registry
│   │   ├── awsprovider/        # AWS provider implementation
│   │   ├── remoteprovider/    # Remote (SSH) provider implementation
│   │   └── localprovider/      # Local Docker provider implementation
│   ├── tunnel/                 # SSM port forwarding
│   └── ui/                     # Spinners, prompts, tables
├── scripts/                    # Embedded shell templates (AWS remote execution)
├── main.go
├── go.mod
└── go.sum

deploy/
└── egress-proxy/               # Nginx proxy for HTTPS/DNS-only egress (local)

terraform/                      # AWS infrastructure (VPC, EC2, IAM, etc.)
router/                         # Slack event router (Node.js)
behavior/                       # Agent personality files (SOUL.md, etc.)
```

## License

This project is licensed under the Apache License 2.0 — see [LICENSE](LICENSE) for details.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to get involved.

## Security

See [SECURITY.md](SECURITY.md) for reporting vulnerabilities.
