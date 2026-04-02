# Spec: Portable Policy Schema

## Overview

This spec introduces a portable policy artifact (`conga-policy.yaml`) that lets operators declare security and routing intent in a single file. Each provider reads the same policy and enforces what it can. This spec covers the data model, parsing, validation, and a CLI command — no enforcement logic.

**Source**: `portable-policy.md` Part 1 (sections 1.1–1.3)

## Deliverables

### 1. New dependency: `gopkg.in/yaml.v3`

```bash
cd cli && go get gopkg.in/yaml.v3 && go mod tidy
```

This is the first YAML dependency in the project. All other config files remain JSON. The distinction is intentional: the policy file is operator-authored intent; JSON files are machine-generated config.

### 2. New package: `cli/pkg/policy/policy.go`

Go types and core logic.

```go
package policy

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const CurrentAPIVersion = "conga.dev/v1alpha1"

// PolicyFile is the top-level structure of conga-policy.yaml.
type PolicyFile struct {
	APIVersion string                    `yaml:"apiVersion"`
	Egress     *EgressPolicy             `yaml:"egress,omitempty"`
	Routing    *RoutingPolicy            `yaml:"routing,omitempty"`
	Posture    *PostureDeclarations      `yaml:"posture,omitempty"`
	Agents     map[string]*AgentOverride `yaml:"agents,omitempty"`
}

// EgressPolicy defines which external domains agents can reach.
type EgressPolicy struct {
	AllowedDomains []string `yaml:"allowed_domains,omitempty"`
	BlockedDomains []string `yaml:"blocked_domains,omitempty"`
	Mode           string   `yaml:"mode,omitempty"` // "validate" (default) or "enforce"
}

// RoutingPolicy defines model selection and routing rules.
// Enforcement deferred to Spec 5 (Bifrost). This spec only validates the schema.
type RoutingPolicy struct {
	DefaultModel  string                `yaml:"default_model,omitempty"`
	Models        map[string]*ModelDef  `yaml:"models,omitempty"`
	FallbackChain []string              `yaml:"fallback_chain,omitempty"`
	CostLimits    *CostLimits           `yaml:"cost_limits,omitempty"`
	TaskRules     map[string]*TaskRule  `yaml:"task_rules,omitempty"`
}

// ModelDef describes an available model.
type ModelDef struct {
	Provider string  `yaml:"provider"`           // e.g., "anthropic", "openai", "ollama"
	Model    string  `yaml:"model"`              // e.g., "claude-sonnet-4-6"
	CostPer1KInput  float64 `yaml:"cost_per_1k_input,omitempty"`
	CostPer1KOutput float64 `yaml:"cost_per_1k_output,omitempty"`
}

// CostLimits defines budget caps.
type CostLimits struct {
	DailyPerAgent   float64 `yaml:"daily_per_agent,omitempty"`
	MonthlyPerAgent float64 `yaml:"monthly_per_agent,omitempty"`
	MonthlyGlobal   float64 `yaml:"monthly_global,omitempty"`
}

// TaskRule maps a task type to a preferred model.
type TaskRule struct {
	Model string `yaml:"model"`
}

// PostureDeclarations state the operator's expectations for security properties.
type PostureDeclarations struct {
	IsolationLevel       string   `yaml:"isolation_level,omitempty"`       // "standard", "hardened", "segmented"
	SecretsBackend       string   `yaml:"secrets_backend,omitempty"`       // "file", "managed", "proxy"
	Monitoring           string   `yaml:"monitoring,omitempty"`            // "basic", "standard", "full"
	ComplianceFrameworks []string `yaml:"compliance_frameworks,omitempty"` // e.g., ["cis-docker", "nist-800-190"]
}

// AgentOverride allows per-agent policy sections. All fields are optional.
// When present, the section replaces (not deep-merges) the corresponding global section.
type AgentOverride struct {
	Egress  *EgressPolicy        `yaml:"egress,omitempty"`
	Routing *RoutingPolicy       `yaml:"routing,omitempty"`
	Posture *PostureDeclarations `yaml:"posture,omitempty"`
}

// Load reads a policy file from disk. Returns nil, nil if the file does not exist.
func Load(path string) (*PolicyFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading policy file: %w", err)
	}

	// Reject empty files
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("policy file is empty")
	}

	var pf PolicyFile
	// yaml.v3 uses KnownFields to reject unknown keys
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&pf); err != nil {
		return nil, fmt.Errorf("parsing policy YAML: %w", err)
	}
	return &pf, nil
}

// Validate checks the structural validity of a loaded policy.
func (pf *PolicyFile) Validate() error {
	if pf.APIVersion == "" {
		return fmt.Errorf("apiVersion is required")
	}
	if pf.APIVersion != CurrentAPIVersion {
		return fmt.Errorf("unsupported apiVersion %q (expected %q)", pf.APIVersion, CurrentAPIVersion)
	}

	if pf.Egress != nil {
		if err := validateEgress(pf.Egress); err != nil {
			return fmt.Errorf("egress: %w", err)
		}
	}
	if pf.Routing != nil {
		if err := validateRouting(pf.Routing); err != nil {
			return fmt.Errorf("routing: %w", err)
		}
	}
	if pf.Posture != nil {
		if err := validatePosture(pf.Posture); err != nil {
			return fmt.Errorf("posture: %w", err)
		}
	}

	// Validate per-agent overrides
	for name, override := range pf.Agents {
		if override.Egress != nil {
			if err := validateEgress(override.Egress); err != nil {
				return fmt.Errorf("agents.%s.egress: %w", name, err)
			}
		}
		if override.Routing != nil {
			if err := validateRouting(override.Routing); err != nil {
				return fmt.Errorf("agents.%s.routing: %w", name, err)
			}
		}
		if override.Posture != nil {
			if err := validatePosture(override.Posture); err != nil {
				return fmt.Errorf("agents.%s.posture: %w", name, err)
			}
		}
	}

	return nil
}

func validateEgress(e *EgressPolicy) error {
	validModes := map[string]bool{"": true, "validate": true, "enforce": true}
	if !validModes[e.Mode] {
		return fmt.Errorf("invalid mode %q (must be \"validate\" or \"enforce\")", e.Mode)
	}
	for _, d := range e.AllowedDomains {
		if err := validateDomain(d); err != nil {
			return fmt.Errorf("allowed_domains: %w", err)
		}
	}
	for _, d := range e.BlockedDomains {
		if err := validateDomain(d); err != nil {
			return fmt.Errorf("blocked_domains: %w", err)
		}
	}
	return nil
}

func validateRouting(r *RoutingPolicy) error {
	if r.CostLimits != nil {
		if r.CostLimits.DailyPerAgent < 0 {
			return fmt.Errorf("cost_limits.daily_per_agent must be non-negative")
		}
		if r.CostLimits.MonthlyPerAgent < 0 {
			return fmt.Errorf("cost_limits.monthly_per_agent must be non-negative")
		}
		if r.CostLimits.MonthlyGlobal < 0 {
			return fmt.Errorf("cost_limits.monthly_global must be non-negative")
		}
	}
	return nil
}

func validatePosture(p *PostureDeclarations) error {
	validIsolation := map[string]bool{"": true, "standard": true, "hardened": true, "segmented": true}
	if !validIsolation[p.IsolationLevel] {
		return fmt.Errorf("invalid isolation_level %q (must be \"standard\", \"hardened\", or \"segmented\")", p.IsolationLevel)
	}
	validBackend := map[string]bool{"": true, "file": true, "managed": true, "proxy": true}
	if !validBackend[p.SecretsBackend] {
		return fmt.Errorf("invalid secrets_backend %q (must be \"file\", \"managed\", or \"proxy\")", p.SecretsBackend)
	}
	validMonitoring := map[string]bool{"": true, "basic": true, "standard": true, "full": true}
	if !validMonitoring[p.Monitoring] {
		return fmt.Errorf("invalid monitoring %q (must be \"basic\", \"standard\", or \"full\")", p.Monitoring)
	}
	return nil
}

// validateDomain checks that a domain string is well-formed.
// Accepts plain domains (api.anthropic.com) and wildcards (*.slack.com).
func validateDomain(d string) error {
	if d == "" {
		return fmt.Errorf("domain must not be empty")
	}
	if strings.ContainsAny(d, " \t\n") {
		return fmt.Errorf("domain %q must not contain whitespace", d)
	}
	// Wildcard must be at the start as *.
	if strings.Contains(d, "*") {
		if !strings.HasPrefix(d, "*.") {
			return fmt.Errorf("domain %q: wildcard must be at the start as *.example.com", d)
		}
		if strings.Count(d, "*") > 1 {
			return fmt.Errorf("domain %q: only one wildcard allowed", d)
		}
	}
	return nil
}

// MergeForAgent returns an effective policy for a specific agent.
// Per-agent overrides shallow-replace entire sections (egress, routing, posture).
// Returns a copy — the original PolicyFile is not modified.
func (pf *PolicyFile) MergeForAgent(agentName string) *PolicyFile {
	merged := &PolicyFile{
		APIVersion: pf.APIVersion,
		Egress:     pf.Egress,
		Routing:    pf.Routing,
		Posture:    pf.Posture,
	}

	override, exists := pf.Agents[agentName]
	if !exists || override == nil {
		return merged
	}

	if override.Egress != nil {
		merged.Egress = override.Egress
	}
	if override.Routing != nil {
		merged.Routing = override.Routing
	}
	if override.Posture != nil {
		merged.Posture = override.Posture
	}

	return merged
}

// MatchDomain checks whether a domain matches a pattern.
// Patterns can be exact ("api.anthropic.com") or wildcard ("*.slack.com").
// Wildcard *.example.com matches sub.example.com but NOT example.com itself.
func MatchDomain(pattern, domain string) bool {
	pattern = strings.ToLower(pattern)
	domain = strings.ToLower(domain)

	if !strings.HasPrefix(pattern, "*.") {
		return pattern == domain
	}

	// Wildcard: *.example.com matches x.example.com, a.b.example.com
	suffix := pattern[1:] // ".example.com"
	return strings.HasSuffix(domain, suffix) && domain != suffix[1:]
}
```

