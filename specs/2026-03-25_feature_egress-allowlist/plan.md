# Plan: Egress Domain Allowlisting

## Approach

Add egress enforcement to each provider by reading the `egress` section from `conga-policy.yaml` (via Spec 1's `policy.Load()`) and applying it with the best mechanism available per provider. The existing egress proxy container (`deploy/egress-proxy/`) is upgraded from passthrough to domain-filtering. No changes to the Provider interface — enforcement is internal to each provider's `ProvisionAgent` and `RefreshAgent` flows.

## Shared: Policy Loading in Providers

Add a shared helper in `cli/internal/policy/` (or `common/`) that each provider calls:

```
func LoadEgressPolicy(configDir string, agentName string) (*EgressPolicy, error)
```

- Reads `conga-policy.yaml` from the provider-appropriate path
- Calls `MergeForAgent(agentName)` to apply per-agent overrides
- Returns the effective `EgressPolicy`, or nil if no policy file / no egress section

Each provider calls this early in `ProvisionAgent` and `RefreshAgent`.

## Local Provider: Validate + Enforce Modes

### Validate mode (mode: "validate" or unset)
- In `ProvisionAgent()`, after loading policy, if egress domains are defined: print warning to stderr
- No container or network changes
- Agent starts normally

### Enforce mode (mode: "enforce")
- **Upgrade egress proxy**: Generate an nginx allowlist file from `allowed_domains` / `blocked_domains`. Mount into the proxy container. Nginx rejects non-whitelisted SNI hostnames by returning 502.
- **Wire agent containers**: Add `--env HTTPS_PROXY=http://conga-egress-proxy:3128` and `--dns <proxy-IP>` to `runAgentContainer()` args. Container already shares a network with the proxy (existing `connectNetwork` call).
- **Allowlist generation**: New function in `cli/internal/policy/egress.go` generates the nginx `map` block from the domain list. Wildcards converted to nginx regex patterns.
- **Proxy rebuild on policy change**: `RefreshAgent` regenerates the allowlist and restarts the proxy if domains changed.

### Key files
- `deploy/egress-proxy/nginx.conf` — add `map` block for SNI filtering with include of allowlist file
- `deploy/egress-proxy/Dockerfile` — no change needed (nginx already supports includes)
- `cli/internal/provider/localprovider/provider.go` — `ProvisionAgent()` and `RefreshAgent()` load policy
- `cli/internal/provider/localprovider/docker.go` — `runAgentContainer()` conditionally adds proxy env vars
- New: `cli/internal/policy/egress.go` — generates nginx allowlist, iptables rules from policy

## Remote Provider: iptables Rules

- After loading policy, resolve `allowed_domains` to IPs on the remote host via SSH (`dig +short <domain>`)
- Generate iptables OUTPUT rules: ACCEPT for each resolved IP on port 443, then DROP default for port 443
- Also ACCEPT DNS (port 53 to VPC DNS) and established connections
- Apply via SSH command execution during `ProvisionAgent` and `RefreshAgent`
- Persist via `iptables-save > /etc/iptables/rules.v4`
- Document limitation: IP-based, not SNI-based. DNS changes can bypass. Best-effort enforcement.

### Key files
- `cli/internal/provider/remoteprovider/provider.go` — `ProvisionAgent()` and `RefreshAgent()` apply rules
- New: `cli/internal/policy/egress.go` — shared iptables rule generation logic

## Enterprise (AWS) Provider: Squid Proxy

- Deploy Squid container alongside agent containers in the bootstrap script
- Generate Squid config from policy's `allowed_domains` (one `acl` per domain, `http_access allow`)
- Agent containers set `HTTPS_PROXY=http://conga-squid-proxy:3128`
- Squid runs in CONNECT-tunnel mode (no TLS termination)
- Policy file read from SSM parameter or local config on the EC2 host
- Squid access log shows blocked attempts — already shipped to CloudWatch via existing agent

### Key files
- New: `deploy/squid-proxy/Dockerfile` — Squid on Alpine
- New: `deploy/squid-proxy/squid.conf.tmpl` — template with domain ACLs
- `terraform/user-data.sh.tftpl` — add Squid proxy section to bootstrap
- `cli/internal/provider/awsprovider/provider.go` — pass policy to SSM scripts

## What This Does NOT Do

- No changes to the Provider interface
- No changes to `conga-policy.yaml` schema (uses Spec 1 as-is)
- No DNS-level filtering (considered and rejected — iptables/proxy is more reliable)
- No per-agent proxy instances (one proxy per host, serving all agents)
- No TLS termination (proxy sees SNI hostname only, not request content)

## Test Plan

1. **Local validate mode**: Unit test — policy with domains + validate mode → warning printed, no proxy env vars in container args
2. **Local enforce mode**: Integration test with Docker — start agent, verify allowed domain reachable (`curl api.anthropic.com`), verify blocked domain fails (`curl evil.com`)
3. **Allowlist generation**: Unit test — policy domains → correct nginx map block
4. **Remote iptables**: Unit test — policy domains → correct iptables commands
5. **Per-agent override**: Unit test — global policy + agent override → correct merged egress rules
6. **No policy file**: Verify existing behavior unchanged on all providers
7. **RefreshAgent**: Modify policy file, refresh agent, verify new rules take effect
