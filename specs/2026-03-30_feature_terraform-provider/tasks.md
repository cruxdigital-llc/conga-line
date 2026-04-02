# Tasks: Terraform Provider

## Phase 1: Provider Skeleton
- [x] Create `terraform-provider-conga/` Go module with `go.mod`
- [x] Implement `main.go` plugin server entry point
- [x] Implement provider struct with `Configure()` — maps HCL config to `provider.Get()`
- [x] Provider schema: `provider_type`, `ssh_host`, `ssh_user`, `ssh_key_path`, `region`, `profile`
- [x] Verify: `go build` succeeds

## Phase 2: Core Resources
- [x] `conga_environment` resource — Create→`Setup()`, Read→`ListAgents()`, Update→`Setup()`, Delete→`Teardown()`
- [x] `conga_agent` resource — Create→`ProvisionAgent()`, Read→`GetAgent()`, Delete→`RemoveAgent()`, name/type RequiresReplace
- [x] `conga_secret` resource — Create/Update→`SetSecret()`, Read→`ListSecrets()`, Delete→`DeleteSecret()`, value Sensitive

## Phase 3: Channel Resources
- [x] `conga_channel` resource — Create/Update→`AddChannel()`, Read→`ListChannels()`, Delete→`RemoveChannel()`
- [x] `conga_channel_binding` resource — Create→`BindChannel()`, Read→`GetAgent().Channels`, Delete→`UnbindChannel()`

## Phase 4: Policy Resource
- [x] `conga_policy` resource — Create/Update→`Save()+RefreshAll()`, Read→`Load()`, Delete→`os.Remove()`
- [x] Schema: egress block (mode, allowed_domains, blocked_domains), routing block, posture block, agent overrides

## Phase 5: Data Sources
- [x] `conga_agent_status` data source — `GetAgent()` + `GetStatus()`
- [x] `conga_policy` data source — `policy.Load()`
- [x] `conga_channels` data source — `ListChannels()`

## Phase 6: Import Support
- [x] Import for `conga_environment` (singleton)
- [x] Import for `conga_agent` (by name)
- [x] Import for `conga_secret` (by agent/name)
- [x] Import for `conga_channel` (by platform)
- [x] Import for `conga_channel_binding` (by agent/platform)
- [x] Import for `conga_policy` (singleton)

## Phase 7: Acceptance Tests
- [x] Test framework setup with local provider
- [x] `conga_environment` lifecycle test (create, read, destroy)
- [x] `conga_agent` lifecycle test (create, read, destroy, recreate on type change)
- [x] `conga_secret` lifecycle test (create, update, read, destroy)
- [x] `conga_channel` lifecycle test
- [x] `conga_channel_binding` lifecycle test
- [x] `conga_policy` lifecycle test
- [x] Import tests for each resource
- [x] Drift detection test (modify live state, verify plan detects it)

## Phase 8: Registry Publishing
- [x] Add GH Actions goreleaser workflow (`.goreleaser.yml`)
- [x] Add Terraform Registry manifest (`terraform-registry-manifest.json`)
- [x] Add provider docs in `docs/` (terraform-plugin-docs format)
- [x] Add minimal per-provider examples in `examples/` for registry documentation
- [x] Create `cruxdigital-llc/terraform-provider-conga` repo with provider binary, CI, and release workflows
- [x] Rename `cli/internal/` → `cli/pkg/` to allow external module imports
- [x] Register provider namespace on Terraform Registry
- [ ] After merge: tag CongaLine as `v0.X.X`
- [ ] After tag: update `terraform-provider-conga/go.mod` and imports — replace local `replace` directive with tagged version, drop `/cli` from all import paths
- [ ] Add `GPG_PRIVATE_KEY` secret to `terraform-provider-conga` repo
- [ ] Tag `terraform-provider-conga` `v0.1.0` — triggers GoReleaser, registry picks up signed binaries
