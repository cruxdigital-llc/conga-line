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

### Epic 4: Config Integrity + Monitoring
- [ ] Systemd timer: hash-check openclaw.json, alert on change
- [ ] CloudWatch log group for gateway logs
- [ ] CloudWatch alarm for config integrity alert

### Milestone: Aaron's local gateway replaced by AWS deployment ✅
- [x] Local OpenClaw gateway stopped (launchd unloaded)
- [x] End-to-end test: message in `C0ALL272SV8` → response from AWS container
- [x] Slack socket mode connected, channel resolved
- [x] Secrets in env file (root:root 0400), config integrity monitoring deferred to Epic 4

---

## Phase 2 — Onboarding Flow for User 2

Goal: A repeatable process to add a second employee. He provides his credentials and channel, and gets a working OpenClaw agent.

### Epic 5: Config Template + Onboarding Script
- [ ] Base `openclaw.json` template with shared settings (gateway, Slack app, security policies)
- [ ] Onboarding script that:
  - [ ] Prompts for: Slack channel ID, Anthropic API key, and any skill credentials (Trello, etc.)
  - [ ] Stores per-user secrets in Secrets Manager under `openclaw/{user_id}/*`
  - [ ] Generates per-user openclaw.json from template + user inputs
- [ ] Documentation: step-by-step onboarding guide for the new user

### Epic 6: Multi-User Terraform
- [ ] Terraform variables for user list/config (channel, secret ARNs, etc.)
- [ ] For-each pattern to stamp out per-user resources (secrets, configs, containers, systemd units)
- [ ] `terraform apply` deploys user 2's container alongside Aaron's
- [ ] Validate: both containers running, isolated networks, correct channel routing
- [ ] End-to-end test: messages in both channels → responses from correct containers only

### Milestone: 2 users operational
- [ ] Both users receiving responses in their respective Slack channels
- [ ] No cross-channel leakage
- [ ] Onboarding process documented and tested

---

## Horizon 2 — Operational Maturity

- [ ] Automated secret rotation (Lambda or scheduled task)
- [ ] EBS snapshot backups of OpenClaw memory/config
- [ ] CloudWatch dashboard: per-container resource usage, NAT throughput, error rates
- [ ] Idle-shutdown alarm to save costs when host is unused
- [ ] Runbook: common operations (rotate keys, add user, tear down user, recover NAT)

## Horizon 3 — Hardening + Scale

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
