# Plan Product — Session Log

**Started**: 2026-03-15
**Status**: Complete

## Context
Planning the roadmap and tech stack for OpenClaw's hardened AWS deployment. Based on mission defined in `product-knowledge/MISSION.md` and reference architecture in `ingest/openclaw-aws-reference.md`.

## Key Decisions
- **MVP: 2 users** — one Slack app, 2 channels, 2 EC2 instances with channel allowlists
- **Shared VPC model** — per-user subnets, not per-user VPCs (~$16-17/mo vs ~$60-88/mo per user)
- **fck-nat over NAT Gateway** — $3/mo vs $33/mo, sufficient for HTTPS-only egress
- **Config immutability as security boundary** — channel allowlist treated as security-critical, protected by root ownership + systemd + Docker read-only mount + integrity monitoring
- **Security standards documented first** — evolve as we learn, but start with a clear baseline

## Files Created
- `product-knowledge/ROADMAP.md` — MVP (6 epics) + Horizon 2 + Horizon 3
- `product-knowledge/TECH_STACK.md` — Terraform + AWS + Docker, no app code
- `product-knowledge/standards/security.md` — Security standards with open questions
- `product-knowledge/PROJECT_STATUS.md` — Updated with architecture summary and active tasks
