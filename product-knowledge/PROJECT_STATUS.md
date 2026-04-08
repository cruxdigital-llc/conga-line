# GLaDOS System Status

This document reflects the *current state* of the codebase and project.
It should be updated whenever a significant change occurs in the architecture, roadmap, or standards.

## Project Overview
**Mission**: Hardened, per-user-isolated deployment of OpenClaw via pluggable providers. See [MISSION.md](MISSION.md).
**Current Phase**: Active Development

## Architecture
Pure infrastructure project — no application code. Go CLI + Terraform deploying OpenClaw as Docker containers via pluggable providers.

- **Provider model**: CLI uses `Provider` interface with implementations for AWS, local Docker, and remote (SSH)
- **AWS**: Single EC2 host with per-agent Docker containers, SSM access, Secrets Manager, zero ingress (~$10/mo)
- **Local**: Per-agent Docker containers on local machine, file-based secrets, no cloud services
- **Remote**: Per-agent Docker containers on any SSH host (VPS, bare metal, RPi), file-based secrets (~$5-10/mo)
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

### 9. Remote Provider (formerly VPS) — ✅ Verified Complete, E2E Tested on Raspberry Pi
*See `specs/2026-03-22_feature_vps-provider/` for full trace*
- Full lifecycle verified on Raspberry Pi (Debian 13, ARM64, 905MB RAM): setup, add-user, status, logs, secrets, connect (SSH tunnel, HTTP 200), pause, unpause, teardown
- 3 bugs found and fixed during integration testing (SSH auth ordering, first-time setup flow, non-root sudo)
- [x] Phase 1: SSH foundation (`ssh.go`)
- [x] Phase 2: Docker helpers (`docker.go`)
- [x] Phase 3: Core provider + agent lifecycle (`provider.go`)
- [x] Phase 4: Secrets + integrity (`secrets.go`, `integrity.go`)
- [x] Phase 5: Setup wizard (`setup.go`)
- [x] Phase 6: Config + wiring (`config.go`, `root.go`, `go.mod`)
- [x] Phase 7: Documentation

### 10. CLI JSON Input — Verified Complete
*See `specs/2026-03-23_feature_cli-json-input/` for full trace*

### 12. Portable Policy Schema — ✅ Verified and Complete
*See `specs/2026-03-25_feature_policy-schema/` for full trace*

### 13. Egress Domain Allowlisting — ✅ Verified and Complete
*See `specs/2026-03-25_feature_egress-allowlist/` for full trace*

### 14. Channel Abstraction — Verified Complete
*See `specs/2026-03-26_feature_channel-abstraction/` for full trace*


### 15. MCP Policy Tools — ✅ Verified and Complete
*See `specs/2026-03-26_feature_mcp-policy-tools/` for full trace*

### 16. Network-Level Egress Enforcement — ✅ Verified and Complete
*See `specs/2026-03-26_feature_network-level-egress-enforcement/` for full trace*
- Phase 3 (AWS) completed as part of Feature #18 (Portable Egress Policy Compliance)

### 17. Channel Management CLI — ✅ Verified and Complete
*See `specs/2026-03-27_feature_channel-management-cli/` for full trace*

### 18. Portable Egress Policy Compliance — Verified Complete
*Lead: Architect + QA*
*See `specs/2026-03-28_feature_portable-egress-policy-compliance/` for full trace*
- [x] Requirements defined
- [x] Plan defined
- [x] Spec defined
- [x] Persona review passed (PM + Architect + QA)
- [x] Standards gate passed (2 warnings, 0 violations)
- [x] Phase 1: Default mode change — normalize empty to `enforce` (security-first)
- [x] Phase 2: Remote provider — respect `mode` field in ProvisionAgent, RefreshAgent, ensureEgressIptables
- [x] Phase 3: AWS bootstrap — parse `mode` in generate_egress_conf(), add iptables DROP rules + systemd hooks
- [x] Phase 4: Enforcement report — unify egressReport() to be mode-driven for all providers
- [x] Phase 5: Tests & documentation updates — all 17 packages pass

### 19. Non-Root Container Enforcement — Verified Complete
*Lead: Architect + QA*
*See `specs/2026-03-29_feature_non-root-containers/` for full trace*
- [x] Requirements defined: `specs/2026-03-29_feature_non-root-containers/requirements.md`
- [x] Plan defined: `specs/2026-03-29_feature_non-root-containers/plan.md`
- [x] Spec defined: `specs/2026-03-29_feature_non-root-containers/spec.md`
- [x] Persona review passed (Architect + QA)
- [x] Standards gate passed (8/8 checks clear, pre and post implementation)
- [x] Phase 1: Agent containers — `--user 1000:1000` across all providers (6 files)
- [x] Phase 2: Router containers — `--user 1000:1000` across all providers (3 files)
- [x] Phase 3: Security documentation update

