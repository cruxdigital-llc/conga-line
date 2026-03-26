<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-25
To modify: Edit directly.
-->

# Product Roadmap

## Completed Phases

### Phase 1 — First User Live on AWS ✅
Single-user OpenClaw on hardened AWS infrastructure.
- Epic 0: Terraform foundation (S3 state + DynamoDB locks)
- Epic 1: VPC + networking (zero ingress, fck-nat, NACLs, flow logs — 31 resources)
- Epic 2: IAM + secrets (least-privilege role, deny-dangerous policy, KMS, Secrets Manager)
- Epic 3: EC2 + Docker bootstrap (cap-drop ALL, no-new-privileges, resource limits, systemd hardening)
- Epic 4: Config integrity monitoring (SHA256 timer, CloudWatch alarm, SNS alerts)

### Phase 2 — Multi-User with Shared Slack App ✅
Single Slack app with centralized event router, repeatable onboarding.
- Epics 5+6: Multi-user onboarding, Slack event router (Socket Mode → HTTP fan-out)
- Containers use HTTP webhook mode via forked OpenClaw image

### Phase 3 — Modular Deployment ✅
Pluggable provider architecture decoupled from AWS.
- Provider interface (17 methods) with AWS, local Docker, and remote SSH implementations
- Common package for config generation, routing, behavior composition
- Per-agent network isolation, file-based secrets (mode 0400), config integrity on all providers
- Egress proxy infrastructure deployed but not wired (enforcement is current work)

### Operational Maturity (Ongoing) ✅
- Conga Line CLI (13 commands, Go + Cobra)
- CLI hardening (silent failure fixes, validation, 28 unit tests)
- Agent pause/unpause (all providers)
- Behavior management (version-controlled SOUL.md, per-type composition)
- CLI JSON input/output for LLM-driven automation
- Remote provider (full lifecycle on VPS/bare-metal/RPi, SSH tunneling)
- SSH auto-reconnect for MCP server
- Open-source sanitization (gitignored config, .example templates)

---

## Active: Promotion Pipeline

The organizing principle: local → remote → enterprise is a promotion pipeline. Security and routing policy is a portable artifact (`conga-policy.yaml`) that travels with the deployment. Each provider enforces what it can.

### Pipeline Phase 1: Policy Foundation + Egress

**Goal:** Establish the portable policy artifact and close the #1 security gap (egress domain allowlisting).

| Area | Deliverable | Status |
|---|---|---|
| **Policy schema** | `conga-policy.yaml` with egress, routing, posture sections. Go types, validation, enforcement reporting. `conga policy validate` command. | ✅ Complete — `specs/2026-03-25_feature_policy-schema/` |
| **Egress — All providers** | Per-agent Squid proxy with domain-based CONNECT filtering. Local: validate (warn) or enforce modes. Remote/AWS: always enforce. Unified mechanism. | ✅ Complete — `specs/2026-03-25_feature_egress-allowlist/` |
| **Version awareness** | `conga status` shows OpenClaw version + security update availability. | Planned |
| **Demo playbook** | 5 ready-today scenarios (container escape, network isolation, config tamper, SSRF, IAM deny). | Planned |

### Pipeline Phase 2: Multi-Provider Routing + Promotion

**Goal:** Model routing via Bifrost proxy, promotion command, cost tracking.

| Area | Deliverable | Status |
|---|---|---|
| **Routing policy** | Routing section of `conga-policy.yaml`: model registry, fallback chains, cost limits, task rules. Ollama auto-detection on local. | Planned (Spec 3) |
| **Bifrost sidecar** | Deploy Bifrost as sidecar container on remote/AWS. Generate config from routing policy. Cost tracking via metrics endpoint. | Planned (Spec 5) |
| **Promote command** | `conga admin promote --from local --to remote/aws`. Validates policy against target. Reports enforcement gaps. Copies config + policy (not secrets). | Planned (Spec 6) |
| **Security posture reporting** | `conga status` security section, `conga policy audit`, CVE awareness. | Planned (Spec 4) |
| **Per-user agent binding (remote)** | End users access only their assigned agent via CLI. Admin retains full SSH access. | Planned |
| **Controls matrix** | CIS Docker Benchmark, NIST 800-190, AWS Well-Architected mapping. | Planned (Spec 8) |

### Pipeline Phase 3: Runtime Security + Advanced Routing

**Goal:** Sensitivity-aware routing, OpenShell integration evaluation, advanced hardening.

| Area | Deliverable | Status |
|---|---|---|
| **Sensitivity routing** | Keyword/pattern classification forcing sensitive prompts to self-hosted models (Ollama sidecar). | Planned (Spec 7) |
| **OpenShell evaluation** | Evaluate NVIDIA OpenShell integration for per-action runtime policy. `conga-policy.yaml` references optional OpenShell policy. | Planned |
| **Docker rootless** | Evaluate on Ubuntu 24.04 / Debian 12. If feasible, default for new remote setups. | Planned |
| **Custom seccomp** | Profile OpenClaw's syscall patterns, tighten beyond Docker default. | Planned |
| **GuardDuty + AWS Config** | Anomaly detection + security group drift detection. | Planned |
| **Proxy-based credential injection** | Agent sees placeholder tokens; proxy resolves to real secrets at request time. | Planned |

### Pipeline Phase 4: Enterprise Hardening + Ecosystem

**Goal:** Compliance reporting, advanced isolation, ecosystem integrations.

| Area | Deliverable | Status |
|---|---|---|
| **Compliance reporting** | `conga policy compliance` command generates report from `compliance_frameworks` declaration. | Planned (Spec 8) |
| **gVisor (Level 2)** | `--runtime=runsc` for stronger container sandboxing. | Planned |
| **Per-agent subnets (Level 3)** | Separate private subnet per agent with NACLs. | Planned |
| **Per-user VPCs (Level 4)** | Separate VPC per user via Transit Gateway. Documented pattern. | Planned |
| **Demo: policy promotion** | Define policy locally → `conga admin promote --to aws` → show enforcement in production. | Planned |
| **Routing analytics** | Cost per model, savings vs single-provider baseline, compliance-grade audit trail. | Planned |

---

## Backlog (Unscheduled)

### Operational
- [ ] Per-user SSO permission sets (CongaUser vs CongaAdmin)
- [ ] Per-user custom SSM documents (each user can only use their own port)
- [ ] Rewrite Slack router in Go (replace Node.js `router/src/index.js`)
- [ ] Self-service container restart via signal file
- [ ] Automated secret rotation
- [ ] EBS snapshot backups
- [ ] CloudWatch dashboard (per-container resources, NAT throughput, error rates)
- [ ] Idle-shutdown alarm
- [ ] Runbook: common operations

### Pre-Release
- [ ] Git history rewrite (scrub PII before public release)
- [ ] Evaluate spec files for public repo (keep, strip, or wiki)
- [ ] VPS end-to-end testing (Hetzner/DigitalOcean)
- [ ] User-facing setup guide documentation
- [ ] SECURITY-GUIDE.md for remote provider (VPS hardening best practices)

### Future Providers
- [ ] Kubernetes provider (Helm chart + kubectl)
- [ ] ECS/Fargate provider
- [ ] Multi-cloud (GCP Cloud Run, Azure Container Instances)

See `product-knowledge/standards/security.md` for the security model and isolation upgrade path.
