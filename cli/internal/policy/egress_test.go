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

	if !strings.Contains(result, "http_port 3128") {
		t.Error("expected Squid http_port directive")
	}
	if !strings.Contains(result, "acl allowed_domains dstdomain") {
		t.Error("expected Squid dstdomain ACL")
	}
	if !strings.Contains(result, " api.anthropic.com") {
		t.Error("expected exact domain in ACL")
	}
	if !strings.Contains(result, " .slack.com") {
		t.Error("expected wildcard domain as .slack.com in ACL")
	}
	if !strings.Contains(result, " github.com") {
		t.Error("expected github.com in ACL")
	}
	if !strings.Contains(result, "http_access allow CONNECT allowed_domains SSL_ports") {
		t.Error("expected CONNECT access rule")
	}
	if !strings.Contains(result, "http_access deny all") {
		t.Error("expected default deny in allowlist mode")
	}
	if !strings.Contains(result, "cache deny all") {
		t.Error("expected cache disabled")
	}
	if !strings.Contains(result, "access_log stdio:/dev/stdout") {
		t.Error("expected access logging to stdout")
	}
}

func TestGenerateProxyConfPassthrough(t *testing.T) {
	result := GenerateProxyConf(nil)
	if !strings.Contains(result, "http_access allow all") {
		t.Error("expected passthrough mode with nil domains")
	}
}

func TestGenerateProxyConfEmptySlice(t *testing.T) {
	result := GenerateProxyConf([]string{})
	if !strings.Contains(result, "http_access allow all") {
		t.Error("expected passthrough mode with empty domains")
	}
}

func TestEgressProxyDockerfile(t *testing.T) {
	df := EgressProxyDockerfile()
	if !strings.Contains(df, "FROM alpine:3.21") {
		t.Error("expected alpine base image")
	}
	if !strings.Contains(df, "apk add") && !strings.Contains(df, "squid") {
		t.Error("expected squid installation")
	}
}
