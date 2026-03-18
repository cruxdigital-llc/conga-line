# CLAUDE.md

## Project Overview

This is an infrastructure-as-code project deploying OpenClaw (autonomous AI assistant) on hardened AWS. There is no application code — the deliverable is Terraform configuration + bootstrap scripts.

## Key Context

- **AWS Account**: 123456789012, region us-east-2
- **AWS CLI Profile**: `openclaw`
- **Architecture**: Single EC2 host (t4g.medium, AL2023) with per-user Docker containers in a zero-ingress VPC
- **NAT**: fck-nat via `RaJiska/fck-nat/aws` module v1.4.0 (not AWS NAT Gateway)
- **Terraform state**: S3 bucket `openclaw-terraform-state-123456789012` + DynamoDB `openclaw-terraform-locks`

## Working with Terraform

- All Terraform files are in `terraform/`
- Always `cd terraform` before running terraform commands
- AWS provider is `~> 6.0` (v6.36.0) — required by the fck-nat module
- Backend block values are hardcoded (Terraform limitation — no variables in backend blocks)
- S3 bucket names must include the account ID suffix to avoid global namespace collisions

## Secrets

- Secrets are in AWS Secrets Manager under `openclaw/shared/*` and `openclaw/UEXAMPLE01/*`
- Terraform creates secrets with `REPLACE_ME` placeholders + `ignore_changes` lifecycle
- Real values populated via `terraform/populate-secrets.sh`
- Never put real secret values in Terraform files or state
- OpenClaw reads secrets from environment variables (highest priority over config file)
- Do NOT use `${VAR}` substitution in `openclaw.json` — Issue #9627 causes secret values to be written to disk

## OpenClaw-Specific

- Docker image: `ghcr.io/openclaw/openclaw:latest`
- Container runs as `node` user (uid 1000 inside container)
- Config at `/opt/openclaw/data/{user_id}/openclaw.json` — no secrets in this file
- Env file at `/opt/openclaw/config/{user_id}.env` — secrets, mode 0400
- OpenClaw hot-reload writes `.tmp` files next to `openclaw.json` — the config directory must be writable by the container user
- Container needs `NODE_OPTIONS="--max-old-space-size=1536"` to avoid V8 heap OOM
- Container memory limit: 2GB (1.5GB was too low)
- Users are keyed by Slack member ID (e.g., `UEXAMPLE01`), not username
- Aaron's member ID: `UEXAMPLE01`, Slack channel: `CEXAMPLE01`
- Per-user secrets under `openclaw/{member_id}/*` — users self-serve via `cruxclaw secrets set`
- Shared secrets (Slack tokens) under `openclaw/shared/*` — managed by Terraform

## Planning

- GLaDOS planning docs in `product-knowledge/`
- Feature specs in `specs/YYYY-MM-DD_feature_name/`
- Security standards in `product-knowledge/standards/security.md` — review before making security-relevant changes
- Roadmap in `product-knowledge/ROADMAP.md`

## Slack Architecture

- **Single shared Slack app** — one Slack app for all users. The Slack event router (`router/src/index.js`) holds the single Socket Mode connection and fans out events to per-user containers via HTTP webhook.
- **Containers use HTTP webhook mode** (`mode: "http"`) — they never connect to Slack directly. The router forwards events with signed HTTP requests.
- `signingSecret` and `botToken` MUST be in `openclaw.json` (env var override doesn't work for these)
- `SLACK_APP_TOKEN` is held only by the router (in `router.env`) — containers do not need it
- Router must be connected to each user's Docker network (`docker network connect openclaw-<member_id> openclaw-router`) so it can reach the container's webhook endpoint
- Routing config at `/opt/openclaw/config/routing.json` maps channels and member IDs to container URLs
- The deployed image includes the HTTP webhook fix from our fork (PR openclaw/openclaw#49514)

## OpenClaw Behavioral Issues

- **Billing/rate errors are cached**: When Anthropic returns a billing or rate limit error, OpenClaw's model fallback system caches the rejection. Even after the billing issue is resolved, the container must be restarted to clear the cached error state.
- **Container restart requires router reconnection**: When a user container restarts, the router loses its Docker network connection and must be reconnected via `docker network connect`.

## Known Limitations

- Docker rootless mode deferred — AL2023 lacks `fuse-overlayfs` and `slirp4netns` packages needed for rootless Docker CE. Using standard Docker with cap-drop ALL, no-new-privileges, and resource limits instead.
- Config file cannot be made read-only at the filesystem level due to OpenClaw's hot-reload `.tmp` file behavior. Config integrity will be enforced via hash-check monitoring (Epic 4).
- Env file with secrets is on disk (mode 0400, encrypted EBS) — required for systemd to re-inject env vars on container restart.

## Debugging

- Connect to instance: `aws ssm start-session --target <instance-id> --region us-east-2 --profile openclaw`
- Bootstrap log: `cat /var/log/openclaw-bootstrap.log`
- Service status: `systemctl status openclaw-myagent`
- Container logs: `docker logs openclaw-myagent --tail 50`
- Journal: `journalctl -u openclaw-myagent --no-pager -n 50`
