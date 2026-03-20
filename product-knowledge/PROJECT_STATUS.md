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

### 2. CruxClaw CLI — ✅ Complete
- [x] All 11 phases implemented and verified. See `specs/2026-03-18_feature_cruxclaw-cli/`

### 3. SSM-Driven Bootstrap Discovery — Specified, Ready for Implementation
*Lead: Architect + QA*
- [x] Requirements defined: `specs/2026-03-19_feature_ssm-driven-bootstrap-discovery/requirements.md`
- [x] Plan defined: `specs/2026-03-19_feature_ssm-driven-bootstrap-discovery/plan.md`
- [x] Spec defined: `specs/2026-03-19_feature_ssm-driven-bootstrap-discovery/spec.md`
- [x] Persona review passed (Architect + QA)
- [x] Standards gate passed (1 warning: IAM widening, accepted)
- [ ] Step 1: Unified SSM namespace (`/openclaw/agents/`) + config params
- [ ] Step 2: Widen IAM secrets policy for dynamic agents
- [ ] Step 3: Rewrite bootstrap for SSM discovery + update router.tf + CLI changes
- [ ] Step 4: Verify CLI compatibility + migration

### 4. CLI Hardening — Verified Complete
*See `specs/2026-03-19_feature_cli-hardening/` for full trace*
- Remaining deferred items: CLIContext struct migration, params_test.go, agent_test.go, executor command handler migration

### 5. Behavior Management — Specified, Ready for Implementation
*Lead: Architect + Product Manager + QA*
- [x] Requirements defined: `specs/2026-03-20_feature_behavior-management/requirements.md`
- [x] Plan defined: `specs/2026-03-20_feature_behavior-management/plan.md`
- [x] Spec defined: `specs/2026-03-20_feature_behavior-management/spec.md`
- [x] Persona review passed (Architect + PM + QA)
- [x] Standards gate passed (1 note: no integrity monitoring for workspace files)
- [ ] Implementation

### 6. Backlog / Upcoming
- [ ] Horizon 2: Operational maturity (secret rotation, backups, dashboards)
- [ ] Horizon 3: Advanced hardening (egress allowlisting, GuardDuty, Config rules)

## Known Issues / Technical Debt
- CLI has zero test coverage — addressed by CLI Hardening spec (Phase 4)
- CLI `admin.go` is 549 lines with 6 commands — addressed by CLI Hardening spec (Phase 5)
- Per-user API keys: each employee brings their own credentials and plugins
- Open question: egress domain allowlisting needed or port-443-only sufficient
- Open question: which OpenClaw skills/plugins to enable and sandbox requirements

## Recent Changes
- 2026-03-19: CLI Hardening — fixed 3 silent failure bugs, tightened Slack ID validation, added --timeout flag, AWS service interfaces for testability, HostExecutor interface for future local mode, 28 unit tests (7 test files), split admin.go into 4 files, human-readable uptime display, CI test/coverage steps. See `specs/2026-03-19_feature_cli-hardening/`.
- 2026-03-18: Open-source sanitization — removed all hardcoded environment-specific values (account IDs, Slack IDs, SSO URLs, usernames). Gitignored `backend.tf` + `terraform.tfvars` with `.example` files. Added `openclaw_image` variable. New `cruxclaw init` command for first-run config. Consolidated README. See `specs/2026-03-18_feature_open-source-sanitization/`.
- 2026-03-18: CruxClaw CLI — implemented. Go CLI with 13 commands (auth, secrets, connect, refresh, status, logs, admin). Terraform SSM parameters for discovery. GoReleaser + GitHub Actions for releases. See `specs/2026-03-18_feature_cruxclaw-cli/`.
- 2026-03-17: SSM port forwarding for web UI — per-user `gateway_port`, localhost Docker binding, SSM output commands. Phase 2 (auth tokens, per-user SSM docs) pending.
- 2026-03-17: Epics 5+6 complete — multi-user onboarding, Slack event router, patched OpenClaw image (HTTP webhook fix), ECR, persistent EBS volume
- 2026-03-16: Epic 4 complete — config integrity timer, CloudWatch agent + alarm, SNS topic
- 2026-03-16: Epic 3 complete — EC2 host running, OpenClaw container healthy, Slack socket mode connected, local gateway decommissioned
- 2026-03-15: Epic 2 complete — IAM role + deny-dangerous policy, KMS key, 5 secrets populated
- 2026-03-15: Epic 1 complete — VPC + networking (31 resources: VPC, subnets, fck-nat ASG, zero-ingress SG, NACLs, flow logs)
- 2026-03-15: Epic 0 complete — Terraform foundation (S3 state backend + DynamoDB locks) verified and working
- 2026-03-15: GLaDOS initialized, mission defined, security standards + roadmap + tech stack created
