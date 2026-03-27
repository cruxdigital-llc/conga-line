# Requirements: Egress Domain Allowlisting

## Goal

Enforce egress domain restrictions from `conga-policy.yaml` across all three providers, closing the #1 security gap. A compromised agent on port-443-only egress can currently exfiltrate to any HTTPS endpoint. Domain allowlisting restricts it to declared destinations.

## Success Criteria

### Local Provider
1. **Validate mode (default)**: When `conga-policy.yaml` defines egress domains and `mode: validate`, `ProvisionAgent` and `RefreshAgent` print a warning: "Egress rules defined but not enforced. Use mode: enforce to activate the egress proxy." Agent starts normally.
2. **Enforce mode**: When `mode: enforce`, a per-agent Squid proxy container is started to filter by destination domain against the policy's `allowed_domains` and `blocked_domains`. Agent containers are wired through the proxy via `HTTPS_PROXY`/`HTTP_PROXY` env vars. Squid handles HTTP CONNECT for HTTPS tunneling. Blocked requests fail with 403.
3. **No policy file**: Behavior unchanged. No warnings, no proxy wiring. Egress proxy still runs (existing behavior) but in passthrough mode.

### Remote Provider
4. **Enforcement via iptables**: When policy defines egress domains, generate iptables OUTPUT rules on the remote host allowing only resolved IPs for allowed domains on port 443. Default policy: DROP on port 443 for non-allowed IPs.
5. **Rules applied during ProvisionAgent and RefreshAgent.** Persisted via `iptables-save`.
6. **No policy file**: Behavior unchanged. No iptables rules added.

### Enterprise (AWS) Provider
7. **Enforcement via Squid proxy**: Deploy a Squid forward proxy container on the EC2 host. Config generated from policy's `allowed_domains`. Agent containers route through Squid via `HTTPS_PROXY`.
8. **Blocked requests logged**: Squid access log captures blocked domain attempts. CloudWatch shipping existing.
9. **No policy file**: Behavior unchanged. No Squid proxy deployed.

### Cross-Provider
10. **`blocked_domains` takes precedence** over `allowed_domains` on all providers.
11. **Per-agent overrides work**: `MergeForAgent()` from Spec 1 is used to compute effective egress policy per agent before enforcement.
12. **`conga policy validate` enforcement report is accurate**: Local validate mode shows "validate-only", enforce mode shows "enforced". Remote shows "partial". AWS shows "enforced".
13. **Wildcard matching**: `*.slack.com` in `allowed_domains` allows `wss-primary.slack.com`. Uses `MatchDomain()` from Spec 1.
14. **RefreshAgent picks up policy changes**: When the policy file changes and an agent is refreshed, the new egress rules take effect.
