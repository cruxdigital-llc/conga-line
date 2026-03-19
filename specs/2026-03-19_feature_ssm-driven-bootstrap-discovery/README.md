# Trace Log: SSM-Driven Bootstrap Discovery

## Session Start
- **Date**: 2026-03-19
- **Feature**: SSM-Driven Bootstrap Discovery
- **Goal**: Refactor the bootstrap script to discover agents from SSM Parameter Store at boot time instead of having them baked in via Terraform template loops. Makes SSM the single source of truth so CLI-added agents survive instance replacement without requiring Terraform.

## Active Personas
- **Architect** — system integrity, pattern consistency, dependency/performance review
- **QA** — edge cases, failure modes, test coverage

## Active Capabilities
- Standard file editing tools (Read, Edit, Write, Glob, Grep)
- Bash for Terraform validate/plan, Go build
- No browser/UI, database, or project management tools needed

## Files Created
- [requirements.md](requirements.md) — Goal, success criteria, constraints
- [plan.md](plan.md) — 4-step implementation plan with rollout order

## Decisions
- **SSM as single source of truth**: Bootstrap discovers agents from SSM at boot instead of Terraform template loops. `var.agents` still drives SSM param creation + Terraform-time resources (dashboards, outputs, IAM).
- **Personas selected**: Architect (system integrity, IAM scoping) + QA (edge cases, failure modes)
- **IAM widening**: Replace per-user secret ARN enumeration with `openclaw/U*` + `openclaw/teams/*` wildcards to support CLI-added agents. Acceptable risk — single-tenant infra.
- **`jq` dependency**: Added to bootstrap for JSON parsing. Available in AL2023 default repos.
- **Static bootstrap**: After refactor, bootstrap S3 object hash no longer changes per-agent. Adding agents never forces instance replacement.

## Session: Spec (2026-03-19)
- Resumed trace for detailed specification
- Created `spec.md` — unified `/openclaw/agents/` namespace, bootstrap discovery loop, CLI command changes
- Decision: Restructure SSM from 3 paths (`/users/`, `/teams/`, `/users/by-iam/`) to single `/openclaw/agents/<name>` path with `type` discriminator and `iam_identity` in value
- Decision: Unify `remove-user`/`remove-team` into `remove-agent <name>`
- Decision: `add-user` takes `<name> <member_id>` (2 args) for consistency with Terraform naming

### Persona Review

**Architect:**
- Single namespace is a good simplification. No new dependencies beyond `jq`.
- `add-user` arg change is breaking but low blast radius (internal tooling, 2 users).
- Identity resolution O(1)→O(n) is fine at current scale. Note as scaling consideration.
- ✅ Approved

**QA:**
- Migration: old SSM params must not be deleted until new bootstrap is deployed and verified.
- `GetParametersByPath` does not recurse by default — verify no nested paths under `/openclaw/agents/`.
- Empty `iam_identity` on team agents handled gracefully by scan logic (no match = skip).
- Agent name validation should use consistent pattern (lowercase alphanumeric + hyphens) for both user and team names since name becomes SSM path component and Docker container ID prefix.
- ✅ Approved with notes above incorporated

### Standards Gate Report (Pre-Implementation)

| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Zero trust the AI agent | all | must | ✅ PASSES — No change to agent trust model |
| Immutable configuration | all | must | ✅ PASSES — Config generation unchanged, still root-owned + hash-checked |
| Least privilege everywhere | iam | must | ⚠️ WARNING — IAM secrets policy widens from per-user ARNs to `openclaw/U*` + `openclaw/teams/*` wildcards. Slightly broader but still scoped to `openclaw/` prefix. Acceptable for single-tenant infra. |
| Defense in depth | all | must | ✅ PASSES — Container isolation, network isolation, config integrity monitoring all unchanged |
| Secrets never touch disk | secrets | must | ✅ PASSES — Env file pattern unchanged (mode 0400, encrypted EBS). Known accepted risk per CLAUDE.md. |
| Detect what you can't prevent | monitoring | must | ✅ PASSES — Config integrity check, CloudWatch metrics, session metrics all unchanged |
| Isolated Docker networks | network | must | ✅ PASSES — Per-container networks unchanged |
| Container isolation | container | must | ✅ PASSES — cap-drop, no-new-privileges, resource limits all unchanged |
| IMDSv2 hop limit | host | must | ✅ PASSES — No change to instance metadata config |

**Gate decision:** No violations. One warning (IAM widening) — acceptable per architect review. Proceed.