### 20. Manifest Bootstrap — ✅ Verified and Complete
*See `specs/2026-03-30_feature_manifest-apply/` for full trace*
- `conga bootstrap` is additive-only, no state management. Policy section seeds `conga-policy.yaml` only on first run (existing policy file takes precedence).

### 21. Terraform Provider — Planned (Future Roadmap)
*Lead: Architect + PM + QA*
*See `specs/2026-03-30_feature_terraform-provider/` for full trace*
- [x] Requirements defined
- [x] Plan defined (resource model, architecture, implementation phases)
- [ ] Spec (deferred — implement when enterprise use case materializes)
- [ ] Implementation (8 phases: skeleton → core resources → channels → policy → data sources → import → tests → registry)

### 22. Model Routing — Planned (Future Roadmap)
*See `specs/2026-03-27_feature_model-routing/` for full trace*
- [x] Schema defined (`RoutingPolicy` in `pkg/policy/policy.go`)
- [x] Validation implemented
- [x] MCP tool scaffolding (`conga_policy_set_routing`)
- [ ] Spec (deferred — requires Bifrost integration design)
- [ ] Implementation (model selection logic, sidecar proxy, cost limits enforcement)

### 23. Agent Portability — Verified (Local Provider Complete)
*Lead: Architect + QA + PM*
*See `specs/2026-04-05_feature_agent-portability/` for full trace*
- [x] Requirements, plan, spec, persona review, standards gate
- [x] Phase 1-5: Runtime interface, OpenClaw extraction, local provider wiring, Hermes runtime, CLI changes
- [x] Phase 6: 38 runtime tests, all 16 test suites pass, go vet clean, gofmt clean
- [x] Verification: automated + persona + standards gate (post-impl) + spec retrospection
- [ ] Phase 6 (remaining): Remote & AWS provider integration
- [ ] Phase 6 (remaining): Routing webhook path parameterization for mixed-runtime Slack delivery
- [ ] Phase 1: Runtime interface & registry (`pkg/runtime/`)
- [ ] Phase 2: Extract OpenClaw runtime (`pkg/runtime/openclaw/`)
- [ ] Phase 3: Wire local provider to Runtime interface
- [ ] Phase 4: Hermes runtime implementation (`pkg/runtime/hermes/`)
- [ ] Phase 5: CLI & data model changes (`--runtime` flag)
- [ ] Phase 6: Remote & AWS provider integration
- [ ] Phase 7: Testing & verification

### 24. CLI Integration Tests — Planning
*Lead: QA + Architect*
*See `specs/2026-04-07_feature_cli-integration-tests/` for full trace*
- [x] Requirements defined
- [x] Plan defined
- [ ] Spec
- [ ] Implementation (helpers, 4 test functions, CI job)
- [ ] Verification

### Backlog / Upcoming
- [ ] Horizon 2: Operational maturity (secret rotation, backups, dashboards)
- [ ] Horizon 3: Advanced hardening (GuardDuty, Config rules)

## Known Issues / Technical Debt
- CLI test coverage at ~27% (aws), ~28% (ui), ~10% (cmd) — see CLI Hardening spec (Phase 4). Deferred items: `params_test.go`, `agent_test.go`
- CLI `admin.go` split into 4 files — see CLI Hardening spec (Phase 5)
- Per-user API keys: each employee brings their own credentials and plugins
- Egress proxy enforcement uses HTTPS_PROXY env vars + iptables DROP rules to prevent bypass. See Network-Level Egress Enforcement spec (Feature 16)
- Open question: which OpenClaw skills/plugins to enable and sandbox requirements
- Behavior defaults (`behavior/default/SOUL.md`, `AGENTS.md`) are manually maintained — will drift on OpenClaw image upgrades and need periodic reconciliation

