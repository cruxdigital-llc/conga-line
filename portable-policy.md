# CONGA LINE — Strategic Research Brief

## Security Posture & Model Routing

**The Promotion Pipeline: Define Locally, Test on Remote, Enforce in Production**

March 25, 2026 — v4 | Prepared for Crux Digital LLC

---

# Executive Summary

This brief addresses two priorities for Conga Line: security posture and model routing. This revision introduces the organizing principle that changes both: the three providers are not three separate products for three separate audiences. They are a promotion pipeline.

> ### The Promotion Pipeline
>
> **Local (Dev)** — Define your agent's configuration, security policy, and routing rules on your own machine. Fast iteration, no cloud cost, no deploy cycle. The local provider validates your policy and warns about rules it can't enforce locally.
>
> **Remote (Staging)** — Promote to any SSH-accessible host. Validate that your configuration works on real infrastructure with real network conditions, multiple agents, and tunneled access. The remote provider enforces a broader subset of your policy.
>
> **Enterprise / AWS (Production)** — Promote to hardened cloud infrastructure. Every policy rule is enforced—egress allowlisting, IAM controls, runtime monitoring, audit trail. The same configuration you tested locally now runs with full enterprise controls layered on top.
>
> Each tier also works as a standalone destination. A hobbyist's local setup is their production. A small team's VPS is their production. But the configuration model is designed so that moving up the stack is a promotion, not a rewrite.

This reframe has a concrete architectural implication: security and routing policy should be a portable artifact—a deployment policy file that travels with the agent configuration. Each provider reads the same policy and enforces what it can, with the tools available to that tier. The local provider validates; the remote provider enforces partially; the enterprise provider enforces fully.

The good news is that Conga Line is already most of the way there. Agent configuration (openclaw.json), behavioral files (SOUL.md), and secrets naming conventions are already portable across providers. What's missing is a shared policy layer for security rules and routing configuration that follows the same pattern.

> ### Three Priorities
>
> 1. **Design the portable policy artifact** — A `conga-policy.yaml` (or equivalent) that defines egress rules, routing policy, and security posture declarations. Each provider enforces what it can. This is the architectural foundation for both initiatives.
>
> 2. **Egress control** — Enterprise: Squid forward proxy. Remote: iptables/dnsmasq. Local: configurable validate-only or enforce mode. All driven by the same egress rules in the policy file.
>
> 3. **Model routing via Bifrost** — Routing policy defined in the same portable artifact. Locally you test routing decisions against a single provider. On remote, real multi-provider routing. On enterprise, full sensitivity-aware routing with audit trail.

---

# Part 1: The Portable Policy Artifact

## 1.1 Why Policy Portability Matters

Today, Conga Line's security controls are implemented per-provider in Go code. Each provider makes its own decisions about Docker flags, network rules, and secrets handling. This works, and the controls are strong—but it means security posture is defined implicitly by which provider you choose, not explicitly by what you configure.

In the promotion pipeline model, the operator defines their intent once and the provider enforces it with available tools. This is the same pattern NVIDIA OpenShell uses—declarative YAML policies that are separate from the runtime that enforces them. Conga Line doesn't need to adopt OpenShell to adopt the pattern.

> ### The Core Idea
>
> The operator writes policy. The provider enforces policy. The policy travels with the deployment.
>
> When the same policy runs on a more capable provider, it gets stricter enforcement—not different rules. An egress allowlist that's informational on local becomes iptables rules on remote and a Squid proxy on AWS. The operator's intent is the same across all three.

## 1.2 What Goes in the Policy File

The policy artifact is a single file that lives alongside agent configuration. It defines three categories of policy:

### Egress Policy

Which external domains each agent (or all agents) can reach. This is the single highest-impact security control and the #1 gap in the current architecture.

| Field | Purpose |
|---|---|
| **allowed_domains** | List of domains the agent can reach (e.g., api.anthropic.com, api.openai.com, slack.com). Wildcards supported for subdomains. |
| **blocked_domains** | Explicit deny list. Takes precedence over allowed_domains. |
| **scope** | Global (all agents) or per-agent overrides. |

