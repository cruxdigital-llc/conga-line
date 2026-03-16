# Tasks: Terraform Foundation

- [x] Task 1: Create `terraform/` directory
- [x] Task 2: Write `terraform/bootstrap.sh`
- [x] Task 3: Write `terraform/backend.tf`
- [x] Task 4: Write `terraform/providers.tf`
- [x] Task 5: Write `terraform/variables.tf`
- [x] Task 6: Write `terraform/outputs.tf`
- [x] Task 7: Write `terraform/terraform.tfvars.example`
- [x] Task 8: Write `terraform/.gitignore`
- [x] Task 9: Run bootstrap script and validate
- [x] Task 10: Run `terraform init` and `terraform plan` to confirm

## Notes
- Bucket name `openclaw-terraform-state` was taken globally; used `openclaw-terraform-state-123456789012` (account ID suffix)
- AWS provider v5.100.0 installed
- `terraform plan` shows only output values (no resources yet) — correct for empty state
