# Feature Trace: Terraform Provider

## Session Log

### 2026-03-30 — Plan Feature

**Goal**: Design a Terraform provider (`terraform-provider-conga`) that wraps the existing Go `Provider` interface for declarative lifecycle management of CongaLine environments. Enterprise complement to `conga bootstrap`.

**Motivation**: The CLI is transactional (atomic, idempotent operations). `conga bootstrap` is a one-shot standup accelerator. Neither provides state management, drift detection, or declarative destroy. Rather than reimplementing these in the CLI, we leverage Terraform — which already solves these problems — and build a thin provider that maps Terraform resources to existing `Provider` interface methods.

**Active Personas**: Architect, Product Manager, QA
**Active Capabilities**: Go build/test, Terraform CLI (acceptance tests, future)

## Files Created
- `specs/2026-03-30_feature_terraform-provider/README.md` (this file)
- `specs/2026-03-30_feature_terraform-provider/requirements.md`
- `specs/2026-03-30_feature_terraform-provider/plan.md`

## Decisions
- **Enterprise lifecycle management** — Terraform handles state, drift, dependencies, destroy
- **Future roadmap item** — spec now, implement later
- **Thin wrapper** — provider calls existing `Provider` interface, zero business logic duplication
- **Coexists with bootstrap** — different audiences, same underlying engine
- **6 resources**: `conga_environment`, `conga_agent`, `conga_secret`, `conga_channel`, `conga_channel_binding`, `conga_policy`
- **Separate Go module** — `terraform-provider-conga/` imports `cli/internal/provider`
- **terraform-plugin-framework** (not deprecated SDK)
