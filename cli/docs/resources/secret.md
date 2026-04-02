---
page_title: "conga_secret Resource"
subcategory: ""
description: |-
  Manages a secret for a CongaLine agent.
---

# conga_secret

Manages a secret for a CongaLine agent. Secrets are stored as environment variables injected into the agent's container at startup.

On **local/remote**, secrets are stored as files with mode 0400. On **aws**, secrets are stored in AWS Secrets Manager.

The `value` attribute is **write-only** — it cannot be read back after creation. After import, you must set the correct value and run `terraform apply`.

## Example Usage

```hcl
resource "conga_secret" "api_key" {
  agent = "aaron"
  name  = "anthropic-api-key"
  value = var.anthropic_api_key

  depends_on = [conga_agent.aaron]
}
```

## Schema

### Required

- `agent` (String) — Agent name this secret belongs to. Forces replacement on change.
- `name` (String) — Secret name in kebab-case (e.g. `anthropic-api-key`). Forces replacement on change.
- `value` (String, Sensitive) — Secret value. Cannot be read back after creation.

### Read-Only

- `id` (String) — Secret identifier (`agent/name`).

## Import

```shell
terraform import conga_secret.api_key aaron/anthropic-api-key
```

~> After import, the secret value is empty in state. Set the correct value in your configuration and run `terraform apply` to reconcile.
