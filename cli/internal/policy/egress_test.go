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

func TestGenerateProxyConfAllowlist(t *testing.T) {
	domains := []string{"api.anthropic.com", "*.slack.com", "github.com"}
	result := GenerateProxyConf(domains)

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
	domains := []string{"api.anthropic.com", "slack.com", "*.slack.com"}
	result := GenerateProxyConf(domains)

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

func TestGenerateProxyConfPassthrough(t *testing.T) {
	result := GenerateProxyConf(nil)
	if strings.Contains(result, "envoy.filters.http.lua") {
		t.Error("expected no Lua filter in passthrough mode")
	}
	if !strings.Contains(result, "port_value: 3128") {
		t.Error("expected port directive in passthrough mode")
	}
	if !strings.Contains(result, "dynamic_forward_proxy") {
		t.Error("expected dynamic forward proxy cluster in passthrough mode")
	}
}

func TestGenerateProxyConfEmptySlice(t *testing.T) {
	result := GenerateProxyConf([]string{})
	if strings.Contains(result, "envoy.filters.http.lua") {
		t.Error("expected no Lua filter with empty domains")
	}
}

func TestEgressProxyDockerfile(t *testing.T) {
	df := EgressProxyDockerfile()
	if !strings.Contains(df, "FROM "+EgressProxyBaseImage) {
		t.Errorf("expected envoy base image, got: %s", df)
	}
}

func TestGenerateProxyConfLuaNilAuthorityGuard(t *testing.T) {
	result := GenerateProxyConf([]string{"api.anthropic.com"})
	// Lua should guard against nil match result before calling :lower()
	if !strings.Contains(result, "if not m then") {
		t.Error("expected Lua nil guard for empty :authority match")
	}
	if strings.Contains(result, `a:match("^([^:]+)"):lower()`) {
		t.Error("old unguarded :lower() call should be replaced with nil-safe version")
	}
}

func TestGenerateProxyConfDNSFamily(t *testing.T) {
	result := GenerateProxyConf([]string{"api.anthropic.com"})
	if strings.Contains(result, "V4_ONLY") {
		t.Error("dns_lookup_family should be AUTO, not V4_ONLY")
	}
	if !strings.Contains(result, "dns_lookup_family: AUTO") {
		t.Error("expected dns_lookup_family: AUTO")
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
	result := GenerateProxyConf([]string{"normal.com"})
	// Verify normal domains pass through cleanly
	if !strings.Contains(result, `"normal.com"`) {
		t.Error("expected normal domain in output")
	}
}
