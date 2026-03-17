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
- Per-user secrets under `openclaw/{member_id}/*` — users self-serve via `scripts/onboard-user.sh`
- Shared secrets (Slack tokens) under `openclaw/shared/*` — managed by Terraform

## Planning

- GLaDOS planning docs in `product-knowledge/`
- Feature specs in `specs/YYYY-MM-DD_feature_name/`
- Security standards in `product-knowledge/standards/security.md` — review before making security-relevant changes
- Roadmap in `product-knowledge/ROADMAP.md`

## Slack Architecture

- **Each user needs their own Slack app** — Slack Socket Mode load-balances events across connections to the same app. Multiple containers on one app = missed messages (~50% go to wrong container).
- A router/proxy approach was prototyped (`router/src/index.js`) but blocked by an OpenClaw bug — HTTP webhook mode has a module identity split where the route registers in one module instance but the gateway reads from a different empty instance. See `specs/2026-03-17_feature_slack-router/LEARNINGS.md`.
- Each user's Slack app tokens (`xapp-`, `xoxb-`) go in their per-user secrets path (`openclaw/{member_id}/slack-app-token`, `openclaw/{member_id}/slack-bot-token`)
- `signingSecret` and `botToken` MUST be in `openclaw.json` (env var override doesn't work for these)
- OpenClaw's health monitor triggers `stale-socket` restarts every ~30 min on shared apps due to Socket Mode event distribution

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
