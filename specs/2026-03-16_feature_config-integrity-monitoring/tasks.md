# Tasks: Config Integrity + Monitoring

- [x] Task 1: Add new variables to `terraform/variables.tf`
- [x] Task 2: Create `terraform/monitoring.tf`
- [x] Task 3: Update `terraform/user-data.sh.tftpl` with config check + CloudWatch agent
- [x] Task 4: Update `terraform/compute.tf` templatefile params
- [x] Task 5: Update `terraform/iam.tf` CloudWatch policy
- [x] Task 6: Update `terraform/outputs.tf`
- [x] Task 7: `terraform plan` + `terraform apply`
- [x] Task 8: Instance replaced with new user-data
- [x] Task 9: Verified via SSM — timer active, agent running, hash check OK
- [x] Task 10: Verified CloudWatch — both log streams active (container + integrity)

## Issues Resolved
1. **`aws_cloudwatch_alarm` → `aws_cloudwatch_metric_alarm`**: Resource renamed in AWS provider v6
2. **CloudWatch agent JSON config invalid**: Journald config format was wrong. Switched to file-based log collection (tail log files) — more reliable and simpler.
3. **Config integrity violation on first 5-min check**: OpenClaw hot-reload modifies config immediately after boot. This is expected behavior — the monitoring correctly detects it. The known-good hash is from the template; OpenClaw adds runtime state (like `meta.lastTouchedAt`).

## TODO
- [x] Move hash baseline computation to AFTER OpenClaw's first boot settles (~60s delay). Currently the baseline is taken from the template before OpenClaw starts, which causes a false positive on first 5-min check. See plan: start container → sleep 60 → compute hash. Supply chain attack (compromised image mutating config) is a separate threat model.
