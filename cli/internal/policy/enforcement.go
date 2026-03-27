package policy

import "fmt"

// EnforcementLevel describes how a provider handles a policy rule.
type EnforcementLevel string

const (
	Enforced      EnforcementLevel = "enforced"
	Partial       EnforcementLevel = "partial"
	ValidateOnly  EnforcementLevel = "validate-only"
	NotApplicable EnforcementLevel = "not-applicable"
)

// RuleReport describes enforcement of a single policy rule on a specific provider.
type RuleReport struct {
	Section string           `json:"section"`
	Rule    string           `json:"rule"`
	Level   EnforcementLevel `json:"level"`
	Detail  string           `json:"detail"`
}

// EnforcementReport generates a report of how the given provider enforces this policy.
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
			detail = "Per-agent Squid proxy with domain-based CONNECT filtering"
		case "remote":
			level = Enforced
			detail = "Per-agent Squid proxy with domain-based CONNECT filtering"
		case "local":
			if e.Mode == "enforce" {
				level = Enforced
				detail = "Per-agent Squid proxy with domain-based CONNECT filtering"
			} else {
				level = ValidateOnly
				detail = "Warnings only; use mode: enforce to activate egress proxy"
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
		default:
			level = NotApplicable
			detail = fmt.Sprintf("Unknown provider %q", providerName)
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
		default:
			level = NotApplicable
			detail = fmt.Sprintf("Unknown provider %q", providerName)
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
		default:
			level = NotApplicable
			detail = fmt.Sprintf("Unknown provider %q", providerName)
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
