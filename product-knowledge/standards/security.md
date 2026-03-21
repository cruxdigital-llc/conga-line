<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-15
To modify: Edit directly. These standards are expected to evolve as we learn more.
-->

# Security Standards — Conga Line AWS Deployment

> These standards represent our current best understanding. They should be reviewed
> and updated as we gain operational experience and discover new threat vectors.

## Principles

1. **Zero trust the AI agent** — OpenClaw can execute arbitrary commands. Assume it will be prompt-injected and design controls that hold even when it is.
2. **Immutable configuration** — Runtime config that controls security boundaries (channel allowlists, permissions) must not be writable by the process it governs.
3. **Least privilege everywhere** — IAM roles, filesystem permissions, network access, and Docker capabilities should all be minimally scoped.
4. **Defense in depth** — No single control should be the only thing preventing a breach. Layer network, filesystem, process, and container isolation.
5. **Secrets never touch disk** — API keys are injected at boot from Secrets Manager into systemd environment variables. Never written to config files or environment files on the filesystem.
6. **Detect what you can't prevent** — Where we accept residual risk, add monitoring and alerting.

## Network Security

| Control | Implementation | Rationale |
|---|---|---|
| Zero ingress | Security group has zero inbound rules | No attack surface from the internet |
| No SSH | openssh-server removed entirely | Eliminates credential-based remote access |
| SSM-only access | AWS Session Manager via VPC endpoint | Auditable, IAM-authenticated access |
| HTTPS-only egress | SG egress limited to port 443 + DNS | Only traffic needed is Slack WSS and LLM APIs |
| Isolated Docker networks | Each container on its own Docker network, no inter-container communication | Prevents lateral movement between user containers |
| NACLs | Stateless subnet ACLs, 443 egress + ephemeral return only | Defense-in-depth at the subnet level |

## Configuration Integrity

| Control | Implementation | Rationale |
|---|---|---|
| Root-owned config | `openclaw.json` owned by `root:root`, mode `0444` | OpenClaw process (uid 1000) cannot modify its own config |
| Systemd read-only paths | `ReadOnlyPaths=/home/openclaw/.openclaw/openclaw.json` | Kernel-level enforcement even if uid 1000 is compromised |
| Docker read-only mount | Config mounted with `:ro` flag | Container cannot modify config even with container-level root |
| Config integrity monitoring | Systemd timer hashes config file, alerts on unexpected change | Detects tampering that bypasses other controls |
| Channel allowlist is security-critical | Treat `groupPolicy` and channel allowlist as a security boundary, not just a preference | Prevents cross-user channel access via config modification |

## Container Isolation

| Control | Implementation | Rationale |
|---|---|---|
| Isolated Docker networks | Each container on its own bridge network (`--network=conga-user-N`) | Containers cannot communicate with each other |
| Per-container resource limits | `--memory`, `--cpus`, `--pids-limit` flags | Prevents one user from starving the other |
| Non-root container | Each container runs as uid 1000 | Limits blast radius of container compromise |
| Per-container env vars | Secrets injected as env vars per container, not shared | One container cannot read another's API keys |
| Read-only root filesystem | `--read-only` flag + explicit tmpfs mounts for writable paths | Limits attacker's ability to persist changes |
| Drop all capabilities | `--cap-drop=ALL`, add back only what OpenClaw requires | Removes kernel capabilities that enable most escape techniques |
| Seccomp profile | Docker default seccomp profile at minimum; custom tighter profile if feasible | Restricts syscalls available to the container (~44 dangerous calls blocked by default) |
| No new privileges | `--security-opt=no-new-privileges` | Prevents privilege escalation via setuid binaries or capability inheritance |
| Docker rootless mode | Docker daemon runs as non-root user on the host | Container escape lands as unprivileged host user, neutralizing most known escape techniques |

## Host Hardening

