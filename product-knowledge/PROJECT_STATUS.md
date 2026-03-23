# GLaDOS System Status

This document reflects the *current state* of the codebase and project.
It should be updated whenever a significant change occurs in the architecture, roadmap, or standards.

## Project Overview
**Mission**: Hardened, per-user-isolated deployment of OpenClaw via pluggable providers. See [MISSION.md](MISSION.md).
**Current Phase**: Active Development

## Architecture
Pure infrastructure project — no application code. Go CLI + Terraform deploying OpenClaw as Docker containers via pluggable providers.

- **Provider model**: CLI uses `Provider` interface with implementations for AWS, local Docker, and VPS (in progress)
- **AWS**: Single EC2 host with per-agent Docker containers, SSM access, Secrets Manager, zero ingress (~$10/mo)
- **Local**: Per-agent Docker containers on local machine, file-based secrets, no cloud services
- **VPS** (planned): Per-agent Docker containers on any VPS, SSH access, file-based secrets (~$5-10/mo)
- **Common**: Per-agent network isolation, optional Slack router, cap-drop ALL hardening

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

### 2. Conga Line CLI — ✅ Complete
- [x] All 11 phases implemented and verified. See `specs/2026-03-18_feature_cruxclaw-cli/`

### 3. SSM-Driven Bootstrap Discovery — Specified, Ready for Implementation
*Lead: Architect + QA*
- [x] Requirements defined: `specs/2026-03-19_feature_ssm-driven-bootstrap-discovery/requirements.md`
- [x] Plan defined: `specs/2026-03-19_feature_ssm-driven-bootstrap-discovery/plan.md`
- [x] Spec defined: `specs/2026-03-19_feature_ssm-driven-bootstrap-discovery/spec.md`
- [x] Persona review passed (Architect + QA)
- [x] Standards gate passed (1 warning: IAM widening, accepted)
- [ ] Step 1: Unified SSM namespace (`/conga/agents/`) + config params
- [ ] Step 2: Widen IAM secrets policy for dynamic agents
- [ ] Step 3: Rewrite bootstrap for SSM discovery + update router.tf + CLI changes
- [ ] Step 4: Verify CLI compatibility + migration

### 4. CLI Hardening — Verified Complete
*See `specs/2026-03-19_feature_cli-hardening/` for full trace*
- Remaining deferred items: CLIContext struct migration, params_test.go, agent_test.go, executor command handler migration

### 5. Behavior Management — Verified Complete
*See `specs/2026-03-20_feature_behavior-management/` for full trace*

### 6. Conga Line Rename — Verified Complete
*See `specs/2026-03-20_feature_conga-line-rename/` for full trace*

### 7. Modular Deployment — Verified Complete
*See `specs/2026-03-21_feature_modular-deployment/` for full trace*

### 8. Agent Pause / Unpause — Verified Complete
*See `specs/2026-03-21_feature_agent-pause/` for full trace*

### 9. VPS Provider — Verified Complete
*See `specs/2026-03-22_feature_vps-provider/` for full trace*
- [ ] Phase 1: SSH foundation (`ssh.go`)
- [ ] Phase 2: Docker helpers (`docker.go`)
- [ ] Phase 3: Core provider + agent lifecycle (`provider.go`)
- [ ] Phase 4: Secrets + integrity (`secrets.go`, `integrity.go`)
- [ ] Phase 5: Setup wizard (`setup.go`)
- [ ] Phase 6: Config + wiring (`config.go`, `root.go`, `go.mod`)
- [ ] Phase 7: Documentation

### 10. Backlog / Upcoming
- [ ] Horizon 2: Operational maturity (secret rotation, backups, dashboards)
- [ ] Horizon 3: Advanced hardening (egress allowlisting, GuardDuty, Config rules)

## Known Issues / Technical Debt
- CLI has zero test coverage — addressed by CLI Hardening spec (Phase 4)
- CLI `admin.go` is 549 lines with 6 commands — addressed by CLI Hardening spec (Phase 5)
- Per-user API keys: each employee brings their own credentials and plugins
- Open question: egress domain allowlisting needed or port-443-only sufficient
- Open question: which OpenClaw skills/plugins to enable and sandbox requirements
- Behavior files (`behavior/base/SOUL.md`, `AGENTS.md`) are manually maintained copies of OpenClaw's defaults — will drift on image upgrades and need periodic reconciliation

