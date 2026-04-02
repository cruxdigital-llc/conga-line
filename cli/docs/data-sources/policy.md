---
page_title: "conga_policy Data Source"
subcategory: ""
description: |-
  Reads the current CongaLine policy.
---

# conga_policy (Data Source)

Reads the current CongaLine policy configuration from disk.

## Example Usage

```hcl
data "conga_policy" "current" {}

output "egress_mode" {
  value = data.conga_policy.current.egress_mode
}
```

## Schema

### Read-Only

- `id` (String) — Always `"policy"`.
- `egress_mode` (String) — Egress enforcement mode.
- `egress_allowed_domains` (List of String) — Allowed external domains.
- `egress_blocked_domains` (List of String) — Blocked domains.
- `routing_default_model` (String) — Default routing model.
