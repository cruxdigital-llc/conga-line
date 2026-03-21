# Requirements: Config Integrity + Monitoring

## Goal
Detect config tampering via periodic hash checks, ship container logs to CloudWatch, and set up alerting infrastructure.

## Success Criteria
1. Systemd timer hashes openclaw.json every 5 minutes (configurable) and logs violations
2. CloudWatch agent ships container logs to `/conga/gateway` log group
3. CloudWatch alarm fires on config integrity violations
4. SNS topic created (no recipients for now, configurable via Terraform variable)
5. All check intervals and alert recipients configurable in Terraform variables

## Design Decisions
- **CloudWatch agent** (not awslogs driver) — compatible with future Docker rootless migration
- **5-minute check interval** — configurable via Terraform variable
- **SNS topic with no subscribers** — ready for email notifications, configurable