| Provider | Enforcement | Mechanism | Failure Mode |
|---|---|---|---|
| **Local** | Configurable: validate or enforce | Validate mode (default): CLI warns about unenforced rules. Enforce mode: routes agent traffic through the egress proxy container (deploy/egress-proxy/) with domain allowlisting. | Validate: agent can reach any domain, user is informed. Enforce: blocked requests fail, matching production behavior. |
| **Remote** | Partial enforcement | iptables OUTPUT rules or dnsmasq-based DNS filtering on the remote host | Best-effort. Depends on host OS capabilities. |
| **Enterprise** | Full enforcement | Squid forward proxy with domain allowlist. All container traffic routes through proxy. | Blocked requests logged and alerted. Agent receives connection refused. |

### Routing Policy

Which models to use, when, and under what constraints. This is the configuration that drives the Bifrost proxy.

| Field | Purpose |
|---|---|
| **default_model** | The model used when no routing rule matches (e.g., claude-sonnet-4-6). |
| **models** | Registry of available models with provider, cost-per-token, latency, and capability tags. |
| **sensitivity_rules** | Keyword/pattern lists that trigger routing to self-hosted models. Enterprise: enforced by OpenShell privacy router in Phase 3. |
| **cost_limits** | Daily/monthly budget per agent, per user, or global. Actions when exceeded: downgrade model, pause agent, alert. |
| **fallback_chain** | Ordered list of providers to try when the primary is unavailable. Addresses OpenClaw's billing/rate error caching issue. |
| **task_rules** | Mapping of task types (code, reasoning, simple) to preferred models. Enterprise: semantic classification. Team: rule-based. Hobbyist: optional. |

| Provider | Routing Capability | Mechanism | What the User Sees |
|---|---|---|---|
| **Local** | Model selection + optional Ollama | Config points OpenClaw to chosen model. If Ollama running locally, auto-detected during setup. | Pick your model in setup. Optionally use a local model. No proxy infrastructure needed. |
| **Remote** | Rule-based routing + failover | Bifrost sidecar. Tier 1 rules: simple/complex, budget cap, fallback chain. | Smarter model selection, cost tracking via conga status, automatic failover. |
| **Enterprise** | Full three-tier routing | Bifrost behind egress proxy. Tier 1 rules + Tier 2 semantic classification + Tier 3 quality gate. OpenShell privacy router (Phase 3). | Sensitivity-aware routing, audit trail, compliance-grade cost reporting. |

### Security Posture Declarations

The operator's stated expectations for the deployment's security properties. The provider enforces what it can and reports the gap.

| Declaration | Meaning |
|---|---|
| **isolation_level** | Desired container isolation: "standard" (Docker default), "hardened" (gVisor), "segmented" (per-agent subnets within the VPC). Provider warns if it can't meet the level. |
| **secrets_backend** | Preferred: "managed" (Secrets Manager), "file" (mode 0400), "proxy" (Phase 3 credential injection). Provider uses best available. |
| **monitoring** | Desired: "basic" (config integrity), "standard" (+ cloud logging), "full" (+ GuardDuty/runtime). Provider enables what's available. |
| **compliance_frameworks** | List of frameworks to map against (CIS Docker, NIST 800-190, AWS Well-Architected). Enterprise only—ignored on local/remote. |

> ### The Promotion Command
>
> `conga admin promote --from local --to remote` (or `--to aws`)
>
> Validates the policy against the target provider's capabilities. Reports which rules will be enforced, which will be partially enforced, and which require additional infrastructure. Copies agent configuration, behavioral files, and policy to the target. Does NOT copy secrets—those are set per-environment via `conga secrets set`.
>
> This is the bridge between "I tested this on my laptop" and "it's running in production."

## 1.3 OpenShell: Complementary, Not Competitive

NVIDIA OpenShell and Conga Line operate at different layers of the stack, which makes them naturally complementary. Understanding the boundary is important for both policy file design and go-to-market positioning.

