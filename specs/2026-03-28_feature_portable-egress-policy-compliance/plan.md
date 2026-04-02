# Plan: Portable Egress Policy Compliance

## Approach

Fix all three providers to consistently respect the `mode` field in `conga-policy.yaml` egress section, change the default to `enforce` (security-first), and add iptables DROP rules to the AWS bootstrap.

The local provider already implements correct mode-checking. The remote provider and AWS bootstrap need to be aligned, and AWS needs iptables rules to match the enforcement level of local/remote.

## Phases

### Phase 1: Default Mode Change
- Normalize empty/missing `mode` to `enforce` after loading the policy
- Update example file and docs to reflect new default

### Phase 2: Remote Provider — Respect `mode` field
- In `ProvisionAgent`: check `egressPolicy.Mode == "enforce"` instead of unconditionally setting `egressEnforce = true`
- In `RefreshAgent`: same check
- In `ensureEgressIptables`: add `Mode` check (matching local provider's pattern)
- Add warning message when `mode != "enforce"` (matching local provider's pattern)

### Phase 3: AWS Bootstrap — Respect `mode` field + iptables enforcement
- In `generate_egress_conf()`: parse the `mode` field from the YAML
- Skip Envoy config generation when `mode` is not `enforce`
- Add iptables DROP rules to bootstrap (same DOCKER-USER chain rules as local/remote)
- Add iptables to systemd unit (`ExecStartPost` / `ExecStopPost`) for container restart resilience
- Add iptables to `refresh-user.sh.tmpl` for refresh resilience
- Log a warning when domains exist but mode is `validate`

### Phase 4: Enforcement Report — Reflect actual behavior
- Update `egressReport()` to check `e.Mode` uniformly for all providers
- All providers report `enforced` when `mode == "enforce"`, `validate-only` otherwise

### Phase 5: Tests & Documentation
- Update enforcement report tests for all provider/mode combinations
- Add test for default mode normalization
- Update `conga-policy.yaml.example`, security standards, CLAUDE.md

## Test Plan
- Unit: enforcement report returns correct level for all provider/mode combinations
- Unit: default mode normalization resolves empty to `enforce`
- Integration: remote provider with `mode: validate` starts egress proxy in logging-only mode (no iptables)
- Manual: verify `conga policy validate` output reflects mode on each provider
- Manual: verify iptables rules appear on AWS after bootstrap/refresh

## Risk Assessment
- **Low risk for remote/AWS**: These providers were already enforcing — the change just makes them respect `mode: validate` when explicitly requested.
- **Behavioral change for local**: Local previously defaulted to `validate`. With the new default of `enforce`, local deployments with no explicit mode will start enforcing. This is intentional — security-first.
- **AWS iptables**: New enforcement mechanism on AWS. Uses the same proven rule pattern from `cli/pkg/provider/iptables/rules.go`. Idempotent insertion, idempotent removal.
