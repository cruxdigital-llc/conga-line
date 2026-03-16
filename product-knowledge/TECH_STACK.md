<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-15
To modify: Edit directly.
-->

# Tech Stack

## Infrastructure as Code
- **Terraform** — all AWS resources defined declaratively
- **HCL** — Terraform configuration language
- **Shell (bash)** — EC2 user-data bootstrap scripts

## AWS Services
| Service | Purpose |
|---|---|
| EC2 (t4g.medium, Graviton) | Single shared host for all user containers |
| VPC + subnets | Network isolation |
| fck-nat (t4g.nano) | Cost-optimized NAT instance for egress |
| Secrets Manager | API key storage, injected at boot |
| KMS | Per-user EBS encryption keys |
| SSM Session Manager | Instance access (replaces SSH) |
| CloudWatch Logs | Gateway and flow log aggregation |
| IAM | Per-user roles with least-privilege + explicit denies |

## Application
| Component | Technology |
|---|---|
| OpenClaw | `ghcr.io/openclaw/openclaw:latest` Docker image |
| Runtime | Node.js ≥22 (inside container) |
| Container engine | Docker on host, OpenClaw runs as uid 1000 |
| Messaging | Slack Socket Mode (outbound WSS, no inbound ports) |
| LLM backend | Anthropic Claude (via API key in Secrets Manager) |

## No Frontend / No Backend / No Database
This is a pure infrastructure project. There is no application code to write — OpenClaw is consumed as a Docker image. The deliverable is Terraform configuration + bootstrap scripts.