> ### The Layer Boundary
>
> **Conga Line controls the box the agent runs in** — containers, networks, egress, secrets, resource limits. This is infrastructure isolation. A compromised agent hits these walls when it tries to reach the network, access files outside its mount, or escape the container.
>
> **OpenShell controls what the agent does inside the box** — per-action policy evaluation, tool invocation gating, privacy routing, audit trail. This is runtime behavior enforcement. A prompt-injected agent hits these walls when it tries to call a tool, read a file, or send data to an endpoint that policy doesn't allow.
>
> Neither replaces the other. A compromised agent that escapes OpenShell's runtime policy still hits Conga Line's container isolation and egress filtering. An agent that bypasses Conga Line's container controls (via a Docker CVE) still hits OpenShell's action interception. Defense in depth requires both layers.

**Policy file design implication:**

The `conga-policy.yaml` should define Conga Line's infrastructure-layer policy (egress rules, routing configuration, isolation level, secrets backend, monitoring). It should NOT try to define OpenShell's runtime-layer policy (per-action rules, tool invocation permissions, privacy routing rules). Instead, the policy file should optionally reference an external OpenShell policy file. When the deployment promotes to an OpenShell-enabled environment, Conga Line passes the referenced policy to the OpenShell runtime. When OpenShell isn't present, the reference is ignored.

This keeps the concerns clean: Conga Line owns infrastructure policy, OpenShell owns runtime policy, and the operator can author both as part of the same promotion pipeline without either project needing to subsume the other's domain.

**Open question to resolve before implementation:**

Should Conga Line validate OpenShell policy files even when OpenShell isn't the enforcement engine? There's an argument for yes: if the local provider can parse and validate an OpenShell policy, operators get early feedback on runtime policy errors before promoting to an environment where those errors would block deployment. But this creates a dependency on OpenShell's policy schema, which is early-stage and evolving. The safer approach may be to validate only the reference (does the file exist, is it valid YAML) without interpreting its contents.

---

# Part 2: Security Posture

## 2.1 Universal Baseline (All Providers, Automatic)

These controls are already implemented, cost nothing to the user, and apply identically regardless of provider. They happen automatically on `conga admin setup`. This is the foundation that makes every tier secure by default.

- **Container:** --cap-drop ALL, --no-new-privileges, --memory 2g, --cpus 0.75, --pids-limit 256, non-root uid 1000, default seccomp
- **Network:** per-agent Docker bridge networks, localhost-only port binding, no inter-container communication
- **Secrets:** mode 0400 files with atomic write (local/remote), env var injection, never in openclaw.json
- **Config:** SHA256 integrity hash monitoring with alerts on mismatch
- **Router:** HMAC-SHA256 event signing, 30-second dedup TTL, bot message filtering
- **Auth:** gateway token authentication, device pairing
- **Image:** pinned to known-good version (currently v2026.3.11)

These controls should grow slowly and only with additions that are invisible to the user. They are the floor, not the ceiling.

## 2.2 Enforcement Escalation by Provider

When a policy defines a security control, each provider enforces it with the best mechanism available. The operator sees the same policy; the enforcement gets stricter as the deployment moves up the pipeline.

| Control | Local (Dev) | Remote (Staging) | Enterprise (Prod) |
|---|---|---|---|
| **Egress filtering** | Configurable: validate mode (default, warns only) or enforce mode (egress proxy container with domain allowlisting). | iptables OUTPUT rules or dnsmasq DNS filtering. Best-effort enforcement. | Squid forward proxy. Full domain allowlisting. Blocked requests logged and alerted. |
| **Host access** | N/A. User's machine. | SSH key-only auth. No password. Gateway via SSH tunnel. | Zero ingress. No SSH. SSM-only. Gateway via SSM tunnel. |
| **Secrets backend** | File, mode 0400. User owns disk encryption. | File, mode 0400 on remote. Document: encrypt disk. | AWS Secrets Manager. Encrypted at rest. IAM-scoped. No on-disk secrets. |
| **IAM / RBAC** | N/A. Single user. | Admin: direct SSH access to host. End users: CLI-only access scoped to their assigned agent. conga connect/logs/secrets enforces agent-level binding. | AWS SSO + IAM roles with explicit deny. Per-user permission sets (Phase 2). |
| **Monitoring** | Config integrity hash. Container logs via conga logs. | Config integrity + container logs. Optional: forward to log aggregator. | VPC flow logs (30-day), CloudWatch alerting, config integrity. Phase 3: GuardDuty. |
| **Runtime policy** | Validate mode: check policy syntax, report unenforced rules. Enforce mode: activate egress proxy and routing enforcement locally. | Enforce egress + routing rules. Report what requires enterprise. | Full enforcement. Phase 3: OpenShell for per-action interception + privacy routing. |
| **Container read-only** | Router container only (existing). | Router container only. Investigate agent container feasibility. | Router + agent containers where feasible. systemd ReadOnlyPaths as backup. |
| **Isolation upgrade** | N/A. | N/A. Docker default is sufficient. | Phase 3: gVisor. Phase 4: per-agent subnets with NACLs. Beyond that: separate AWS accounts per tenant (documented, not automated). |

