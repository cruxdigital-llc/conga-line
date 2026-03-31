# Feature: Multi-User Onboarding (Epics 5+6) — Trace Log

**Started**: 2026-03-16
**Status**: ✅ Verified and Complete

## Active Personas
- Architect — Terraform refactoring, multi-user patterns, onboarding UX

## Decisions
- **Admin provides**: user_id + slack_channel only
- **User provides**: their own secrets via self-service script
- **No per-user secrets in Terraform** — users manage their own under `conga/{user_id}/*`
- **Generic openclaw.json** — no per-user skill config; users configure via Conga Line
- **Secret discovery at boot** — user-data lists all secrets under each user's path dynamically
- **Env var naming**: secret name `foo-bar` → env var `FOO_BAR` (uppercase, hyphens to underscores)
- **Adding a user requires instance replacement** (user-data changes) — acceptable

## Migration Concern
- Aaron's existing secrets in Secrets Manager must be preserved. Need `terraform state rm` before apply to detach them from Terraform without deleting them.

## Files Created
- [requirements.md](requirements.md)
- [plan.md](plan.md)
- [spec.md](spec.md) — full refactoring spec with migration plan, all changed files

## Persona Review
**Architect**: ✅ Approved. Dynamic secret discovery, per-user configs, clean separation of admin/user flows.

## Standards Gate
| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Least privilege | iam | must | ✅ PASSES |
| Container isolation | container | must | ✅ PASSES |
| Config integrity | config | must | ✅ PASSES |
