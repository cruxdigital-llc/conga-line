# Feature: SSM Port Forwarding for Web UI — Trace Log

**Started**: 2026-03-17
**Status**: ✅ Verified and closed

## Active Personas
- Architect — port binding, security surface, SSM document choice
- QA — edge cases, validation logic
- Product Manager — scope, user value, Phase 1 boundaries

## Decisions
- **Localhost-only binding** (`127.0.0.1`) — no external exposure, security group unchanged
- **Built-in SSM document** (`AWS-StartPortForwardingSession`) — no custom documents for Phase 1
- **Port range 18789-18889** — room for 100 users, avoids well-known ports
- **No gateway auth token in Phase 1** — SSM tunnel itself provides IAM-based authentication
- **Phase 2 deferred**: per-user SSM documents, IAM restrictions, gateway auth tokens

## Files Created
- [requirements.md](requirements.md)
- [spec.md](spec.md) — variables.tf + user-data.sh.tftpl + outputs.tf changes

## Files to Modify (Implementation)
- `terraform/variables.tf` — add `gateway_port` field + validation
- `terraform/user-data.sh.tftpl` — add `-p` flag to docker run (line 273), update echo (line 458)
- `terraform/outputs.tf` — add `ssm_port_forward_commands` output

## Persona Review

**Product Manager**: Scope is clear and tight. Phase 1/Phase 2 boundary well-defined. Success criteria are testable (open browser, see UI). No scope creep.

**Architect**: No new dependencies or infrastructure. Localhost binding is correct — no external exposure. Built-in SSM document avoids custom resource management. Validation prevents port collisions. No changes to security groups, IAM, or network topology.

**QA**: Edge cases covered — port collisions (Terraform validation), container down (connection refused), instance replacement (re-run output). Uniqueness validation logic (`length == length(distinct(...))`) is correct. Transient connection refused during container startup is acceptable and self-resolving.

## Implementation Log
- `terraform/variables.tf` — added `gateway_port` field, defaults (18789, 18790), range + uniqueness validation
- `terraform/user-data.sh.tftpl` — added `-p 127.0.0.1:${user_config.gateway_port}:18789` to docker run, updated status echo
- `terraform/outputs.tf` — added `ssm_port_forward_commands` output with per-user SSM commands
- `terraform validate` — passed
- `terraform plan` — 0 add, 1 change (bootstrap script S3 object), 0 destroy

## Active Capabilities
- Terraform CLI (validate, plan)

## Verification Log
- `terraform validate` — ✅ passed
- `terraform plan` — ✅ 0 add, 1 change, 0 destroy (expected)
- Diff review — 3 Terraform files, 43 insertions, 4 deletions, no extraneous changes
- Spec alignment — implementation matches spec exactly, no divergences

## Post-Implementation Standards Gate Report
| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Zero ingress | network | must | ✅ PASSES — no inbound rules added; SSM uses outbound HTTPS |
| SSM-only access | network | must | ✅ PASSES — port forwarding via SSM, not direct access |
| Least privilege | iam | must | ✅ PASSES — no IAM changes; instance role already has SSM |
| Defense in depth | architecture | must | ⚠️ WARNING — no gateway auth token in Phase 1; SSM provides IAM auth but no app-layer auth. Acceptable for Phase 1, must be addressed in Phase 2. |
| Isolated Docker networks | container | must | ✅ PASSES — `-p` publishes to host localhost only, does not bridge container networks |
| Secrets never touch disk | secrets | must | ✅ PASSES — no secrets involved in this change |
| Zero trust the AI agent | architecture | should | ⚠️ WARNING — container's web UI is accessible via tunnel without app-layer token. Phase 2 adds gateway auth token as defense in depth. |
