// Package policy defines the portable policy artifact schema (conga-policy.yaml),
// parsing, validation, and per-provider enforcement reporting.
package policy

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const CurrentAPIVersion = "conga.dev/v1alpha1"

// EgressMode defines the enforcement behavior for egress policy.
type EgressMode string

const (
	EgressModeEnforce  EgressMode = "enforce"
	EgressModeValidate EgressMode = "validate"
)

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
	AllowedDomains []string   `yaml:"allowed_domains,omitempty"`
	BlockedDomains []string   `yaml:"blocked_domains,omitempty"`
	Mode           EgressMode `yaml:"mode,omitempty"` // EgressModeEnforce (default) or EgressModeValidate
}

// RoutingPolicy defines model selection and routing rules.
// Enforcement deferred to Bifrost integration. This package only validates the schema.
type RoutingPolicy struct {
	DefaultModel  string               `yaml:"default_model,omitempty"`
	Models        map[string]*ModelDef `yaml:"models,omitempty"`
	FallbackChain []string             `yaml:"fallback_chain,omitempty"`
	CostLimits    *CostLimits          `yaml:"cost_limits,omitempty"`
	TaskRules     map[string]*TaskRule `yaml:"task_rules,omitempty"`
}

// ModelDef describes an available model.
type ModelDef struct {
	Provider        string  `yaml:"provider"`
	Model           string  `yaml:"model"`
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
	IsolationLevel       string   `yaml:"isolation_level,omitempty"`
	SecretsBackend       string   `yaml:"secrets_backend,omitempty"`
	Monitoring           string   `yaml:"monitoring,omitempty"`
	ComplianceFrameworks []string `yaml:"compliance_frameworks,omitempty"`
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
	return LoadFromBytes(data)
}

// LoadFromBytes parses a PolicyFile from raw YAML bytes.
// Returns an error if the bytes are empty or not valid policy YAML.
func LoadFromBytes(data []byte) (*PolicyFile, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("policy file is empty")
	}

	var pf PolicyFile
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&pf); err != nil {
		return nil, fmt.Errorf("parsing policy YAML: %w", err)
	}
	normalizeDefaults(&pf)
	return &pf, nil
}

// normalizeDefaults fills in default values for optional fields after parsing.
// This ensures all downstream consumers see resolved values.
func normalizeDefaults(pf *PolicyFile) {
	if pf.Egress != nil && pf.Egress.Mode == "" {
		pf.Egress.Mode = EgressModeEnforce
	}
	for _, override := range pf.Agents {
		if override != nil && override.Egress != nil && override.Egress.Mode == "" {
			override.Egress.Mode = EgressModeEnforce
		}
	}
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
	validModes := map[EgressMode]bool{"": true, EgressModeValidate: true, EgressModeEnforce: true}
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
	// Warn about domains appearing in both lists (blocked takes precedence at enforcement time).
	if len(e.AllowedDomains) > 0 && len(e.BlockedDomains) > 0 {
		allowed := make(map[string]bool, len(e.AllowedDomains))
		for _, d := range e.AllowedDomains {
			allowed[strings.ToLower(d)] = true
		}
		for _, d := range e.BlockedDomains {
			if allowed[strings.ToLower(d)] {
				return fmt.Errorf("domain %q appears in both allowed_domains and blocked_domains", d)
			}
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
	if strings.Contains(d, "*") {
		if !strings.HasPrefix(d, "*.") {
			return fmt.Errorf("domain %q: wildcard must be at the start as *.example.com", d)
		}
		if strings.Count(d, "*") > 1 {
			return fmt.Errorf("domain %q: only one wildcard allowed", d)
		}
	}
	// Only allow DNS-safe characters to prevent injection into generated configs
	// (e.g., Lua source in Envoy proxy config). Valid DNS: [a-zA-Z0-9.-] plus leading *.
	cleaned := strings.TrimPrefix(d, "*.")
	for _, c := range cleaned {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			return fmt.Errorf("domain %q contains invalid character %q", d, c)
		}
	}
	return nil
}

// MergeForAgent returns an effective policy for a specific agent.
// Per-agent overrides shallow-replace entire sections (egress, routing, posture).
// The returned policy is a deep copy — safe to mutate without affecting the original.
func (pf *PolicyFile) MergeForAgent(agentName string) *PolicyFile {
	merged := &PolicyFile{
		APIVersion: pf.APIVersion,
		Egress:     copyEgress(pf.Egress),
		Routing:    copyRouting(pf.Routing),
		Posture:    copyPosture(pf.Posture),
	}

	override, exists := pf.Agents[agentName]
	if !exists || override == nil {
		return merged
	}

	if override.Egress != nil {
		merged.Egress = copyEgress(override.Egress)
	}
	if override.Routing != nil {
		merged.Routing = copyRouting(override.Routing)
	}
	if override.Posture != nil {
		merged.Posture = copyPosture(override.Posture)
	}

	normalizeDefaults(merged)
	return merged
}

func copyEgress(e *EgressPolicy) *EgressPolicy {
	if e == nil {
		return nil
	}
	cp := *e
	cp.AllowedDomains = append([]string(nil), e.AllowedDomains...)
	cp.BlockedDomains = append([]string(nil), e.BlockedDomains...)
	return &cp
}

func copyRouting(r *RoutingPolicy) *RoutingPolicy {
	if r == nil {
		return nil
	}
	cp := *r
	cp.FallbackChain = append([]string(nil), r.FallbackChain...)
	if r.Models != nil {
		cp.Models = make(map[string]*ModelDef, len(r.Models))
		for k, v := range r.Models {
			m := *v
			cp.Models[k] = &m
		}
	}
	if r.CostLimits != nil {
		cl := *r.CostLimits
		cp.CostLimits = &cl
	}
	if r.TaskRules != nil {
		cp.TaskRules = make(map[string]*TaskRule, len(r.TaskRules))
		for k, v := range r.TaskRules {
			tr := *v
			cp.TaskRules[k] = &tr
		}
	}
	return &cp
}

func copyPosture(p *PostureDeclarations) *PostureDeclarations {
	if p == nil {
		return nil
	}
	cp := *p
	cp.ComplianceFrameworks = append([]string(nil), p.ComplianceFrameworks...)
	return &cp
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

	suffix := pattern[1:] // ".example.com"
	return strings.HasSuffix(domain, suffix) && domain != suffix[1:]
}
