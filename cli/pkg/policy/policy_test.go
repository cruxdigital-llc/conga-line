package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidFullPolicy(t *testing.T) {
	yaml := `
apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
    - "*.slack.com"
  blocked_domains:
    - evil.com
  mode: validate
routing:
  default_model: claude-sonnet-4-6
  fallback_chain:
    - claude-haiku-4-5
  cost_limits:
    daily_per_agent: 10.0
posture:
  isolation_level: standard
  secrets_backend: file
  monitoring: basic
agents:
  myagent:
    egress:
      allowed_domains:
        - api.anthropic.com
        - "*.trello.com"
`
	pf := loadFromString(t, yaml)
	if err := pf.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if pf.APIVersion != CurrentAPIVersion {
		t.Errorf("apiVersion = %q, want %q", pf.APIVersion, CurrentAPIVersion)
	}
	if len(pf.Egress.AllowedDomains) != 2 {
		t.Errorf("allowed_domains count = %d, want 2", len(pf.Egress.AllowedDomains))
	}
	if pf.Routing.DefaultModel != "claude-sonnet-4-6" {
		t.Errorf("default_model = %q, want claude-sonnet-4-6", pf.Routing.DefaultModel)
	}
}

func TestLoadMinimalPolicy(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1`
	pf := loadFromString(t, yaml)
	if err := pf.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	pf, err := Load("/nonexistent/path/conga-policy.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf != nil {
		t.Error("expected nil policy for missing file")
	}
}

func TestLoadEmptyFile(t *testing.T) {
	path := writeTemp(t, "")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := writeTemp(t, "{{invalid yaml")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadUnknownField(t *testing.T) {
	yaml := `
apiVersion: conga.dev/v1alpha1
unknown_section:
  foo: bar
`
	path := writeTemp(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestValidateMissingAPIVersion(t *testing.T) {
	pf := &PolicyFile{}
	if err := pf.Validate(); err == nil {
		t.Fatal("expected error for missing apiVersion")
	}
}

func TestValidateUnsupportedAPIVersion(t *testing.T) {
	pf := &PolicyFile{APIVersion: "conga.dev/v999"}
	if err := pf.Validate(); err == nil {
		t.Fatal("expected error for unsupported apiVersion")
	}
}

func TestValidateInvalidEgressMode(t *testing.T) {
	yaml := `
apiVersion: conga.dev/v1alpha1
egress:
  mode: turbo
`
	pf := loadFromString(t, yaml)
	if err := pf.Validate(); err == nil {
		t.Fatal("expected error for invalid egress mode")
	}
}

func TestValidateDomainFormat(t *testing.T) {
	tests := []struct {
		domain  string
		wantErr bool
	}{
		{"api.anthropic.com", false},
		{"*.slack.com", false},
		{"", true},
		{"has spaces.com", true},
		{"bad*.com", true},
		{"*.*.com", true},
	}
	for _, tt := range tests {
		err := validateDomain(tt.domain)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateDomain(%q) error = %v, wantErr = %v", tt.domain, err, tt.wantErr)
		}
	}
}

func TestMatchDomain(t *testing.T) {
	tests := []struct {
		pattern string
		domain  string
		want    bool
	}{
		{"api.anthropic.com", "api.anthropic.com", true},
		{"api.anthropic.com", "other.anthropic.com", false},
		{"*.slack.com", "wss-primary.slack.com", true},
		{"*.slack.com", "a.b.slack.com", true},
		{"*.slack.com", "slack.com", false},
		{"*.slack.com", "notslack.com", false},
		{"API.Anthropic.Com", "api.anthropic.com", true},
	}
	for _, tt := range tests {
		got := MatchDomain(tt.pattern, tt.domain)
		if got != tt.want {
			t.Errorf("MatchDomain(%q, %q) = %v, want %v", tt.pattern, tt.domain, got, tt.want)
		}
	}
}

func TestMergeForAgentWithOverride(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress: &EgressPolicy{
			AllowedDomains: []string{"api.anthropic.com", "*.slack.com"},
			Mode:           EgressModeValidate,
		},
		Posture: &PostureDeclarations{
			IsolationLevel: "standard",
		},
		Agents: map[string]*AgentOverride{
			"myagent": {
				Egress: &EgressPolicy{
					AllowedDomains: []string{"api.anthropic.com", "*.trello.com"},
				},
			},
		},
	}

	merged := pf.MergeForAgent("myagent")

	if len(merged.Egress.AllowedDomains) != 2 {
		t.Fatalf("expected 2 allowed domains, got %d", len(merged.Egress.AllowedDomains))
	}
	if merged.Egress.AllowedDomains[1] != "*.trello.com" {
		t.Errorf("expected *.trello.com, got %s", merged.Egress.AllowedDomains[1])
	}
	// Mode should be normalized to enforce (default) after merge
	if merged.Egress.Mode != EgressModeEnforce {
		t.Errorf("expected mode %q after normalization, got %q", EgressModeEnforce, merged.Egress.Mode)
	}
	// Posture should remain from global (no override)
	if merged.Posture.IsolationLevel != "standard" {
		t.Errorf("expected standard isolation, got %q", merged.Posture.IsolationLevel)
	}
}

func TestMergeForAgentWithoutOverride(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress: &EgressPolicy{
			AllowedDomains: []string{"api.anthropic.com"},
			Mode:           EgressModeValidate,
		},
	}

	merged := pf.MergeForAgent("unknown-agent")
	if len(merged.Egress.AllowedDomains) != 1 {
		t.Fatalf("expected 1 allowed domain, got %d", len(merged.Egress.AllowedDomains))
	}
	if merged.Egress.Mode != EgressModeValidate {
		t.Errorf("expected validate mode, got %q", merged.Egress.Mode)
	}
}

func TestEnforcementReportLocal(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: EgressModeValidate},
		Posture:    &PostureDeclarations{IsolationLevel: "standard", Monitoring: "basic"},
	}
	reports := pf.EnforcementReport("local")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != ValidateOnly {
			t.Errorf("local validate mode: expected validate-only, got %s", r.Level)
		}
		if r.Rule == "isolation_level" && r.Level != Enforced {
			t.Errorf("local standard isolation: expected enforced, got %s", r.Level)
		}
	}
}

func TestEnforcementReportLocalEnforce(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: EgressModeEnforce},
	}
	reports := pf.EnforcementReport("local")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != Enforced {
			t.Errorf("local enforce mode: expected enforced, got %s", r.Level)
		}
	}
}

func TestEnforcementReportAWS(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}},
		Posture:    &PostureDeclarations{SecretsBackend: "managed", Monitoring: "standard"},
	}
	reports := pf.EnforcementReport("aws")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != Enforced {
			t.Errorf("aws egress: expected enforced, got %s", r.Level)
		}
		if r.Rule == "secrets_backend" && r.Level != Enforced {
			t.Errorf("aws managed secrets: expected enforced, got %s", r.Level)
		}
		if r.Rule == "monitoring" && r.Level != Enforced {
			t.Errorf("aws standard monitoring: expected enforced, got %s", r.Level)
		}
	}
}

func TestEnforcementReportRemote(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}},
	}
	reports := pf.EnforcementReport("remote")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != Enforced {
			t.Errorf("remote egress: expected enforced, got %s", r.Level)
		}
	}
}

func TestEnforcementReportAWSValidate(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: EgressModeValidate},
	}
	reports := pf.EnforcementReport("aws")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != ValidateOnly {
			t.Errorf("aws validate mode: expected validate-only, got %s", r.Level)
		}
	}
}

func TestEnforcementReportRemoteValidate(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: EgressModeValidate},
	}
	reports := pf.EnforcementReport("remote")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != ValidateOnly {
			t.Errorf("remote validate mode: expected validate-only, got %s", r.Level)
		}
	}
}

func TestDefaultModeIsEnforce(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/conga-policy.yaml"
	if err := os.WriteFile(path, []byte("apiVersion: conga.dev/v1alpha1\negress:\n  allowed_domains:\n    - api.anthropic.com\n"), 0644); err != nil {
		t.Fatal(err)
	}
	pf, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if pf.Egress.Mode != EgressModeEnforce {
		t.Errorf("expected default mode 'enforce', got %q", pf.Egress.Mode)
	}
}

func TestDefaultModeAgentOverride(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/conga-policy.yaml"
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
agents:
  myagent:
    egress:
      allowed_domains:
        - api.anthropic.com
        - "*.trello.com"
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	pf, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if pf.Egress.Mode != EgressModeEnforce {
		t.Errorf("expected global default mode 'enforce', got %q", pf.Egress.Mode)
	}
	if pf.Agents["myagent"].Egress.Mode != EgressModeEnforce {
		t.Errorf("expected agent override default mode 'enforce', got %q", pf.Agents["myagent"].Egress.Mode)
	}
}

func TestMergeForAgentDeepCopy(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress: &EgressPolicy{
			AllowedDomains: []string{"api.anthropic.com"},
			Mode:           EgressModeValidate,
		},
		Agents: map[string]*AgentOverride{
			"myagent": {
				Egress: &EgressPolicy{
					AllowedDomains: []string{"api.anthropic.com", "*.trello.com"},
				},
			},
		},
	}

	merged := pf.MergeForAgent("myagent")
	merged.Egress.AllowedDomains = append(merged.Egress.AllowedDomains, "evil.com")

	// Original agent override must not be affected
	if len(pf.Agents["myagent"].Egress.AllowedDomains) != 2 {
		t.Errorf("mutation leaked to original: got %d domains, want 2", len(pf.Agents["myagent"].Egress.AllowedDomains))
	}

	// Merge without override — mutating merged must not affect global
	merged2 := pf.MergeForAgent("other")
	merged2.Egress.AllowedDomains = append(merged2.Egress.AllowedDomains, "evil.com")
	if len(pf.Egress.AllowedDomains) != 1 {
		t.Errorf("mutation leaked to global egress: got %d domains, want 1", len(pf.Egress.AllowedDomains))
	}
}

func TestValidateDomainOverlap(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress: &EgressPolicy{
			AllowedDomains: []string{"api.anthropic.com", "evil.com"},
			BlockedDomains: []string{"evil.com"},
		},
	}
	if err := pf.Validate(); err == nil {
		t.Fatal("expected error for domain in both allowed and blocked lists")
	}
}

func TestEnforcementReportUnknownProvider(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}},
		Posture:    &PostureDeclarations{IsolationLevel: "standard", SecretsBackend: "file", Monitoring: "basic"},
	}
	reports := pf.EnforcementReport("unknown")
	for _, r := range reports {
		if r.Level != NotApplicable {
			t.Errorf("unknown provider rule %s.%s: expected not-applicable, got %s", r.Section, r.Rule, r.Level)
		}
	}
}

func TestValidatePostureInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		posture PostureDeclarations
	}{
		{"bad isolation", PostureDeclarations{IsolationLevel: "maximum"}},
		{"bad backend", PostureDeclarations{SecretsBackend: "vault"}},
		{"bad monitoring", PostureDeclarations{Monitoring: "extreme"}},
	}
	for _, tt := range tests {
		pf := &PolicyFile{APIVersion: CurrentAPIVersion, Posture: &tt.posture}
		if err := pf.Validate(); err == nil {
			t.Errorf("%s: expected validation error", tt.name)
		}
	}
}

func TestValidateNegativeCostLimits(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Routing: &RoutingPolicy{
			CostLimits: &CostLimits{DailyPerAgent: -5.0},
		},
	}
	if err := pf.Validate(); err == nil {
		t.Fatal("expected error for negative cost limit")
	}
}

func TestValidateDomainRejectsSpecialChars(t *testing.T) {
	// These characters could enable injection into Lua or other generated configs.
	badDomains := []string{
		`evil.com"; os.execute("rm")--`,
		`evil.com\nprint("hi")`,
		"domain with]bracket",
		"domain;semicolon.com",
		"domain'quote.com",
		`domain"doublequote.com`,
		"domain\\backslash.com",
		"domain{brace.com",
		"domain(paren.com",
		"domain/slash.com",
		"domain@at.com",
		"*.evil.com\"]; --",
	}
	for _, d := range badDomains {
		err := validateDomain(d)
		if err == nil {
			t.Errorf("validateDomain(%q) should reject special characters", d)
		}
	}
}

