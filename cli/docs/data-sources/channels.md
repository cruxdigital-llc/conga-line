---
page_title: "conga_channels Data Source"
subcategory: ""
description: |-
  Lists all configured messaging channels.
---

# conga_channels

Lists all configured messaging channels and their current status.

## Example Usage

```hcl
data "conga_channels" "all" {}

output "slack_configured" {
  value = [for ch in data.conga_channels.all.channels : ch.configured if ch.platform == "slack"]
}
```

## Schema

### Read-Only

- `id` (String) — Always `"channels"`.
- `channels` (List of Object) — List of channel statuses.
  - `platform` (String) — Channel platform name.
  - `configured` (Boolean) — Whether credentials are present.
  - `router_running` (Boolean) — Whether the router is running.
  - `bound_agents` (List of String) — Agent names bound to this channel.
