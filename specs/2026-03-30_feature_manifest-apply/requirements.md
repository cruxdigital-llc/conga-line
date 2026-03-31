# Requirements: Manifest Bootstrap

## Problem Statement

The demo flow (DEMO.md) requires 8+ sequential CLI commands to provision a complete Conga Line environment: setup, provision agents, set secrets, configure channels, bind channels, set policy, deploy, and refresh. This takes too long for live demos and is error-prone for repeated deployments.

## Goal

Add a `conga bootstrap <manifest.yaml>` command that reads a declarative YAML manifest describing the desired environment state and executes all provisioning steps in one shot.

**Scope**: Demo-focused MVP designed to support future declarative management. The YAML format should be extensible for production use (drift detection, diff preview, teardown) but the initial implementation focuses on create-and-configure.

## User Stories

1. **As a demo presenter**, I want to run a single command that provisions a complete environment from a YAML file, so I can set up a demo in under 60 seconds (excluding Docker image pull).

2. **As a DevOps engineer**, I want to re-run `conga bootstrap` after fixing a configuration error without duplicating already-provisioned resources.

3. **As a team lead**, I want to define our agent topology in a version-controlled YAML file that anyone on the team can apply.

## Success Criteria

1. **Speed**: `conga bootstrap demo.yaml` provisions 2 agents with secrets, channels, bindings, and egress policy in under 60 seconds (on a warm host with Docker image cached).
2. **Idempotency**: Running `conga bootstrap` twice produces the same result — completed steps are skipped, no errors on re-apply.
3. **Secret safety**: Secrets are referenced via environment variables (`$VAR`), never stored in the YAML manifest.
4. **Provider parity**: Works identically on all three providers (local, remote, AWS).
5. **Composability**: The manifest reuses existing schemas (policy, agent config) so users can learn one format.
6. **Observability**: Each step shows progress output with clear skip/done/error status.

## Non-Goals (Deferred)

- `--dry-run` / diff preview
- Drift detection (comparing manifest to live state)
- Declarative teardown (removing agents not in the manifest)
- Partial apply (applying only specific sections)
- Multi-file manifests or includes

## Constraints

- Must use the existing `Provider` interface — no new provider methods
- Must use the existing `gopkg.in/yaml.v3` dependency — no new YAML libraries
- YAML format must follow `apiVersion: conga.dev/v1alpha1` convention (matching policy schema)
- Secrets must never appear in YAML — only `$ENV_VAR` references allowed for secret values
- Single `RefreshAll` at the end — not per-step refreshes
