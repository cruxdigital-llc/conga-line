---
page_title: "Conga Provider"
subcategory: ""
description: |-
  Manage CongaLine AI agent environments declaratively.
---

# Conga Provider

The Conga provider enables declarative lifecycle management of [CongaLine](https://github.com/cruxdigital-llc/CongaLine) environments — autonomous AI assistant deployments powered by OpenClaw.

It wraps the same Go `Provider` interface used by the `conga` CLI, so Terraform and the CLI always produce identical results.

## Supported Providers

- **local** — Docker containers on your machine. State in `~/.conga/`.
- **remote** — Docker containers on any SSH-accessible host. State on remote at `/opt/conga/`.
- **aws** — EC2 host with Docker containers in a zero-ingress VPC. State in SSM Parameter Store and Secrets Manager.

## Example Usage

### Local

```hcl
provider "conga" {
  provider_type = "local"
}
```

### Remote (SSH)

```hcl
provider "conga" {
  provider_type = "remote"
  ssh_host      = "vps.example.com"
  ssh_user      = "root"
  ssh_key_path  = "~/.ssh/id_ed25519"
}
```

### AWS

```hcl
provider "conga" {
  provider_type = "aws"
  region        = "us-east-2"
  profile       = "myprofile"
}
```

## Schema

### Required

- `provider_type` (String) — Deployment target: `"local"`, `"remote"`, or `"aws"`.

### Optional

- `data_dir` (String) — Override the default data directory (`~/.conga/`).
- `ssh_host` (String) — SSH hostname. Required when `provider_type` is `"remote"`.
- `ssh_user` (String) — SSH user (default: `root`). Remote provider only.
- `ssh_key_path` (String) — Path to SSH private key. Remote provider only.
- `ssh_port` (Number) — SSH port (default: 22). Remote provider only.
- `region` (String) — AWS region. Required when `provider_type` is `"aws"`.
- `profile` (String) — AWS profile name. AWS provider only.
