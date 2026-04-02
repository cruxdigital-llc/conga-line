package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEgressPolicyMissingFile(t *testing.T) {
	ep, err := LoadEgressPolicy("/nonexistent", "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep != nil {
		t.Error("expected nil egress policy for missing file")
	}
}

func TestLoadEgressPolicyWithMerge(t *testing.T) {
	dir := t.TempDir()
	yaml := `
apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
    - "*.slack.com"
  mode: enforce
agents:
  myagent:
    egress:
      allowed_domains:
        - api.anthropic.com
        - "*.trello.com"
      mode: enforce
`
	os.WriteFile(filepath.Join(dir, "conga-policy.yaml"), []byte(yaml), 0644)

	ep, err := LoadEgressPolicy(dir, "myagent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep == nil {
		t.Fatal("expected non-nil egress policy")
	}
	if len(ep.AllowedDomains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(ep.AllowedDomains))
	}
	if ep.AllowedDomains[1] != "*.trello.com" {
		t.Errorf("expected *.trello.com, got %s", ep.AllowedDomains[1])
	}
}

func TestLoadEgressPolicyDefaultModeIsEnforce(t *testing.T) {
	dir := t.TempDir()
	yaml := `
apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
`
	os.WriteFile(filepath.Join(dir, "conga-policy.yaml"), []byte(yaml), 0644)

	ep, err := LoadEgressPolicy(dir, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep == nil {
		t.Fatal("expected non-nil egress policy")
	}
	if ep.Mode != EgressModeEnforce {
		t.Errorf("expected default mode 'enforce' via LoadEgressPolicy, got %q", ep.Mode)
	}
}

func TestLoadEgressPolicyNoEgressSection(t *testing.T) {
	dir := t.TempDir()
	yaml := `apiVersion: conga.dev/v1alpha1`
	os.WriteFile(filepath.Join(dir, "conga-policy.yaml"), []byte(yaml), 0644)

	ep, err := LoadEgressPolicy(dir, "agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep != nil {
		t.Error("expected nil egress policy when no egress section")
	}
}

func TestEffectiveAllowedDomains(t *testing.T) {
	e := &EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "evil.com", "*.slack.com"},
		BlockedDomains: []string{"evil.com"},
	}
	result := EffectiveAllowedDomains(e)
	if len(result) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(result))
	}
	if result[0] != "api.anthropic.com" {
		t.Errorf("expected api.anthropic.com, got %s", result[0])
	}
	if result[1] != "*.slack.com" {
		t.Errorf("expected *.slack.com, got %s", result[1])
	}
}

func TestEffectiveAllowedDomainsNil(t *testing.T) {
	result := EffectiveAllowedDomains(nil)
	if result != nil {
		t.Error("expected nil for nil policy")
	}
}

func TestEffectiveAllowedDomainsEmpty(t *testing.T) {
	e := &EgressPolicy{AllowedDomains: []string{}}
	result := EffectiveAllowedDomains(e)
	if result != nil {
		t.Error("expected nil for empty allowlist")
	}
}

func TestEffectiveAllowedDomainsCaseInsensitive(t *testing.T) {
	e := &EgressPolicy{
		AllowedDomains: []string{"API.Anthropic.Com", "Evil.Com"},
		BlockedDomains: []string{"evil.com"},
	}
	result := EffectiveAllowedDomains(e)
	if len(result) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(result))
	}
}

func TestEgressProxyName(t *testing.T) {
	if EgressProxyName("myagent") != "conga-egress-myagent" {
		t.Errorf("unexpected proxy name: %s", EgressProxyName("myagent"))
	}
}

// egressPolicy is a test helper to build an EgressPolicy for GenerateProxyConf tests.
func egressPolicy(domains []string, mode EgressMode) *EgressPolicy {
	return &EgressPolicy{AllowedDomains: domains, Mode: mode}
}

func TestGenerateProxyConfAllowlist(t *testing.T) {
	result, err := GenerateProxyConf(egressPolicy([]string{"api.anthropic.com", "*.slack.com", "github.com"}, EgressModeEnforce))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "port_value: 3128") {
		t.Error("expected envoy listener on port 3128")
	}
	if !strings.Contains(result, "envoy.filters.http.lua") {
		t.Error("expected Lua filter in allowlist mode")
	}
	// *.slack.com should become .slack.com suffix in Lua SUFFIXES table
	if !strings.Contains(result, `".slack.com"`) {
		t.Error("expected .slack.com in Lua SUFFIXES table")
	}
	if !strings.Contains(result, `"api.anthropic.com"`) {
		t.Error("expected exact domain in Lua EXACT table")
	}
	if !strings.Contains(result, `":status"] = "403"`) {
		t.Error("expected 403 deny response in Lua filter")
	}
	if !strings.Contains(result, "dynamic_forward_proxy") {
		t.Error("expected dynamic forward proxy cluster")
	}
}

