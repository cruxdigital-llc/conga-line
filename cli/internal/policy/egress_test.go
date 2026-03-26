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

func TestGenerateNginxConfAllowlist(t *testing.T) {
	domains := []string{"api.anthropic.com", "*.slack.com", "github.com"}
	result := GenerateNginxConf(domains)

	if !strings.Contains(result, "api.anthropic.com api.anthropic.com:443") {
		t.Error("expected exact domain mapping")
	}
	if !strings.Contains(result, `"~^.+\.slack\.com$"`) {
		t.Error("expected wildcard regex pattern")
	}
	if !strings.Contains(result, `default "";`) {
		t.Error("expected default reject in allowlist mode")
	}
	if !strings.Contains(result, "listen 3128") {
		t.Error("expected HTTPS proxy listener")
	}
	if !strings.Contains(result, "resolver 127.0.0.11") {
		t.Error("expected Docker internal DNS resolver")
	}
	if strings.Contains(result, "listen 53") {
		t.Error("DNS forwarder should not be present (DNS tunneling risk)")
	}
	if !strings.Contains(result, "access_log /dev/stdout") {
		t.Error("expected access logging")
	}
}

func TestGenerateNginxConfPassthrough(t *testing.T) {
	result := GenerateNginxConf(nil)
	if !strings.Contains(result, "default $ssl_preread_server_name:443") {
		t.Error("expected passthrough mode with nil domains")
	}
}

func TestGenerateNginxConfEmptySlice(t *testing.T) {
	result := GenerateNginxConf([]string{})
	if !strings.Contains(result, "default $ssl_preread_server_name:443") {
		t.Error("expected passthrough mode with empty domains")
	}
}
