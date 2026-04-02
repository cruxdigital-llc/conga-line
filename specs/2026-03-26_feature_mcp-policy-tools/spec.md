# Spec: MCP Policy Tools

## Overview

Seven new MCP tools for full policy lifecycle management: read, validate, mutate, and deploy `conga-policy.yaml` via the existing MCP server. Backed by new mutation helpers in the `policy` package.

---

## 1. Data Model

No new types. All tools operate on the existing `policy.PolicyFile` and its nested structs. The key addition is **write-back capability** — the policy package today can only read and validate.

### Policy Path Resolution

All three providers read policy from the same local path:

```go
policyPath := filepath.Join(dataDir, "conga-policy.yaml")
```

Where `dataDir` is `~/.conga/` (resolved from `$HOME`). This is already the convention in `LoadEgressPolicy()` and `defaultPolicyPath()` in `cli/cmd/policy.go`.

**Decision**: No new `PolicyPath()` method on the Provider interface. Instead, the MCP server resolves the path the same way the CLI does — via `os.UserHomeDir()` + `/.conga/conga-policy.yaml`. This keeps the interface unchanged and matches the existing pattern.

If the policy file does not exist, read tools return an empty policy (`apiVersion` only), and mutation tools create it.

---

## 2. API Interface — Mutation Helpers

**File**: `cli/pkg/policy/mutate.go`

### `Save(pf *PolicyFile, path string) error`

Marshals the `PolicyFile` to YAML and writes atomically:
1. `os.MkdirAll(filepath.Dir(path), 0755)` — ensure parent directory exists
2. Marshal via `yaml.Marshal(pf)`
3. Write to `path + ".tmp"` with mode `0644`
4. `os.Rename(path + ".tmp", path)` for atomic replacement

**Why atomic**: Prevents partial writes if the process crashes mid-write. `os.Rename` is atomic on POSIX filesystems.

**YAML comment preservation**: Not supported. `yaml.Marshal` produces clean output without comments. This is an accepted trade-off — the policy file is small and machine-managed when using MCP tools. Documented in tool descriptions.

```go
func Save(pf *PolicyFile, path string) error
```

### `SetEgress(pf *PolicyFile, agentName string, patch *EgressPolicy)`

Sets the egress section. When `agentName` is empty, sets the global section. When non-empty, creates/updates the per-agent override.

**Merge behavior**: The patch **replaces** the target egress section entirely (consistent with `AgentOverride` semantics — shallow-replace, not deep-merge). If the caller wants to add a domain to the existing list, they should read first, append, then set.

```go
func SetEgress(pf *PolicyFile, agentName string, patch *EgressPolicy)
```

### `SetRouting(pf *PolicyFile, agentName string, patch *RoutingPolicy)`

Same pattern as `SetEgress` for the routing section.

```go
func SetRouting(pf *PolicyFile, agentName string, patch *RoutingPolicy)
```

### `SetPosture(pf *PolicyFile, agentName string, patch *PostureDeclarations)`

Same pattern for posture declarations.

```go
func SetPosture(pf *PolicyFile, agentName string, patch *PostureDeclarations)
```

### Design Notes

- All `Set*` functions modify `pf` in place. Caller is responsible for `Validate()` + `Save()`.
- When `agentName` is non-empty and `pf.Agents` is nil, the map is initialized.
- When `agentName` is non-empty and the override doesn't exist, a new `AgentOverride` is created.
- `MergeForAgent()` already exists and handles the read path (deep copy + override application).

---

## 3. API Interface — MCP Tools

**File**: `cli/pkg/mcpserver/tools_policy.go`

