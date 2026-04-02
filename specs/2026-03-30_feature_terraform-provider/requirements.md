# Requirements: Terraform Provider

## Problem Statement

The CongaLine CLI provides transactional, idempotent operations for managing agent environments. `conga bootstrap` accelerates initial standup from a YAML manifest. However, neither provides state management, drift detection, dependency resolution, or declarative destroy. Teams managing CongaLine environments as code need these capabilities — and Terraform already provides all of them.

## Goal

Design a Terraform provider (`terraform-provider-conga`) that maps Terraform resources to existing `Provider` interface methods. The provider is a thin wrapper — no business logic is duplicated. Terraform handles state, planning, drift, and destroy.

**Scope**: Future roadmap item. This spec captures the architecture and resource model while the design is fresh. Implementation is deferred.

## User Stories

1. **As a DevOps engineer**, I want to define my CongaLine environment in HCL alongside my infrastructure code, so I can manage agents, policies, and channels with the same `terraform plan/apply` workflow I use for everything else.

2. **As a team lead**, I want to run `terraform plan` and see exactly what will change before applying, so I can review agent topology changes in PRs.

3. **As an operator**, I want `terraform destroy` to cleanly tear down an environment in reverse dependency order, so I don't have to manually remove agents, channels, and config.

4. **As a security engineer**, I want drift detection to alert me when the live environment differs from the declared state, so I know if someone made ad-hoc changes.

## Success Criteria

1. **Resource parity**: Every operation available via `conga bootstrap` is expressible as a Terraform resource.
2. **Zero business logic duplication**: The provider calls the same Go `Provider` interface methods as the CLI.
3. **Plan accuracy**: `terraform plan` correctly predicts creates, updates, and deletes.
4. **Import support**: Existing environments (provisioned via CLI/bootstrap) can be imported into Terraform state.
5. **Provider parity**: The `provider_type` config selects local, remote, or AWS — same as `--provider` flag.

## Non-Goals

- Replacing `conga bootstrap` — both paths coexist for different audiences
- Managing AWS infrastructure (VPC, EC2, IAM) — that's the existing `terraform/` directory
- Runtime operations (connect, logs, status) — those remain CLI/MCP only
- Model routing enforcement (Bifrost) — deferred to routing implementation

## Constraints

- Must use `terraform-plugin-framework` (not the deprecated `terraform-plugin-sdk`)
- Separate Go module (`terraform-provider-conga/`) that imports `cli/pkg/provider`
- Published to Terraform Registry under `cruxdigital-llc/conga`
- Secrets must use `sensitive = true` in the Terraform schema
