# Requirements: Portable Egress Policy Compliance

## Goal

Make all three providers (local, remote, AWS) respect the `mode` field in `conga-policy.yaml`'s egress section. Currently only the local provider honors `mode: validate` vs `mode: enforce`. The remote provider ignores `mode` and always enforces, and the AWS provider has no egress proxy implementation in its Go provider code (the bootstrap script always starts the proxy when domains are defined, ignoring `mode`).

The policy schema spec (Feature 12) established that `conga-policy.yaml` is a portable artifact: "Each provider reads the same policy and enforces what it can." The `mode` field is part of that schema. All providers must respect it.

## Problem Statement

1. **Remote provider** (line 238 of `remoteprovider/provider.go`): Hardcodes `egressEnforce = true` when domains are defined, ignoring `egressPolicy.Mode`. The comment says "Remote always enforces when domains defined."
2. **AWS provider**: Has zero egress-related code in `awsprovider/provider.go`. The bootstrap script (`user-data.sh.tftpl`) calls `generate_egress_conf()` which parses domains but never checks the `mode` field — it always generates the Envoy config and starts the proxy if domains exist.
3. **Enforcement report** (`enforcement.go`): Reports AWS and remote as always `Enforced` for egress, regardless of the `mode` field. This is inaccurate when `mode: validate`.
4. **Documentation**: The example file and spec comments say "remote/AWS always enforce" — this was an intentional design choice, not a bug. However, it contradicts the portability principle and prevents operators from running in validate-only mode to test policy changes before enforcing them.

## Success Criteria

1. Default `mode` is `enforce` (security-first). Operators must explicitly set `mode: validate` to disable enforcement.
2. When `mode: validate`, all three providers:
   - Start the egress proxy in logging-only mode (Lua filter logs via `logWarn`, no 403 deny)
   - Do NOT apply iptables DROP rules
   - Report `validate-only` in `EnforcementReport()`
3. When `mode: enforce` (or empty/default), all three providers:
   - Start the per-agent Envoy egress proxy
   - Apply iptables DROP rules in the DOCKER-USER chain
   - Report `enforced` in `EnforcementReport()`
3. The `conga-policy.yaml.example` comment is updated to reflect that `mode` is respected on all providers.
4. Existing tests are updated; new tests cover the remote and AWS mode-awareness.
5. No behavioral change for operators who already have `mode: enforce` set.

## Non-Goals

- Implementing `--internal` Docker networks on AWS (that's Feature 16 Phase 3)

## Affected Files

- `cli/pkg/policy/policy.go` — default mode normalization
- `cli/pkg/provider/remoteprovider/provider.go` — `ProvisionAgent`, `RefreshAgent`, `ensureEgressIptables`
- `terraform/user-data.sh.tftpl` — `generate_egress_conf()`, iptables rules, systemd unit
- `cli/scripts/refresh-user.sh.tmpl` — iptables re-application after refresh
- `cli/pkg/policy/enforcement.go` — `egressReport()`
- `cli/pkg/policy/policy_test.go` — enforcement report tests
- `conga-policy.yaml.example` — default mode and comment update
- `product-knowledge/standards/security.md` — enforcement escalation table update
- `CLAUDE.md` — if any documentation references change
