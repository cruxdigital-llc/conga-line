# Implementation Tasks: Egress Domain Allowlisting

- [x] Task 1: Create `cli/pkg/policy/egress.go` — LoadEgressPolicy, EffectiveAllowedDomains, EgressProxyName, GenerateProxyConf, EgressProxyDockerfile
- [x] Task 2: Create `cli/pkg/policy/egress_test.go` (11 tests pass) — unit tests for egress helpers
- [x] Task 3: Modify `cli/pkg/provider/localprovider/docker.go` — extend agentContainerOpts, add proxy env vars
- [x] Task 4: Modify `cli/pkg/provider/localprovider/provider.go` — policy loading in ProvisionAgent/RefreshAgent, startAgentEgressProxy, stopAgentEgressProxy, RemoveAgent cleanup
- [x] Task 5: Modify `cli/pkg/provider/remoteprovider/docker.go` — same opts extension as local
- [x] Task 6: Modify `cli/pkg/provider/remoteprovider/provider.go` — policy loading + proxy management via SSH
- [x] Task 7: Modify `terraform/user-data.sh.tftpl` — per-agent proxy section in bootstrap
- [x] Task 8: Build + test — all packages compile, all tests pass (11 new egress tests + 22 existing policy tests), go vet clean, 0 regressions
