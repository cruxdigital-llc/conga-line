<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-15
To modify: Edit directly.
-->

# Product Roadmap

## Phase 1 — First User Live on AWS

Goal: Migrate a locally-running OpenClaw gateway to a hardened AWS deployment. End state: message in Slack channel → response from AWS-hosted container.

### Epic 0: Terraform Foundation ✅
- [x] S3 backend bucket `<project_name>-terraform-state-<account_id>` (versioned, AES256, public access blocked)
- [x] DynamoDB table `<project_name>-terraform-locks` (PAY_PER_REQUEST)
- [x] backend.tf configuration (S3 backend with state locking)
- [x] Bootstrap shell script (`terraform/bootstrap.sh`, idempotent)

### Epic 1: VPC + Networking ✅
- [x] VPC (10.0.0.0/24)
- [x] Public subnet (10.0.0.0/28) for fck-nat instance
- [x] Private subnet (10.0.0.16/28) for OpenClaw host
- [x] fck-nat (t4g.nano, ASG-backed, self-healing) via `RaJiska/fck-nat/aws` v1.4.0
- [x] Route tables: private subnet 0.0.0.0/0 → fck-nat ENI
- [x] NACLs: 443 egress + DNS + ephemeral return only
- [x] Security group: zero ingress, HTTPS + DNS egress
- [x] VPC Flow Logs → CloudWatch `/openclaw/vpc-flow-logs` (30-day retention)

### Epic 2: IAM + Secrets
- [ ] Instance IAM role with least-privilege policies
- [ ] Deny-dangerous IAM policy (iam:*, ec2:RunInstances, lambda:*, etc.)
- [ ] KMS key for EBS encryption
- [ ] SSM instance profile policies
- [ ] Secrets Manager secrets per user:
  - [ ] Slack botToken (shared — will be reused by user 2)
  - [ ] Slack appToken (shared)
  - [ ] Anthropic API key
  - [ ] Gateway auth token
  - [ ] Trello API key + token
  - [ ] Brave API key (if needed)

### Epic 3: EC2 + Docker Bootstrap (Single User) ✅
- [x] Launch template: t4g.medium (4GB RAM), Graviton, encrypted EBS, IMDSv2 hop limit 1
- [x] User-data script: install Docker, pull OpenClaw image, fetch secrets from Secrets Manager
- [x] Per-user openclaw.json generated with:
  - [x] Channel allowlist: `<channel_id>`
  - [x] Model: `anthropic/claude-opus-4-6`
  - [x] Skills: trello (credentials from env vars)
  - [x] Tools profile: coding
  - [x] Gateway: loopback
- [x] Docker container with:
  - [x] Isolated Docker network (`openclaw-<member_id>`)
  - [x] Resource limits: 2GB memory, 1.5 CPU, 256 pids
  - [x] Non-root user (uid 1000, `node`)
  - [x] `--cap-drop=ALL`, `--security-opt=no-new-privileges`
  - [x] Secrets injected as env vars via systemd EnvironmentFile
  - [ ] ~~Docker rootless mode~~ — deferred to Horizon 3 (AL2023 missing `fuse-overlayfs`, `slirp4netns`)
  - [ ] ~~Seccomp profile~~ — using Docker default; custom profile deferred to Horizon 3
  - [ ] ~~Read-only config mount~~ — not possible due to OpenClaw hot-reload .tmp files; integrity via monitoring (Epic 4)
- [x] Systemd unit with restart policy (`Restart=always`, `RestartSec=10`)
- [x] OS hardening: openssh-server removed, sysctl lockdown, dnf-automatic enabled
- [x] `NODE_OPTIONS="--max-old-space-size=1536"` to prevent V8 heap OOM

### Epic 4: Config Integrity + Monitoring ✅
- [x] Systemd timer: hash-check openclaw.json every 5 minutes (configurable via `config_check_interval_minutes`)
- [x] CloudWatch agent shipping container logs + integrity check logs to `/openclaw/gateway`
- [x] CloudWatch metric filter for `CONFIG_INTEGRITY_VIOLATION`
- [x] CloudWatch alarm → SNS topic (`alert_email` configurable, no subscribers by default)
- [ ] TODO: Move hash baseline to after OpenClaw's first boot settles (~60s delay) to avoid false positive

