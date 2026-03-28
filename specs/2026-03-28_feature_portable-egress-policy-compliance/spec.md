# Spec: Portable Egress Policy Compliance

## Overview

Fix all three providers to consistently respect the `mode` field in `conga-policy.yaml`'s egress section, and add iptables enforcement to the AWS bootstrap. Currently only the local provider honors `mode: validate` vs `mode: enforce`. The remote provider ignores `mode` and always enforces, the AWS provider has no mode check, and the AWS bootstrap lacks iptables DROP rules entirely (relying solely on the Envoy proxy).

This spec aligns all three providers. The egress proxy is **always deployed** when domains are defined, regardless of mode. The `mode` field controls enforcement intensity:

- **`validate`**: Proxy deployed with **Lua domain-match logging** (`logWarn`, not 403). `HTTPS_PROXY` set on agent. No iptables DROP rules. Traffic is evaluated against the allowlist and violations are logged, but all requests are allowed through.
- **`enforce`** (default): Proxy deployed with **domain filtering** (Lua allowlist, 403 deny). `HTTPS_PROXY` set on agent. iptables DROP rules applied (hard enforcement — bypassing the proxy is impossible).

**Default mode is `enforce`** — security-first. Operators who want warn-only must explicitly set `mode: validate`.

**Source**: Policy schema spec (Feature 12), security standards principle 7 ("Policy is portable, enforcement is tiered").

---

## Phase 1: Default Mode Change

### 1.1 Update policy schema default

The `mode` field currently defaults to `validate` when empty/absent. Change to default to `enforce`.

**File**: `cli/internal/policy/policy.go`

In `Validate()` or after loading, normalize empty mode to `enforce`:

```go
// In Load() or MergeForAgent(), after loading:
if pf.Egress != nil && pf.Egress.Mode == "" {
    pf.Egress.Mode = "enforce"
}
```

This must also apply to agent overrides — if an agent override has an egress section with no mode, it inherits the global mode (already handled by `MergeForAgent()` shallow merge, since the entire egress section is replaced).

### 1.2 Update validation

**File**: `cli/internal/policy/policy.go`

The `validateEgress()` function accepts `""` as valid. Keep this — the normalization in 1.1 means empty mode is resolved to `enforce` before reaching provider code.

---

## Phase 2: All Providers — Split proxy deployment from iptables enforcement

The key design change: **proxy deployment** and **iptables enforcement** are separate concerns.

- `egressProxy`: true when domains are defined (ANY mode) — deploy proxy, set HTTPS_PROXY
- `egressEnforce`: true only when `mode == "enforce"` — apply iptables DROP rules, use domain filtering in proxy