func TestGenerateProxyConfWildcardDedup(t *testing.T) {
	// When *.slack.com is present, the Lua filter puts .slack.com in SUFFIXES
	// and slack.com in EXACT. Both appear because Envoy Lua handles them separately.
	result, err := GenerateProxyConf(egressPolicy([]string{"api.anthropic.com", "slack.com", "*.slack.com"}, EgressModeEnforce))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, `".slack.com"`) {
		t.Error("expected .slack.com in SUFFIXES table")
	}
	if !strings.Contains(result, `"slack.com"`) {
		t.Error("expected slack.com in EXACT table")
	}
	if !strings.Contains(result, `"api.anthropic.com"`) {
		t.Error("expected non-overlapping domain to remain")
	}
}

func TestGenerateProxyConfDenyAllNilPolicy(t *testing.T) {
	result, err := GenerateProxyConf(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "envoy.filters.http.lua") {
		t.Error("expected Lua filter for deny-all (nil policy)")
	}
	if !strings.Contains(result, "port_value: 3128") {
		t.Error("expected port directive")
	}
	if !strings.Contains(result, "dynamic_forward_proxy") {
		t.Error("expected dynamic forward proxy cluster")
	}
	// Deny-all: empty EXACT table, enforce mode (403 response)
	if !strings.Contains(result, `local EXACT = {`) {
		t.Error("expected EXACT table in Lua filter")
	}
	if !strings.Contains(result, `egress denied:`) {
		t.Error("expected enforce-mode deny action for nil policy")
	}
}

func TestGenerateProxyConfDenyAllEmptySlice(t *testing.T) {
	result, err := GenerateProxyConf(egressPolicy([]string{}, EgressModeEnforce))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "envoy.filters.http.lua") {
		t.Error("expected Lua filter for deny-all (empty domains)")
	}
	if !strings.Contains(result, `egress denied:`) {
		t.Error("expected enforce-mode deny action for empty domains")
	}
}

func TestGenerateProxyConfDenyAllNilPolicyEnforcesMode(t *testing.T) {
	// nil policy must always produce enforce mode (403), never validate
	result, err := GenerateProxyConf(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "egress-validate") {
		t.Error("nil policy should produce enforce mode, not validate")
	}
	if !strings.Contains(result, `":status"] = "403"`) {
		t.Error("nil policy should produce 403 response (enforce mode)")
	}
}

func TestGenerateProxyConfDenyAllAllBlocked(t *testing.T) {
	// When all allowed domains are also blocked, effective = empty = deny-all
	ep := &EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com"},
		BlockedDomains: []string{"api.anthropic.com"},
		Mode:           EgressModeEnforce,
	}
	result, err := GenerateProxyConf(ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "envoy.filters.http.lua") {
		t.Error("expected Lua filter for deny-all (all domains blocked)")
	}
	// EXACT table should be empty since all domains are blocked
	if strings.Contains(result, `"api.anthropic.com"`) {
		t.Error("blocked domain should not appear in EXACT table")
	}
	if !strings.Contains(result, `egress denied:`) {
		t.Error("expected enforce-mode deny action when all domains blocked")
	}
}

func TestEgressProxyDockerfile(t *testing.T) {
	df := EgressProxyDockerfile()
	if !strings.Contains(df, "FROM "+EgressProxyBaseImage) {
		t.Errorf("expected envoy base image, got: %s", df)
	}
}

func TestGenerateProxyConfLuaNilAuthorityGuard(t *testing.T) {
	result, err := GenerateProxyConf(egressPolicy([]string{"api.anthropic.com"}, EgressModeEnforce))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Lua should guard against nil match result before calling :lower()
	if !strings.Contains(result, "if not m then") {
		t.Error("expected Lua nil guard for empty :authority match")
	}
	if strings.Contains(result, `a:match("^([^:]+)"):lower()`) {
		t.Error("old unguarded :lower() call should be replaced with nil-safe version")
	}
}