### Milestone: First user's local gateway replaced by AWS deployment ✅
- [x] Local OpenClaw gateway stopped (launchd unloaded)
- [x] End-to-end test: message in Slack channel → response from AWS container
- [x] Slack socket mode connected, channel resolved
- [x] Secrets in env file (root:root 0400), config integrity monitoring deferred to Epic 4

---

## Phase 2 — Multi-User with Shared Slack App + Router

Goal: Single shared Slack app with a centralized event router. Repeatable onboarding via `cruxclaw admin add-user`.

### Architecture
Single Slack app → Socket Mode connection held by the router (`router/src/index.js`) → HTTP fan-out to per-user containers in webhook mode. This avoids the Socket Mode event-splitting problem (multiple connections to the same app = ~50% missed messages). The approach was unblocked by our fork's HTTP webhook fix (PR openclaw/openclaw#49514).

### Epic 5+6: Multi-User Onboarding
- [x] `users` Terraform variable drives all per-user resources
- [x] Dynamic secret discovery — users self-serve via `cruxclaw secrets set`
- [x] User-data loops over users to create containers
- [x] Persistent EBS data volume survives instance replacement
- [x] SSM-based user provisioning via `cruxclaw admin add-user` (replaces `scripts/add-user.sh`)
- [x] **Slack event router deployed** — single Socket Mode connection, HTTP fan-out to per-user containers
- [x] **Containers use HTTP webhook mode** — `mode: "http"` with `botToken`/`signingSecret` in config
- [x] **`add-user.sh.tmpl` aligned with router architecture** — updates `routing.json`, connects router to user network
- [ ] Validate: both users receiving 100% of messages in their channels
- [ ] End-to-end test: no missed messages, no stale-socket restarts
- [ ] Onboarding guide update: document shared app setup and `cruxclaw admin add-user` workflow

### Milestone: 2 users operational
- [ ] Both users receiving responses in their respective Slack channels
- [ ] No cross-channel leakage
- [ ] Onboarding process documented and tested

---

## Horizon 2 — Operational Maturity

