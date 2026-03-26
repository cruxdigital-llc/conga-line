# Requirements: MCP Policy Tools

## Goal

Enable AI assistants to fully manage the policy lifecycle — read, validate, modify, and deploy policies to running agents — entirely through MCP tools.

Today, managing policies requires hand-editing `conga-policy.yaml` and running `conga policy validate` from the CLI. Deploying policy changes requires a separate `conga admin refresh` or `conga admin cycle-host`. This feature unifies the entire workflow into MCP tools so an AI assistant can manage policies end-to-end without dropping to the shell.

## Success Criteria

1. Policy can be read, validated, and modified entirely through MCP tools
2. Policy changes can be **deployed** to specific agents or all agents via MCP
3. Deploy validates the policy before applying — no deploying invalid config
4. Enforcement report available via MCP showing what's enforced vs validate-only per provider
5. Per-agent effective policy (with overrides merged) is queryable
6. Follows existing MCP tool patterns (annotations, timeouts, error handling)
7. Test coverage for all new tools and policy mutation helpers

## Tool Surface

| Tool | Purpose | Annotation |
|---|---|---|
| `conga_policy_get` | Read current policy file (parsed YAML → JSON) | readOnly |
| `conga_policy_validate` | Validate policy + return enforcement report | readOnly |
| `conga_policy_set_egress` | Update egress allowed/blocked domains and mode | destructive |
| `conga_policy_set_routing` | Update routing model/cost config | destructive |
| `conga_policy_set_posture` | Update posture declarations | destructive |
| `conga_policy_get_agent` | Get effective policy for a specific agent (overrides merged) | readOnly |
| `conga_policy_deploy` | Validate then apply policy to agent(s) via refresh | destructive |

## Constraints

- Policy file location is provider-dependent: `~/.conga/conga-policy.yaml` (local/remote), S3 (AWS)
- Deploy on local/remote = `RefreshAgent` per agent (regenerates Squid proxy config, restarts container)
- Deploy on AWS = `RefreshAgent` per agent or `CycleHost` for full reboot
- Mutation helpers must preserve YAML comments and formatting where possible (use `gopkg.in/yaml.v3` node-level manipulation)
- Validate-before-deploy is mandatory — `conga_policy_deploy` must reject invalid policy
- Per-agent overrides in `agents:` section must be supported by set tools (e.g., `conga_policy_set_egress --agent myagent`)
