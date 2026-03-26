# Feature: Egress Domain Allowlisting

**Started**: 2026-03-25
**Status**: ✅ Verified and complete
**Depends on**: `specs/2026-03-25_feature_policy-schema/` (Spec 1 — complete)

## Active Personas
- **Architect** — provider contract, network architecture, proxy design
- **Product Manager** — scope guard, user value per tier
- **QA** — edge cases, failure modes, integration testing

## Active Capabilities
- **Conga MCP** — test policy against live local deployments
- **Docker** — integration testing of egress proxy filtering

## Decisions
- **Per-agent proxy, not shared** — a shared proxy requires a union allowlist, leaking one agent's allowed domains to all others. Per-agent Squid proxies give true egress isolation consistent from local through production. ~15MB memory per proxy is negligible.
- **Squid everywhere** — same enforcement mechanism on all providers (local, remote, AWS). Squid handles HTTP CONNECT tunneling natively, which is required because `HTTPS_PROXY=http://...` causes clients to send HTTP CONNECT requests. (Originally designed with nginx stream + ssl_preread, but that cannot handle HTTP CONNECT — it expects raw TLS.)
- **No TLS termination** — proxy tunnels CONNECT requests to allowed domains, never decrypts traffic. No MITM CA needed.
- **No Provider interface changes** — enforcement is internal to each provider's ProvisionAgent/RefreshAgent. The policy package provides shared logic.
- **Locally-built image** — per-agent proxies use `conga-egress-proxy` (built from `alpine:3.21` + squid on first use) with a generated `squid.conf` mounted in.

## Session Log
- 2026-03-25: plan-feature completed — requirements.md, plan.md created
- 2026-03-25: spec-feature completed — spec.md created, personas reviewed, standards gate passed
- 2026-03-25: implement-feature completed — all 8 tasks done, 11 new tests pass, 0 regressions
- 2026-03-26: verify-feature completed — all tests pass, all personas approve, standards gate pass

## Verification Results
- **Test suite**: 11 egress + 22 policy tests pass, 0 regressions across all packages
- **Linting**: `go vet ./...` clean
- **Architect**: Approved — unified per-agent Squid proxy pattern, consistent across providers
- **Product Manager**: Approved — clear progression (no policy → validate → enforce)
- **QA**: Approved — all public functions tested

## Standards Gate Report (Post-Implementation)
| Principle | Verdict |
|---|---|
| 1. Zero trust the AI agent | ✅ PASSES |
| 2. Immutable configuration | ✅ PASSES |
| 3. Least privilege everywhere | ✅ PASSES |
| 4. Defense in depth | ✅ PASSES |
| 5. Secrets are protected at rest | ✅ PASSES |
| 6. Detect what you can't prevent | ✅ PASSES |
| 7. Policy is portable, enforcement is tiered | ✅ PASSES |
| 8. Own the box, not the behavior | ✅ PASSES |
| Arch 1. Provider contract | ✅ PASSES |
| Arch 2. Shared logic in packages | ✅ PASSES |
| Arch 4. No enforcement without policy | ✅ PASSES |

## Persona Review
- **Architect**: Approved with note — union allowlist for local proxy documented; per-agent filtering is future enhancement.
- **Product Manager**: Approved — clear upgrade path (no policy → validate → enforce), no breaking changes.
- **QA**: Approved — 8 unit tests for generation functions, 13 edge cases documented. iptables integration test deferred (requires SSH host).

## Standards Gate Report (Pre-Implementation)
| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| 1. Zero trust the AI agent | all | should | ✅ PASSES |
| 2. Immutable configuration | config | should | ✅ PASSES |
| 3. Least privilege everywhere | all | should | ✅ PASSES |
| 4. Defense in depth | all | should | ✅ PASSES |
| 5. Secrets are protected at rest | secrets | should | ✅ PASSES |
| 6. Detect what you can't prevent | all | should | ✅ PASSES |
| 7. Policy is portable, enforcement is tiered | policy | should | ✅ PASSES |
| 8. Own the box, not the behavior | arch | should | ✅ PASSES |
| Arch 1. Provider contract | arch | should | ✅ PASSES |
| Arch 2. Shared logic in packages | arch | should | ✅ PASSES |
| Arch 4. No enforcement without policy | arch | should | ✅ PASSES |

## Files Created / Modified
- `specs/2026-03-25_feature_egress-allowlist/requirements.md` — created
- `specs/2026-03-25_feature_egress-allowlist/plan.md` — created
- `specs/2026-03-25_feature_egress-allowlist/spec.md` — created
- `specs/2026-03-25_feature_egress-allowlist/tasks.md` — created
- `cli/internal/policy/egress.go` — created (LoadEgressPolicy, EffectiveAllowedDomains, EgressProxyName, GenerateProxyConf, EgressProxyDockerfile)
- `cli/internal/policy/egress_test.go` — created (11 unit tests)
- `cli/internal/policy/enforcement.go` — modified (remote egress now Enforced, not Partial)
- `cli/internal/policy/policy_test.go` — modified (updated remote enforcement test)
- `cli/internal/provider/localprovider/docker.go` — modified (EgressEnforce/EgressProxyName on opts, proxy env vars)
- `cli/internal/provider/localprovider/provider.go` — modified (policy loading in ProvisionAgent/RefreshAgent, startAgentEgressProxy, stopAgentEgressProxy, RemoveAgent cleanup)
- `cli/internal/provider/remoteprovider/docker.go` — modified (same opts extension)
- `cli/internal/provider/remoteprovider/provider.go` — modified (policy loading, startAgentEgressProxy/stopAgentEgressProxy via SSH)
- `terraform/user-data.sh.tftpl` — modified (per-agent proxy section in setup_agent_common)
- `product-knowledge/standards/security.md` — modified (enforcement escalation table updated)
