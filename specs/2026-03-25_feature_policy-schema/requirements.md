# Requirements: Portable Policy Schema

## Goal

Design and implement a portable policy artifact (`conga-policy.yaml`) that defines an operator's security and routing intent in a single file. Each provider reads the same policy and enforces what it can with available tools. This is the data model foundation — no enforcement logic in this spec.

## Success Criteria

1. A `conga-policy.yaml` YAML schema exists with three sections: egress rules, routing policy, and security posture declarations, plus per-agent overrides.
2. Go types in a new `cli/internal/policy/` package parse and validate the schema using `gopkg.in/yaml.v3`.
3. `Load()` reads the policy file from the provider-appropriate path (`~/.conga/conga-policy.yaml` for local, `/opt/conga/conga-policy.yaml` for remote, SSM parameter for AWS). Returns nil (no error) when the file doesn't exist — policy is optional.
4. `Validate()` checks structural validity: required fields when sections are present, valid enum values (egress mode, isolation level, secrets backend, monitoring level), domain format validation, no unknown fields.
5. `MergeForAgent(agentName)` returns an effective policy for a specific agent by shallow-merging the agent's override block onto the global defaults.
6. `EnforcementReport(providerName)` returns a per-rule report: "enforced", "partial", "validate-only", or "not-applicable" — so operators see what will actually happen on their target provider.
7. `conga policy validate` CLI command loads, validates, and prints the enforcement report for the current provider. Exits 0 on valid, 1 on invalid with descriptive errors.
8. `conga-policy.yaml.example` in the project root documents all fields with comments.
9. Schema includes `apiVersion: conga.dev/v1alpha1` for future versioning.
10. Unit tests cover: valid parse, invalid YAML, missing optional sections, per-agent override merging, wildcard domain matching (`*.slack.com`), enforcement report generation for all three providers.