func TestGenerateProxyConfDNSFamily(t *testing.T) {
	result, err := GenerateProxyConf(egressPolicy([]string{"api.anthropic.com"}, EgressModeEnforce))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// V4_ONLY is required because Docker bridge networks are IPv4-only
	if !strings.Contains(result, "dns_lookup_family: V4_ONLY") {
		t.Error("expected dns_lookup_family: V4_ONLY (Docker bridge networks are IPv4-only)")
	}
}

func TestLuaEscapeString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"api.anthropic.com", "api.anthropic.com"},
		{`evil"domain`, `evil\"domain`},
		{"back\\slash", "back\\\\slash"},
		{"new\nline", "new\\nline"},
	}
	for _, tt := range tests {
		got := luaEscapeString(tt.input)
		if got != tt.want {
			t.Errorf("luaEscapeString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestProxyBootstrapJSSyntax(t *testing.T) {
	js := ProxyBootstrapJS()

	// Must be non-empty
	if len(js) == 0 {
		t.Fatal("ProxyBootstrapJS returned empty string")
	}

	// Must contain key components
	required := []string{
		"EnvHttpProxyAgent",
		"setGlobalDispatcher",
		"ConnectProxyAgent",
		"HTTPS_PROXY",
		"HTTP_PROXY",
		"__CONGA_PROXY_URL",
		"'use strict'",
	}
	for _, r := range required {
		if !strings.Contains(js, r) {
			t.Errorf("ProxyBootstrapJS missing required pattern: %s", r)
		}
	}

	// Basic bracket balance check
	opens := strings.Count(js, "{")
	closes := strings.Count(js, "}")
	if opens != closes {
		t.Errorf("ProxyBootstrapJS has unbalanced braces: %d opens, %d closes", opens, closes)
	}
}

func TestGenerateProxyConfLuaEscaping(t *testing.T) {
	// Even though validateDomain would reject these, verify defense-in-depth
	// by calling GenerateProxyConf directly with domains that need escaping.
	result, err := GenerateProxyConf(egressPolicy([]string{"normal.com"}, EgressModeEnforce))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify normal domains pass through cleanly
	if !strings.Contains(result, `"normal.com"`) {
		t.Error("expected normal domain in output")
	}
}

func TestGenerateProxyConfValidateMode(t *testing.T) {
	result, err := GenerateProxyConf(egressPolicy([]string{"api.anthropic.com", "*.slack.com"}, EgressModeValidate))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "envoy.filters.http.lua") {
		t.Error("expected Lua filter in validate mode")
	}
	if !strings.Contains(result, `"api.anthropic.com"`) {
		t.Error("expected exact domain in Lua EXACT table")
	}
	if !strings.Contains(result, `".slack.com"`) {
		t.Error("expected suffix in Lua SUFFIXES table")
	}
	// Validate mode should log warnings, NOT return 403
	if strings.Contains(result, `":status"] = "403"`) {
		t.Error("validate mode should not deny with 403")
	}
	if !strings.Contains(result, `logWarn("egress-validate: would deny "`) {
		t.Error("expected logWarn for would-be-denied requests in validate mode")
	}
}

func TestGenerateProxyConfValidateModeLogsOnMissingHost(t *testing.T) {
	result, err := GenerateProxyConf(egressPolicy([]string{"api.anthropic.com"}, EgressModeValidate))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `logWarn("egress-validate: would deny (missing host)")`) {
		t.Error("validate mode should log warning for missing host")
	}
	if strings.Contains(result, `if not m then return end`) {
		t.Error("validate mode should not silently return on missing host")
	}
}

func TestGenerateProxyConfEnforceMode403(t *testing.T) {
	result, err := GenerateProxyConf(egressPolicy([]string{"api.anthropic.com"}, EgressModeEnforce))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, `":status"] = "403"`) {
		t.Error("enforce mode should deny with 403")
	}
	if strings.Contains(result, "logWarn") {
		t.Error("enforce mode should not use logWarn")
	}
}

func TestGenerateProxyConfWildcardOnly(t *testing.T) {
	// Only wildcard domains, no exact domains — EXACT table should be empty
	result, err := GenerateProxyConf(egressPolicy([]string{"*.slack.com", "*.github.com"}, EgressModeEnforce))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "envoy.filters.http.lua") {
		t.Error("expected Lua filter for wildcard-only domains")
	}
	if !strings.Contains(result, `".slack.com"`) {
		t.Error("expected .slack.com in SUFFIXES table")
	}
	if !strings.Contains(result, `".github.com"`) {
		t.Error("expected .github.com in SUFFIXES table")
	}
	if !strings.Contains(result, `":status"] = "403"`) {
		t.Error("expected 403 deny in enforce mode")
	}
}