## 2.3 OpenClaw CVE Exposure Across the Pipeline

OpenClaw's 2026 CVE wave includes 8+ high-severity vulnerabilities. Exposure varies by provider because each offers different network and access controls. The promotion pipeline adds a new dimension: you can test your agent's behavior against these CVEs locally before deploying to production.

| CVE | Local (Dev) | Remote (Staging) | Enterprise (Prod) |
|---|---|---|---|
| **25253 (WebSocket RCE)** | EXPOSED if user browses malicious sites while gateway is open on localhost. Pin to patched version. | MITIGATED. SSH tunnel—gateway not internet-exposed. | MITIGATED. Zero ingress. SSM tunnel only. |
| **32048 (sandbox escape)** | EXPOSED. Prompt injection can trigger. Container isolation is only boundary. | EXPOSED. Same, plus lateral movement to other agents possible. | EXPOSED. Egress control (Phase 1) limits exfiltration. OpenShell (Phase 3) addresses root cause. |
| **32055 (path traversal)** | EXPOSED. Agent can write outside workspace. Container filesystem is the limit. | EXPOSED. Same as local. | EXPOSED. --read-only on agent containers would mitigate. Investigate feasibility. |
| **32056 (shell injection)** | EXPOSED. Arbitrary code within container. cap-drop ALL limits blast radius. | EXPOSED. Same as local. | EXPOSED. Egress control prevents exfiltration even if code execution achieved. |
| **32025 (WS brute-force)** | EXPOSED on localhost. Browser-origin attack possible. | MITIGATED. SSH tunnel. | MITIGATED. SSM tunnel. |
| **32042, 32051 (priv esc, authz)** | Low risk. Single user—privilege escalation on own agent has limited impact. | Medium. Could affect other team agents. Pin to patched version. | Partially mitigated. SSM limits access. Pin to patched version. |

> ### Pipeline Insight
>
> The local tier is MORE exposed to browser-origin CVEs (25253, 32025) than remote/enterprise, because the gateway runs on localhost and is browser-accessible. The most impactful local-tier security feature is version awareness: `conga status` should show the current OpenClaw version and whether a security update is available.
>
> The in-container CVEs (32048, 32055, 32056) are equally exposed across all tiers. The difference is blast radius: on enterprise, egress control prevents exfiltration; on local/remote, container isolation is the only boundary. This is exactly why the egress policy in `conga-policy.yaml` matters—even if local can't enforce it (in validate mode), the operator has defined their intent and it will be enforced when promoted.

## 2.4 Framework Alignment (Enterprise)

Framework alignment is relevant when the deployment reaches enterprise. The controls matrix maps existing Conga Line controls to CIS Docker Benchmark, NIST 800-190, and AWS Well-Architected. This is a Phase 2 documentation exercise—the controls exist; the mapping doesn't.

The portable policy artifact adds value here: when the policy file declares `compliance_frameworks: ["cis-docker", "nist-800-190"]`, the enterprise provider can generate a compliance report showing which controls are enforced and where gaps remain. This is a Phase 4 feature but the data model supports it from the start.

