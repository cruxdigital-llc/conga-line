<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-28
To modify: Edit directly. These standards are expected to evolve as we learn more.
-->

# Security Standards — Conga Line

> These standards apply to all deployment providers (local, remote, AWS).
> Provider-specific controls are marked. Standards are reviewed and updated
> as we gain operational experience and discover new threat vectors.

## Principles

1. **Zero trust the AI agent** — OpenClaw can execute arbitrary commands. Assume it will be prompt-injected and design controls that hold even when it is.
2. **Immutable configuration** — Runtime config that controls security boundaries (channel allowlists, permissions) must not be writable by the process it governs.
3. **Least privilege everywhere** — IAM roles, filesystem permissions, network access, and Docker capabilities should all be minimally scoped.
4. **Defense in depth** — No single control should be the only thing preventing a breach. Layer network, filesystem, process, and container isolation.
5. **Secrets are protected at rest** — On AWS, secrets are in Secrets Manager (encrypted, never on disk). On local and remote, secrets are files with mode 0400 on operator-managed storage. Secrets are injected via env vars, never embedded in config files (Issue #9627).
6. **Detect what you can't prevent** — Where we accept residual risk, add monitoring and alerting.
7. **Policy is portable, enforcement is tiered** — Security intent is defined once in `conga-policy.yaml`. Each provider enforces what it can with available tools. The gap between intent and enforcement is visible via `conga policy validate`.
8. **Own the box, not the behavior** — Conga Line controls infrastructure: containers, networks, egress, secrets, resource limits. Runtime behavior enforcement (per-action policy, tool invocation, privacy routing) is the domain of OpenShell or similar. Neither replaces the other.

## Universal Baseline (All Providers, Automatic)

These controls apply identically regardless of provider. They happen automatically on `conga admin setup` and require no operator configuration.

| Control | Implementation | Rationale |
|---|---|---|
| Drop all capabilities | `--cap-drop=ALL` | Removes kernel capabilities that enable most escape techniques |
| No new privileges | `--security-opt=no-new-privileges` | Prevents privilege escalation via setuid binaries |
| Resource limits | `--memory 2g`, `--cpus 0.75`, `--pids-limit 256` | Prevents resource starvation across agents |
| Non-root container | Explicit `--user 1000:1000` on all containers | Limits blast radius of container compromise; independent of image USER directive |
| Seccomp profile | Docker default seccomp (~44 dangerous syscalls blocked) | Restricts syscalls available to the container |
| Isolated Docker networks | Each agent on its own bridge network | Prevents lateral movement between agents |
| Localhost-only port binding | `-p 127.0.0.1:<port>:<port>` | Gateway not exposed to network |
| Secrets via env vars | Env file (mode 0400), never in openclaw.json | Prevents secret exposure via config (Issue #9627) |
| Config integrity monitoring | SHA256 hash baseline, periodic check, alert on mismatch | Detects tampering that bypasses other controls |
| Router event signing | HMAC-SHA256 on forwarded Slack events | Prevents event forgery between router and containers |
| Gateway token auth | Random token + device pairing | Prevents unauthorized web UI access |
| Pinned image | Known-good OpenClaw version (currently v2026.3.11) | Avoids regressions (e.g., Slack socket mode bug in v2026.3.12) |

## Enforcement Escalation by Provider

When a policy defines a security control, each provider enforces it with the best mechanism available.

| Control | Local (Dev) | Remote (Staging) | Enterprise (Prod) |
|---|---|---|---|
| **Egress filtering** | Configurable: validate (warns only) or enforce (per-agent Envoy proxy + iptables DROP rules). Default: enforce. | Per-agent Envoy proxy + iptables DROP rules. Respects policy mode. Default: enforce. | Per-agent Envoy proxy + iptables DROP rules. Respects policy mode. Default: enforce. |
| **Host access** | N/A (user's machine) | SSH key-only auth. Gateway via SSH tunnel. | Zero ingress. No SSH. SSM-only. Gateway via SSM tunnel. |
| **Secrets backend** | File, mode 0400. User owns disk encryption. | File, mode 0400 on remote. | AWS Secrets Manager. Encrypted at rest. IAM-scoped. |
| **IAM / RBAC** | N/A (single user) | Admin: SSH. End users: CLI-only, scoped to assigned agent. | AWS SSO + IAM roles with explicit deny. Per-user permission sets (planned). |
| **Monitoring** | Config integrity + container logs via `conga logs` | Config integrity + container logs. Optional: log aggregator. | VPC flow logs (30-day), CloudWatch alerting, config integrity. Planned: GuardDuty. |
| **Runtime policy** | Validate: check policy syntax, report unenforced rules. Enforce: activate egress proxy locally. | Enforce egress + routing rules. Report what requires enterprise. | Full enforcement. Planned: OpenShell for per-action interception. |
| **Container read-only** | Router + egress proxy (--read-only + tmpfs). | Router + egress proxy. | Router + egress proxy + agent where feasible. systemd ReadOnlyPaths as backup. |
| **Isolation upgrade** | N/A | Docker default is sufficient. | Planned: gVisor (Level 2), per-agent subnets (Level 3), per-user VPCs (Level 4). |

## Egress Policy

Egress domain allowlisting is the single highest-impact security control and the #1 priority. A compromised agent on port-443-only egress can exfiltrate to any HTTPS endpoint. Domain allowlisting restricts it to declared destinations.

| Field | Purpose |
|---|---|
| `allowed_domains` | Domains the agent can reach (e.g., api.anthropic.com, *.slack.com). Wildcards supported. |
| `blocked_domains` | Explicit deny list. Takes precedence over allowed_domains. |
| `mode` | `enforce` (activate proxy + iptables, default) or `validate` (warn-only). All providers respect this field. |

Defined in `conga-policy.yaml` egress section. See `specs/2026-03-25_feature_policy-schema/` for schema.

## Network Security (AWS-Specific)

| Control | Implementation | Rationale |
|---|---|---|
| Zero ingress | Security group has zero inbound rules | No attack surface from the internet |
| No SSH | openssh-server removed entirely | Eliminates credential-based remote access |
| SSM-only access | AWS Session Manager via VPC endpoint | Auditable, IAM-authenticated access |
| HTTPS-only egress | SG egress limited to port 443 + DNS | Only traffic needed is Slack WSS and LLM APIs |
| NACLs | Stateless subnet ACLs, 443 egress + ephemeral return only | Defense-in-depth at the subnet level |

## Configuration Integrity

| Control | Implementation | Rationale |
|---|---|---|
| Root-owned config (AWS) | `openclaw.json` owned by `root:root`, mode `0444` | Agent process (uid 1000) cannot modify its own config |
| Systemd read-only paths (AWS) | `ReadOnlyPaths=/home/openclaw/.openclaw/openclaw.json` | Kernel-level enforcement even if uid 1000 is compromised |
| Docker read-only mount (AWS) | Config mounted with `:ro` flag | Container cannot modify config even with container-level root |
| Hash-based integrity (all) | SHA256 baseline, periodic check, alert on mismatch | Detects tampering that bypasses other controls |
| Channel allowlist is security-critical | Treat `groupPolicy` and channel allowlist as a security boundary | Prevents cross-agent channel access via config modification |

## Host Hardening (AWS-Specific)

| Control | Implementation | Rationale |
|---|---|---|
| IMDSv2 enforced, hop limit 1 | `http_tokens = "required"`, `http_put_response_hop_limit = 1` | Prevents container SSRF to instance metadata |
| Systemd sandboxing | `NoNewPrivileges=true`, `ProtectSystem=strict`, per-unit `MemoryMax` | Constrains each container's systemd unit |
| No IP forwarding | `sysctl net.ipv4.ip_forward=0` (except Docker NAT) | Host cannot be used as a network pivot |
| Auto security updates | `dnf-automatic` enabled | Patches applied without manual intervention |
| Encrypted EBS | KMS-encrypted volume with auto-rotation | Data at rest encryption |

## IAM (AWS-Specific)

| Control | Implementation | Rationale |
|---|---|---|
| Single instance IAM role | Scoped to read secrets under `conga/*` path | Host fetches secrets at boot and injects per-container |
| Explicit deny policy | Denies `iam:*`, `ec2:RunInstances`, `lambda:*`, `s3:DeleteBucket`, etc. | Even if the role is expanded, dangerous actions are blocked |
| Containers have no IAM access | IMDSv2 hop limit 1 blocks container metadata access | Containers cannot assume the host's IAM role |

## Isolation Upgrade Path

| Level | Isolation Model | Container Escape Protection | When to consider |
|---|---|---|---|
| **Current** | Docker seccomp + cap-drop on shared host | No inter-container network; escape lands as host user | Internal team, low sensitivity data |
| **Level 2: gVisor** | Add `--runtime=runsc` to Docker containers | User-space kernel intercepts all syscalls | Higher sensitivity data, or after a Docker CVE |
| **Level 3: Per-agent subnets** | Separate private subnet per agent, per-subnet NACLs | Network-level isolation layered on container isolation | Compliance requires network segmentation |
| **Level 4: Per-user VPCs** | Separate VPC per user, connected via Transit Gateway | Full network boundary isolation | Client contractual requirements, regulated data |

Each level is additive — higher levels include all controls from lower levels.

## Shared Resources — Security Boundaries

| Shared Resource | Risk | Mitigation |
|---|---|---|
| Host (all providers) | Container escape gives access to all agents' data | Docker isolation + non-root + cap-drop; upgrade path documented |
| Slack app tokens | All containers receive all Slack events | Channel allowlist + config immutability |
| fck-nat instance (AWS) | Shared egress path | No access to application traffic (TLS end-to-end) |
| Public IP / NAT EIP (AWS) | External IP correlation | Low severity; acceptable for internal use |

## Accepted Residual Risks

| Risk | Severity | Rationale for acceptance |
|---|---|---|
| Container escape on shared host | Medium | cap-drop + seccomp + no-new-privileges make this difficult; upgrade path documented |
| Shared Slack tokens across containers | Low | Channel allowlist + immutable config prevents cross-agent access |
| Cooperative proxy enforcement | Low | Egress proxy is set via `HTTPS_PROXY` env var and enforced by iptables DROP rules in the DOCKER-USER chain that block direct outbound traffic from agent containers. Only traffic to the bridge subnet (proxy + Docker DNS) is allowed. A compromised agent would need to exploit the proxy itself or escalate privileges to modify iptables rules. |
| Secrets on disk (local/remote) | Low | Mode 0400 files; disk encryption is operator responsibility. AWS uses Secrets Manager. |
| IP correlation by external APIs | Low | All traffic exits through same NAT; reveals shared infrastructure but not content |
| Shared kernel vulnerabilities | Medium | Host kernel CVE affects all containers; mitigated by auto-upgrades; gVisor eliminates |

## Open Questions

- What OpenClaw skills/plugins will be enabled, and do any need additional sandboxing?
- Is Docker's default seccomp profile sufficient, or do we need a custom profile for OpenClaw's syscall patterns?
- Should Conga Line validate OpenShell policy files even when OpenShell isn't the enforcement engine?
