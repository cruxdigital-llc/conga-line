# Implementation Tasks: Network-Level Egress Enforcement

## Phase 1: Remote Provider (demo-critical)

- [x] 1.1 Add `EgressExtNetwork` constant and `socat` to proxy Dockerfile (`policy/egress.go`)
- [x] 1.2 Add `internal` param to `createNetwork` (`remoteprovider/docker.go`)
- [x] 1.3 Drop `-p` from agent container when enforcing (`remoteprovider/docker.go`)
- [x] 1.4 Add `startPortForwarder` / `stopPortForwarder` helpers (`remoteprovider/docker.go`)
- [x] 1.5 Dual-home egress proxy + ensure external network (`remoteprovider/provider.go`)
- [x] 1.6 Update `RefreshAgent` — network recreation + port forwarder lifecycle (`remoteprovider/provider.go`)
- [x] 1.7 Update `ProvisionAgent` — same pattern (`remoteprovider/provider.go`)
- [x] 1.8 Cleanup in `RemoveAgent`, `PauseAgent`, `Teardown` (`remoteprovider/provider.go`)
- [x] 1.9 Install `socat` during setup (`remoteprovider/setup.go`)
- [x] 1.10 Build and verify compilation

## Phase 2: Local Provider

- [x] 2.1 Add `internal` param to `createNetwork` (`localprovider/docker.go`)
- [x] 2.2 Drop `-p` from agent container when enforcing (`localprovider/docker.go`)
- [x] 2.3 Add forwarder container helpers (`localprovider/docker.go`)
- [x] 2.4 Dual-home egress proxy + ensure external network (`localprovider/provider.go`)
- [x] 2.5 Update `RefreshAgent` — network recreation + forwarder lifecycle (`localprovider/provider.go`)
- [x] 2.6 Update `ProvisionAgent` — same pattern (`localprovider/provider.go`)
- [x] 2.7 Cleanup in `RemoveAgent`, `PauseAgent`, `Teardown` (`localprovider/provider.go`)
- [x] 2.8 Build and verify compilation

## Phase 3: AWS Provider (deferred — not needed for demo)

- [ ] 3.1 Bootstrap script updates (`terraform/user-data.sh.tftpl`)
- [ ] 3.2 Refresh script updates
- [ ] 3.3 Systemd integration
