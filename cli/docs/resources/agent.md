---
page_title: "conga_agent Resource"
subcategory: ""
description: |-
  Manages a CongaLine agent (AI assistant container).
---

# conga_agent

Manages a CongaLine agent — an autonomous AI assistant running in a Docker container.

Agents are **immutable**: changing `name` or `type` forces recreation. Each agent gets its own Docker network, container, egress proxy, and data directory.

## Example Usage

```hcl
resource "conga_agent" "aaron" {
  name = "aaron"
  type = "user"

  depends_on = [conga_environment.this]
}

resource "conga_agent" "engineering" {
  name         = "engineering"
  type         = "team"
  gateway_port = 18790

  depends_on = [conga_environment.this]
}
```

## Schema

### Required

- `name` (String) — Unique agent name. Lowercase alphanumeric with hyphens, starting with a letter, max 63 characters. Forces replacement on change.
- `type` (String) — Agent type: `"user"` (DM-only) or `"team"` (channel-based). Forces replacement on change.

### Optional

- `gateway_port` (Number) — Gateway port on the host. Auto-assigned from 18789 if omitted.

### Read-Only

- `id` (String) — Agent identifier (same as name).
- `paused` (Boolean) — Whether the agent is paused.

## Import

```shell
terraform import conga_agent.aaron aaron
```