func TestValidateDomainAcceptsValidDNS(t *testing.T) {
	validDomains := []string{
		"api.anthropic.com",
		"my-service.example.com",
		"*.slack.com",
		"a.b.c.d.e.f.example.com",
		"123.456.789.com",
		"UPPER.case.COM",
		"xn--nxasmq6b.com", // punycode
	}
	for _, d := range validDomains {
		err := validateDomain(d)
		if err != nil {
			t.Errorf("validateDomain(%q) should accept valid DNS name, got: %v", d, err)
		}
	}
}

func TestParseEgressMode(t *testing.T) {
	tests := []struct {
		input   string
		want    EgressMode
		wantErr bool
	}{
		{"enforce", EgressModeEnforce, false},
		{"validate", EgressModeValidate, false},
		{"", EgressModeEnforce, false},
		{"turbo", "", true},
		{"ENFORCE", "", true},
	}
	for _, tt := range tests {
		got, err := ParseEgressMode(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseEgressMode(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseEgressMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSaveRejectsInvalidPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conga-policy.yaml")

	pf := &PolicyFile{} // missing apiVersion
	if err := Save(pf, path); err == nil {
		t.Fatal("expected Save to reject invalid policy")
	}
	// File should not exist
	if _, err := os.Stat(path); err == nil {
		t.Error("invalid policy file should not have been written")
	}
}

func TestLoadFromBytesEdgeCases(t *testing.T) {
	// Whitespace-only bytes
	if _, err := LoadFromBytes([]byte("   \n\t  ")); err == nil {
		t.Error("expected error for whitespace-only bytes")
	}

	// Valid minimal
	pf, err := LoadFromBytes([]byte("apiVersion: conga.dev/v1alpha1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf.APIVersion != CurrentAPIVersion {
		t.Errorf("apiVersion = %q", pf.APIVersion)
	}

	// Invalid YAML bytes
	if _, err := LoadFromBytes([]byte("{{invalid")); err == nil {
		t.Error("expected error for invalid YAML bytes")
	}
}

func TestValidateRejectsEmptyModeAfterNormalization(t *testing.T) {
	// Construct a policy with empty mode WITHOUT going through Load/normalizeDefaults
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: ""},
	}
	if err := pf.Validate(); err == nil {
		t.Fatal("expected validation to reject empty mode (normalization must run before validation)")
	}
}

// --- helpers ---

func loadFromString(t *testing.T, content string) *PolicyFile {
	t.Helper()
	path := writeTemp(t, content)
	pf, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	return pf
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "conga-policy.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
