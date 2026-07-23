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
- Behavior management (version-controlled SOUL.md, per-agent overrides via `agents/<name>/`)
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
| **Egress — All providers** | Per-agent Envoy proxy with domain-based CONNECT filtering + iptables DROP rules. All providers respect `mode: enforce` (default) or `mode: validate`. AWS includes iptables enforcement. | ✅ Complete — `specs/2026-03-25_feature_egress-allowlist/`, `specs/2026-03-28_feature_portable-egress-policy-compliance/` |
| **Version awareness** | `conga status` shows OpenClaw version + security update availability. | Planned |
| **Demo playbook** | 5 ready-today scenarios (container escape, network isolation, config tamper, SSRF, IAM deny). | Planned |

### Pipeline Phase 2: Multi-Provider Routing + Promotion

**Goal:** Model routing via Bifrost proxy, promotion command, cost tracking.

**Precursors (landed):**
- Per-agent model override via `agents/<name>/agent.yaml` (schema v1). Supports `ollama` (native) and `openai` (compatible) providers. See `specs/2026-05-19_feature_local-model-routing/`.
- **In-runtime delegation routing (schema v2)**: top-level `subagents:` block declares a cheaper secondary model; OpenClaw / Hermes runtimes spawn subagents via `sessions_spawn` / `delegate_task` at the runtime's discretion (nudged by `delegation_mode: prefer|suggest`). Five canonical role packages (Ops/Data/Research Qwen-backed; Code-Dev/Writing Opus-backed with Qwen subagent) provisioned via `conga admin add-user|add-team --role <slug>`. See `specs/2026-05-22_feature_delegation-routing/`.

Fallback chains, cost limits, and cross-provider request-time routing (Bifrost) live in this phase and build on those precursors.

| Area | Deliverable | Status |
|---|---|---|
| **Routing policy** | Routing section of `conga-policy.yaml`: model registry, fallback chains, cost limits, task rules. Ollama auto-detection on local. Extends the existing `agent.yaml` `model:` block via reserved `model.fallbacks` key (schema v2). | Planned (Spec 3) |
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
- [ ] Admin-customizable agent config — `agent-custom.json` `$include` layering so operators can add MCP servers / skills / tools that survive restarts. **Implemented on PR #57** (verified live on the pinned image); pending merge + `terraform-provider-conga` release + deployed-path verification. See [specs/2026-06-09_feature_infrastructure-only-simplification/](../specs/2026-06-09_feature_infrastructure-only-simplification/).
- [ ] Declarative fleet + per-agent config — version-controlled custom-config layers (`agents/_defaults/<runtime>/fleet-custom.json` for all agents, `agents/<name>/custom.json` per-agent) deployed via the `$include` array (precedence root > admin-drift > per-agent > fleet), plus de-embedded operator-editable `openclaw-defaults.json` and `conga agent show-config`. **Implemented + code-verified on PR #61** (two review passes + verify-feature; `go test`/`vet`/`gofmt` green, standards gate PASS); pending **live T9.2** verification + merge + `terraform-provider-conga` release. Follow-up T2.4: unify the AWS bash boot path to consume the de-embedded defaults. See [specs/2026-06-10_feature_fleet-baseline-configuration/](../specs/2026-06-10_feature_fleet-baseline-configuration/).
- [ ] Per-user SSO permission sets (CongaUser vs CongaAdmin)
- [ ] Per-user custom SSM documents (each user can only use their own port)
- [ ] Rewrite Slack router in Go (replace Node.js `router/slack/src/index.js`, land in `channels/slack/`)
- [ ] Update AWS bootstrap scripts (`add-user.sh.tmpl`, `add-team.sh.tmpl`) for `channels` JSON format
- [x] Telegram channel implementation (Hermes-only; OpenClaw + Telegram gated as unsupported — see [specs/2026-05-22_feature_telegram-v2026.5-revamp/](../specs/2026-05-22_feature_telegram-v2026.5-revamp/))
- [ ] Add second OpenClaw-compatible channel implementation (Discord) to fully validate Channel interface for OpenClaw runtime
- [ ] OpenClaw + Telegram (Option B in the telegram-v2026.5-revamp spec) — per-agent direct bots, deferred until an operator needs it
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

### Terraform Provider
- [ ] `terraform-provider-conga` — wraps transactional CLI logic for declarative lifecycle management
- [ ] Resources: `conga_agent`, `conga_secret`, `conga_channel`, `conga_channel_binding`, `conga_policy`
- [ ] Provider config selects deployment target (local, remote, AWS) — same Go provider packages underneath
- [ ] Enables plan/apply/destroy, drift detection, state management — all handled by Terraform, not reimplemented
- [ ] See `product-knowledge/TERRAFORM_PROVIDER.md` for architecture details
- [ ] **`conga_secret` destroy should purge the underlying secret store entry.** Today, destroying a `conga_secret` (e.g. removing an agent secret from tfvars) drops it from terraform state but leaves the secret **active in AWS Secrets Manager** — orphaned, untracked, and still injected into the agent's env. Revoked credentials linger until manually deleted (`aws secretsmanager delete-secret`). The destroy path should schedule deletion of the backing Secrets Manager secret (and the local/remote file-store equivalents). Found 2026-06-11 when removing `linear-api-key` from team-a after moving Linear to OAuth/MCP. See also the runtime-image propagation gap (the `image` var doesn't drive `/conga/config/image` on AWS).

### Future Providers
- [ ] Kubernetes provider (Helm chart + kubectl)
- [ ] ECS/Fargate provider
- [ ] Multi-cloud (GCP Cloud Run, Azure Container Instances)

See `product-knowledge/standards/security.md` for the security model and isolation upgrade path.
