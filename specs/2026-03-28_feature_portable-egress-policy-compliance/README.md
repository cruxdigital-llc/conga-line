# Trace: Portable Egress Policy Compliance

## Session Log

### 2026-03-28 — Spec Creation

**Trigger**: User discovered that `mode: validate` in `conga-policy.yaml` was not respected by the remote provider (hardcoded enforce) or AWS bootstrap (no mode check). Only the local provider honored the field.

**Files Created**:
- [requirements.md](requirements.md) — Problem statement and success criteria
- [plan.md](plan.md) — 5-phase approach
- [spec.md](spec.md) — Detailed implementation spec

**Key Decisions**:
- Default mode changed from `validate` to `enforce` (security-first, per user feedback)
- iptables DROP rules added to AWS bootstrap — not deferred (per user feedback: "core part of enforcing the policy")
- Align all providers to the local provider's existing mode-check pattern
- Single consistent default (`enforce`) across all providers
- AWS iptables uses same DOCKER-USER chain rules as local/remote (proven pattern from `iptables/rules.go`)
- Systemd `ExecStartPost`/`ExecStopPost` for iptables resilience across container restarts

**Persona Review**:
- **Product Manager**: Approved — clear why, tight scope, migration impact flagged
- **Architect**: Approved — consistent pattern, no new dependencies, iptables uses proven shared package pattern
- **QA**: Approved — edge cases covered, transition scenarios handled via RefreshAgent, container restart resilience via systemd hooks

**Standards Gate**:
- 6/8 checks pass, 2 warnings (local default change, AWS iptables is new enforcement) — both intentional and documented
- Gate: PROCEED

### 2026-03-28 — Implementation Complete

**Modified Files**:
- `cli/internal/policy/policy.go` — Added `normalizeDefaults()` to resolve empty mode to `enforce`, updated mode comment
- `cli/internal/policy/enforcement.go` — Unified `egressReport()` to be mode-driven for all providers
- `cli/internal/policy/policy_test.go` — Added 4 new tests: AWSValidate, RemoteValidate, DefaultModeIsEnforce, DefaultModeAgentOverride
- `cli/internal/provider/remoteprovider/provider.go` — Updated ProvisionAgent, RefreshAgent, ensureEgressIptables to check mode
- `terraform/user-data.sh.tftpl` — Added mode parsing to generate_egress_conf(), iptables DROP rules in bootstrap + systemd unit hooks + agent removal cleanup
- `cli/scripts/refresh-user.sh.tmpl` — Added iptables re-application after refresh
- `conga-policy.yaml.example` — Updated default to enforce, fixed comment
- `product-knowledge/standards/security.md` — Updated enforcement escalation table and egress mode description
- `product-knowledge/standards/architecture.md` — Added Agent Data Safety and Interface Parity standards

**Test Results**: All packages pass (17 packages, 0 failures)

### 2026-03-28 — Verification Complete

**Automated Verification**:
- Full test suite: 17 packages, 0 failures
- `go vet`: clean
- `gofmt`: clean
- Policy tests: 58 results, 0 failures (4 new tests added by this feature)

**Persona Verification**:
- **Product Manager**: APPROVE — delivers on request, no scope creep
- **Architect**: APPROVE — consistent patterns, no new dependencies, simplified egressReport
- **QA**: APPROVE — edge cases covered, retry loop for IP readiness in refresh script

**Standards Gate (Post-Implementation)**:
- 9 checks, 0 violations, 0 warnings — all pass

**Spec Divergences** (all improvements):
1. `normalizeDefaults()` placed in `Load()` with agent override handling — cleaner than spec
2. `e.Mode != "validate"` instead of `== "enforce"` — handles direct struct construction edge case
3. `refresh-all.sh.tmpl` not updated — systemd hooks handle it automatically
4. Validate mode uses Lua log-and-allow filter with full domain list (Phase 3b design), not passthrough-no-filter (Phase 2 design). Phase 3b superseded Phase 2's approach — provides better operational visibility

**Status: VERIFIED**

### 2026-03-29 — Secure-by-Default Egress Extension

**Trigger**: During remote provider E2E testing, user discovered that agents with no policy had no egress proxy — unrestricted outbound access. Requested secure-by-default: proxy always deploys, deny-all when no policy.

**Modified Files**:
- `cli/internal/policy/egress.go` — `GenerateProxyConf` always emits Lua filter (empty allowlist = deny all)
- `cli/internal/provider/localprovider/provider.go` — Removed `egressPolicy != nil` gates in ProvisionAgent and RefreshAgent
- `cli/internal/provider/remoteprovider/provider.go` — Same changes for remote provider
- `cli/internal/provider/awsprovider/provider.go` — ProvisionAgent now generates egress config, passes to scripts
- `cli/scripts/add-user.sh.tmpl` — Deploys egress proxy + iptables during provisioning
- `cli/scripts/add-team.sh.tmpl` — Same
- `cli/internal/mcpserver/tools_policy.go` — toolPolicyDeploy deploys to all agents including empty domains
- `terraform/user-data.sh.tftpl` — Generates deny-all config when no policy file exists
- `cli/internal/policy/egress_test.go` — 4 new tests: deny-all nil, deny-all empty, enforce mode assertion, all-blocked
- `cli/internal/mcpserver/tools_policy_test.go` — Mock captures config/mode, deploy test verifies deny-all
- `cli/scripts/scripts_test.go` — 2 new tests: add-user and add-team template rendering with egress fields
- `product-knowledge/standards/architecture.md` — Principle 4 updated: "Secure by default, open by policy"
- `DEMO.md` — Updated demo flow: agents start locked down, policy opens them up

**Live E2E Verification** (remote provider):
1. Provisioned agent with no policy → proxy deployed, `HTTPS_PROXY` set
2. `fetch('https://api.anthropic.com')` → 403 ("Proxy response (403) !== 200 when HTTP Tunneling")
3. Set egress policy → `policy deploy` → agent picks up allowlist
4. `fetch('https://api.anthropic.com')` → 404 (allowed through, API returns 404 without auth)
5. `fetch('https://google.com')` → 403 (still denied)

**Test Results**: 17 packages, 0 failures. go vet clean. gofmt clean.

**Persona Verification**:
- **Product Manager**: APPROVE — stronger security narrative, no scope creep
- **Architect**: APPROVE — consistent pattern, elegant reuse of empty Lua tables for deny-all
- **QA**: APPROVE — edge cases covered, live E2E verified

**Standards Gate**: 8 passes, 1 warning (architecture principle 4 updated to reflect new behavior)

**Status: VERIFIED**
