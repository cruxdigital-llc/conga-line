# Tasks: Multi-User Onboarding

- [x] Task 1: State migration — rm Aaron's secrets, mv shared secrets
- [x] Task 2: Update `terraform/variables.tf` — add users variable with member ID
- [x] Task 3: Rewrite `terraform/secrets.tf` — shared only
- [x] Task 4: Update `terraform/iam.tf` — dynamic user paths + ListSecrets
- [x] Task 5: Rewrite `terraform/user-data.sh.tftpl` — multi-user loop
- [x] Task 6: Update `terraform/compute.tf` — pass users map
- [x] Task 7: Create `scripts/onboard-user.sh`
- [x] Task 8: Update `terraform/populate-secrets.sh` — shared only
- [x] Task 9: Update `terraform/terraform.tfvars.example`
- [x] Task 10: Migrate secrets from `openclaw/aaron/*` to `openclaw/UA13HEGTS/*`
- [x] Task 11: `terraform plan` + `terraform apply` — no secret destruction
- [x] Task 12: Instance replaced, container healthy, Slack connected
- [ ] Task 13: Test onboarding flow with second user

## Issues Resolved
1. **Systemd `$$` escaping**: Terraform templatefile + bash heredoc double-interpolation mangled `$$` into `1711`. Fixed by building `-e` flags at bootstrap time and baking into the unit file directly.
2. **Member ID migration**: Renamed all paths from `aaron` to `UA13HEGTS`. Secrets copied to new path, old ones deleted.