| Control | Implementation | Rationale |
|---|---|---|
| IMDSv2 enforced, hop limit 1 | `http_tokens = "required"`, `http_put_response_hop_limit = 1` | Prevents container SSRF to instance metadata |
| Systemd sandboxing | `NoNewPrivileges=true`, `ProtectSystem=strict`, per-unit `MemoryMax` | Constrains each container's systemd unit |
| No IP forwarding | `sysctl net.ipv4.ip_forward=0` (except as needed for Docker NAT) | Host cannot be used as a network pivot |
| Auto security updates | `unattended-upgrades` enabled | Patches applied without manual intervention |
| Encrypted EBS | KMS-encrypted volume | Data at rest encryption |

## IAM

| Control | Implementation | Rationale |
|---|---|---|
| Single instance IAM role | Role scoped to read all user secrets under `conga/*` path | Host fetches secrets at boot and injects per-container |
| Explicit deny policy | Denies `iam:*`, `ec2:RunInstances`, `lambda:*`, `s3:DeleteBucket`, etc. | Even if the role is expanded, dangerous actions are blocked |
| Containers have no IAM access | IMDSv2 hop limit 1 blocks container metadata access | Containers cannot assume the host's IAM role |

## Shared Resources — Security Boundaries

| Shared Resource | Risk | Mitigation |
|---|---|---|
| EC2 host | Container escape gives access to all users' data | Docker isolation + systemd hardening + non-root; accept as residual risk for internal team, upgrade to per-host if needed |
| Slack app tokens (`xapp-`, `xoxb-`) | Both containers receive all Slack events | Channel allowlist + config immutability controls above |
| fck-nat instance | Shared egress path | No access to application traffic (TLS end-to-end) |
| Public IP (NAT EIP) | External IP correlation | Low severity; acceptable for internal team use |

## Isolation Upgrade Path

The current architecture uses Docker container isolation on a shared host. This is appropriate for
a small internal team but can be upgraded incrementally if client requirements or threat landscape change.

| Level | Isolation Model | Container Escape Protection | When to consider |
|---|---|---|---|
| **Current** | Docker rootless + seccomp + cap-drop on shared host | Escape lands as unprivileged user; no inter-container network | Internal team, low sensitivity data |
| **Level 2: gVisor** | Add `--runtime=runsc` to Docker containers | User-space kernel intercepts all syscalls; host kernel not directly reachable | Higher sensitivity data, or after a Docker CVE shakes confidence |
| **Level 3: Per-user subnets** | Separate private subnet per user within the shared VPC, per-subnet NACLs denying cross-subnet traffic | Network-level isolation layered on top of container isolation; even a host compromise on one subnet can't reach another | Multiple hosts needed for capacity, or compliance requires network segmentation |
| **Level 4: Per-user VPCs** | Separate VPC per user, connected via Transit Gateway or VPC Peering to a shared NAT hub | Full network boundary isolation; no shared kernel, no shared network | Client contractual requirements, regulated data, or multi-tenant with untrusted users |

Each level is additive — higher levels include all controls from lower levels. The Terraform
module should be structured so that moving up a level is a configuration change, not a re-architecture.

## Accepted Residual Risks

| Risk | Severity | Rationale for acceptance |
|---|---|---|
| Container escape on shared host | Medium | Docker rootless + seccomp + capability drops make this difficult; blast radius is limited to internal team data; upgrade path documented above |
| Shared Slack tokens across containers | Low | Both containers receive all events but only act on allowlisted channels; config is immutable; no cross-user data in the Slack events themselves |
| IP correlation by external APIs | Low | All traffic exits through the same NAT EIP; reveals shared infrastructure but not traffic content |
| Shared kernel vulnerabilities | Medium | A host kernel CVE affects all containers; mitigated by unattended-upgrades and monitoring; gVisor (Level 2) eliminates this |

## Open Questions (to revisit as we learn)

- Should Anthropic API keys be per-user for usage tracking, or shared?
- Do we need egress domain allowlisting (e.g., Squid proxy) or is port-443-only sufficient?
- What OpenClaw skills/plugins will be enabled, and do any need additional sandboxing?
- Should we add GuardDuty or AWS Config rules for drift detection in v2?
- Is Docker's default seccomp profile sufficient, or do we need a custom profile for OpenClaw's syscall patterns?
- Should we evaluate gVisor before first deploy, or start with Docker rootless and upgrade later?
