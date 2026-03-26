# Plan: MCP Policy Tools

## Approach

Three phases: (1) add policy mutation helpers to the existing `policy` package, (2) add MCP tool handlers, (3) wire up deploy via existing provider refresh methods.

The mutation layer is the key new capability — the policy package today can parse and validate but not write back. The MCP tools themselves are thin wrappers following the established pattern in `cli/internal/mcpserver/`.

---

## Phase 1: Policy Mutation Helpers

**Files**: `cli/internal/policy/mutate.go`, `cli/internal/policy/mutate_test.go`

Add read-modify-write helpers to the `policy` package:

1. **`LoadFile(path string) (*PolicyFile, error)`** — already exists, confirm it round-trips cleanly
2. **`SaveFile(pf *PolicyFile, path string) error`** — marshal to YAML and write atomically (write to `.tmp`, rename)
3. **`SetEgress(pf *PolicyFile, agent string, egress *EgressPolicy)`** — set global or per-agent egress; `agent=""` means global
4. **`SetRouting(pf *PolicyFile, agent string, routing *RoutingPolicy)`** — same pattern for routing
5. **`SetPosture(pf *PolicyFile, agent string, posture *PostureDeclarations)`** — same pattern for posture
6. **`EffectivePolicy(pf *PolicyFile, agent string) *PolicyFile`** — return a merged view with agent overrides applied (already partially exists via `EffectiveAllowedDomains`, generalize it)

Design notes:
- Set functions modify the `*PolicyFile` in place; caller is responsible for `SaveFile`
- When `agent` is non-empty, create/update `pf.Agents[agent]` override
- Atomic write via temp file + `os.Rename` prevents partial writes on crash

**Tests**: Round-trip (load → mutate → save → reload → verify), per-agent override creation, empty-to-populated transitions, blocked-domains precedence preserved.

---

## Phase 2: MCP Tool Handlers

**Files**: `cli/internal/mcpserver/tools_policy.go`, `cli/internal/mcpserver/tools_policy_test.go`

Seven tools following the existing pattern (see `tools_secrets.go` for a good model):

### Read-only tools

**`conga_policy_get`**
- Params: none
- Behavior: Load policy file from provider's policy path, return as JSON
- Annotations: `readOnly: true`

**`conga_policy_validate`**
- Params: `agent` (optional — scope enforcement report to one agent)
- Behavior: Load + validate + generate enforcement report for current provider
- Returns: validation result + `[]RuleReport` as JSON
- Annotations: `readOnly: true`

**`conga_policy_get_agent`**
- Params: `agent` (required)
- Behavior: Load policy, merge per-agent overrides, return effective policy as JSON
- Annotations: `readOnly: true`

### Mutation tools

**`conga_policy_set_egress`**
- Params: `allowed_domains` ([]string, optional), `blocked_domains` ([]string, optional), `mode` (string, optional: "validate"|"enforce"), `agent` (string, optional — if set, creates per-agent override)
- Behavior: Load → SetEgress → Validate → SaveFile
- Validates before saving; returns error if invalid
- Annotations: `destructive: true, idempotent: true`

**`conga_policy_set_routing`**
- Params: `default_model` (optional), `fallback_chain` ([]string, optional), `cost_limits` (object, optional), `agent` (optional)
- Behavior: Load → SetRouting → Validate → SaveFile
- Annotations: `destructive: true, idempotent: true`

**`conga_policy_set_posture`**
- Params: `isolation_level` (optional), `secrets_backend` (optional), `monitoring` (optional), `compliance_frameworks` ([]string, optional), `agent` (optional)
- Behavior: Load → SetPosture → Validate → SaveFile
- Annotations: `destructive: true, idempotent: true`

### Deploy tool

**`conga_policy_deploy`**
- Params: `agent` (optional — deploy to one agent, or all if omitted)
- Behavior:
  1. Load + validate policy (fail fast if invalid)
  2. If `agent` specified: `s.prov.RefreshAgent(ctx, agent)`
  3. If no agent: `s.prov.RefreshAll(ctx)`
- Returns: per-agent deploy status (success/failure + any error messages)
- Annotations: `destructive: true`

### Policy path resolution

The MCP server already has access to the provider (`s.prov`). Add a `PolicyPath() string` method to the Provider interface (or use a convention: `~/.conga/conga-policy.yaml` for local/remote, provider-specific for AWS). Check if the provider already exposes config paths.

---

## Phase 3: Provider Integration

**Files**: Possibly `cli/internal/provider/provider.go` (interface), provider implementations

1. **Add `PolicyPath() string`** to the Provider interface if not already present — returns the path to the policy YAML file for this provider
2. **Local**: returns `~/.conga/conga-policy.yaml`
3. **Remote**: returns `~/.conga/conga-policy.yaml` (policy is local, deployed via refresh which pushes to remote)
4. **AWS**: returns `~/.conga/conga-policy.yaml` (same — policy is local, `RefreshAgent` handles deployment)
5. **Verify `RefreshAgent` reloads policy**: local and remote already call `LoadEgressPolicy()` during refresh — confirm this picks up changes from disk

If all three providers already use `~/.conga/conga-policy.yaml` as the source (and they do based on research), we may not need a provider method at all — just a constant or config lookup.

---

## Phase 4: Registration & Wiring

**Files**: `cli/internal/mcpserver/tools.go`

1. Add all 7 tools to `registerTools()` in tools.go
2. Group them together as a policy section (consistent with existing grouping)

---

## Testing Strategy

| Layer | What | How |
|---|---|---|
| Mutation helpers | Round-trip, merge, validate-on-save | Unit tests in `mutate_test.go` with temp files |
| MCP tools | Parameter handling, error cases, mock provider | Unit tests in `tools_policy_test.go` using existing test patterns |
| Deploy flow | Validate-before-deploy, per-agent vs all | Unit tests with mock provider that records RefreshAgent/RefreshAll calls |
| Integration | End-to-end via local provider | Manual: set egress → deploy → verify proxy container updated |

---

## File Summary

| File | Action | Description |
|---|---|---|
| `cli/internal/policy/mutate.go` | **New** | SaveFile, SetEgress, SetRouting, SetPosture, EffectivePolicy |
| `cli/internal/policy/mutate_test.go` | **New** | Tests for mutation helpers |
| `cli/internal/mcpserver/tools_policy.go` | **New** | 7 MCP tool handlers |
| `cli/internal/mcpserver/tools_policy_test.go` | **New** | Tests for MCP tools |
| `cli/internal/mcpserver/tools.go` | **Edit** | Register policy tools in registerTools() |
| `cli/internal/provider/provider.go` | **Edit** (maybe) | Add PolicyPath() if needed |
| Provider implementations | **Edit** (maybe) | Implement PolicyPath() if added to interface |

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| YAML formatting lost on round-trip | Use `yaml.v3` Marshal (preserves key order); accept that comments may be lost — document this |
| Concurrent policy edits (two MCP clients) | Atomic write via temp+rename prevents corruption; last-write-wins is acceptable for this use case |
| Deploy to AWS requires full CycleHost | RefreshAgent on AWS already handles per-agent refresh via SSM scripts; CycleHost only needed for bootstrap-level changes |
| Large policy files slow MCP response | Policy files are tiny (<1KB typically); not a real concern |
