# Feature: IAM + Secrets — Trace Log

**Started**: 2026-03-15
**Status**: Planning

## Active Personas
- Architect — IAM policy design, secrets structure, KMS

## Active Capabilities
- AWS CLI (profile: `123456789012_AdministratorAccess`)
- Terraform CLI (S3 backend, VPC already deployed)

## Decisions
- **Brave API key**: Skipped — not in use
- **Secret paths**: `conga/shared/*` for shared, `conga/myagent/*` for per-user
- **Placeholder approach**: Terraform creates secrets with `REPLACE_ME`, real values set manually via CLI
- **Single KMS key**: Shared across all EBS volumes (sufficient for shared-host model)
- **SSM**: AWS-managed `AmazonSSMManagedInstanceCore` policy

## Files Created
- [requirements.md](requirements.md)
- [plan.md](plan.md)
- [spec.md](spec.md) — full Terraform code for iam.tf, kms.tf, secrets.tf + populate-secrets.sh

## Persona Review
**Architect**: ✅ Approved. Clean for_each pattern, proper ignore_changes lifecycle, standard KMS key policy.

## Standards Gate Report
| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Secrets never touch disk | secrets | must | ✅ PASSES |
| Least privilege | iam | must | ✅ PASSES |
| Defense in depth | iam | must | ✅ PASSES |
| Encrypted storage | storage | must | ✅ PASSES |
