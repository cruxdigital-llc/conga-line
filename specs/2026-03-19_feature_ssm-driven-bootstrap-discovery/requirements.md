# Requirements: SSM-Driven Bootstrap Discovery

## Goal

Refactor the bootstrap script to discover agents from SSM Parameter Store at boot time, making SSM the single source of truth so CLI-added agents survive instance replacement without requiring Terraform.

## Background

Currently there are two provisioning paths that can diverge:
- **Terraform** (`var.agents` in `terraform.tfvars`) bakes agent configs into the bootstrap template at apply time. Changing agents changes the bootstrap hash, forcing instance replacement.
- **CLI** (`cruxclaw admin add-user`/`add-team`) writes SSM parameters and runs setup on the live instance. Works immediately but agents are lost on instance replacement since they aren't in the baked-in bootstrap.

Both paths already write to the same SSM Parameter Store paths (`/openclaw/users/`, `/openclaw/teams/`), making SSM the natural single source of truth.

## Success Criteria

1. An admin can add an agent via CLI and it works immediately on the live instance
2. That CLI-added agent survives instance cycle (`cruxclaw admin cycle-host`) without any Terraform involvement
3. Agents defined in `var.agents` continue to work — Terraform writes SSM params, bootstrap discovers them at boot
4. Adding/removing agents in `var.agents` no longer forces instance replacement (bootstrap content hash becomes static)
5. `routing.json` is built dynamically at boot from discovered agents
6. No regression for existing user/team agent functionality (secrets, config, systemd, networking)

## Constraints

- Single EC2 host (t4g.medium, AL2023) — no orchestration layer
- Instance IAM role already has SSM read access
- `jq` is not currently installed but can be added to the bootstrap
- `var.agents` must continue to drive Terraform-time resources (CloudWatch dashboards, outputs, IAM) — these cannot be dynamic at boot
- CLI-added agents won't appear in Terraform-managed dashboards/outputs (acceptable)
- Per-agent secrets IAM policy must support dynamically-discovered agents (wildcard scoping)
