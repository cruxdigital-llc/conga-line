<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-04-02
To modify: Edit directly.
-->

# Tech Stack

## Infrastructure as Code
- **Terraform** — AWS resources defined declaratively
- **HCL** — Terraform configuration language
- **Shell (bash)** — EC2 user-data bootstrap scripts (AWS provider)

## Deployment Providers

| Provider | Target | Discovery | Secrets | Container Ops | Networking |
|----------|--------|-----------|---------|---------------|------------|
| **AWS** | EC2 host in hardened VPC | SSM Parameter Store | AWS Secrets Manager | SSM RunCommand | Zero-ingress VPC, per-agent Docker networks |
| **Remote** | Any SSH-accessible host (VPS, bare metal, RPi) | File-based (`/opt/conga/agents/`) | File-based (`/opt/conga/secrets/`, mode 0400) | SSH + Docker CLI | Per-agent bridge networks, SSH tunnel for gateway |
| **Local** | Local Docker Desktop | File-based (`~/.conga/agents/`) | File-based (`~/.conga/secrets/`, mode 0400) | Docker CLI | Per-agent bridge networks, localhost-only ports |

## AWS Services (AWS provider only)
| Service | Purpose |
|---|---|
| EC2 (t4g.medium, Graviton) | Single shared host for all agent containers |
| VPC + subnets | Network isolation |
| fck-nat (t4g.nano) | Cost-optimized NAT instance for egress |
| Secrets Manager | API key storage, injected at boot |
| KMS | EBS encryption keys |
| SSM Session Manager | Instance access (replaces SSH) |
| SSM Parameter Store | Agent config and deployment manifest |
| CloudWatch Logs | Gateway and flow log aggregation |
| IAM | Roles with least-privilege + explicit denies |

## Application
| Component | Technology |
|---|---|
| OpenClaw | `ghcr.io/openclaw/openclaw:2026.3.11` Docker image |
| Runtime | Node.js ≥22 (inside container) |
| Container engine | Docker (host-level, containers run as uid 1000) |
| Messaging | Slack via HTTP webhook (optional — gateway-only mode supported) |
| LLM backend | Anthropic Claude (via API key) |
| CLI | Go 1.25+ with Cobra, provider-based architecture |
| Policy | YAML (`conga-policy.yaml`) via `gopkg.in/yaml.v3` |

## Go Module Architecture

Module: `github.com/cruxdigital-llc/conga-line` (go.mod at repo root)

### Public library (`pkg/`) — importable by external modules
| Package | Purpose |
|---|---|
| `pkg/provider/` | Provider interface (17+ methods), registry, config |
| `pkg/provider/awsprovider/` | AWS implementation (wraps SSM, Secrets Manager, EC2, STS) |
| `pkg/provider/localprovider/` | Local Docker implementation (Docker CLI, file secrets) |
| `pkg/provider/remoteprovider/` | Remote SSH implementation (SSH + Docker CLI, file secrets, tunneling) |
| `pkg/policy/` | Portable policy schema: YAML parsing, validation, enforcement reporting |
| `pkg/channels/` | Channel abstraction, registry, platform integrations |
| `pkg/common/` | Shared logic: config gen, routing, behavior composition, validation |
| `pkg/aws/` | AWS SDK wrappers and interfaces |
| `pkg/discovery/` | Agent and identity resolution (AWS) |
| `pkg/tunnel/` | SSM port forwarding (AWS) |
| `pkg/ui/` | Spinners, prompts, tables, JSON output |

### Internal interfaces (`internal/`) — private to the conga binary
| Package | Purpose |
|---|---|
| `internal/cmd/` | CLI commands (Cobra), flag parsing, user interaction |
| `internal/mcpserver/` | MCP server for AI agent integration |

## No Frontend / No Backend / No Database
This is a pure infrastructure project. There is no application code to write — OpenClaw is consumed as a Docker image. The deliverable is Terraform configuration + bootstrap scripts + a Go CLI with pluggable deployment providers and a portable policy artifact.
