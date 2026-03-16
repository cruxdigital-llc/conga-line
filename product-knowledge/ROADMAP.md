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

### Epic 3: EC2 + Docker Bootstrap (Single User: Aaron)
- [ ] Launch template: t4g.medium (4GB RAM), Graviton, encrypted EBS, IMDSv2 enforced
- [ ] User-data script: install Docker, pull OpenClaw image, fetch secrets from Secrets Manager
- [ ] Aaron's openclaw.json generated from template with:
  - [ ] Channel allowlist: `C0ALL272SV8`
  - [ ] Model: `anthropic/claude-opus-4-6`
  - [ ] Skills: trello (credentials from env vars)
  - [ ] Tools profile: coding, web search via brave
  - [ ] Gateway: loopback, token auth
- [ ] Config file: root-owned, mode 0444
- [ ] Docker container with:
  - [ ] Read-only config mount
  - [ ] Isolated Docker network
  - [ ] Resource limits (memory, CPU, pids)
  - [ ] Non-root user (uid 1000)
  - [ ] Docker rootless mode
  - [ ] --cap-drop=ALL, --security-opt=no-new-privileges, seccomp profile
  - [ ] Secrets injected as env vars
- [ ] Systemd unit with hardening
- [ ] OS hardening: remove SSH, sysctl lockdown, unattended-upgrades

### Epic 4: Config Integrity + Monitoring
- [ ] Systemd timer: hash-check openclaw.json, alert on change
- [ ] CloudWatch log group for gateway logs
- [ ] CloudWatch alarm for config integrity alert

### Milestone: Aaron's local gateway replaced by AWS deployment
- [ ] Stop local OpenClaw gateway
- [ ] End-to-end test: message in `C0ALL272SV8` → response from AWS container
- [ ] Trello skill working
- [ ] Verify secrets not on disk, config immutable

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

- [ ] Egress domain allowlisting (Squid proxy or AWS Network Firewall)
- [ ] GuardDuty for anomaly detection
- [ ] AWS Config rules for security group drift detection
- [ ] Blue/green deploys via launch template versioning
- [ ] Scale pattern: when to split to multiple hosts (evaluate per-container resource usage)
- [ ] **Isolation Level 2**: gVisor runtime (`--runtime=runsc`) for stronger container sandboxing
- [ ] **Isolation Level 3**: Per-user subnets with NACLs for network-level segmentation (when scaling to multiple hosts)
- [ ] **Isolation Level 4**: Per-user VPCs via Transit Gateway/VPC Peering (if client contracts require full network isolation)

See `product-knowledge/standards/security.md` → "Isolation Upgrade Path" for detailed criteria.
