---
page_title: "conga_channel_binding Resource"
subcategory: ""
description: |-
  Binds a messaging channel to a CongaLine agent.
---

# conga_channel_binding

Binds a messaging channel to a CongaLine agent. For Slack, this maps a member ID (user agents) or channel ID (team agents) to a specific agent so the router knows where to deliver events.

All fields are **immutable** — any change forces recreation.

## Example Usage

```hcl
# Bind a Slack user to a user agent
resource "conga_channel_binding" "aaron_slack" {
  agent      = "aaron"
  platform   = "slack"
  binding_id = "U01ABCDEF12"

  depends_on = [conga_agent.aaron, conga_channel.slack]
}

# Bind a Slack channel to a team agent
resource "conga_channel_binding" "engineering_slack" {
  agent      = "engineering"
  platform   = "slack"
  binding_id = "C01ABCDEF12"

  depends_on = [conga_agent.engineering, conga_channel.slack]
}
```

## Schema

### Required

- `agent` (String) — Agent name to bind the channel to. Forces replacement on change.
- `platform` (String) — Channel platform (e.g. `"slack"`). Forces replacement on change.
- `binding_id` (String) — Platform-specific ID (Slack member ID for user agents, channel ID for team agents). Forces replacement on change.

### Optional

- `label` (String) — Human-readable label for the binding. Forces replacement on change.

### Read-Only

- `id` (String) — Binding identifier (`agent/platform`).

## Import

```shell
terraform import conga_channel_binding.aaron_slack aaron/slack
```
