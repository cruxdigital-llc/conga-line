<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-15
To modify: Edit directly.
-->

# Product Roadmap

## MVP — 2 Users via Slack

Goal: Two team members using OpenClaw via dedicated Slack channels, running as isolated Docker containers on a single hardened EC2 host. Target cost: ~$10/mo total.

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
- [ ] Shared Secrets Manager secrets for Slack tokens (xapp-, xoxb-)
- [ ] Per-user Secrets Manager secret for Anthropic API key (or shared — TBD)
- [ ] KMS key for EBS encryption
- [ ] SSM instance profile policies

### Epic 3: EC2 + Docker Bootstrap
- [ ] Launch template: t4g.medium (4GB RAM), Graviton, encrypted EBS, IMDSv2 enforced
- [ ] User-data script: install Docker, pull OpenClaw image, fetch secrets from Secrets Manager
- [ ] Per-user openclaw.json with channel allowlist (root-owned, mode 0444)
- [ ] Per-user Docker container with:
  - [ ] Read-only config mount
  - [ ] Isolated Docker network (no inter-container communication)
  - [ ] Per-container resource limits (memory, CPU, pids)
  - [ ] Non-root user (uid 1000)
  - [ ] Per-user env vars (API keys injected, not shared across containers)
- [ ] Per-user systemd units with hardening (NoNewPrivileges, ProtectSystem, ReadOnlyPaths, MemoryMax)
- [ ] OS hardening: remove SSH, sysctl lockdown, unattended-upgrades

### Epic 4: Config Integrity + Monitoring
- [ ] Systemd timer: hash-check all openclaw.json files, alert on change
- [ ] CloudWatch log group for gateway logs (per-user log streams)
- [ ] CloudWatch alarm for config integrity alert

### Epic 5: Slack Integration
> Slack app already exists and is validated locally with a working gateway.
- [ ] Store existing Slack tokens (xapp-, xoxb-) in Secrets Manager
- [ ] Create 2 dedicated channels (#openclaw-user1, #openclaw-user2)
- [ ] Configure each container's openclaw.json with groupPolicy allowlist for its channel
- [ ] Validate two containers connect to the same Slack app simultaneously via Socket Mode
- [ ] End-to-end test: message in Slack → response from correct container only

### Epic 6: Terraform Packaging
- [ ] Modular Terraform structure (networking, iam, compute, secrets modules)
- [ ] variables.tf with user list/config (channel name, API key secret ARN, etc.)
- [ ] For-each pattern to stamp out per-user resources (secrets, configs, containers)
- [ ] terraform.tfvars.example with documented variables
- [ ] terraform plan produces clean output for 2 users

---

## Horizon 2 — Operational Maturity

- [ ] Automated secret rotation (Lambda or scheduled task)
- [ ] EBS snapshot backups of OpenClaw memory/config
- [ ] CloudWatch dashboard: per-container resource usage, NAT throughput, error rates
- [ ] Idle-shutdown alarm to save costs when host is unused
- [ ] Runbook documentation for common operations (rotate keys, add user, tear down user)

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