### 3. New file: `cli/pkg/policy/enforcement.go`

Per-provider enforcement capability reporting.

```go
package policy

// EnforcementLevel describes how a provider handles a policy rule.
type EnforcementLevel string

const (
	Enforced     EnforcementLevel = "enforced"
	Partial      EnforcementLevel = "partial"
	ValidateOnly EnforcementLevel = "validate-only"
	NotApplicable EnforcementLevel = "not-applicable"
)

// RuleReport describes enforcement of a single policy rule on a specific provider.
type RuleReport struct {
	Section string           `json:"section"` // "egress", "routing", "posture"
	Rule    string           `json:"rule"`
	Level   EnforcementLevel `json:"level"`
	Detail  string           `json:"detail"`
}

// EnforcementReport generates a report of how the given provider enforces this policy.
// The report is static — it describes provider capabilities, not runtime state.
func (pf *PolicyFile) EnforcementReport(providerName string) []RuleReport {
	var reports []RuleReport

	if pf.Egress != nil {
		reports = append(reports, egressReport(pf.Egress, providerName)...)
	}
	if pf.Routing != nil {
		reports = append(reports, routingReport(pf.Routing, providerName)...)
	}
	if pf.Posture != nil {
		reports = append(reports, postureReport(pf.Posture, providerName)...)
	}

	return reports
}

func egressReport(e *EgressPolicy, providerName string) []RuleReport {
	var reports []RuleReport

	if len(e.AllowedDomains) > 0 || len(e.BlockedDomains) > 0 {
		var level EnforcementLevel
		var detail string
		switch providerName {
		case "aws":
			level = Enforced
			detail = "Squid forward proxy with domain allowlist"
		case "remote":
			level = Partial
			detail = "iptables OUTPUT rules (IP-based, not SNI-based)"
		case "local":
			if e.Mode == "enforce" {
				level = Enforced
				detail = "Squid proxy container with domain-based CONNECT filtering"
			} else {
				level = ValidateOnly
				detail = "Warnings only; use mode: enforce to activate egress proxy"
			}
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

func routingReport(r *RoutingPolicy, providerName string) []RuleReport {
	var reports []RuleReport

	if r.DefaultModel != "" {
		reports = append(reports, RuleReport{
			Section: "routing",
			Rule:    "default_model",
			Level:   ValidateOnly,
			Detail:  "Model selection validated; proxy enforcement requires Bifrost (future spec)",
		})
	}
	if len(r.FallbackChain) > 0 {
		reports = append(reports, RuleReport{
			Section: "routing",
			Rule:    "fallback_chain",
			Level:   ValidateOnly,
			Detail:  "Fallback chain validated; enforcement requires Bifrost (future spec)",
		})
	}
	if r.CostLimits != nil {
		reports = append(reports, RuleReport{
			Section: "routing",
			Rule:    "cost_limits",
			Level:   ValidateOnly,
			Detail:  "Cost limits validated; enforcement requires Bifrost (future spec)",
		})
	}

	return reports
}

func postureReport(p *PostureDeclarations, providerName string) []RuleReport {
	var reports []RuleReport

	if p.IsolationLevel != "" {
		var level EnforcementLevel
		var detail string
		switch providerName {
		case "aws":
			switch p.IsolationLevel {
			case "standard":
				level = Enforced
				detail = "Docker cap-drop ALL, no-new-privileges, seccomp, isolated networks"
			case "hardened":
				level = ValidateOnly
				detail = "Requires gVisor (future spec)"
			case "segmented":
				level = ValidateOnly
				detail = "Requires per-agent subnets with NACLs (future spec)"
			}
		case "remote":
			if p.IsolationLevel == "standard" {
				level = Enforced
				detail = "Docker cap-drop ALL, no-new-privileges, isolated networks"
			} else {
				level = ValidateOnly
				detail = "Only standard isolation available on remote"
			}
		case "local":
			if p.IsolationLevel == "standard" {
				level = Enforced
				detail = "Docker cap-drop ALL, no-new-privileges, isolated networks"
			} else {
				level = ValidateOnly
				detail = "Only standard isolation available on local"
			}
		}
		reports = append(reports, RuleReport{
			Section: "posture",
			Rule:    "isolation_level",
			Level:   level,
			Detail:  detail,
		})
	}

	if p.SecretsBackend != "" {
		var level EnforcementLevel
		var detail string
		switch providerName {
		case "aws":
			switch p.SecretsBackend {
			case "managed":
				level = Enforced
				detail = "AWS Secrets Manager, encrypted at rest"
			case "file":
				level = Enforced
				detail = "File-based secrets on encrypted EBS (mode 0400)"
			case "proxy":
				level = ValidateOnly
				detail = "Proxy-based credential injection (future spec)"
			}
		case "remote":
			switch p.SecretsBackend {
			case "file":
				level = Enforced
				detail = "File-based secrets (mode 0400)"
			case "managed":
				level = ValidateOnly
				detail = "Managed secrets not available on remote; using file backend"
			case "proxy":
				level = ValidateOnly
				detail = "Proxy-based credential injection (future spec)"
			}
		case "local":
			switch p.SecretsBackend {
			case "file":
				level = Enforced
				detail = "File-based secrets (mode 0400)"
			case "managed":
				level = ValidateOnly
				detail = "Managed secrets not available on local; using file backend"
			case "proxy":
				level = ValidateOnly
				detail = "Proxy-based credential injection (future spec)"
			}
		}
		reports = append(reports, RuleReport{
			Section: "posture",
			Rule:    "secrets_backend",
			Level:   level,
			Detail:  detail,
		})
	}

	if p.Monitoring != "" {
		var level EnforcementLevel
		var detail string
		switch providerName {
		case "aws":
			switch p.Monitoring {
			case "basic":
				level = Enforced
				detail = "Config integrity monitoring + container logs"
			case "standard":
				level = Enforced
				detail = "Config integrity + CloudWatch logging + VPC flow logs"
			case "full":
				level = ValidateOnly
				detail = "Requires GuardDuty integration (future spec)"
			}
		case "remote":
			if p.Monitoring == "basic" {
				level = Enforced
				detail = "Config integrity monitoring + container logs"
			} else {
				level = Partial
				detail = "Remote supports basic monitoring; standard/full require log aggregator"
			}
		case "local":
			if p.Monitoring == "basic" {
				level = Enforced
				detail = "Config integrity monitoring + container logs"
			} else {
				level = ValidateOnly
				detail = "Only basic monitoring available on local"
			}
		}
		reports = append(reports, RuleReport{
			Section: "posture",
			Rule:    "monitoring",
			Level:   level,
			Detail:  detail,
		})
	}

	if len(p.ComplianceFrameworks) > 0 {
		var level EnforcementLevel
		var detail string
		if providerName == "aws" {
			level = ValidateOnly
			detail = "Compliance reporting requires enterprise compliance spec (future)"
		} else {
			level = NotApplicable
			detail = "Compliance frameworks only applicable on enterprise (AWS) provider"
		}
		reports = append(reports, RuleReport{
			Section: "posture",
			Rule:    "compliance_frameworks",
			Level:   level,
			Detail:  detail,
		})
	}

	return reports
}
```