When `egressProxy` is true but `egressEnforce` is false (validate mode), the proxy is started with the full domain list and `mode=validate`. `GenerateProxyConf()` / `generate_egress_conf()` generates a Lua filter that evaluates domains and logs warnings via `logWarn` but allows all traffic through. (This supersedes the original Phase 2 passthrough design — Phase 3b's log-and-allow approach provides better operational visibility.)

### 2.1 Local Provider — `ProvisionAgent` and `RefreshAgent`

**File**: `cli/internal/provider/localprovider/provider.go`

Replace:
```go
egressEnforce := false
if egressPolicy != nil && len(egressPolicy.AllowedDomains) > 0 {
    if egressPolicy.Mode != "enforce" {
        fmt.Fprintf(os.Stderr, "Warning: Egress rules defined but not enforced in validate mode. Set mode: enforce in conga-policy.yaml to activate the egress proxy.\n")
    } else {
        egressEnforce = true
    }
}
```

With:
```go
egressProxy := false
egressEnforce := false
if egressPolicy != nil && len(egressPolicy.AllowedDomains) > 0 {
    egressProxy = true
    if egressPolicy.Mode == "enforce" {
        egressEnforce = true
    } else {
        fmt.Fprintf(os.Stderr, "Egress proxy active in validate mode (logging violations, allowing all traffic). Set mode: enforce to activate domain filtering + iptables.\n")
    }
}
```

Then update all proxy-related code to use `egressProxy` instead of `egressEnforce`:
- Start proxy: `if egressProxy` (always when domains defined)
- Pass domains to proxy: always pass `EffectiveAllowedDomains()` with `mode` (Lua filter logs in validate, denies in enforce)
- Set HTTPS_PROXY on container: `if egressProxy`
- Write proxy bootstrap JS: `if egressProxy`
- Apply iptables: `if egressEnforce` (unchanged)

### 2.2 Remote Provider — `ProvisionAgent` and `RefreshAgent`

**File**: `cli/internal/provider/remoteprovider/provider.go`

Same split as local provider. Both `ProvisionAgent` (~line 236) and `RefreshAgent` (~line 599) get the same pattern.

### 2.3 Remote Provider — `ensureEgressIptables`

**File**: `cli/internal/provider/remoteprovider/provider.go`

No change from current implementation — already checks `egressPolicy.Mode != "enforce"`.

### 2.4 Local Provider — `ensureEgressIptables`

No change — already checks `egressPolicy.Mode != "enforce"`.

---

## Phase 3: AWS Bootstrap — Respect `mode` field + iptables enforcement

### 3.1 Parse `mode` in `generate_egress_conf()`

**File**: `terraform/user-data.sh.tftpl`

Add mode detection to the YAML parser. After the existing domain parsing variables (line ~450), add:

```bash
local GLOBAL_MODE=""
local AGENT_MODE=""
```

In the global egress parsing section, add mode detection:
```bash
# Detect global egress mode
if $IN_GLOBAL_EGRESS && echo "$line" | grep -qE "^  mode:"; then
    GLOBAL_MODE=$(echo "$line" | sed 's/^  mode: *//' | tr -d '"' | tr -d "'" | xargs)
fi
```

In the agent egress parsing section, add mode detection:
```bash
# Detect agent egress mode
if $IN_AGENT_EGRESS && echo "$line" | grep -qE "^      mode:"; then
    AGENT_MODE=$(echo "$line" | sed 's/^      mode: *//' | tr -d '"' | tr -d "'" | xargs)
fi
```

After the agent override merge section (line ~563), add mode resolution:
```bash
# Resolve effective mode (agent override takes precedence, default is "enforce")
local EFFECTIVE_MODE=""
if [ -n "$AGENT_ALLOWED" ]; then
    EFFECTIVE_MODE="${AGENT_MODE:-$GLOBAL_MODE}"
else
    EFFECTIVE_MODE="$GLOBAL_MODE"
fi
EFFECTIVE_MODE="${EFFECTIVE_MODE:-enforce}"
```

### 3.2 Generate log-and-allow config in validate mode

> **Note:** This section was superseded by Phase 3b during implementation. Validate mode uses a Lua filter with `logWarn` (not passthrough/no-filter).

At the end of `generate_egress_conf()`, the mode determines the Lua deny action:
- `validate`: `h:logWarn("egress-validate: would deny " .. host)` (log and allow)
- `enforce`: `h:respond({[":status"] = "403"}, "egress denied: " .. host)` (deny)

The proxy is **always started** when domains are defined. In both modes, the Lua filter evaluates every request against the allowlist. The difference is the action taken for non-allowlisted requests.

### 3.3 Proxy startup is no longer gated on mode

The existing proxy startup code (lines 711-743) is gated on `[ -f "$EGRESS_CONF" ]`. Since `generate_egress_conf()` now always generates a config when domains exist (log-and-allow in validate, filtering in enforce), the proxy always starts. No changes needed to the proxy startup section.

### 3.4 Add iptables DROP rules to bootstrap (enforce mode only)

**File**: `terraform/user-data.sh.tftpl`

After the proxy startup section (line ~743) and before the systemd unit generation, add iptables enforcement **only when mode is enforce**. The proxy is always running, but iptables are the hard enforcement that prevents bypassing it:

```bash
  # Apply iptables egress DROP rules (when proxy is active)
  if [ -f "$EGRESS_CONF" ]; then
    # Get agent container IP and network subnet for iptables rules.
    # Container must be running — start it temporarily if needed to get the IP.
    # The systemd unit will manage the actual lifecycle.
    AGENT_NET="conga-$AGENT_NAME"
    AGENT_IP=$(docker inspect -f "{{(index .NetworkSettings.Networks \"$AGENT_NET\").IPAddress}}" "conga-$AGENT_NAME" 2>/dev/null || echo "")
    NET_CIDR=$(docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' "$AGENT_NET" 2>/dev/null || echo "")
    if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ]; then
      # Idempotent insertion: check-or-insert pattern
      # Rule order (iptables -I pushes to top, so insert in reverse):
      #   1. ESTABLISHED,RELATED → RETURN (allow response traffic)
      #   2. dst=subnet → RETURN (allow proxy + Docker DNS)
      #   3. DROP (block everything else from this source)
      iptables -C DOCKER-USER -s "$AGENT_IP" -j DROP 2>/dev/null || \
        iptables -I DOCKER-USER -s "$AGENT_IP" -j DROP
      iptables -C DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN 2>/dev/null || \
        iptables -I DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN
      iptables -C DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || \
        iptables -I DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
      log "Egress iptables: DROP rules applied for conga-$AGENT_NAME ($AGENT_IP)"
    else
      log "WARNING: Could not determine container IP or subnet for iptables egress rules"
    fi
  fi
```

This mirrors the exact same iptables rule structure used by the shared `iptables.AddRulesCmd()` in `cli/internal/provider/iptables/rules.go` (lines 21-35).

### 3.5 Add iptables cleanup to systemd unit

In the systemd unit template (line ~752), add `ExecStopPost` to clean up iptables rules when the container stops, and `ExecStartPost` to re-apply them when it starts:

After the existing `ExecStartPost` line (router reconnect), add:
```bash
ExecStartPost=-/bin/bash -c 'AGENT_IP=$(docker inspect -f "{{(index .NetworkSettings.Networks \"conga-$AGENT_NAME\").IPAddress}}" conga-$AGENT_NAME 2>/dev/null); NET_CIDR=$(docker network inspect -f "{{range .IPAM.Config}}{{.Subnet}}{{end}}" conga-$AGENT_NAME 2>/dev/null); if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ] && [ -f "/opt/conga/config/egress-$AGENT_NAME.yaml" ]; then iptables -C DOCKER-USER -s $AGENT_IP -j DROP 2>/dev/null || iptables -I DOCKER-USER -s $AGENT_IP -j DROP; iptables -C DOCKER-USER -s $AGENT_IP -d $NET_CIDR -j RETURN 2>/dev/null || iptables -I DOCKER-USER -s $AGENT_IP -d $NET_CIDR -j RETURN; iptables -C DOCKER-USER -s $AGENT_IP -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || iptables -I DOCKER-USER -s $AGENT_IP -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN; fi'
```

Add before `ExecStop`:
```bash
ExecStopPost=-/bin/bash -c 'AGENT_IP=$(docker inspect -f "{{(index .NetworkSettings.Networks \"conga-$AGENT_NAME\").IPAddress}}" conga-$AGENT_NAME 2>/dev/null); NET_CIDR=$(docker network inspect -f "{{range .IPAM.Config}}{{.Subnet}}{{end}}" conga-$AGENT_NAME 2>/dev/null); if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ]; then iptables -D DOCKER-USER -s $AGENT_IP -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || true; iptables -D DOCKER-USER -s $AGENT_IP -d $NET_CIDR -j RETURN 2>/dev/null || true; iptables -D DOCKER-USER -s $AGENT_IP -j DROP 2>/dev/null || true; fi'
```

### 3.6 Add iptables to refresh script

**File**: `cli/scripts/refresh-user.sh.tmpl`

After the `systemctl restart` and router reconnect, add iptables re-application:

```bash
# Re-apply egress iptables rules (if egress proxy config exists)
EGRESS_CONF="/opt/conga/config/egress-$AGENT_NAME.yaml"
if [ -f "$EGRESS_CONF" ]; then
  # Wait for container to be running
  for i in $(seq 1 10); do
    AGENT_IP=$(docker inspect -f '{{(index .NetworkSettings.Networks "conga-'$AGENT_NAME'").IPAddress}}' conga-$AGENT_NAME 2>/dev/null || echo "")
    [ -n "$AGENT_IP" ] && break
    sleep 1
  done
  NET_CIDR=$(docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' conga-$AGENT_NAME 2>/dev/null || echo "")
  if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ]; then
    iptables -C DOCKER-USER -s "$AGENT_IP" -j DROP 2>/dev/null || \
      iptables -I DOCKER-USER -s "$AGENT_IP" -j DROP
    iptables -C DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN 2>/dev/null || \
      iptables -I DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN
    iptables -C DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || \
      iptables -I DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
    echo "Egress iptables: DROP rules applied for conga-$AGENT_NAME ($AGENT_IP)"
  else
    echo "WARNING: Could not apply egress iptables rules — container IP or subnet not found"
  fi
fi
```

### 3.7 Add iptables cleanup on agent removal

**File**: `cli/scripts/refresh-all.sh.tmpl` (if it exists) and in the bootstrap cleanup section

When stopping/removing an agent, clean up its iptables rules before stopping the container:

```bash
# Clean up egress iptables rules before stopping
AGENT_IP=$(docker inspect -f '{{(index .NetworkSettings.Networks "conga-'$UNIT_NAME'").IPAddress}}' "$UNIT_NAME" 2>/dev/null || echo "")
NET_CIDR=$(docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' "conga-${UNIT_NAME#conga-}" 2>/dev/null || echo "")
if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ]; then
  iptables -D DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || true
  iptables -D DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN 2>/dev/null || true
  iptables -D DOCKER-USER -s "$AGENT_IP" -j DROP 2>/dev/null || true
fi
```

---

## Phase 3b: Validate-Mode Lua Filter (Log-but-Allow)

### 3b.1 Add `mode` parameter to `GenerateProxyConf()`

**File**: `cli/internal/policy/egress.go`

Change signature from:
```go
func GenerateProxyConf(domains []string) string
```

To:
```go
func GenerateProxyConf(domains []string, mode string) string
```

Pass `mode` through to the template data:
```go
type envoyConfigData struct {
    HasDomains   bool
    ValidateMode bool     // true = log-but-allow, false = deny
    ExactDomains []string
    Suffixes     []string
}
```

When `mode == "validate"` and domains are provided, set `HasDomains = true` AND `ValidateMode = true`. The template will generate a Lua filter that matches domains but logs warnings instead of returning 403.

### 3b.2 Update Envoy config template

**File**: `cli/internal/policy/templates/envoy-config.yaml.tmpl`

Replace the Lua `envoy_on_request` function body:

```
{{- if .HasDomains}}
          - name: envoy.filters.http.lua
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
              default_source_code:
                inline_string: |
                  local EXACT = {
{{range .ExactDomains}}                    ["{{.}}"] = true,
{{end}}                  }
                  local SUFFIXES = {
{{range .Suffixes}}                    ".{{.}}",
{{end}}                  }
                  function envoy_on_request(h)
                    local a = h:headers():get(":authority") or ""
                    local m = a:match("^([^:]+)")
                    if not m then
{{- if .ValidateMode}}
                      return
{{- else}}
                      h:respond({[":status"] = "403"}, "egress denied: missing host\n"); return
{{- end}}
                    end
                    local host = m:lower()
                    if EXACT[host] then return end
                    for _, s in ipairs(SUFFIXES) do
                      if host == s:sub(2) or host:sub(-#s) == s then return end
                    end
{{- if .ValidateMode}}
                    h:logWarn("egress-validate: would deny " .. host)
{{- else}}
                    h:respond({[":status"] = "403"}, "egress denied: " .. host .. "\n")
{{- end}}
                  end
{{- end}}
```

In validate mode, the filter:
- Still evaluates every request against the allowlist
- Logs `egress-validate: would deny <host>` for requests that would be blocked
- **Allows the request to proceed**

Administrators see these warnings in `docker logs conga-egress-<agent>`, giving full visibility into what enforcement would do before enabling it.

### 3b.3 Update AWS bootstrap Lua generation

**File**: `terraform/user-data.sh.tftpl`

The bootstrap's `generate_egress_conf()` generates the same Envoy config inline. In validate mode, replace the deny line with a log-and-allow:

```bash
if [ "$EFFECTIVE_MODE" = "validate" ]; then
    DENY_ACTION='                    h:logWarn("egress-validate: would deny " .. host)'
else
    DENY_ACTION='                    h:respond({[":status"] = "403"}, "egress denied: " .. host .. "\\n")'
fi
```

And for missing host:
```bash
if [ "$EFFECTIVE_MODE" = "validate" ]; then
    MISSING_HOST_ACTION='                      return'
else
    MISSING_HOST_ACTION='                      h:respond({[":status"] = "403"}, "egress denied: missing host\\n"); return'
fi
```

### 3b.4 Update all callers of `GenerateProxyConf()`

All call sites must pass the mode:
- `localprovider/provider.go` — `GenerateProxyConf(domains, egressPolicy.Mode)`
- `remoteprovider/provider.go` — `GenerateProxyConf(domains, egressPolicy.Mode)`
- Tests in `egress_test.go` — update existing calls

---

## Phase 4: Enforcement Report — Reflect actual behavior

### 4.1 Update `egressReport()`

**File**: `cli/internal/policy/enforcement.go`

Replace the current `egressReport` function (lines 40-74):

```go
func egressReport(e *EgressPolicy, providerName string) []RuleReport {
	var reports []RuleReport

	if len(e.AllowedDomains) > 0 || len(e.BlockedDomains) > 0 {
		var level EnforcementLevel
		var detail string
		switch providerName {
		case "aws", "remote", "local":
			if e.Mode != "validate" {
				level = Enforced
				detail = "Per-agent Envoy proxy with domain filtering + iptables DROP rules"
			} else {
				level = ValidateOnly
				detail = "Per-agent Envoy proxy with domain logging (violations logged, not blocked). Set mode: enforce to activate domain filtering + iptables."
			}
		default:
			level = NotApplicable
			detail = fmt.Sprintf("Unknown provider %q", providerName)
		}
		reports = append(reports, RuleReport{
			Section: "egress",
			Rule:    "domain_allowlist",
			Level:   level,
			Detail:  detail,
		})
	}

	return reports
}
```

This unifies all three providers under the same mode-driven logic. The provider name no longer changes the egress enforcement level — only the `mode` field does. The detail string now mentions iptables DROP rules since all providers will enforce them.

---

## Phase 5: Tests & Documentation

### 5.1 Update enforcement report tests

**File**: `cli/internal/policy/policy_test.go`

Update `TestEnforcementReportAWS` (line ~251) — with the new default of `enforce`, a policy with no explicit mode should report `Enforced`:
```go
func TestEnforcementReportAWS(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: "enforce"},
		Posture:    &PostureDeclarations{SecretsBackend: "managed", Monitoring: "standard"},
	}
	reports := pf.EnforcementReport("aws")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != Enforced {
			t.Errorf("aws enforce mode: expected enforced, got %s", r.Level)
		}
		// ... posture checks unchanged ...
	}
}
```

Add `TestEnforcementReportAWSValidate`:
```go
func TestEnforcementReportAWSValidate(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: "validate"},
	}
	reports := pf.EnforcementReport("aws")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != ValidateOnly {
			t.Errorf("aws validate mode: expected validate-only, got %s", r.Level)
		}
	}
}
```

Update `TestEnforcementReportRemote` (line ~271) — same pattern, test both modes explicitly.

Update `TestEnforcementReportLocal` (line ~221) — currently tests with `Mode: "validate"` which should still pass. The test at line ~238 (`TestEnforcementReportLocalEnforce`) also still passes.

Add `TestDefaultModeIsEnforce`:
```go
func TestDefaultModeIsEnforce(t *testing.T) {
	pf, err := Load("testdata/no-mode-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if pf.Egress.Mode != "enforce" {
		t.Errorf("expected default mode 'enforce', got %q", pf.Egress.Mode)
	}
}
```

With test fixture `testdata/no-mode-policy.yaml`:
```yaml
apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
```

### 5.2 Update `conga-policy.yaml.example`

Replace:
```yaml
  # Enforcement mode (local provider only; remote/AWS always enforce).
  #   validate — warn about unenforced rules (default)
  #   enforce  — activate egress proxy container
  mode: validate
```

With:
```yaml
  # Enforcement mode (all providers).
  #   enforce  — proxy with domain filtering + iptables DROP rules (default)
  #   validate — proxy with domain logging, no iptables (violations logged, not blocked)
  mode: enforce
```

### 5.3 Update policy schema spec example

**File**: `specs/2026-03-25_feature_policy-schema/spec.md` (line ~1058)

Same comment update as above.

### 5.4 Update security standards

**File**: `product-knowledge/standards/security.md`

In the Enforcement Escalation table (line ~49), update the Egress filtering row for Enterprise (Prod):

Replace:
```
Per-agent Envoy proxy with domain allowlist. No iptables enforcement (deferred — Phase 3). Blocked attempts logged.
```

With:
```
Per-agent Envoy proxy with domain allowlist + iptables DROP rules. Blocked attempts logged.
```

Update the "Cooperative proxy enforcement" residual risk entry (line ~134) to note that all providers now have iptables enforcement:

Replace:
```
Cooperative proxy enforcement | Low | Egress proxy is set via `HTTPS_PROXY` env var and enforced by iptables DROP rules in the DOCKER-USER chain...
```

Update detail to clarify all providers now enforce.

### 5.5 Update CLAUDE.md

In the "Known Limitations" section, remove or update any references to AWS lacking iptables enforcement.

In the `conga-policy.yaml.example` comment reference in the Secrets section, no change needed.

---

## Edge Cases

| Scenario | Expected Behavior |
|----------|------------------|
| No `mode` field in YAML | Defaults to `enforce` (proxy with domain filtering + iptables) |
| `mode: validate` with domains | All providers: proxy deployed with Lua domain-match logging (logWarn, not 403), HTTPS_PROXY set, no iptables |
| `mode: enforce` with domains | All providers: proxy with domain filtering, HTTPS_PROXY set, iptables DROP rules |
| `mode: enforce` with no domains | No proxy (no domains = nothing to proxy) |
| `mode: validate` with no domains | No proxy (no domains = nothing to proxy) |
| Agent override with different mode | Agent's mode overrides global (per existing `MergeForAgent()` shallow merge) |
| Policy file doesn't exist | No proxy (nil policy, no-op — unchanged) |
| Transition enforce → validate | `RefreshAgent` / `RefreshAll` reconfigures proxy to log-and-allow, removes iptables |
| Transition validate → enforce | `RefreshAgent` / `RefreshAll` reconfigures proxy with filtering, applies iptables |
| Container restart (IP change) | Systemd `ExecStartPost` applies iptables (enforce only); proxy stays running |
| Docker daemon restart | Systemd restarts containers; `ExecStartPost` re-applies iptables (enforce only) |
| Host reboot (AWS) | `conga-image-refresh.service` runs, agent services start, `ExecStartPost` applies iptables |

## Migration Impact

- **Operators with `mode: enforce`**: Proxy and iptables active as before (local/remote). AWS gains iptables rules.
- **Operators with `mode: validate`**: Proxy now deployed in log-and-allow mode (previously skipped). Traffic flows through proxy with domain logging but nothing is blocked. No iptables.
- **Operators with no mode field**: The default is now `enforce`. Full enforcement on all providers.
- **Existing AWS deployments**: Gain egress proxy + iptables DROP rules on next host cycle. This strengthens security — a container that bypasses `HTTPS_PROXY` can no longer reach the internet directly.
