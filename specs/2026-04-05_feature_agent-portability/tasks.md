# Implementation Tasks: Agent Portability

## Phase 1: Runtime Interface & Registry

- [x] **1.1** Create `pkg/runtime/runtime.go` — Runtime interface, RuntimeName type, supporting types (ContainerSpec, ReadyPhase, ConfigParams, EnvParams)
- [x] **1.2** Create `pkg/runtime/registry.go` — Register/Get/Names functions
- [x] **1.3** Move `SharedSecrets` from `pkg/common/config.go` to `pkg/provider/provider.go` with type alias in common for backward compat

## Phase 2: Extract OpenClaw Runtime

- [x] **2.1** Create `pkg/runtime/openclaw/runtime.go` — struct, Name(), init() registration
- [x] **2.2** Create `pkg/runtime/openclaw/config.go` — move GenerateOpenClawConfig + buildGatewayConfig + openclaw-defaults.json embed
- [x] **2.3** Create `pkg/runtime/openclaw/env.go` — move GenerateEnvFile (NODE_OPTIONS, heap size)
- [x] **2.4** Create `pkg/runtime/openclaw/container.go` — ContainerSpec, DefaultImage, ContainerDataPath, SupportsNodeProxy
- [x] **2.5** Create `pkg/runtime/openclaw/dirs.go` — CreateDirectories (data/workspace, memory, logs, etc.)
- [x] **2.6** Create `pkg/runtime/openclaw/health.go` — DetectReady with OpenClaw log markers
- [x] **2.7** Create `pkg/runtime/openclaw/token.go` — ReadGatewayToken, GatewayTokenDockerExec
- [x] **2.8** Create `pkg/runtime/openclaw/channels.go` — ChannelConfig, PluginConfig, WebhookPath delegates
- [x] **2.9** Update `pkg/common/config.go` — backward-compat wrappers delegating to openclaw runtime
- [x] **2.10** Verify all existing tests still pass

## Phase 3: Wire Local Provider to Runtime

- [x] **3.1** Add `Runtime` field to `AgentConfig` in `pkg/provider/provider.go`
- [x] **3.2** Add `Runtime` field to `Config` in `pkg/provider/config.go`
- [x] **3.3** Add `Runtime` field to `SetupConfig` in `pkg/provider/setup_config.go`
- [x] **3.4** Update `pkg/provider/localprovider/provider.go` — ProvisionAgent uses Runtime interface
- [x] **3.5** Update `pkg/provider/localprovider/docker.go` — parameterize runAgentContainer with ContainerSpec
- [x] **3.6** Update `pkg/provider/localprovider/provider.go` — GetStatus uses rt.DetectReady()
- [x] **3.7** Update `pkg/provider/localprovider/provider.go` — Connect uses rt.ReadGatewayToken/GatewayTokenDockerExec
- [ ] **3.8** Update `pkg/common/routing.go` — webhook path from runtime (deferred to Phase 6 remote/AWS)
- [x] **3.9** Verify all existing tests still pass

## Phase 4: Hermes Runtime

- [x] **4.1** Create `pkg/runtime/hermes/runtime.go` — struct, Name(), init() registration
- [x] **4.2** Create `pkg/runtime/hermes/config.go` — YAML config generation
- [x] **4.3** Create `pkg/runtime/hermes/env.go` — env file generation
- [x] **4.4** Create `pkg/runtime/hermes/container.go` — ContainerSpec, DefaultImage, ContainerDataPath
- [x] **4.5** Create `pkg/runtime/hermes/dirs.go` — CreateDirectories
- [x] **4.6** Create `pkg/runtime/hermes/health.go` — DetectReady with Hermes log markers
- [x] **4.7** Create `pkg/runtime/hermes/token.go` — ReadGatewayToken, GatewayTokenDockerExec
- [x] **4.8** Create `pkg/runtime/hermes/channels.go` — ChannelConfig, PluginConfig, WebhookPath

## Phase 5: CLI & Data Model

- [x] **5.1** Add `Runtime` field to Manifest structs in `pkg/manifest/manifest.go`
- [x] **5.2** Add `--runtime` flag to CLI commands in `internal/cmd/`
- [x] **5.3** Add runtime to `conga status` output
- [ ] **5.4** Add runtime validation (deferred — registry already validates on Get())

## Phase 6: Tests

- [x] **6.1** Runtime contract tests (`pkg/runtime/runtime_test.go`) — 13 tests x 2 runtimes
- [x] **6.2** OpenClaw runtime unit tests — 6 specific tests
- [x] **6.3** Hermes runtime unit tests — 7 specific tests
- [ ] **6.4** Mixed-runtime routing test (deferred to routing.go webhook path update)
- [x] **6.5** Full test suite pass verification — 16 packages, 0 failures