### 4. New file: `cli/pkg/policy/policy_test.go`

```go
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
		{"API.Anthropic.Com", "api.anthropic.com", true}, // case-insensitive
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
			Mode:           "validate",
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

	// Egress should be replaced entirely by the agent override
	if len(merged.Egress.AllowedDomains) != 2 {
		t.Fatalf("expected 2 allowed domains, got %d", len(merged.Egress.AllowedDomains))
	}
	if merged.Egress.AllowedDomains[1] != "*.trello.com" {
		t.Errorf("expected *.trello.com, got %s", merged.Egress.AllowedDomains[1])
	}
	// Mode should be empty (agent override replaces entire section, not deep-merge)
	if merged.Egress.Mode != "" {
		t.Errorf("expected empty mode (shallow replace), got %q", merged.Egress.Mode)
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
			Mode:           "validate",
		},
	}

	merged := pf.MergeForAgent("unknown-agent")
	if len(merged.Egress.AllowedDomains) != 1 {
		t.Fatalf("expected 1 allowed domain, got %d", len(merged.Egress.AllowedDomains))
	}
	if merged.Egress.Mode != "validate" {
		t.Errorf("expected validate mode, got %q", merged.Egress.Mode)
	}
}

func TestEnforcementReportLocal(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: "validate"},
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
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: "enforce"},
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
		if r.Rule == "domain_allowlist" && r.Level != Partial {
			t.Errorf("remote egress: expected partial, got %s", r.Level)
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
```