All tools follow the established handler pattern:
- `toolCtx(ctx)` for 5-minute timeout
- `mcp.NewToolResultError(err.Error())` for errors (surface to user, don't crash server)
- `jsonResult(v)` for structured responses
- `okResult(msg)` for simple success messages

### Helper: `policyPath() string`

Private method on `*Server` that resolves `~/.conga/conga-policy.yaml`:

```go
func (s *Server) policyPath() (string, error) {
    home, err := os.UserHomeDir()
    if err != nil {
        return "", fmt.Errorf("resolving home directory: %w", err)
    }
    return filepath.Join(home, ".conga", "conga-policy.yaml"), nil
}
```

### Helper: `loadPolicy() (*policy.PolicyFile, string, error)`

Loads the policy file, returning the file and its path. Creates a default policy if the file doesn't exist:

```go
func (s *Server) loadPolicy() (*policy.PolicyFile, string, error) {
    path, err := s.policyPath()
    if err != nil {
        return nil, "", err
    }
    pf, err := policy.Load(path)
    if err != nil {
        return nil, "", err
    }
    if pf == nil {
        // No policy file — return a default skeleton
        pf = &policy.PolicyFile{APIVersion: policy.CurrentAPIVersion}
    }
    return pf, path, nil
}
```

---

### Tool: `conga_policy_get`

**Purpose**: Read the current policy file as JSON.

| Field | Value |
|---|---|
| Name | `conga_policy_get` |
| Params | *(none)* |
| Annotations | `readOnly: true` |
| Returns | `PolicyFile` as JSON, or empty skeleton if no file exists |

**Handler**:
1. `loadPolicy()`
2. `jsonResult(pf)`

**Edge cases**:
- No policy file → return `{"apiVersion":"conga.dev/v1alpha1"}` (not an error)
- Malformed YAML → return error with parse details

---

### Tool: `conga_policy_validate`

**Purpose**: Validate the policy and return the enforcement report for the current provider.

| Field | Value |
|---|---|
| Name | `conga_policy_validate` |
| Params | `agent` (string, optional — scope report to one agent) |
| Annotations | `readOnly: true` |
| Returns | `ValidationResult` JSON |

**Response structure**:
```go
type ValidationResult struct {
    Valid   bool               `json:"valid"`
    Error   string             `json:"error,omitempty"`
    Policy  *policy.PolicyFile `json:"policy"`
    Report  []policy.RuleReport `json:"enforcement_report"`
}
```

**Handler**:
1. `loadPolicy()`
2. If no file exists, return `{valid: true, policy: skeleton, report: []}`
3. `pf.Validate()` — if error, return `{valid: false, error: "...", report: []}`
4. If `agent` param provided: `pf = pf.MergeForAgent(agent)` before generating report
5. `pf.EnforcementReport(s.prov.Name())` → `report`
6. Return `{valid: true, policy: pf, report: report}`

**Edge cases**:
- No policy file → valid with empty report (policy is optional per architecture standard)
- Invalid YAML → `valid: false` with parse error
- Unknown agent name → valid report (just uses global policy since no override exists)

---

### Tool: `conga_policy_get_agent`

**Purpose**: Get the effective policy for a specific agent (global + overrides merged).

| Field | Value |
|---|---|
| Name | `conga_policy_get_agent` |
| Params | `agent` (string, **required**) |
| Annotations | `readOnly: true` |
| Returns | Merged `PolicyFile` as JSON (no `agents` map — it's been flattened) |

**Handler**:
1. `loadPolicy()`
2. `pf.MergeForAgent(agent)` → `effective`
3. `jsonResult(effective)`

**Edge cases**:
- No policy file → return empty skeleton (same as `conga_policy_get`)
- Agent has no override → returns global policy
- Agent exists in `agents:` map → returns merged result

---

### Tool: `conga_policy_set_egress`

**Purpose**: Update the egress policy (allowed/blocked domains and mode).

| Field | Value |
|---|---|
| Name | `conga_policy_set_egress` |
| Params | `allowed_domains` (array of strings, optional), `blocked_domains` (array of strings, optional), `mode` (string, optional: `"validate"` or `"enforce"`), `agent` (string, optional) |
| Annotations | `destructive: true, idempotent: true` |
| Returns | Updated `PolicyFile` as JSON |

**InputSchema**:
```go
Properties: map[string]any{
    "allowed_domains": map[string]any{
        "type":        "array",
        "items":       map[string]any{"type": "string"},
        "description": "Domains the agent can reach (e.g., api.anthropic.com, *.slack.com)",
    },
    "blocked_domains": map[string]any{
        "type":        "array",
        "items":       map[string]any{"type": "string"},
        "description": "Domains to explicitly block (takes precedence over allowed_domains)",
    },
    "mode": map[string]any{
        "type":        "string",
        "enum":        []string{"validate", "enforce"},
        "description": "Enforcement mode: 'validate' (warn only) or 'enforce' (activate egress proxy)",
    },
    "agent": map[string]any{
        "type":        "string",
        "description": "If set, creates a per-agent override instead of modifying the global policy",
    },
}
```

**Handler**:
1. `loadPolicy()`
2. Extract params. Build `*EgressPolicy` from provided fields.
3. `policy.SetEgress(pf, agent, patch)`
4. `pf.Validate()` — if error, return error (don't save invalid policy)
5. `policy.Save(pf, path)`
6. `jsonResult(pf)` — return the full updated policy

**Parameter extraction for array params**:

The `mcp-go` library's `CallToolRequest` doesn't have a built-in `GetStringSlice` method. Extract from the raw arguments map:

```go
func getStringSlice(req mcp.CallToolRequest, key string) []string {
    raw, ok := req.Params.Arguments[key]
    if !ok {
        return nil
    }
    arr, ok := raw.([]any)
    if !ok {
        return nil
    }
    result := make([]string, 0, len(arr))
    for _, v := range arr {
        if s, ok := v.(string); ok {
            result = append(result, s)
        }
    }
    return result
}
```

**Edge cases**:
- No existing policy file → creates new file with `apiVersion` + egress section
- Empty `allowed_domains` → clears the allowlist (agent can reach any domain)
- Domain in both allowed and blocked → validation error (existing `validateEgress` catches this)
- Invalid domain format (e.g., `*bad.com`) → validation error
- All params omitted → no-op (saves unchanged policy)

---

### Tool: `conga_policy_set_routing`

**Purpose**: Update the routing policy (model selection and cost limits).

| Field | Value |
|---|---|
| Name | `conga_policy_set_routing` |
| Params | `default_model` (string, optional), `fallback_chain` (array of strings, optional), `cost_limits` (object, optional), `agent` (string, optional) |
| Annotations | `destructive: true, idempotent: true` |
| Returns | Updated `PolicyFile` as JSON |

**InputSchema**:
```go
Properties: map[string]any{
    "default_model": map[string]any{
        "type":        "string",
        "description": "Default model name for agent conversations",
    },
    "fallback_chain": map[string]any{
        "type":        "array",
        "items":       map[string]any{"type": "string"},
        "description": "Ordered list of fallback models",
    },
    "cost_limits": map[string]any{
        "type": "object",
        "properties": map[string]any{
            "daily_per_agent":   map[string]any{"type": "number", "description": "Max daily cost per agent (USD)"},
            "monthly_per_agent": map[string]any{"type": "number", "description": "Max monthly cost per agent (USD)"},
            "monthly_global":    map[string]any{"type": "number", "description": "Max monthly cost across all agents (USD)"},
        },
        "description": "Cost budget caps",
    },
    "agent": map[string]any{
        "type":        "string",
        "description": "If set, creates a per-agent override instead of modifying the global policy",
    },
}
```

**Handler**:
1. `loadPolicy()`
2. Extract params. Build `*RoutingPolicy` from provided fields.
3. `policy.SetRouting(pf, agent, patch)`
4. `pf.Validate()` → error if invalid
5. `policy.Save(pf, path)`
6. `jsonResult(pf)`

**Cost limits extraction**: Extract from nested map:
```go
func getCostLimits(req mcp.CallToolRequest) *policy.CostLimits {
    raw, ok := req.Params.Arguments["cost_limits"]
    if !ok {
        return nil
    }
    m, ok := raw.(map[string]any)
    if !ok {
        return nil
    }
    cl := &policy.CostLimits{}
    if v, ok := m["daily_per_agent"].(float64); ok {
        cl.DailyPerAgent = v
    }
    if v, ok := m["monthly_per_agent"].(float64); ok {
        cl.MonthlyPerAgent = v
    }
    if v, ok := m["monthly_global"].(float64); ok {
        cl.MonthlyGlobal = v
    }
    return cl
}
```

**Edge cases**:
- Negative cost limits → validation error
- Cost limits without a default model → valid (limits can exist independently)
- Routing is validate-only today (Bifrost not yet integrated) — tool still saves the config

---

### Tool: `conga_policy_set_posture`

**Purpose**: Update posture declarations (security properties).

| Field | Value |
|---|---|
| Name | `conga_policy_set_posture` |
| Params | `isolation_level` (string, optional), `secrets_backend` (string, optional), `monitoring` (string, optional), `compliance_frameworks` (array of strings, optional), `agent` (string, optional) |
| Annotations | `destructive: true, idempotent: true` |
| Returns | Updated `PolicyFile` as JSON |

**InputSchema**:
```go
Properties: map[string]any{
    "isolation_level": map[string]any{
        "type":        "string",
        "enum":        []string{"standard", "hardened", "segmented"},
        "description": "Container isolation level",
    },
    "secrets_backend": map[string]any{
        "type":        "string",
        "enum":        []string{"file", "managed", "proxy"},
        "description": "Secrets storage backend",
    },
    "monitoring": map[string]any{
        "type":        "string",
        "enum":        []string{"basic", "standard", "full"},
        "description": "Monitoring level",
    },
    "compliance_frameworks": map[string]any{
        "type":        "array",
        "items":       map[string]any{"type": "string"},
        "description": "Compliance frameworks to declare (e.g., SOC2, HIPAA)",
    },
    "agent": map[string]any{
        "type":        "string",
        "description": "If set, creates a per-agent override instead of modifying the global policy",
    },
}
```

**Handler**: Same load → set → validate → save → return pattern.

**Edge cases**:
- Invalid enum value (e.g., `isolation_level: "maximum"`) → validation error
- Compliance frameworks only applicable on AWS → valid to save, but enforcement report shows `not-applicable` on other providers

---

### Tool: `conga_policy_deploy`

**Purpose**: Validate the current policy and deploy it to running agents via refresh.

| Field | Value |
|---|---|
| Name | `conga_policy_deploy` |
| Params | `agent` (string, optional — deploy to one agent, or all if omitted) |
| Annotations | `destructive: true` |
| Returns | `DeployResult` JSON |

**Response structure**:
```go
type DeployResult struct {
    Validated bool     `json:"validated"`
    Deployed  []string `json:"deployed"`
    Errors    []string `json:"errors,omitempty"`
}
```

**Handler**:
1. `loadPolicy()`
2. If no policy file: return error "no policy file found — create one with conga_policy_set_egress or conga_policy_set_routing first"
3. `pf.Validate()` — if error, return error (refuse to deploy invalid policy)
4. If `agent` specified:
   - Verify agent exists via `s.prov.GetAgent(ctx, agent)`
   - `s.prov.RefreshAgent(ctx, agent)` — this reloads policy from disk, regenerates egress proxy config, restarts container
   - Return `{validated: true, deployed: ["agent-name"]}`
5. If no agent:
   - `s.prov.RefreshAll(ctx)` — refreshes all non-paused agents
   - List agents via `s.prov.ListAgents(ctx)` to report which were deployed
   - Return `{validated: true, deployed: ["agent1", "agent2", ...]}`

**Edge cases**:
- No policy file → error (nothing to deploy)
- Invalid policy → error with validation details (no deploy attempted)
- Agent not found → error
- Agent is paused → `RefreshAll` skips paused agents (existing behavior); for single-agent deploy, return error
- Refresh failure for one agent in `RefreshAll` → existing `RefreshAll` behavior (errors propagated by provider)
- Policy has no egress section → valid deploy, no proxy changes (RefreshAgent handles this)

**Why RefreshAgent is sufficient**: Both local and remote providers call `LoadEgressPolicy()` inside `RefreshAgent()`, which reads the policy file from disk, regenerates the Squid proxy config, and restarts the egress proxy container. No additional mechanism needed — the deploy tool just validates first, then delegates.

---

## 4. Registration

**File**: `cli/pkg/mcpserver/tools.go`

Add to `registerTools()`:

```go
s.tools = []server.ServerTool{
    // ... existing tools ...

    // Policy
    s.toolPolicyGet(),
    s.toolPolicyValidate(),
    s.toolPolicyGetAgent(),
    s.toolPolicySetEgress(),
    s.toolPolicySetRouting(),
    s.toolPolicySetPosture(),
    s.toolPolicyDeploy(),
}
```

---

## 5. Edge Cases Summary

| Scenario | Behavior |
|---|---|
| No policy file exists | Read tools return empty skeleton; set tools create the file; deploy returns error |
| Malformed YAML | Read/validate return parse error; set tools refuse to overwrite (load fails) |
| Concurrent MCP edits | Atomic write prevents corruption; last writer wins |
| Set with all params empty | No-op — saves unchanged policy |
| Deploy with no running agents | RefreshAll succeeds (no agents to refresh) |
| Deploy paused agent directly | Error — agent must be unpaused first |
| YAML comments lost on round-trip | Accepted — documented in tool descriptions |
| Policy valid but posture not enforceable | Valid; enforcement report shows `validate-only` or `not-applicable` |

---

## 6. Testing Plan

### `cli/pkg/policy/mutate_test.go`

| Test | What it verifies |
|---|---|
| `TestSaveAndReload` | Round-trip: Save → Load → compare. Atomic write produces valid YAML |
| `TestSaveAtomicOnError` | Temp file cleanup on marshal error (unlikely but defensive) |
| `TestSetEgressGlobal` | Set global egress on empty policy, verify fields |
| `TestSetEgressAgent` | Set per-agent egress, verify `Agents` map populated |
| `TestSetEgressAgentCreatesMap` | Set per-agent when `Agents` is nil, verify map initialized |
| `TestSetRoutingGlobal` | Set global routing with cost limits |
| `TestSetRoutingAgent` | Set per-agent routing override |
| `TestSetPostureGlobal` | Set global posture with all fields |
| `TestSetPostureAgent` | Set per-agent posture override |
| `TestSetPreservesOtherSections` | Set egress doesn't clobber existing routing/posture |
| `TestSetAgentPreservesOtherOverrides` | Set egress for agent A doesn't affect agent B's overrides |

### `cli/pkg/mcpserver/tools_policy_test.go`

| Test | What it verifies |
|---|---|
| `TestPolicyGetNoFile` | Returns empty skeleton, not error |
| `TestPolicyGetExistingFile` | Returns full policy as JSON |
| `TestPolicyValidateValid` | Returns `valid: true` with enforcement report |
| `TestPolicyValidateInvalid` | Returns `valid: false` with error message |
| `TestPolicyValidateWithAgent` | Merges agent overrides before reporting |
| `TestPolicyGetAgent` | Returns merged policy for specific agent |
| `TestPolicyGetAgentNoOverride` | Returns global policy when no agent override |
| `TestPolicySetEgress` | Creates/updates egress, verify file on disk |
| `TestPolicySetEgressValidationError` | Rejects invalid domain, file unchanged |
| `TestPolicySetEgressAgent` | Creates per-agent override |
| `TestPolicySetRouting` | Creates/updates routing with cost limits |
| `TestPolicySetPosture` | Creates/updates posture |
| `TestPolicyDeployValidatesFirst` | Invalid policy → error, no refresh called |
| `TestPolicyDeploySingleAgent` | Valid policy → RefreshAgent called with correct name |
| `TestPolicyDeployAll` | Valid policy → RefreshAll called |
| `TestPolicyDeployNoFile` | No policy file → error |
| `TestGetStringSlice` | Helper correctly extracts `[]string` from request args |
| `TestGetCostLimits` | Helper correctly extracts nested cost limits object |

### Mock Provider for Deploy Tests

Use a recording mock that tracks which methods were called:

```go
type mockProvider struct {
    provider.Provider // embed for unimplemented methods
    refreshAgentCalls []string
    refreshAllCalled  bool
    agents            []provider.AgentConfig
}

func (m *mockProvider) Name() string { return "local" }
func (m *mockProvider) RefreshAgent(_ context.Context, name string) error {
    m.refreshAgentCalls = append(m.refreshAgentCalls, name)
    return nil
}
func (m *mockProvider) RefreshAll(_ context.Context) error {
    m.refreshAllCalled = true
    return nil
}
func (m *mockProvider) ListAgents(_ context.Context) ([]provider.AgentConfig, error) {
    return m.agents, nil
}
func (m *mockProvider) GetAgent(_ context.Context, name string) (*provider.AgentConfig, error) {
    for i := range m.agents {
        if m.agents[i].Name == name {
            return &m.agents[i], nil
        }
    }
    return nil, fmt.Errorf("agent %q not found", name)
}
```

---

## 7. File Summary

| File | Action | Lines (est.) |
|---|---|---|
| `cli/pkg/policy/mutate.go` | **New** | ~80 |
| `cli/pkg/policy/mutate_test.go` | **New** | ~200 |
| `cli/pkg/mcpserver/tools_policy.go` | **New** | ~350 |
| `cli/pkg/mcpserver/tools_policy_test.go` | **New** | ~400 |
| `cli/pkg/mcpserver/tools.go` | **Edit** | +8 lines (registration) |

**Total**: ~4 new files, ~1,030 new lines, 1 edit.

No changes to:
- `provider.go` (no new interface methods)
- Provider implementations (deploy uses existing `RefreshAgent`/`RefreshAll`)
- CLI commands (MCP tools are independent of CLI)
- Terraform / bootstrap scripts
