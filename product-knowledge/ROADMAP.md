<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-15
To modify: Edit directly.
-->

# Product Roadmap

## Phase 1 — Get Aaron Live on AWS

Goal: Migrate Aaron's locally-running OpenClaw gateway to a hardened AWS deployment. End state: message in Slack channel `C0ALL272SV8` → response from AWS-hosted container.

### Epic 0: Terraform Foundation ✅
- [x] S3 backend bucket `openclaw-terraform-state-167595588574` (versioned, AES256, public access blocked)
- [x] DynamoDB table `openclaw-terraform-locks` (PAY_PER_REQUEST)
- [x] backend.tf configuration (S3 backend with state locking)
- [x] Bootstrap shell script (`terraform/bootstrap.sh`, idempotent)

### Epic 1: VPC + Networking ✅
- [x] VPC `vpc-067ea4b769f7e994a` (10.0.0.0/24)
- [x] Public subnet (10.0.0.0/28) for fck-nat instance
- [x] Private subnet `subnet-06119ed58d773bd9d` (10.0.0.16/28) for OpenClaw host
- [x] fck-nat (t4g.nano, ASG-backed, self-healing) via `RaJiska/fck-nat/aws` v1.4.0
- [x] Route tables: private subnet 0.0.0.0/0 → fck-nat ENI
- [x] NACLs: 443 egress + DNS + ephemeral return only
- [x] Security group `sg-0f0c53457d0220f7c`: zero ingress, HTTPS + DNS egress
- [x] VPC Flow Logs → CloudWatch `/openclaw/vpc-flow-logs` (30-day retention)

### Epic 2: IAM + Secrets
- [ ] Instance IAM role with least-privilege policies
- [ ] Deny-dangerous IAM policy (iam:*, ec2:RunInstances, lambda:*, etc.)
- [ ] KMS key for EBS encryption
- [ ] SSM instance profile policies
- [ ] Secrets Manager secrets for Aaron:
  - [ ] Slack botToken (shared — will be reused by user 2)
  - [ ] Slack appToken (shared)
  - [ ] Anthropic API key
  - [ ] Gateway auth token
  - [ ] Trello API key + token
  - [ ] Brave API key (if needed)

### Epic 3: EC2 + Docker Bootstrap (Single User: Aaron) ✅
- [x] Launch template: t4g.medium (4GB RAM), Graviton, encrypted EBS, IMDSv2 hop limit 1
- [x] User-data script: install Docker, pull OpenClaw image, fetch secrets from Secrets Manager
- [x] Aaron's openclaw.json generated with:
  - [x] Channel allowlist: `C0ALL272SV8`
  - [x] Model: `anthropic/claude-opus-4-6`
  - [x] Skills: trello (credentials from env vars)
  - [x] Tools profile: coding
  - [x] Gateway: loopback
- [x] Docker container with:
  - [x] Isolated Docker network (`openclaw-aaron`)
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

### Milestone: Aaron's local gateway replaced by AWS deployment ✅
- [x] Local OpenClaw gateway stopped (launchd unloaded)
- [x] End-to-end test: message in `C0ALL272SV8` → response from AWS container
- [x] Slack socket mode connected, channel resolved
- [x] Secrets in env file (root:root 0400), config integrity monitoring deferred to Epic 4

---

## Phase 2 — Multi-User with Separate Slack Apps

Goal: Each user gets their own Slack app (Socket Mode), solving the event-splitting problem. Repeatable onboarding process.

### Why separate apps?
Slack Socket Mode load-balances events across multiple connections to the same app. Two containers on one app means each only receives ~50% of messages. A router/proxy approach was prototyped but blocked by an OpenClaw bug (HTTP webhook mode has a module identity split — see `specs/2026-03-17_feature_slack-router/LEARNINGS.md`).

### Epic 5+6: Multi-User Onboarding (partially complete)
- [x] `users` Terraform variable drives all per-user resources
- [x] Dynamic secret discovery — users self-serve via `cruxclaw secrets set`
- [x] User-data loops over users to create containers
- [x] Persistent EBS data volume survives instance replacement
- [x] SSM-based user provisioning via `cruxclaw admin add-user` (replaces `scripts/add-user.sh`)
- [ ] **Switch to per-user Slack apps**: Each user entry in `users` variable includes their own `slack_app_token` and `slack_bot_token` secret paths
- [ ] **Remove shared Slack tokens** — each user manages their own Slack app tokens
- [ ] **Onboarding guide update**: Include Slack app creation steps per user
- [ ] **Revert containers to Socket Mode** — remove router, revert `mode: "http"` to `mode: "socket"`
- [ ] **Clean up router artifacts** — remove router container, systemd unit, S3 objects
- [ ] Validate: both users receiving 100% of messages in their channels
- [ ] End-to-end test: no missed messages, no stale-socket restarts

### Milestone: 2 users operational
- [ ] Both users receiving responses in their respective Slack channels
- [ ] No cross-channel leakage
- [ ] Onboarding process documented and tested

---

## Horizon 2 — Operational Maturity

- [x] **SSM port forwarding for web UI (Phase 1)**: Per-user `gateway_port` in `users` variable, Docker `-p 127.0.0.1:<port>:18789`, SSM output commands. See `specs/2026-03-17_feature_ssm-port-forwarding/`.
- [x] **CruxClaw CLI**: Cross-platform Go CLI for non-technical users. AWS SSO auth, SSM Parameter Store discovery, embedded bash scripts. 13 commands: auth, secrets, connect, refresh, status, logs, admin (add-user, remove-user, list-users, cycle-host). See `specs/2026-03-18_feature_cruxclaw-cli/`.
- [ ] **SSO permission sets for CLI access control**: Create scoped IAM Identity Center permission sets — `OpenClawUser` (secrets on own path, SSM StartSession, EC2 DescribeInstances) and `OpenClawAdmin` (SSM SendCommand, SSM PutParameter, EC2 Stop/Start, secrets on shared path). Currently all SSO users get `AdministratorAccess`. The CLI enforces user-level isolation (blocks `--user` override on non-admin commands), but IAM-level enforcement is defense-in-depth.
- [ ] **SSM port forwarding Phase 2 — user isolation**: Per-user custom SSM documents (hardcode allowed port), IAM policy restrictions (each user can only use their document), gateway auth token (`openclaw/{user_id}/gateway-token` secret).
- [ ] **Slack event router (single app for all users)**: Blocked on OpenClaw HTTP webhook mode bug (module identity split). Router code exists at `router/src/index.js` and works correctly. Revisit when OpenClaw fixes HTTP mode or when we can build from source. Would eliminate the need for separate Slack apps per user. See `specs/2026-03-17_feature_slack-router/LEARNINGS.md`.
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
