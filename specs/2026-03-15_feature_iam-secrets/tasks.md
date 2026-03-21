# Tasks: IAM + Secrets

- [x] Task 1: Add `data.aws_caller_identity` data source
- [x] Task 2: Write `terraform/iam.tf`
- [x] Task 3: Write `terraform/kms.tf`
- [x] Task 4: Write `terraform/secrets.tf`
- [x] Task 5: Write `terraform/populate-secrets.sh`
- [x] Task 6: Update `terraform/outputs.tf`
- [x] Task 7: `terraform plan` — 21 resources
- [x] Task 8: `terraform apply` — 21 added, 0 changed, 0 destroyed
- [x] Task 9: Run `populate-secrets.sh` to set real values
- [x] Task 10: Verify via AWS CLI — all 5 secrets populated
- [x] Task 11: Removed gateway-token secret (not needed with zero-ingress)

## Notes
- AWS provider v6.36.0 — `aws_iam_role_policy_attachment` uses `id` field (v6 behavior)
- KMS key ID: 08d82a78-8b67-4c1f-8135-7204ccfe34a0
- Instance profile: conga-host-2026031605312486820000000b
- Secret values show as `(sensitive value)` in plan — Terraform correctly masks them
