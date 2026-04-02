---
page_title: "conga_agent_status Data Source"
subcategory: ""
description: |-
  Reads the current status of a CongaLine agent.
---

# conga_agent_status

Reads the current status of a CongaLine agent, including container state, uptime, and resource usage.

## Example Usage

```hcl
data "conga_agent_status" "aaron" {
  name = "aaron"
}

output "aaron_state" {
  value = data.conga_agent_status.aaron.container_state
}
```

## Schema

### Required

- `name` (String) — Agent name.

### Read-Only

- `id` (String) — Agent name.
- `service_state` (String) — Service state: `"running"`, `"stopped"`, or `"not-found"`.
- `container_state` (String) — Container state: `"running"`, `"exited"`, `"created"`, or `"not found"`.
- `uptime_seconds` (Number) — Container uptime in seconds.
- `memory_usage` (String) — Current memory usage.
- `restart_count` (Number) — Number of container restarts.
