---
page_title: "conga_channel Resource"
subcategory: ""
description: |-
  Manages a messaging channel platform (e.g. Slack) for CongaLine agents.
---

# conga_channel

Manages a messaging channel platform for CongaLine agents. Currently supports Slack.

Adding a channel stores shared secrets, generates the router configuration, and starts the event router. The router holds a single Socket Mode connection to Slack and fans out events to per-agent containers via HTTP webhooks.

Slack is **optional** — agents can run in gateway-only mode (web UI) without any channel configuration.

## Example Usage

```hcl
resource "conga_channel" "slack" {
  platform = "slack"
  secrets = {
    "slack-bot-token"      = var.slack_bot_token
    "slack-signing-secret" = var.slack_signing_secret
    "slack-app-token"      = var.slack_app_token
  }

  depends_on = [conga_environment.this]
}
```

## Schema

### Required

- `platform` (String) — Channel platform (e.g. `"slack"`). Forces replacement on change.

### Optional

- `secrets` (Map of String, Sensitive) — Platform secrets as key-value pairs. Secret values cannot be read back after creation.

### Read-Only

- `id` (String) — Channel identifier (platform name).
- `configured` (Boolean) — Whether the channel credentials are present.
- `router_running` (Boolean) — Whether the router container is running.

## Import

```shell
terraform import conga_channel.slack slack
```