OpenShell alignment becomes relevant at enterprise Phase 3. As described in Section 1.3, Conga Line and OpenShell are complementary—infrastructure isolation and runtime behavior enforcement are separate layers. The policy file references an optional OpenShell policy for runtime rules; Conga Line's own policy handles infrastructure. Ecosystem partners (Cisco DefenseClaw, CrowdStrike, TrendAI) provide third-party validation that strengthens the enterprise security story without requiring Conga Line to compete with OpenShell's domain.

## 2.5 Demo Playbook (Enterprise)

Five scenarios are ready today. The sixth requires Phase 1 egress work. A seventh leverages the promotion pipeline itself.

1. **Container escape attempt** — cap-drop ALL, no-new-privileges, PID limit blocking privilege escalation.
2. **Network isolation** — Per-agent networks prevent lateral movement. Zero-ingress SG: nmap shows zero open ports.
3. **Config tampering** — Modify openclaw.json from within container. SHA256 detects it. systemd ReadOnlyPaths blocks it.
4. **Metadata SSRF blocked** — curl 169.254.169.254 from container. IMDSv2 hop limit 1 blocks.
5. **IAM explicit deny** — Even with expanded role, explicit deny blocks dangerous API calls.
6. **Egress control (Phase 1)** — Prompt injection attempts exfiltration. Squid proxy blocks and logs. Most compelling enterprise demo.
7. **Policy promotion (Phase 2)** — Define an egress allowlist and routing policy locally. Run `conga admin promote --to aws`. Show the same policy now enforced by Squid proxy, with blocked requests alerting in CloudWatch. This demos the entire pipeline story.

---

# Part 3: Model Routing

## 3.1 Routing Through the Pipeline Lens

The routing policy is part of the portable artifact. What changes across tiers is not what the operator wants (which models, which rules) but what infrastructure enforces it.

| Routing Feature | Local (Dev) | Remote (Staging) | Enterprise (Prod) |
|---|---|---|---|
| **Model selection** | Choose model in config or setup. Test responses from different models locally. | Same config, now running against real providers via Bifrost. | Same config, behind egress proxy, with audit trail. |
| **Provider failover** | Test locally: simulate provider errors, verify fallback chain works. | Real failover: if Anthropic rate-limits, Bifrost tries OpenAI. | Same + alerting on failover events. Addresses OpenClaw billing cache bug. |
| **Cost tracking** | Estimate: conga status shows projected cost based on model and usage. | Real: Bifrost tracks actual spend. conga status shows real costs. | Same + budget enforcement. Alerts when thresholds hit. Compliance reporting. |
| **Sensitivity routing** | Test: define sensitivity rules, verify classification against test prompts. | Enforce: sensitive prompts route to Ollama sidecar (if GPU available). | Full enforcement: sensitivity gate + OpenShell privacy router (Phase 3). Audit trail. |
| **Task classification** | Config-only: map task types to models. Manual classification. | Rule-based: simple/complex classification based on token count and keywords. | Semantic: Tier 2 ML-based classification. Tier 3 cascade/quality gate. |
| **Self-hosted models** | Ollama on localhost. Auto-detected during setup. Direct API call, no proxy. | Ollama sidecar container on same host. Routed via Bifrost. | Ollama sidecar + egress policy enforcement. Sensitive data provably stays on-infra. |

## 3.2 Competitive Landscape

### AI Gateways

| Gateway | Key Strength | License | Conga Line Fit |
|---|---|---|---|
| **Bifrost** | 11µs overhead, 12+ providers, OpenAI-compatible, single binary | Apache 2.0 | Recommended. Light enough for a VPS, capable enough for enterprise. Works across the entire pipeline. |
| **LiteLLM** | Broadest provider support, Python, large community | Open source | Alternative for Python-oriented teams. Heavier footprint. |
| **Cloudflare AI** | Edge, semantic caching, DLP, unified billing | SaaS | Not self-hostable. Rules out data sovereignty. |
| **Kong AI** | Semantic routing, PII sanitization, enterprise API mgmt | Self-hosted | Interesting for enterprise customers already on Kong. Overkill for the pipeline. |

### Intelligent Routers

