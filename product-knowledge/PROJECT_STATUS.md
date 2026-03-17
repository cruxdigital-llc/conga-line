# GLaDOS System Status

This document reflects the *current state* of the codebase and project.
It should be updated whenever a significant change occurs in the architecture, roadmap, or standards.

## Project Overview
**Mission**: Hardened, per-user-isolated AWS deployment of OpenClaw for internal team use. See [MISSION.md](MISSION.md).
**Current Phase**: Planning

## Architecture
Pure infrastructure project — no application code. Terraform + shell bootstrap scripts deploying OpenClaw as a Docker container on AWS.

- **Single EC2 host (t4g.medium)** with per-user Docker containers (isolated networks, separate configs/secrets)
- **fck-nat (t4g.nano)** for cost-optimized egress (~$3/mo vs $33/mo NAT Gateway)
- **Zero ingress**, SSM-only access, secrets off-disk, encrypted EBS
- **~$10/mo total** for 2 users

See [TECH_STACK.md](TECH_STACK.md) for full details.

## Current Focus

### 1. MVP Planning — 2 Users via Slack
*Lead: Architect*
- [x] **Mission defined**: `product-knowledge/MISSION.md`
- [x] **Security standards defined**: `product-knowledge/standards/security.md`
- [x] **Roadmap defined**: `product-knowledge/ROADMAP.md`
- [x] **Tech stack defined**: `product-knowledge/TECH_STACK.md`
- [x] **Epic 0**: Terraform foundation (S3 state + DynamoDB locks) — complete
- [x] **Epic 1**: VPC + networking — complete (31 resources)
- [x] **Epic 2**: IAM + secrets — complete (5 secrets populated)
- [x] **Epic 3**: EC2 + Docker bootstrap — complete, Slack connected, end-to-end verified
- [x] **Epic 4**: Config integrity + monitoring — complete (timer + CW agent + alarm)
- **Milestone**: Aaron's local gateway replaced by AWS deployment
- [x] **Epics 5+6**: Multi-user onboarding + Slack router — complete

### 2. Backlog / Upcoming
- [ ] Horizon 2: Operational maturity (secret rotation, backups, dashboards)
- [ ] Horizon 3: Advanced hardening (egress allowlisting, GuardDuty, Config rules)

## Known Issues / Technical Debt
- Per-user API keys: each employee brings their own credentials and plugins
- Open question: egress domain allowlisting needed or port-443-only sufficient
- Open question: which OpenClaw skills/plugins to enable and sandbox requirements

## Recent Changes
- 2026-03-17: Epics 5+6 complete — multi-user onboarding, Slack event router, patched OpenClaw image (HTTP webhook fix), ECR, persistent EBS volume
- 2026-03-16: Epic 4 complete — config integrity timer, CloudWatch agent + alarm, SNS topic
- 2026-03-16: Epic 3 complete — EC2 host running, OpenClaw container healthy, Slack socket mode connected, local gateway decommissioned
- 2026-03-15: Epic 2 complete — IAM role + deny-dangerous policy, KMS key, 5 secrets populated
- 2026-03-15: Epic 1 complete — VPC + networking (31 resources: VPC, subnets, fck-nat ASG, zero-ingress SG, NACLs, flow logs)
- 2026-03-15: Epic 0 complete — Terraform foundation (S3 state backend + DynamoDB locks) verified and working
- 2026-03-15: GLaDOS initialized, mission defined, security standards + roadmap + tech stack created
