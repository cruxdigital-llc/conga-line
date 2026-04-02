---
page_title: "conga_environment Resource"
subcategory: ""
description: |-
  Manages a CongaLine environment (shared infrastructure for agents).
---

# conga_environment

Manages a CongaLine environment — the shared infrastructure that agents run on. This is typically the first resource created.

On **local**, this sets up `~/.conga/`, pulls the Docker image, and builds the egress proxy.
On **remote**, this connects via SSH, installs Docker if needed, and sets up `/opt/conga/`.
On **aws**, this configures SSM parameters and prepares the EC2 instance.

Environment is a singleton — only one per provider deployment.

## Example Usage

```hcl
resource "conga_environment" "this" {
  image = "ghcr.io/openclaw/openclaw:2026.3.11"
}
```

## Schema

### Required

- `image` (String) — Docker image for OpenClaw containers.

### Optional

- `install_docker` (Boolean) — Automatically install Docker if not present (remote/AWS).

### Read-Only

- `id` (String) — Environment identifier (provider type name).

## Import

```shell
terraform import conga_environment.this local
```

~> After import, set the correct `image` in your configuration and run `terraform apply` to reconcile state. The image cannot be read back from the provider.
