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
- `cli/pkg/terraform/provider.go` — terraform-plugin-framework provider (Configure → provider.Get)
- `cli/pkg/terraform/helpers.go` — shared utilities (splitImportID, extractProvider)
- `cli/pkg/terraform/environment_resource.go` — conga_environment resource
- `cli/pkg/terraform/agent_resource.go` — conga_agent resource
- `cli/pkg/terraform/secret_resource.go` — conga_secret resource
- `cli/pkg/terraform/channel_resource.go` — conga_channel resource
- `cli/pkg/terraform/binding_resource.go` — conga_channel_binding resource
- `cli/pkg/terraform/policy_resource.go` — conga_policy resource
- `cli/pkg/terraform/agent_status_datasource.go` — conga_agent_status data source
- `cli/pkg/terraform/policy_datasource.go` — conga_policy data source
- `cli/pkg/terraform/channels_datasource.go` — conga_channels data source
- `cli/pkg/terraform/provider_test.go` — test factory setup
- `cli/pkg/terraform/environment_resource_test.go` — environment acceptance tests
- `cli/pkg/terraform/agent_resource_test.go` — agent acceptance tests
- `cli/pkg/terraform/secret_resource_test.go` — secret acceptance tests
- `cli/pkg/terraform/channel_resource_test.go` — channel + binding acceptance tests
- `cli/pkg/terraform/policy_resource_test.go` — policy acceptance tests

## Decisions
- **Enterprise lifecycle management** — Terraform handles state, drift, dependencies, destroy
- **Thin wrapper** — provider calls existing `Provider` interface, zero business logic duplication
- **Coexists with bootstrap** — different audiences, same underlying engine
- **6 resources**: `conga_environment`, `conga_agent`, `conga_secret`, `conga_channel`, `conga_channel_binding`, `conga_policy`
- **3 data sources**: `conga_agent_status`, `conga_policy`, `conga_channels`
- **Same Go module** — `cli/pkg/terraform/` (not separate module, due to Go `internal` package visibility rules)
- **terraform-plugin-framework** (not deprecated SDK)

### 2026-04-01 — Implement Feature

**Goal**: Implement all 8 phases of the terraform provider.

**Active Personas**: Architect, QA
**Active Capabilities**: Go build/test, context7 (library docs)

### 2026-04-01 — Verify Feature

**Verification Results**:
- **Test suite**: All 14 packages pass (0 failures)
- **Vet/lint**: Clean
- **Build**: Both terraform provider binary and CLI compile clean
- **E2E**: Full lifecycle tested on local (create/update/destroy), remote (create/update/destroy), AWS (15 resources applied including channels/bindings/policy)
- **Standards gate**: All observed standards pass
- **Spec divergences**: Module structure updated (cli/pkg/terraform/ instead of separate module), AWS channel management implemented (was "not yet implemented"), per-agent policy overrides added (not in original spec), terraform modules restructured into terraform/modules/ + terraform/environments/

**Additional work beyond original spec**:
- AWS provider channel management (AddChannel, RemoveChannel, ListChannels, BindChannel, UnbindChannel)
- Non-interactive AWS Setup for Terraform/programmatic callers
- Docker idempotency fixes across all three providers
- SFTP rename race condition fix
- Slack ID validation widened for older workspaces
- Terraform module structure (terraform/modules/infrastructure + congaline)
- Generic secrets model (global_secrets, channel_secrets, agent_secrets)
- Deterministic gateway port auto-assignment

**Remaining (Phase 8)**:
- Provider docs (terraform-plugin-docs format)
- Minimal per-provider examples for registry
- Registry publishing under cruxdigital-llc/conga
