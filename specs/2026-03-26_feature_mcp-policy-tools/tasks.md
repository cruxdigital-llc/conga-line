# Implementation Tasks: MCP Policy Tools

## Phase 1: Policy Mutation Helpers
- [x] **Task 1.1**: Create `cli/internal/policy/mutate.go` — `Save`, `SetEgress`, `SetRouting`, `SetPosture`
- [x] **Task 1.2**: Create `cli/internal/policy/mutate_test.go` — 11 tests covering round-trip, set operations, section preservation

## Phase 2: MCP Tool Handlers
- [x] **Task 2.1**: Create `cli/internal/mcpserver/tools_policy.go` — helpers (`policyPath`, `loadPolicy`, `getStringSlice`, `getCostLimits`) + 3 read-only tools (`conga_policy_get`, `conga_policy_validate`, `conga_policy_get_agent`)
- [x] **Task 2.2**: Add 3 mutation tools (`conga_policy_set_egress`, `conga_policy_set_routing`, `conga_policy_set_posture`)
- [x] **Task 2.3**: Add deploy tool (`conga_policy_deploy`)

## Phase 3: Registration & Wiring
- [x] **Task 3.1**: Edit `cli/internal/mcpserver/tools.go` — register all 7 policy tools

## Phase 4: Tests
- [x] **Task 4.1**: Create `cli/internal/mcpserver/tools_policy_test.go` — mock provider + 16 MCP tool tests

## Phase 5: Verify
- [x] **Task 5.1**: Run all tests, confirm compilation — all tests pass (27 new + all existing)
