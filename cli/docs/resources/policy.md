---
page_title: "conga_policy Resource"
subcategory: ""
description: |-
  Manages the CongaLine policy (egress, routing, posture).
---

# conga_policy

Manages the CongaLine policy — controlling egress rules, routing defaults, security posture, and per-agent overrides.

Policy is a **singleton** — only one `conga_policy` resource per deployment. Changes are applied by writing the policy file and refreshing all running agents.

## Example Usage

```hcl
resource "conga_policy" "this" {
  egress_mode            = "enforce"
  egress_allowed_domains = ["api.anthropic.com", "*.slack.com"]

  agent_override {
    name                   = "aaron"
    egress_allowed_domains = ["api.anthropic.com", "*.slack.com", "*.trello.com"]
  }

  depends_on = [conga_environment.this]
}
```

## Schema

### Optional

- `egress_mode` (String) — Egress enforcement mode: `"enforce"` (default) or `"validate"`.
- `egress_allowed_domains` (List of String) — Allowed external domains (supports wildcards like `*.example.com`).
- `egress_blocked_domains` (List of String) — Blocked domains (takes precedence over allowed).
- `posture_isolation_level` (String) — Isolation level: `"standard"`, `"hardened"`, or `"segmented"`.
- `posture_secrets_backend` (String) — Secrets backend: `"file"`, `"managed"`, or `"proxy"`.
- `posture_monitoring` (String) — Monitoring level: `"basic"`, `"standard"`, or `"full"`.
- `posture_compliance_frameworks` (List of String) — Compliance frameworks (e.g. `"SOC2"`, `"HIPAA"`).
- `routing_default_model` (String) — Default model for agent routing.

### Blocks

- `agent_override` (Block List) — Per-agent policy overrides. Each block **replaces** (not merges) the corresponding global section for that agent.
  - `name` (String, Required) — Agent name.
  - `egress_mode` (String) — Egress mode override.
  - `egress_allowed_domains` (List of String) — Allowed domains override.
  - `egress_blocked_domains` (List of String) — Blocked domains override.
  - `routing_default_model` (String) — Default model override.

### Read-Only

- `id` (String) — Always `"policy"`.

## Import

```shell
terraform import conga_policy.this policy
```