### 5. New file: `cli/cmd/policy.go`

```go
package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/cruxdigital-llc/conga-line/cli/pkg/policy"
	provpkg "github.com/cruxdigital-llc/conga-line/cli/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/ui"
	"github.com/spf13/cobra"
)

var policyFilePath string

func init() {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage deployment policy",
	}

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the policy file and show enforcement report",
		Long: `Validate the conga-policy.yaml file and display which rules each
provider can enforce. The report shows enforcement levels:

  enforced       — Provider fully enforces this rule
  partial        — Provider partially enforces (best-effort)
  validate-only  — Provider validates but does not enforce
  not-applicable — Rule does not apply to this provider`,
		RunE: policyValidateRun,
	}
	validateCmd.Flags().StringVar(&policyFilePath, "file", "", "Path to policy file (default: auto-detect from provider)")

	policyCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(policyCmd)
}

// defaultPolicyPath returns the policy file path for the current provider.
func defaultPolicyPath() string {
	cfg, _ := provpkg.LoadConfig(provpkg.DefaultConfigPath())
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = provpkg.DefaultDataDir()
	}
	return filepath.Join(dataDir, "conga-policy.yaml")
}

func policyValidateRun(cmd *cobra.Command, args []string) error {
	path := policyFilePath
	if path == "" {
		path = defaultPolicyPath()
	}

	pf, err := policy.Load(path)
	if err != nil {
		return fmt.Errorf("failed to load policy: %w", err)
	}

	if pf == nil {
		if ui.OutputJSON {
			ui.EmitJSON(struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Path    string `json:"path"`
			}{
				Status:  "no_policy",
				Message: "No policy file found",
				Path:    path,
			})
			return nil
		}
		fmt.Printf("No policy file found at %s\n", path)
		fmt.Println("Create one from conga-policy.yaml.example to define your deployment policy.")
		return nil
	}

	if err := pf.Validate(); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	providerName := prov.Name()

	// If --agent specified, show merged policy for that agent
	agentName, _ := cmd.Flags().GetString("agent")
	if agentName == "" && flagAgent != "" {
		agentName = flagAgent
	}

	effectivePolicy := pf
	if agentName != "" {
		effectivePolicy = pf.MergeForAgent(agentName)
	}

	reports := effectivePolicy.EnforcementReport(providerName)

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Status   string              `json:"status"`
			Path     string              `json:"path"`
			Provider string              `json:"provider"`
			Agent    string              `json:"agent,omitempty"`
			Rules    []policy.RuleReport `json:"rules"`
		}{
			Status:   "valid",
			Path:     path,
			Provider: providerName,
			Agent:    agentName,
			Rules:    reports,
		})
		return nil
	}

	fmt.Printf("Policy: %s\n", path)
	fmt.Printf("Provider: %s\n", providerName)
	if agentName != "" {
		fmt.Printf("Agent: %s (merged)\n", agentName)
	}
	fmt.Println()

	if len(reports) == 0 {
		fmt.Println("No policy rules defined.")
		return nil
	}

	headers := []string{"SECTION", "RULE", "LEVEL", "DETAIL"}
	var rows [][]string
	for _, r := range reports {
		rows = append(rows, []string{r.Section, r.Rule, string(r.Level), r.Detail})
	}
	ui.PrintTable(headers, rows)

	return nil
}
```

