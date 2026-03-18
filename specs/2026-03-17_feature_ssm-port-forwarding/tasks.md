# Tasks: SSM Port Forwarding for Web UI

## Implementation Steps

- [x] **Task 1**: Update `terraform/variables.tf` — add `gateway_port` to `users` type + defaults + validation
- [x] **Task 2**: Update `terraform/user-data.sh.tftpl` — add `-p` flag to docker run (line 273), update echo (line 458)
- [x] **Task 3**: Update `terraform/outputs.tf` — add `ssm_port_forward_commands` output
- [x] **Task 4**: `terraform validate` — Success
- [x] **Task 5**: `terraform plan` — 0 to add, 1 to change (bootstrap script), 0 to destroy. Output looks correct.