- [x] **SSM port forwarding for web UI (Phase 1)**: Per-user `gateway_port` in `users` variable, Docker `-p 127.0.0.1:<port>:18789`, SSM output commands. See `specs/2026-03-17_feature_ssm-port-forwarding/`.
- [x] **CruxClaw CLI**: Cross-platform Go CLI for non-technical users. AWS SSO auth, SSM Parameter Store discovery, embedded bash scripts. 13 commands: auth, secrets, connect, refresh, status, logs, admin (add-user, remove-user, list-users, cycle-host). See `specs/2026-03-18_feature_cruxclaw-cli/`.
- [x] **CLI Hardening**: Fixed silent failures, tightened Slack ID validation, added --timeout flag, AWS service interfaces + HostExecutor interface for testability and future local mode, 28 unit tests, admin.go split, CI test/coverage. See `specs/2026-03-19_feature_cli-hardening/`.
- [ ] **Git history rewrite before public release**: Git history contains real AWS account IDs, Slack member/channel IDs, SSO URLs, and usernames from before the OSS sanitization (PR #3). Run `git filter-repo` or equivalent before making the repo public. Alternatively, accept that the account ID is public info and scrub only PII (usernames, Slack IDs).
- [ ] **Evaluate spec files for public repo**: `specs/` contains internal planning artifacts (requirements, plans, task lists, trace logs). Decide whether to keep (transparency), strip (noise reduction), or move to a wiki before open-sourcing.
- [ ] **SSO permission sets for CLI access control**: Create scoped IAM Identity Center permission sets — `OpenClawUser` (secrets on own path, SSM StartSession, EC2 DescribeInstances) and `OpenClawAdmin` (SSM SendCommand, SSM PutParameter, EC2 Stop/Start, secrets on shared path). Currently all SSO users get `AdministratorAccess`. The CLI enforces user-level isolation (blocks `--user` override on non-admin commands), but IAM-level enforcement is defense-in-depth.
- [ ] **SSM port forwarding Phase 2 — user isolation**: Per-user custom SSM documents (hardcode allowed port), IAM policy restrictions (each user can only use their document), gateway auth token (`openclaw/{user_id}/gateway-token` secret).
- [x] **Slack event router (single app for all users)**: Deployed using forked OpenClaw image with HTTP webhook fix (PR openclaw/openclaw#49514). Router at `router/src/index.js`. Single Slack app, single Socket Mode connection, HTTP fan-out to per-user containers.
- [ ] **Rewrite Slack router in Go**: Replace `router/src/index.js` (Node.js) with a Go binary for consistency with the CruxClaw CLI. ~100 lines, no external dependencies beyond the AWS SDK. Could be built into the `cruxclaw` binary as a `cruxclaw router` subcommand or as a standalone binary deployed to the instance.
- [ ] **Tighten bootstrap cleanup glob**: The pre-discovery cleanup loop stops all `openclaw-*.service` units, which catches non-agent units like `openclaw-config-check.timer`. Harmless (they're recreated later in the same bootstrap), but the glob should exclude known non-agent units for clarity.
- [ ] **Fix router network reconnection on container restart**: When a user container restarts, the router loses its Docker network connection to that container. Need to add `ExecStartPost` to each user's systemd unit (or the router's unit) to reconnect. Currently requires manual `docker network connect` after any container restart.
- [ ] **Auto-approve Slack pairing on user setup**: Run `openclaw pairing approve slack <MEMBER_ID>` automatically after container starts for the first time. Currently requires manual SSM command. Could be added to the user-data bootstrap or the `add-user.sh` script.
- [ ] **Self-service container restart via signal file**: User tells their OpenClaw agent to restart. Agent writes `.restart-requested` to its writable volume. A host-level systemd timer (or inotifywait) watches for the file, rebuilds the env file from Secrets Manager, and restarts the container. No Docker socket or systemd access exposed to the container. Enables users to add secrets and pick them up without admin involvement.
- [ ] Automated secret rotation (Lambda or scheduled task)
- [ ] EBS snapshot backups of OpenClaw memory/config
- [ ] CloudWatch dashboard: per-container resource usage, NAT throughput, error rates
- [ ] Idle-shutdown alarm to save costs when host is unused
- [ ] Runbook: common operations (rotate keys, add user, tear down user, recover NAT)

## Horizon 3 — Hardening + Scale

- [ ] **Proxy-based credential injection** (inspired by NVIDIA OpenShell) — agent process sees placeholder tokens, a proxy resolves to real secrets at request time. Secrets never exist in agent memory. Stronger than env var injection.
- [ ] **Declarative policy engine** (inspired by NVIDIA OpenShell's OPA/Rego) — replace ad-hoc security hardening with formal policy definitions for filesystem, network, process access
- [ ] **Docker rootless mode** — blocked by AL2023 missing `fuse-overlayfs` and `slirp4netns`; revisit with Docker CE on Ubuntu or custom AL2023 build
- [ ] **Custom seccomp profile** — currently using Docker default; profile OpenClaw's syscall patterns and tighten
- [ ] **Read-only config enforcement** — OpenClaw hot-reload writes .tmp files next to config; investigate disabling hot-reload or upstream fix for Issue #9627
- [ ] Egress domain allowlisting (Squid proxy or AWS Network Firewall)
- [ ] GuardDuty for anomaly detection
- [ ] AWS Config rules for security group drift detection
- [ ] Blue/green deploys via launch template versioning
- [ ] Scale pattern: when to split to multiple hosts (evaluate per-container resource usage)
- [ ] **Isolation Level 2**: gVisor runtime (`--runtime=runsc`) for stronger container sandboxing
- [ ] **Isolation Level 3**: Per-user subnets with NACLs for network-level segmentation (when scaling to multiple hosts)
- [ ] **Isolation Level 4**: Per-user VPCs via Transit Gateway/VPC Peering (if client contracts require full network isolation)

See `product-knowledge/standards/security.md` → "Isolation Upgrade Path" for detailed criteria.