## Recent Changes
- 2026-04-07: Per-Agent Behavior Configuration — replaced the base + team/user composition model with a simpler two-layer approach: shared defaults at `behavior/default/` and per-agent overrides at `behavior/agents/<name>/`. Agent files fully replace defaults (no concatenation). New CLI: `conga agent {list,add,rm,show,diff}`. Manifest-tracked deployments with deletion reconciliation. Terraform auto-refresh trigger restarts agents when behavior files change. ExecStartPre now syncs deploy-behavior.sh from S3. OpenClaw-only files supported (SOUL.md, AGENTS.md, USER.md) — arbitrary filenames not loaded by OpenClaw. Tested end-to-end on local and AWS. See `specs/2026-04-04_feature_per-agent-config-overlay/`.
- 2026-04-05: Agent Portability — new `Runtime` interface (`pkg/runtime/`) making the agent runtime pluggable alongside the existing `Provider` interface. OpenClaw runtime extracted from `pkg/common/` into `pkg/runtime/openclaw/` (zero behavioral change). Hermes Agent runtime implemented in `pkg/runtime/hermes/` (YAML config, port 8642, Python health detection). Local provider fully wired to Runtime interface. `--runtime openclaw|hermes` flag, runtime choice persisted during `conga admin setup`, inherited by `add-user`/`add-team`. Data model: `Runtime` field on `AgentConfig`, `Config`, `SetupConfig`, `Manifest`. 20 new files, 13 modified, 38 runtime tests, all 16 test suites pass. Remote/AWS provider wiring deferred. See `specs/2026-04-05_feature_agent-portability/`.
- 2026-03-30: Manifest Bootstrap — new `conga bootstrap <manifest.yaml>` command for one-shot environment provisioning. Declarative YAML manifest describes provider, setup, agents, secrets, channels, bindings, and initial egress policy. Optimized 6-step pipeline, each step idempotent. Secrets referenced via `$VAR` env var expansion from `--env` file, never stored in YAML. Existing `conga-policy.yaml` takes precedence over manifest policy section. New `pkg/manifest/` package (2 files, ~350 lines), CLI command, 25 unit tests. All 17 test packages pass. See `specs/2026-03-30_feature_manifest-apply/`.
- 2026-03-30: Bugfix — BindChannel/UnbindChannel router restart. Router was not restarted after `channels bind`/`unbind`, causing Slack messages to be silently dropped ("No route"). Added `restartRouter()`/`ensureRouter(ctx, true)` calls in both remote and local providers. Also made `connectNetwork` idempotent (ignore "already exists" errors). 4 files, all tests pass. See `specs/2026-03-30_bugfix_bind-channel-router-restart/`.
- 2026-03-29: Non-Root Container Enforcement — added explicit `--user 1000:1000` to all agent and router `docker run` commands across all 3 providers (local, remote, AWS). Router was running as root (`node:22-alpine` default); agent containers relied on fragile image `USER` directive. Also aligned AWS router with local/remote by adding missing `--tmpfs /tmp:rw,noexec,nosuid`. 7 files modified, 17 test packages pass. See `specs/2026-03-29_feature_non-root-containers/`.
- 2026-03-29: Secure-by-Default Egress — egress proxy now always deploys at agent provisioning time with deny-all posture (empty Lua allowlist = 403 on all domains). Policy file opens up specific domains. All three providers (local, remote, AWS) aligned. AWS provisioning scripts (add-user/add-team) updated to deploy proxy + iptables inline. Architecture principle 4 updated: "Secure by default, open by policy." Demo script updated for new flow. 11 files, 6 new tests. See `specs/2026-03-28_feature_portable-egress-policy-compliance/`.
- 2026-03-28: Portable Egress Policy Compliance — all three providers now respect the `mode` field in `conga-policy.yaml` egress section. Default changed from `validate` to `enforce` (security-first). Remote provider no longer hardcodes enforcement — checks mode like local. AWS bootstrap now parses mode, deploys proxy with Lua log-and-allow filter in validate mode (no iptables), and applies iptables DROP rules in DOCKER-USER chain in enforce mode (closing the cooperative-proxy-only gap). Systemd hooks (`ExecStartPost`/`ExecStopPost`) provide iptables resilience across container restarts. Enforcement report unified — all providers report based on mode, not provider name. 4 new tests, 9 files modified. New architecture standards added: Agent Data Safety (must), Interface Parity (must). See `specs/2026-03-28_feature_portable-egress-policy-compliance/`.
- 2026-03-26: Channel Abstraction — extracted all Slack-specific logic from core CLI into `pkg/channels/` behind a `Channel` interface. `AgentConfig.Channels []ChannelBinding` replaces `SlackMemberID`/`SlackChannel`. `SharedSecrets.Values map[string]string` replaces Slack-named fields. `--channel slack:ID` CLI flag replaces positional Slack ID args. Slack is the sole implementation in `channels/slack/`. All providers, CLI commands, MCP tools, routing, config generation, and behavior templates delegate to the channel interface. 5 new files, ~25 modified, 17 new test cases. Breaking change to agent JSON, SetupConfig JSON, and CLI args. AWS bootstrap scripts deferred. See `specs/2026-03-26_feature_channel-abstraction/`.
- 2026-03-26: Egress Domain Allowlisting — per-agent Envoy proxy for domain-based CONNECT filtering across all three providers. Unified enforcement mechanism with iptables DROP rules for network-level isolation. Policy-driven via `conga-policy.yaml` egress section. Local: validate (warn) or enforce (proxy + iptables) modes. Remote/AWS: always enforce when domains defined. Envoy handles HTTP CONNECT tunneling with Lua-based domain filtering. See `specs/2026-03-25_feature_egress-allowlist/`.
- 2026-03-25: Portable Policy Schema — `conga-policy.yaml` schema for declaring security and routing policy as a portable artifact. New `pkg/policy/` package with YAML parsing (`gopkg.in/yaml.v3`), validation (enum checks, domain format, unknown field rejection), per-agent override merging, and per-provider enforcement reporting. `conga policy validate` CLI command with `--file`, `--agent`, `--output json` support. 5 new files, 19 unit tests. See `specs/2026-03-25_feature_policy-schema/`.
- 2026-03-24: SSH Auto-Reconnect — MCP server's SSH connection now transparently recovers from stale/dead connections instead of requiring a Claude Code restart. Added `reconnect()`, `session()`, `sftpClient()` methods to `SSHClient` with single-retry semantics. 4 new tests with in-process SSH server. See `specs/2026-03-24_bugfix_ssh-reconnect/`.
- 2026-03-23: CLI JSON Input — `--json` and `--output json` flags for LLM/agent-driven CLI automation. All 20 commands support structured JSON input (replacing interactive prompts) and JSON output (replacing human-formatted text). Schema discovery via `conga json-schema <command>`. 4 new files, 18 modified, 25 unit tests. `SetupConfig` struct enables non-interactive `admin setup` across all providers. See `specs/2026-03-23_feature_cli-json-input/`.
- 2026-03-23: Remote Provider PR review fixes — 13 fixes across 7 files: `filepath.Join` → `posixpath.Join` for cross-platform remote path correctness, host key verification warning, shell injection fix in integrity log append, Docker install confirmation prompt, SSHKeyPath persistence, stale VPS naming cleanup, `Close()` method, `detectReadyPhase` tests. See `specs/2026-03-23_bugfix_remote-provider-pr-review/`.
- 2026-03-23: Remote Provider (renamed from VPS) — third provider implementation for managing OpenClaw agent clusters on any SSH-accessible host (VPS, bare metal, Raspberry Pi, Mac Mini, etc.). 7 new files (~2,100 lines): SSH client (connect, exec, SFTP, tunnel), remote Docker CLI helpers, full Provider interface (17 methods), file-based secrets, config integrity monitoring, setup wizard with Docker auto-install. 29 unit tests + full E2E lifecycle verified on Raspberry Pi (Debian 13, ARM64, 905MB RAM). 3 bugs found and fixed during integration: SSH auth ordering, first-time setup chicken-and-egg, non-root sudo. See `specs/2026-03-22_feature_vps-provider/`.
- 2026-03-21: Agent Pause / Unpause — per-agent pause/unpause via `conga admin pause/unpause`. Provider interface methods (`PauseAgent`, `UnpauseAgent`), both AWS (SSM scripts + parameter update) and local (Docker stop + JSON file). Routing excludes paused agents. `RefreshAll`, `CycleHost`, and bootstrap skip paused. `list-agents` shows STATUS column. See `specs/2026-03-21_feature_agent-pause/`.
- 2026-03-21: Modular Deployment — refactored CLI from hardcoded AWS to pluggable Provider interface. 16 new files, 15 modified. Provider interface (16 methods), common package (config/routing/behavior generation), AWS provider (wraps existing code, zero behavioral change), local Docker provider (file-based discovery, Docker CLI operations, secrets with mode 0400, config integrity monitoring), egress proxy for network isolation. New flags: `--provider aws|local`, `--data-dir`. 33 test cases added for common package. All existing tests pass. See `specs/2026-03-21_feature_modular-deployment/`.
- 2026-03-21: Conga Line Rename — comprehensive rebrand from "OpenClaw"/"CruxClaw" to "Conga Line". CLI binary `cruxclaw` → `conga`. Go module path, Terraform resources, SSM/Secrets/S3 paths (`/conga/`), Docker/systemd naming (`conga-`), host paths (`/opt/conga/`), CloudWatch namespace (`CongaLine`), GoReleaser, 80+ files across all layers. Upstream Open Claw references preserved. See `specs/2026-03-20_feature_conga-line-rename/`.
- 2026-03-20: Behavior Management — version-controlled behavior markdown (SOUL.md, AGENTS.md, USER.md) with S3 deployment pipeline, systemd ExecStartPre auto-sync, `admin refresh-all` CLI command. Superseded by Per-Agent Behavior Configuration (2026-04-07). See `specs/2026-03-20_feature_behavior-management/`.
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
