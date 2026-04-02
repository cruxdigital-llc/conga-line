# Implementation Tasks: Agent Pause / Unpause

## Task 1: Add Paused field to agent config structs
- [x] `cli/pkg/provider/provider.go` — add `Paused bool json:"paused,omitempty"` to `AgentConfig`
- [x] `cli/pkg/discovery/agent.go` — add `Paused bool json:"paused,omitempty"` to `AgentConfig`
- [x] `cli/pkg/provider/awsprovider/provider.go` — propagate `Paused` in `convertAgent` helper

## Task 2: Add PauseAgent/UnpauseAgent to Provider interface
- [x] `cli/pkg/provider/provider.go` — add two methods to the `Provider` interface

## Task 3: Update routing to exclude paused agents
- [x] `cli/pkg/common/routing.go` — add `if a.Paused { continue }` in `GenerateRoutingJSON`

## Task 4: Implement local provider pause/unpause
- [x] `cli/pkg/provider/localprovider/provider.go` — implement `PauseAgent`
- [x] `cli/pkg/provider/localprovider/provider.go` — implement `UnpauseAgent`
- [x] `cli/pkg/provider/localprovider/provider.go` — extract `saveAgentConfig` helper
- [x] `cli/pkg/provider/localprovider/provider.go` — add paused guard to `RefreshAgent`
- [x] `cli/pkg/provider/localprovider/provider.go` — skip paused agents in `RefreshAll`
- [x] `cli/pkg/provider/localprovider/provider.go` — skip paused agents in `CycleHost`

## Task 5: Implement AWS provider pause/unpause
- [x] Create `cli/scripts/pause-agent.sh.tmpl`
- [x] Create `cli/scripts/unpause-agent.sh.tmpl`
- [x] Update `cli/scripts/embed.go` — embed new templates
- [x] `cli/pkg/provider/awsprovider/provider.go` — implement `PauseAgent`
- [x] `cli/pkg/provider/awsprovider/provider.go` — implement `UnpauseAgent`
- [x] `cli/pkg/provider/awsprovider/provider.go` — implement `setAgentPaused` helper
- [x] `cli/pkg/provider/awsprovider/provider.go` — add paused guard to `RefreshAgent`
- [x] `cli/pkg/provider/awsprovider/provider.go` — skip paused agents in `RefreshAll`

## Task 6: CLI commands
- [x] Create `cli/cmd/admin_pause.go` — `adminPauseRun`, `adminUnpauseRun`
- [x] Update `cli/cmd/admin.go` — register pause/unpause subcommands
- [x] Update `cli/cmd/admin.go` — add STATUS column to `adminListAgentsRun`

## Task 7: Bootstrap integration (AWS)
- [x] Update `terraform/user-data.sh.tftpl` — skip agents with `paused: true` in discovery loop

## Task 8: Build verification
- [x] `go build` CLI compiles without errors
- [x] `go vet ./...` clean
- [x] `go test ./...` all pass
- [x] `terraform validate` passes
