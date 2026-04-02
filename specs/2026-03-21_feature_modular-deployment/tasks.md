# Implementation Tasks: Modular Deployment

## Phase 1: Provider Interface & Common Package ✅

- [x] **1.1** Create `cli/pkg/provider/provider.go` — Provider interface + all shared types
- [x] **1.2** Create `cli/pkg/provider/registry.go` — Provider registry (Register, Get, Names)
- [x] **1.3** Create `cli/pkg/provider/config.go` — Provider config load/save
- [x] **1.4** Create `cli/pkg/common/config.go` — `GenerateOpenClawConfig()` and `GenerateEnvFile()`
- [x] **1.5** Create `cli/pkg/common/routing.go` — `GenerateRoutingJSON()`
- [x] **1.6** Create `cli/pkg/common/behavior.go` — `ComposeBehaviorFiles()`
- [x] **1.7** Create `cli/pkg/common/ports.go` — `NextAvailablePort()`
- [x] **1.8** Create `cli/pkg/common/secrets.go` — `SecretNameToEnvVar()`
- [x] **1.9** Create `cli/pkg/common/validate.go` — Slack ID validators

## Phase 2: AWS Provider ✅

- [x] **2.1** Create `cli/pkg/provider/awsprovider/provider.go` — AWSProvider wrapping existing code
- [x] **2.2** Refactor `cli/cmd/root.go` — Replace `clients` with `prov`, add `--provider`/`--data-dir` flags
- [x] **2.3** Refactor `cli/cmd/status.go` — Use `prov.GetStatus()`
- [x] **2.4** Refactor `cli/cmd/logs.go` — Use `prov.GetLogs()`
- [x] **2.5** Refactor `cli/cmd/refresh.go` — Use `prov.RefreshAgent()`
- [x] **2.6** Refactor `cli/cmd/connect.go` — Use `prov.Connect()`
- [x] **2.7** Refactor `cli/cmd/secrets.go` — Use `prov.SetSecret/ListSecrets/DeleteSecret()`
- [x] **2.8** Refactor `cli/cmd/admin_setup.go` — Use `prov.Setup()`
- [x] **2.9** Refactor `cli/cmd/admin_provision.go` — Use `prov.ProvisionAgent()`
- [x] **2.10** Refactor `cli/cmd/admin.go` (list-agents) — Use `prov.ListAgents()`
- [x] **2.11** Refactor `cli/cmd/admin_remove.go` — Use `prov.RemoveAgent()`
- [x] **2.12** Refactor `cli/cmd/admin_cycle.go` — Use `prov.CycleHost()`
- [x] **2.13** Refactor `cli/cmd/admin_refresh_all.go` — Use `prov.RefreshAll()`
- [x] **2.14** Refactor `cli/cmd/auth.go` — Use `prov.WhoAmI()`
- [x] **2.15** Verify build compiles and existing tests pass

## Phase 3: Local Provider — Core ✅

- [x] **3.1** Create `cli/pkg/provider/localprovider/provider.go` — LocalProvider struct + all methods
- [x] **3.2** Create `cli/pkg/provider/localprovider/docker.go` — Docker CLI wrapper
- [x] **3.3** Create `cli/pkg/provider/localprovider/secrets.go` — File-based secret storage (mode 0400)
- [x] **3.4** Implement `Setup()` — Directory creation, secret prompts, image pull
- [x] **3.5** Implement `ProvisionAgent()` — Config gen, network create, container start, routing update
- [x] **3.6** Implement `GetStatus()`, `GetLogs()`, `RefreshAgent()`, `RefreshAll()`
- [x] **3.7** Implement `Connect()`, `WhoAmI()`, `ListAgents()`, `GetAgent()`
- [x] **3.8** Implement `RemoveAgent()`, `CycleHost()`
- [x] **3.9** Implement router container management (integrated into ProvisionAgent/Setup)

## Phase 4: Network Isolation ✅

- [x] **4.1** Create `deploy/egress-proxy/` — Dockerfile + nginx.conf for HTTPS/DNS-only egress proxy
- [x] **4.2** Docker networks use `--internal` flag for agent isolation
- [x] **4.3** Egress proxy ready for deployment (container management deferred to integration testing)

## Phase 5: Config Integrity & Behavior ✅

- [x] **5.1** Create `cli/pkg/provider/localprovider/integrity.go` — Config hash checking + logging
- [x] **5.2** Behavior file deployment integrated in `ProvisionAgent()` via `common.ComposeBehaviorFiles()`

## Phase 6: CLI Integration ✅

- [x] **6.1** Provider auto-detection in `root.go` (config file → default AWS)
- [x] **6.2** `auth login` — shows "not applicable" for local provider
- [x] **6.3** `auth status` — shows provider name, local identity
