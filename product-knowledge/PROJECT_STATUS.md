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
- [ ] **Epic 1**: VPC + networking
- [ ] **Epic 2**: IAM + secrets
- [ ] **Epic 3**: EC2 + bootstrap
- [ ] **Epic 4**: Config integrity + monitoring
- [ ] **Epic 5**: Slack integration (1 app, 2 channels, 2 instances)
- [ ] **Epic 6**: Terraform packaging

### 2. Backlog / Upcoming
- [ ] Horizon 2: Operational maturity (secret rotation, backups, dashboards)
- [ ] Horizon 3: Advanced hardening (egress allowlisting, GuardDuty, Config rules)

## Known Issues / Technical Debt
- Open question: per-user vs shared Anthropic API keys
- Open question: egress domain allowlisting needed or port-443-only sufficient
- Open question: which OpenClaw skills/plugins to enable and sandbox requirements

## Recent Changes
- 2026-03-15: Epic 0 complete — Terraform foundation (S3 state backend + DynamoDB locks) verified and working
- 2026-03-15: GLaDOS initialized, mission defined, security standards + roadmap + tech stack created