| Router | Approach | Conga Line Relevance |
|---|---|---|
| **Not Diamond** | ML classifier on human preference data. RouteLLM paper. | Research input. Informs enterprise Tier 2 classification design. |
| **Martian** | Real-time dynamic routing for cost+quality. | Validates 30–70% savings claim. Commercial competitor at enterprise. |
| **OpenRouter** | Multi-model proxy with community pricing. | Could serve as a Bifrost backend provider. Broadest model catalog. |
| **Unify** | Per-prompt routing by model AND provider (same model, different infra). | Interesting optimization for enterprise—cheapest instance of a given model. |

## 3.3 Routing Security

Model routing introduces its own attack surface. The portable policy model handles most of this naturally:

- **Credential management:** multiple provider API keys, all managed through existing per-agent secrets system. No new mechanism needed. Secrets don't promote—they're set per-environment via `conga secrets set`.
- **Provider data policies:** the routing policy defines which providers are allowed for which data classifications. The egress policy enforces it at the network level.
- **Prompt injection to manipulate routing:** Tier 1 rules evaluate on raw prompt features (token count, keyword match) before any model sees the content. Can't be bypassed via prompt injection.
- **Model confusion attacks:** output filtering applied uniformly regardless of responding model. Defined in policy, enforced by Bifrost.
- **Audit trail:** enterprise only. Which model handled which request, cost, sensitivity classification, routing decision. Required for compliance.

---

# Part 4: Implementation Roadmap

Each phase advances all three tiers in parallel. Enterprise work doesn't block local/remote work. The policy artifact is Phase 1 because it's the foundation for everything else.

## Phase 1: Policy Foundation + Egress (Weeks 1–4)

| Tier | Security | Routing |
|---|---|---|
| **All** | Design and implement `conga-policy.yaml` schema. Egress rules, routing policy, security posture declarations. CLI validates policy on all providers. | Define routing policy schema within the portable artifact. Model registry, fallback chains, cost limits. |
| **Local** | `conga status` shows OpenClaw version + security update availability. Policy enforcement mode: validate (default, warnings only) or enforce (activates egress proxy container for realistic testing). | Model selection in setup (Anthropic models + Ollama auto-detection). Config points at chosen model. |
| **Remote** | SECURITY-GUIDE.md: VPS hardening best practices (firewall, fail2ban, disk encryption). Policy: iptables enforcement for egress rules. | Deploy Bifrost sidecar. Single backend (Anthropic). Validate proxy transparency. |
| **Enterprise** | Squid forward proxy for egress domain allowlisting, driven by policy file. OpenClaw image CVE scanning in CI/CD. Demo playbook for 5 ready-today scenarios. | Deploy Bifrost behind egress proxy. Single backend. Validate that egress proxy allowlists Bifrost's outbound domains. |

## Phase 2: Multi-Provider Routing + Promotion (Weeks 5–8)

| Tier | Security | Routing |
|---|---|---|
| **All** | Implement `conga admin promote --from <src> --to <target>`. Validates policy against target capabilities. Reports enforcement gaps. Copies config + policy (not secrets). | Multi-provider key management: `conga secrets set <provider>-api-key`. Per-agent cost tracking via `conga status`. |
| **Local** | No additional security work. Baseline is right-sized. | Ollama integration: local models offered during setup. Simple fallback: if primary errors, try secondary. |
| **Remote** | Per-user agent binding: end users access only their assigned agent via CLI (`conga connect/logs/secrets` scoped to their agent). Admin retains full SSH access. Egress filtering addendum in SECURITY-GUIDE.md. | Multi-provider backends in Bifrost (Anthropic + OpenAI + Ollama). Tier 1 rule-based routing: simple/complex + budget cap. |
| **Enterprise** | Formal controls matrix (CIS Docker, NIST 800-190, AWS Well-Architected). Per-user SSO permission sets. Evaluate rootless Docker. | Tier 1 sensitivity gate: PII/keyword detection forces self-hosted model. Hard routing overrides per agent. 6th demo scenario: egress control. |

## Phase 3: Runtime Security + Advanced Routing (Weeks 9–12)

