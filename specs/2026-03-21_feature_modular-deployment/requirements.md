# Requirements: Modular Deployment

## Problem Statement

Conga Line is currently hardcoded to a single deployment target: a single EC2 host in a hardened AWS VPC. The CLI discovers configuration from SSM Parameter Store, manages secrets via Secrets Manager, and executes remote commands via SSM RunCommand. The bootstrap logic lives in a Terraform template (`user-data.sh.tftpl`).

This tight coupling to AWS means:
- Developers cannot run Conga Line locally for testing or personal use without an AWS account
- Adding new deployment targets (ECS, Kubernetes, another cloud) would require rewriting the CLI
- The bootstrap script conflates infrastructure concerns (Docker setup, networking) with provider-specific concerns (SSM discovery, Secrets Manager fetching)

## Goal

Refactor the deployment architecture into a **modular provider system** where deployment targets are pluggable. Implement the first non-AWS target: **local Docker deployment** with operational parity to the AWS deployment.

## Functional Requirements

### FR-1: Provider Abstraction
- The CLI must support a `--provider` flag (or config setting) to select a deployment target
- Provider selection must be persistent (stored in local config after initial setup)
- All CLI commands must work identically regardless of provider
- The provider interface must cover: discovery, secrets, container lifecycle, logs, status, and connectivity

### FR-2: AWS Provider (Refactor Existing)
- All existing AWS functionality must be preserved unchanged
- Current behavior becomes the `aws` provider implementation
- No breaking changes to existing workflows or Terraform

### FR-3: Local Docker Provider
- `conga admin setup --provider local` bootstraps the local environment:
  - Creates config directory structure (`~/.conga/` or configurable)
  - Prompts for shared secrets (same interactive flow as AWS setup)
  - Pulls the OpenClaw Docker image
  - Sets up the router container
- `conga admin add-user` / `conga admin add-team` provisions agents locally:
  - Creates per-agent Docker network
  - Generates `openclaw.json` (identical config to AWS)
  - Creates `.env` file with secrets (mode 0400)
  - Deploys behavior files
  - Creates and starts the agent container
  - Updates router routing.json and reconnects networks

### FR-4: Network Isolation (Local)
- Each agent runs in its own Docker network (no inter-container communication)
- Router connects to all agent networks (same as AWS)
- Egress restricted to HTTPS (443) and DNS (53) only — implemented via Docker network options and/or iptables rules on the Docker bridge
- No host port exposure except explicitly configured gateway ports (localhost-bound)
- Containers run with: `--cap-drop ALL`, `--no-new-privileges`, `--memory` limits

### FR-5: Secrets Management (Local)
- Secrets stored in local encrypted files (GPG or age encryption, or OS keychain)
- Same CLI interface: `conga secrets set`, `conga secrets list`, `conga secrets delete`
- Secrets injected into containers via env files (same as AWS)
- Shared secrets stored separately from per-agent secrets

### FR-6: Operational Parity
- `conga status` — shows container status, uptime, resource usage (via Docker API)
- `conga logs` — tails container logs (via `docker logs`)
- `conga refresh` — restarts agent container, reconnects router
- `conga connect` — opens localhost port forward to agent web UI (direct, no SSM tunnel needed)
- `conga admin cycle-host` — restarts all containers (local equivalent)
- `conga admin refresh-all` — restarts all agent containers
- `conga admin list-agents` — lists agents from local config
- `conga admin remove-agent` — stops container, removes network, cleans config

### FR-7: Router (Local)
- Same Node.js router code, running in a Docker container
- Single Slack Socket Mode connection (via SLACK_APP_TOKEN)
- HTTP fan-out to per-agent containers
- routing.json generated and hot-reloaded identically
- 128MB memory limit

### FR-8: Config Integrity (Local)
- Same SHA256 hash-check mechanism for openclaw.json
- Runs as a background process or cron job (not systemd timer)
- Logs mismatches to local log file

## Non-Functional Requirements

### NFR-1: Modularity
- Provider implementations must be self-contained in their own packages
- Adding a new provider must not require changes to CLI command definitions
- Shared logic (config generation, routing.json generation, behavior file composition) must be extracted into common modules

### NFR-2: Configuration
- Provider config stored in `~/.conga/config.json` (or XDG-compliant path)
- Local provider stores all state under `~/.conga/` by default (configurable via `--data-dir`)
- AWS provider continues using SSM/Secrets Manager for discovery

### NFR-3: No Regression
- Existing AWS deployment must continue to work with zero changes
- Terraform files remain untouched
- Bootstrap script remains untouched
- All existing CLI tests must pass

### NFR-4: Security
- Local secrets must not be stored in plaintext
- Container isolation must match AWS deployment (cap-drop, no-new-privileges, memory limits)
- Egress restrictions must be enforced, not advisory

## Out of Scope
- Kubernetes provider (future)
- ECS/Fargate provider (future)
- Multi-host local deployment
- Automatic TLS termination for local
- Local CloudWatch equivalent (dashboards, metrics) — logs only for MVP
- Modifying the OpenClaw Docker image itself
