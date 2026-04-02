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
- `specs/2026-03-30_feature_terraform-provider/spec.md`
- `specs/2026-03-30_feature_terraform-provider/tasks.md`

## Files Modified
- `cli/go.mod` — added terraform-plugin-framework, terraform-plugin-go, terraform-plugin-testing
- `cli/go.sum` — updated

## Files Added (Implementation)
- `cli/cmd/terraform-provider-conga/main.go` — plugin server entry point
- `cli/cmd/terraform-provider-conga/.goreleaser.yml` — release build config
- `cli/cmd/terraform-provider-conga/terraform-registry-manifest.json` — registry manifest
- `cli/internal/terraform/provider.go` — terraform-plugin-framework provider (Configure → provider.Get)
- `cli/internal/terraform/helpers.go` — shared utilities (splitImportID, extractProvider)
- `cli/internal/terraform/environment_resource.go` — conga_environment resource
- `cli/internal/terraform/agent_resource.go` — conga_agent resource
- `cli/internal/terraform/secret_resource.go` — conga_secret resource
- `cli/internal/terraform/channel_resource.go` — conga_channel resource
- `cli/internal/terraform/binding_resource.go` — conga_channel_binding resource
- `cli/internal/terraform/policy_resource.go` — conga_policy resource
- `cli/internal/terraform/agent_status_datasource.go` — conga_agent_status data source
- `cli/internal/terraform/policy_datasource.go` — conga_policy data source
- `cli/internal/terraform/channels_datasource.go` — conga_channels data source
- `cli/internal/terraform/provider_test.go` — test factory setup
- `cli/internal/terraform/environment_resource_test.go` — environment acceptance tests
- `cli/internal/terraform/agent_resource_test.go` — agent acceptance tests
- `cli/internal/terraform/secret_resource_test.go` — secret acceptance tests
- `cli/internal/terraform/channel_resource_test.go` — channel + binding acceptance tests
- `cli/internal/terraform/policy_resource_test.go` — policy acceptance tests

## Decisions
- **Enterprise lifecycle management** — Terraform handles state, drift, dependencies, destroy
- **Thin wrapper** — provider calls existing `Provider` interface, zero business logic duplication
- **Coexists with bootstrap** — different audiences, same underlying engine
- **6 resources**: `conga_environment`, `conga_agent`, `conga_secret`, `conga_channel`, `conga_channel_binding`, `conga_policy`
- **3 data sources**: `conga_agent_status`, `conga_policy`, `conga_channels`
- **Same Go module** — `cli/internal/terraform/` (not separate module, due to Go `internal` package visibility rules)
- **terraform-plugin-framework** (not deprecated SDK)

### 2026-04-01 — Implement Feature

**Goal**: Implement all 8 phases of the terraform provider.

**Active Personas**: Architect, QA
**Active Capabilities**: Go build/test, context7 (library docs)