### 6. Modify: `cli/cmd/root.go`

No change needed — the `policyCmd` registers itself via `init()` in `policy.go`, which is picked up automatically by Go's init ordering within the `cmd` package.

### 7. New file: `conga-policy.yaml.example`

```yaml
# Conga Line Deployment Policy
# Copy to ~/.conga/conga-policy.yaml and customize.
# All sections are optional. When absent, no policy is enforced.
# See: portable-policy.md for full documentation.

apiVersion: conga.dev/v1alpha1

# --- Egress Policy ---
# Which external domains agents can reach.
# This is the single highest-impact security control.
egress:
  # Domains the agent is allowed to reach. Wildcards supported (*.example.com).
  allowed_domains:
    - api.anthropic.com
    - "*.slack.com"
    - "*.slack-edge.com"
    - github.com
    - api.github.com
    - registry.npmjs.org

  # Domains explicitly blocked (takes precedence over allowed_domains).
  blocked_domains: []

  # Enforcement mode (local provider only; remote/AWS always enforce).
  #   validate — warn about unenforced rules (default)
  #   enforce  — activate egress proxy container
  mode: validate

# --- Routing Policy ---
# Model selection and routing rules.
# Enforcement requires Bifrost proxy (future spec). This section is validated only.
routing:
  # Model used when no routing rule matches.
  default_model: claude-sonnet-4-6

  # Available models with provider and cost metadata.
  # models:
  #   claude-sonnet:
  #     provider: anthropic
  #     model: claude-sonnet-4-6
  #     cost_per_1k_input: 0.003
  #     cost_per_1k_output: 0.015
  #   gpt-4o:
  #     provider: openai
  #     model: gpt-4o
  #     cost_per_1k_input: 0.005
  #     cost_per_1k_output: 0.015

  # Ordered list of models to try when the primary is unavailable.
  # fallback_chain:
  #   - claude-haiku
  #   - gpt-4o-mini

  # Budget caps. Actions when exceeded: downgrade model, pause agent, alert.
  # cost_limits:
  #   daily_per_agent: 10.00
  #   monthly_per_agent: 200.00
  #   monthly_global: 1000.00

# --- Security Posture Declarations ---
# Operator expectations for deployment security properties.
# Providers enforce what they can and report the gap.
posture:
  # Container isolation level.
  #   standard  — Docker cap-drop ALL, no-new-privileges, seccomp (all providers)
  #   hardened  — gVisor runtime (AWS only, future spec)
  #   segmented — Per-agent subnets with NACLs (AWS only, future spec)
  isolation_level: standard

  # Secrets storage backend.
  #   file    — Mode 0400 files (local/remote)
  #   managed — AWS Secrets Manager (AWS only)
  #   proxy   — Proxy-based credential injection (future spec)
  secrets_backend: file

  # Monitoring level.
  #   basic    — Config integrity hash + container logs (all providers)
  #   standard — + CloudWatch logging + VPC flow logs (AWS)
  #   full     — + GuardDuty + runtime anomaly detection (AWS, future spec)
  monitoring: basic

  # Compliance frameworks to map against (AWS only, future spec).
  # compliance_frameworks:
  #   - cis-docker
  #   - nist-800-190
  #   - aws-well-architected

# --- Per-Agent Overrides ---
# Override any section for a specific agent.
# The override replaces the entire section (shallow merge, not deep merge).
# agents:
#   myagent:
#     egress:
#       allowed_domains:
#         - api.anthropic.com
#         - "*.trello.com"
#     posture:
#       monitoring: standard
```