| Tier | Security | Routing |
|---|---|---|
| **Local** | No additional security. Policy validation continues to improve: better warnings, suggest promote paths. | Cost optimization: `conga status` recommends cheaper models where appropriate. |
| **Remote** | Evaluate Docker rootless on Ubuntu 24.04, Debian 12. If feasible, default for new remote setups. | Self-hosted models: Ollama sidecar on GPU VPS. Sensitivity routing via keyword matching. |
| **Enterprise** | Evaluate OpenShell integration (YAML policy per agent, compatible with `conga-policy.yaml`). GuardDuty + AWS Config. Custom seccomp profile. Proxy-based credential injection. | Tier 2 semantic task classification. Tier 3 cascade/quality gate. OpenShell privacy router as sensitivity enforcement. 7th demo: policy promotion pipeline. |

## Phase 4: Enterprise Hardening + Ecosystem (Weeks 13+)

| Tier | Security | Routing |
|---|---|---|
| **Local** | Maintain baseline. No added complexity. | Community routing presets ("budget", "quality", "privacy") via single flag in policy. |
| **Remote** | Lightweight log-based anomaly detection (no OpenShell overhead). | Routing analytics: cost per model, savings vs. single-provider baseline. |
| **Enterprise** | Cisco DefenseClaw. gVisor (Level 2). Per-agent subnets with NACLs (Level 3). Multi-tenant via separate AWS accounts (documented pattern). Compliance reporting from policy declarations. | LLM-based routing for multi-intent queries. Per-customer routing templates. Centralized analytics with compliance-grade audit trail. |

---

# Appendix A: Design Principles

**Policy is portable. Enforcement is tiered.**
The operator defines what they want once. Each provider does its best. The gap between intent and enforcement is visible and closable by promoting to a more capable tier.

**Local is for iteration. Remote is for validation. Enterprise is for production.**
But each tier is also a valid production destination for its audience. A hobbyist's local setup and a small team's VPS are real deployments with real users—they just have different threat models.

**The universal baseline grows slowly.**
Every control added to all providers is another thing that can break, another thing to document, and another barrier to adoption. The competitive advantage at the lower tiers is simplicity. The baseline should only grow with controls that are invisible to the user.

**Secrets don't promote.**
Configuration and policy travel up the pipeline. Secrets are set per-environment. An API key used for local testing is not the same key used in production. `conga secrets set` is always per-environment.

**Default to warn, offer enforce.**
In validate mode (the default for local), the provider warns about unenforced policy rules without blocking agent startup. The operator decides whether that's acceptable for local testing. In enforce mode, the provider activates available enforcement mechanisms (e.g., the egress proxy container) so the operator can test real enforcement behavior before promoting. The choice is the operator's—Conga Line respects their judgment about which mode fits their workflow.

**Own the box, not the behavior.**
Conga Line controls infrastructure: containers, networks, egress, secrets, resource limits. OpenShell controls runtime behavior: per-action policy, tool invocation, privacy routing. The policy file reflects this boundary—Conga Line's policy defines infrastructure intent and optionally references an OpenShell policy for runtime rules. Neither project subsumes the other's domain. They are complementary layers in a defense-in-depth stack.

---

# Appendix B: Key References

### Security

- CIS Docker Benchmark: cisecurity.org/benchmark/docker
- NIST 800-190: nist.gov/publications/detail/sp/800-190
- AWS Well-Architected Security Pillar: docs.aws.amazon.com/wellarchitected/latest/security-pillar
- NVIDIA OpenShell: github.com/NVIDIA/OpenShell (Apache 2.0)
- Cisco DefenseClaw: announced RSA 2026, open source
- OpenClaw CVEs: blink.new/blog/openclaw-cve-2026-new-vulnerabilities-fix

### Model Routing

- Bifrost: github.com/maxim-ai/bifrost (Apache 2.0, 11µs overhead)
- RouteLLM: arxiv.org/abs/2407.11511
- FrugalGPT: arxiv.org/abs/2305.05176
- Awesome AI Model Routing: github.com/Not-Diamond/awesome-ai-model-routing

### Conga Line

- Repository: github.com/cruxdigital-llc/conga-line
- Security standards: product-knowledge/standards/security.md
- Architecture: CLAUDE.md