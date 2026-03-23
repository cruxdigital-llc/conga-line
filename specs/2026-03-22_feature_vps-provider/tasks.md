# VPS Provider — Implementation Tasks

## Phase 1: Dependencies & Config
- [x] 1.1 Add SSH fields to `provider.Config` struct (`config.go`)
- [x] 1.2 Add `golang.org/x/crypto` and `github.com/pkg/sftp` to `go.mod`
- [x] 1.3 Add vpsprovider blank import to `root.go`, update flag help

## Phase 2: SSH Foundation (`ssh.go`)
- [x] 2.1 SSH client struct and `Connect()` with key resolution
- [x] 2.2 `Run()` and `RunWithStderr()` command execution
- [x] 2.3 SFTP: `Upload()`, `Download()`, `UploadDir()`, `MkdirAll()`
- [x] 2.4 `ForwardPort()` SSH tunnel + `SSHTunnel` struct
- [x] 2.5 `shelljoin()` shell quoting utility
- [x] 2.6 Unit tests for `shelljoin()` — 29 tests pass

## Phase 3: Docker Helpers (`docker.go`)
- [x] 3.1 `dockerRun()` core + `dockerCheck()`
- [x] 3.2 Container/network naming, create/remove/connect/disconnect
- [x] 3.3 `runAgentContainer()` and `runRouterContainer()`
- [x] 3.4 Container lifecycle: stop, remove, restart, logs, inspect, stats
- [x] 3.5 Existence checks + image operations

## Phase 4: Core Provider (`provider.go`)
- [x] 4.1 VPSProvider struct, init/factory, Name(), path helpers
- [x] 4.2 Identity & Discovery: WhoAmI, ListAgents, GetAgent, ResolveAgentByIdentity
- [x] 4.3 Agent Lifecycle: ProvisionAgent, RemoveAgent
- [x] 4.4 Agent Lifecycle: PauseAgent, UnpauseAgent
- [x] 4.5 Container Ops: GetStatus, GetLogs, ContainerExec
- [x] 4.6 Container Ops: RefreshAgent, RefreshAll
- [x] 4.7 Connectivity: Connect (SSH tunnel)
- [x] 4.8 Environment: CycleHost, Teardown
- [x] 4.9 Infrastructure helpers: ensureRouter, ensureEgressProxy, regenerateRouting
- [x] 4.10 File/utility helpers: deployBehavior, getConfigValue, setConfigValue, etc.

## Phase 5: Secrets & Integrity
- [x] 5.1 Secrets: SetSecret, ListSecrets, DeleteSecret, readSharedSecrets, readAgentSecrets (`secrets.go`)
- [x] 5.2 Integrity: checkConfigIntegrity, saveConfigBaseline, RunIntegrityCheck (`integrity.go`)

## Phase 6: Setup Wizard (`setup.go`)
- [x] 6.1 Setup() method: SSH connection verify, Docker auto-install
- [x] 6.2 Setup() method: image/repo/secrets prompts, file uploads, infrastructure start

## Phase 7: Build & Verify
- [x] 7.1 `go build` compiles successfully
- [x] 7.2 `go vet` passes
- [x] 7.3 Existing tests still pass
- [x] 7.4 New shelljoin/shellQuote/isSafeArg tests pass (29 cases)