## Edge Cases

| Scenario | Behavior |
|---|---|
| Policy file doesn't exist | `Load()` returns nil, nil. `conga policy validate` prints info message, exits 0. |
| Policy file is empty | `Load()` returns error. `conga policy validate` prints error, exits 1. |
| Invalid YAML syntax | `Load()` returns error with yaml.v3 parse details. |
| Unknown field in YAML | `yaml.Decoder.KnownFields(true)` rejects it with descriptive error. |
| Missing `apiVersion` | `Validate()` returns error: "apiVersion is required". |
| Unsupported `apiVersion` | `Validate()` returns error with expected version. |
| Invalid enum value (egress mode, isolation, etc.) | `Validate()` returns error with valid options listed. |
| Agent override for nonexistent agent | Allowed — override is inert until an agent with that name is created. |
| Wildcard `*.example.com` matched against `example.com` | Returns false. Wildcard only matches subdomains. |
| Negative cost limit | `Validate()` returns error: "must be non-negative". |
| Domain with whitespace | `validateDomain()` rejects it. |
| Domain with invalid wildcard (`bad*.com`, `*.*`) | `validateDomain()` rejects: "wildcard must be at the start as *.example.com". |
| `--agent` flag with `conga policy validate` | Shows merged policy for that agent using `MergeForAgent()`. |
| `--output json` with `conga policy validate` | Emits structured JSON via `ui.EmitJSON()`. |