## Recent Changes
- 2026-03-22: VPS Provider — third provider implementation for managing OpenClaw agent clusters on any VPS over SSH. 7 new files (2,139 lines): SSH client (connect, exec, SFTP, tunnel), remote Docker CLI helpers, full Provider interface (17 methods), file-based secrets, config integrity monitoring, setup wizard with Docker auto-install (apt/dnf/yum/pacman). 3 modified files (config.go, root.go, go.mod). 29 unit tests for shell injection prevention. SSH tunnel for gateway access, no inbound ports beyond SSH. Gateway-only and Slack modes supported. See `specs/2026-03-22_feature_vps-provider/`.
- 2026-03-21: Agent Pause / Unpause — per-agent pause/unpause via `conga admin pause/unpause`. Provider interface methods (`PauseAgent`, `UnpauseAgent`), both AWS (SSM scripts + parameter update) and local (Docker stop + JSON file). Routing excludes paused agents. `RefreshAll`, `CycleHost`, and bootstrap skip paused. `list-agents` shows STATUS column. See `specs/2026-03-21_feature_agent-pause/`.
- 2026-03-21: Modular Deployment — refactored CLI from hardcoded AWS to pluggable Provider interface. 16 new files, 15 modified. Provider interface (16 methods), common package (config/routing/behavior generation), AWS provider (wraps existing code, zero behavioral change), local Docker provider (file-based discovery, Docker CLI operations, secrets with mode 0400, config integrity monitoring), egress proxy for network isolation. New flags: `--provider aws|local`, `--data-dir`. 33 test cases added for common package. All existing tests pass. See `specs/2026-03-21_feature_modular-deployment/`.
- 2026-03-21: Conga Line Rename — comprehensive rebrand from "OpenClaw"/"CruxClaw" to "Conga Line". CLI binary `cruxclaw` → `conga`. Go module path, Terraform resources, SSM/Secrets/S3 paths (`/conga/`), Docker/systemd naming (`conga-`), host paths (`/opt/conga/`), CloudWatch namespace (`CongaLine`), GoReleaser, 80+ files across all layers. Upstream Open Claw references preserved. See `specs/2026-03-20_feature_conga-line-rename/`.
- 2026-03-20: Behavior Management — version-controlled behavior markdown (SOUL.md, AGENTS.md, USER.md) with base + type-specific composition, S3 deployment pipeline, systemd ExecStartPre auto-sync, `admin refresh-all` CLI command. Supports user vs team agent behavioral differentiation and per-agent overrides. See `specs/2026-03-20_feature_behavior-management/`.
- 2026-03-19: CLI Hardening — fixed 3 silent failure bugs, tightened Slack ID validation, added --timeout flag, AWS service interfaces for testability, HostExecutor interface for future local mode, 28 unit tests (7 test files), split admin.go into 4 files, human-readable uptime display, CI test/coverage steps. See `specs/2026-03-19_feature_cli-hardening/`.
- 2026-03-18: Open-source sanitization — removed all hardcoded environment-specific values (account IDs, Slack IDs, SSO URLs, usernames). Gitignored `backend.tf` + `terraform.tfvars` with `.example` files. Added `openclaw_image` variable. New `conga init` command for first-run config. Consolidated README. See `specs/2026-03-18_feature_open-source-sanitization/`.
- 2026-03-18: Conga Line CLI — implemented. Go CLI with 13 commands (auth, secrets, connect, refresh, status, logs, admin). Terraform SSM parameters for discovery. GoReleaser + GitHub Actions for releases. See `specs/2026-03-18_feature_cruxclaw-cli/`.
- 2026-03-17: SSM port forwarding for web UI — per-user `gateway_port`, localhost Docker binding, SSM output commands. Phase 2 (auth tokens, per-user SSM docs) pending.
- 2026-03-17: Epics 5+6 complete — multi-user onboarding, Slack event router, patched OpenClaw image (HTTP webhook fix), ECR, persistent EBS volume
- 2026-03-16: Epic 4 complete — config integrity timer, CloudWatch agent + alarm, SNS topic
- 2026-03-16: Epic 3 complete — EC2 host running, OpenClaw container healthy, Slack socket mode connected, local gateway decommissioned
- 2026-03-15: Epic 2 complete — IAM role + deny-dangerous policy, KMS key, 5 secrets populated
- 2026-03-15: Epic 1 complete — VPC + networking (31 resources: VPC, subnets, fck-nat ASG, zero-ingress SG, NACLs, flow logs)
- 2026-03-15: Epic 0 complete — Terraform foundation (S3 state backend + DynamoDB locks) verified and working
- 2026-03-15: GLaDOS initialized, mission defined, security standards + roadmap + tech stack created
