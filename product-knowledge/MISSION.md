<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-15
To modify: Edit this file directly. GLaDOS will read the current state before making future updates.
-->

# Project Mission

## Problem
OpenClaw is a powerful autonomous AI assistant, but its default deployment posture carries significant security risks — credential exposure, unrestricted shell access, prompt injection, and malicious skill execution. Running it in a way that meets the security bar required for a professional services environment (where client data and trust are at stake) demands a hardened, auditable deployment that doesn't exist out of the box.

## Audience
- Aaron and his team, deploying OpenClaw internally
- Must meet a security standard acceptable to both the team and their clients

## Solution
A hardened AWS deployment for OpenClaw, codified in Terraform, that provides:
- **Per-user Docker isolation** — isolated containers on a single EC2 host, no inter-container communication
- **Zero inbound traffic** — no SSH, no public IPs, access only via SSM Session Manager
- **Secrets off-disk** — API keys injected at boot from Secrets Manager into per-container env vars
- **Immutable configuration** — per-user openclaw.json is root-owned, read-only mounted into containers
- **Encrypted storage** — EBS encrypted with KMS
- **Defense-in-depth** — NACLs + security groups + systemd hardening + Docker network isolation + OS hardening
- **Auditability** — VPC flow logs, CloudWatch logging, least-privilege IAM with explicit denies

## Scope
This is an internal infrastructure project, not a product. The goal is a Terraform configuration that can be planned, applied, and maintained by the team to run OpenClaw securely in AWS at ~$10/mo total for 2 users.

## Architecture Decisions
- **Single EC2 host, per-user Docker containers** (not per-user instances or VPCs) — simplest and cheapest for a small internal team; Docker isolation + read-only configs + isolated networks provide sufficient separation
- **fck-nat on t4g.nano** instead of AWS NAT Gateway — $3/mo vs $33/mo, sufficient for HTTPS-only egress
- **t4g.medium Graviton** (4GB RAM) — headroom for 2 containers + Docker overhead
- **Upgrade path**: can move to per-host isolation later if client requirements demand it

## Success Criteria
- Team members can use OpenClaw via Slack without any inbound network exposure
- Credentials are never written to disk or config files
- Each user's container is isolated (no inter-container network, separate configs, separate secrets)
- The deployment can be explained and justified to clients as meeting a reasonable security bar
- Total infrastructure cost stays under ~$15/mo for 2 users
