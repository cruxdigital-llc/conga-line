<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-25
To modify: Edit this file directly. GLaDOS will read the current state before making future updates.
-->

# Project Mission

## Problem
Autonomous AI agents (like OpenClaw) are powerful but dangerous to deploy. Their default posture carries significant security risks — credential exposure, unrestricted shell access, prompt injection, sandbox escape, and data exfiltration. No existing tool solves the full deployment problem: controlling the box the agent runs in (containers, networks, egress, secrets) with policy that travels from development to production.

## Audience

Three tiers, each a valid production destination:

- **Hobbyists / solo developers** — running an agent on their laptop for personal use. Local Docker, no cloud cost, no deploy cycle. Security is automatic and invisible.
- **Small teams** — running agents on a VPS or bare-metal host for a team. SSH-based, real infrastructure, shared agents. Security is partially enforced with guidance for the rest.
- **Enterprises** — running agents on hardened cloud infrastructure (AWS) with full compliance requirements. Every policy rule is enforced, audited, and reportable.

## Solution

A promotion pipeline for deploying autonomous AI agents with portable, tiered security policy:

> **Local (Dev)** — Define your agent's configuration, security policy, and routing rules on your own machine. Fast iteration, no cloud cost. The local provider validates your policy and warns about rules it can't enforce locally.
>
> **Remote (Staging)** — Promote to any SSH-accessible host. Validate that your configuration works on real infrastructure. The remote provider enforces a broader subset of your policy.
>
> **Enterprise / AWS (Production)** — Promote to hardened cloud infrastructure. Every policy rule is enforced — egress allowlisting, IAM controls, runtime monitoring, audit trail. The same configuration you tested locally now runs with full enterprise controls.

The deliverable is:
- **Go CLI** (`conga`) with pluggable provider architecture (local, remote, AWS)
- **Terraform configuration** for hardened AWS deployment
- **Portable policy artifact** (`conga-policy.yaml`) that defines operator intent once — each provider enforces what it can
- **Per-agent Docker containers** with defense-in-depth isolation on every tier

## Scope
This is an infrastructure-as-code project deploying OpenClaw via pluggable providers. There is no application code — the deliverable is Terraform configuration + bootstrap scripts + a Go CLI. The project is positioned as complementary to runtime behavior engines like NVIDIA OpenShell: Conga Line controls the box, OpenShell controls what happens inside it.

## Architecture Decisions
- **Provider model** — CLI uses a `Provider` interface with implementations for local Docker, remote SSH, and AWS. Commands work identically on any provider. Policy travels with the deployment.
- **Per-agent Docker containers on a shared host** — simplest and cheapest per tier; Docker isolation + resource limits + isolated networks provide sufficient separation. Upgrade path to gVisor, per-agent subnets, and per-user VPCs is documented.
- **Policy is portable, enforcement is tiered** — the operator defines security and routing intent in `conga-policy.yaml`. Each provider enforces what it can with available tools. The gap between intent and enforcement is visible and closable by promoting to a more capable tier.
- **Own the box, not the behavior** — Conga Line controls infrastructure (containers, networks, egress, secrets, resource limits). Runtime behavior enforcement (per-action policy, tool invocation, privacy routing) is the domain of OpenShell or similar. The policy file reflects this boundary.
- **fck-nat on t4g.nano** (AWS) instead of AWS NAT Gateway — ~$3/mo vs ~$33/mo, sufficient for HTTPS-only egress
- **Slack is optional** — agents can run in gateway-only mode (web UI) without any Slack configuration

## Success Criteria
- An operator can go from zero to a running agent in under 10 minutes on any tier
- Security controls are automatic — the universal baseline (cap-drop, resource limits, isolated networks, secrets protection) requires no configuration
- Policy defined locally produces the same rules enforced in production — no rewrite on promotion
- Each agent's container is isolated (no inter-container network, separate configs, separate secrets)
- The deployment can be explained and justified to enterprise clients via a controls matrix mapping to CIS Docker, NIST 800-190, and AWS Well-Architected
- Egress domain allowlisting prevents data exfiltration even when the agent is compromised
