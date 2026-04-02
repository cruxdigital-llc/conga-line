# Plan: Portable Policy Schema

## Approach

Create a new `cli/pkg/policy/` package that owns the policy data model, loading, validation, and enforcement reporting. Add a `conga policy validate` CLI command following the existing cobra subcommand pattern. The policy file is optional — when absent, all behavior is unchanged. When present, it's parsed and validated but not enforced (enforcement is Spec 2+).

This is the first YAML file in the project. All other config files are JSON. The distinction is intentional: the policy file is operator-authored intent; JSON files are machine-generated config. Add `gopkg.in/yaml.v3` as the sole new dependency.

## Changes

### 1. New package: `cli/pkg/policy/`

**policy.go** — Types and core logic:
- `PolicyFile` struct with `APIVersion`, `Egress`, `Routing`, `Posture`, `Agents` fields
- `EgressPolicy` struct: `AllowedDomains []string`, `BlockedDomains []string`, `Mode string` (validate|enforce)
- `RoutingPolicy` struct: `DefaultModel string`, `Models map[string]ModelDef`, `FallbackChain []string`, `CostLimits CostLimits`
- `PostureDeclarations` struct: `IsolationLevel string`, `SecretsBackend string`, `Monitoring string`, `ComplianceFrameworks []string`
- `AgentOverride` struct: same shape as top-level sections (Egress, Routing, Posture), all fields optional
- `Load(path string) (*PolicyFile, error)` — reads YAML, returns nil if file doesn't exist
- `Validate() error` — structural validation (enums, domain format, no unknown fields)
- `MergeForAgent(name string) *PolicyFile` — shallow merge agent override onto global defaults
- Helper: `MatchDomain(pattern, domain string) bool` — for wildcard matching (`*.slack.com` matches `wss-primary.slack.com`)

**enforcement.go** — Provider capability reporting:
- `EnforcementLevel` type: `Enforced`, `Partial`, `ValidateOnly`, `NotApplicable`
- `RuleReport` struct: `Rule string`, `Level EnforcementLevel`, `Detail string`
- `EnforcementReport(providerName string) []RuleReport` — static mapping of which provider can enforce which rules
- Mapping:
  - Egress allowlist: AWS=Enforced (Squid), Remote=Partial (iptables), Local=ValidateOnly|Enforced (mode-dependent)
  - Isolation level: AWS=Enforced (standard/hardened), Remote=Partial, Local=ValidateOnly
  - Secrets backend: AWS=Enforced (managed), Remote=Enforced (file), Local=Enforced (file)
  - Monitoring: AWS=Enforced (basic/standard), Remote=Partial, Local=Enforced (basic only)
  - Routing: all providers=ValidateOnly until Bifrost lands (Spec 5)

**policy_test.go** — Unit tests:
- Parse valid YAML with all sections
- Parse minimal YAML (apiVersion only)
- Reject invalid YAML (syntax error)
- Reject unknown egress mode
- Validate domain format (reject empty, reject spaces)
- Wildcard domain matching: `*.slack.com` matches subdomains, doesn't match `slack.com` itself
- Agent override merging: global egress + agent egress override = agent's domains replace global
- Agent override merging: agent without override = global defaults unchanged
- Enforcement report: correct levels for each provider
- Load returns nil when file doesn't exist

### 2. New CLI command: `cli/cmd/policy.go`

Following the pattern in `cli/cmd/secrets.go`:
- `policyCmd` parent command (`conga policy`)
- `policyValidateCmd` subcommand (`conga policy validate`)
  - Optional `--file` flag to override default path
  - Optional `--agent` flag to show merged policy for a specific agent
  - Loads policy, validates, prints enforcement report for current provider
  - Exit 0 on valid, exit 1 with error details on invalid
  - Supports `--output json` (existing global flag) for machine-readable output
- Register `policyCmd` in `root.go`

### 3. New file: `conga-policy.yaml.example`

In project root. All fields documented with YAML comments. Includes realistic defaults (Anthropic API, Slack, GitHub as allowed domains).

### 4. Dependency: `gopkg.in/yaml.v3`

`go get gopkg.in/yaml.v3` and `go mod tidy`.

## What This Does NOT Do

- No enforcement of any policy rules (that's Spec 2: Egress, and future specs)
- No changes to the Provider interface
- No changes to `ProvisionAgent()`, `RefreshAgent()`, or any container launch logic
- No changes to `GenerateOpenClawConfig()` or `GenerateEnvFile()`
- No changes to Terraform

## Test Plan

1. `go test ./cli/pkg/policy/` — all unit tests pass
2. `go build ./cli/cmd/` — CLI compiles with new command
3. `conga policy validate --file conga-policy.yaml.example` — exits 0, prints enforcement report
4. `conga policy validate --file /dev/null` — exits 1 with parse error
5. `conga policy validate` (no policy file) — prints "No policy file found" info message, exits 0
