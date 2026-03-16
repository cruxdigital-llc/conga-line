# Tasks: VPC + Networking

- [x] Task 1: Write `terraform/vpc.tf`
- [x] Task 2: Write `terraform/nat.tf`
- [x] Task 3: Write `terraform/security.tf`
- [x] Task 4: Write `terraform/flow-logs.tf`
- [x] Task 5: Update `terraform/outputs.tf` with new outputs
- [x] Task 6: `terraform init` (download fck-nat module)
- [x] Task 7: `terraform plan` — 31 resources to create
- [x] Task 8: `terraform apply` — 31 added, 0 changed, 0 destroyed
- [x] Task 9: Verify resources via AWS CLI

## Notes
- fck-nat module v1.4.0 requires AWS provider >= 6.0; upgraded from ~> 5.0 to ~> 6.0 (v6.36.0 installed)
- fck-nat uses ASG for self-healing (auto-restarts if instance dies)
- fck-nat AMI: ami-0a342b1b279f4ed1a (al2023, arm64)
- AZ selected: us-east-2a (first available)
