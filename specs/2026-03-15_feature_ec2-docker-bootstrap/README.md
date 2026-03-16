# Feature: EC2 + Docker Bootstrap — Trace Log

**Started**: 2026-03-15
**Status**: ✅ Verified — end-to-end working, local gateway decommissioned

## Active Personas
- Architect — instance config, Docker isolation, user-data bootstrap, systemd hardening

## Active Capabilities
- AWS CLI (profile: `167595588574_AdministratorAccess`)
- Terraform CLI (VPC, IAM, secrets all deployed)

## Decisions
- **AMI**: Amazon Linux 2023 arm64
- **Docker rootless**: From day one
- **Persistent storage**: Root EBS volume at `/opt/openclaw/data/aaron/` (KMS encrypted)
- **Systemd management**: System unit running Docker as openclaw user
- **Container hardening**: --read-only, --cap-drop ALL, --security-opt no-new-privileges, memory/cpu/pids limits

## Resolved Questions
- **OpenClaw reads all secrets from env vars** (highest priority over config file). ANTHROPIC_API_KEY, SLACK_BOT_TOKEN, SLACK_APP_TOKEN all supported. openclaw.json contains zero secrets.
- **Do NOT use `${VAR}` substitution in openclaw.json** — Issue #9627: `openclaw update`/`doctor` can resolve vars and write secrets to disk. Pass env vars to container instead.

## Files Created
- [requirements.md](requirements.md)
- [plan.md](plan.md)
- [spec.md](spec.md) — full Terraform + user-data bootstrap script

## Persona Review
**Architect**: ✅ Approved. One accepted tradeoff: env file with secrets on disk (mode 0400, encrypted EBS) — required for systemd restart support.

## Standards Gate Report
| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Zero ingress | network | must | ✅ PASSES |
| No SSH | access | must | ✅ PASSES |
| Immutable config | config | must | ✅ PASSES |
| Container isolation | container | must | ✅ PASSES |
| Encrypted storage | storage | must | ✅ PASSES |
| IMDSv2 hop limit 1 | container | must | ✅ PASSES |
| Secrets never touch disk | secrets | must | ⚠️ WARNING — env file accepted tradeoff |
