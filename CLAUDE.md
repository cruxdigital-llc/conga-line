# CLAUDE.md

## Project Overview

This is an infrastructure-as-code project deploying OpenClaw (autonomous AI assistant) on hardened AWS. There is no application code — the deliverable is Terraform configuration + bootstrap scripts.

## Key Context

- **Architecture**: Single EC2 host (t4g.medium, AL2023) with per-agent Docker containers in a zero-ingress VPC
- **NAT**: fck-nat via `RaJiska/fck-nat/aws` module v1.4.0 (not AWS NAT Gateway)
- **Terraform state**: S3 bucket `<project_name>-terraform-state-<account_id>` + DynamoDB `<project_name>-terraform-locks`
- **Configuration**: Environment-specific values are in gitignored `terraform/terraform.tfvars` and `terraform/backend.tf`. See `.example` files.
- **Separation of concerns**: Terraform manages infrastructure (VPC, EC2, IAM, router). CLI manages configuration (`admin setup`) and agents (`admin add-user/add-team`). Agents are discovered from SSM Parameter Store at `/openclaw/agents/<name>` at boot time.

## Working with Terraform

- All Terraform files are in `terraform/`
- Always `cd terraform` before running terraform commands
- AWS provider is `~> 6.0` (v6.36.0) — required by the fck-nat module
- `backend.tf` is gitignored (Terraform limitation — no variables in backend blocks). Copy from `backend.tf.example`
- `terraform.tfvars` is gitignored. Copy from `terraform.tfvars.example`
- S3 bucket names include the account ID suffix to avoid global namespace collisions

## Secrets

- Secrets are in AWS Secrets Manager under `openclaw/shared/*` and `openclaw/agents/<name>/*`
- Shared secrets created via `cruxclaw admin setup` (prompts interactively for missing values)
- Per-agent secrets under `openclaw/agents/<name>/*` — users self-serve via `cruxclaw secrets set`
- Shared secrets (Slack tokens, Google OAuth) under `openclaw/shared/*` — managed by CLI (`admin setup`)
- Never put real secret values in Terraform files or state
- OpenClaw reads secrets from environment variables (highest priority over config file)
- Do NOT use `${VAR}` substitution in `openclaw.json` — Issue #9627 causes secret values to be written to disk
- **Bootstrap requires `admin setup` first**: shared secrets must exist before cycling the host, or the bootstrap will skip secret fetches and containers will fail to connect

## OpenClaw-Specific

- Docker image: configured via `cruxclaw admin setup`, stored in SSM at `/openclaw/config/openclaw-image`
- Container runs as `node` user (uid 1000 inside container)
- Config at `/opt/openclaw/data/{agent_name}/openclaw.json` — no secrets in this file
- Env file at `/opt/openclaw/config/{agent_name}.env` — secrets, mode 0400
- OpenClaw hot-reload writes `.tmp` files next to `openclaw.json` — the config directory must be writable by the container user
- Container needs `NODE_OPTIONS="--max-old-space-size=1536"` to avoid V8 heap OOM
- Container memory limit: 2GB (1.5GB was too low)
- **Agents are keyed by agent name** (e.g. `myagent`, `leadership`), not Slack member ID or username
- Two agent types: **user agents** (DM-only, `dmPolicy: "allowlist"`) and **team agents** (channel-based, `groupPolicy: "allowlist"`)

## Planning

- GLaDOS planning docs in `product-knowledge/`
- Feature specs in `specs/YYYY-MM-DD_feature_name/`
- Security standards in `product-knowledge/standards/security.md` — review before making security-relevant changes
- Roadmap in `product-knowledge/ROADMAP.md`

## Slack Architecture

- **Single shared Slack app** — one Slack app for all agents. The Slack event router (`router/src/index.js`) holds the single Socket Mode connection and fans out events to per-agent containers via HTTP webhook.
- **Containers use HTTP webhook mode** (`mode: "http"`) — they never connect to Slack directly. The router forwards events with signed HTTP requests.
- `signingSecret` and `botToken` MUST be in `openclaw.json` (env var override doesn't work for these)
- `SLACK_APP_TOKEN` is held only by the router (in `router.env`) — containers do not need it
- Router must be connected to each agent's Docker network (`docker network connect openclaw-<agent_name> openclaw-router`) so it can reach the container's webhook endpoint
- Routing config at `/opt/openclaw/config/routing.json` maps channels and member IDs to container URLs
- The deployed image includes the HTTP webhook fix from our fork (PR openclaw/openclaw#49514)

## OpenClaw Behavioral Issues

- **Billing/rate errors are cached**: When Anthropic returns a billing or rate limit error, OpenClaw's model fallback system caches the rejection. Even after the billing issue is resolved, the container must be restarted to clear the cached error state.
- **Container restart requires router reconnection**: When an agent container restarts, the router loses its Docker network connection and must be reconnected via `docker network connect`.

## Known Limitations

- Docker rootless mode deferred — AL2023 lacks `fuse-overlayfs` and `slirp4netns` packages needed for rootless Docker CE. Using standard Docker with cap-drop ALL, no-new-privileges, and resource limits instead.
- Config file cannot be made read-only at the filesystem level due to OpenClaw's hot-reload `.tmp` file behavior. Config integrity will be enforced via hash-check monitoring (Epic 4).
- Env file with secrets is on disk (mode 0400, encrypted EBS) — required for systemd to re-inject env vars on container restart.

## Debugging

- Connect to instance: `aws ssm start-session --target <instance-id> --region <region> --profile <profile>`
- Bootstrap log: `cat /var/log/openclaw-bootstrap.log`
- Service status: `systemctl status openclaw-<agent_name>`
- Container logs: `docker logs openclaw-<agent_name> --tail 50`
- Journal: `journalctl -u openclaw-<agent_name> --no-pager -n 50`
