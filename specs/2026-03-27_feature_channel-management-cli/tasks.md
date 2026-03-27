# Implementation Tasks: Channel Management CLI

## Phase 1: Provider Interface Extension
- [x] 1.1 Add `ChannelStatus` type and 5 new methods to Provider interface (`provider.go`)
- [x] 1.2 Promote `hasAnyChannel` to `common.HasAnyChannel` (`common/config.go`)
- [x] 1.3 Implement local provider channel methods (`localprovider/channels.go`)
- [x] 1.4 Implement remote provider channel methods (`remoteprovider/channels.go`)
- [x] 1.5 Add AWS provider stubs (`awsprovider/provider.go`)

## Phase 2: Setup Flow Simplification
- [x] 2.1 Remove channel secret prompts and router startup from local Setup()
- [x] 2.2 Remove channel secret prompts and router startup from remote Setup()
- [x] 2.3 Add SetupConfig backwards-compat auto-invoke of AddChannel

## Phase 3: CLI Commands
- [x] 3.1 Create `conga channels` command group with add/remove/list/bind/unbind

## Phase 4: MCP Tools
- [x] 4.1 Create MCP tool handlers for all 5 channel management tools

## Phase 5: Tests
- [x] 5.1 Unit tests for MCP tool handlers (7 tests)
- [x] 5.2 Run full test suite — all 17 packages pass

## Phase 6: Demo Update
- [x] 6.1 Update DEMO.md with gateway-first 10-step flow
