package policy

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/cruxdigital-llc/conga-line/cli/internal/policy/templates"
)

// luaEscapeString escapes characters that are special in Lua string literals.
// Defense-in-depth: validateDomain should reject non-DNS characters, but this
// prevents injection if validation is bypassed.
func luaEscapeString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// EgressProxyImage is the locally-built image used for egress proxy containers.
// Built from EgressProxyBaseImage during first use (see EgressProxyDockerfile).
const EgressProxyImage = "conga-egress-proxy"

// EgressProxyBaseImage is the Envoy image used as the base for the egress proxy.
// Envoy handles HTTP CONNECT tunneling with Lua-based domain filtering.
const EgressProxyBaseImage = "envoyproxy/envoy:v1.32-latest"

// EgressProxyDockerfile returns the Dockerfile content for building the egress proxy image.
func EgressProxyDockerfile() string {
	return "FROM " + EgressProxyBaseImage + "\n"
}

// LoadEgressPolicy loads the policy file, merges for the given agent, and returns
// the effective egress policy. Returns nil, nil if no policy file or no egress section.
func LoadEgressPolicy(policyDir string, agentName string) (*EgressPolicy, error) {
	policyPath := filepath.Join(policyDir, "conga-policy.yaml")
	pf, err := Load(policyPath)
	if err != nil {
		return nil, fmt.Errorf("loading policy: %w", err)
	}
	if pf == nil {
		return nil, nil
	}
	if err := pf.Validate(); err != nil {
		return nil, fmt.Errorf("invalid policy: %w", err)
	}

	effective := pf.MergeForAgent(agentName)
	return effective.Egress, nil
}

// EffectiveAllowedDomains returns the list of domains an agent can reach,
// excluding any that appear in blocked_domains. Blocked takes precedence.
func EffectiveAllowedDomains(e *EgressPolicy) []string {
	if e == nil || len(e.AllowedDomains) == 0 {
		return nil
	}
	blocked := make(map[string]bool, len(e.BlockedDomains))
	for _, d := range e.BlockedDomains {
		blocked[strings.ToLower(d)] = true
	}
	var result []string
	for _, d := range e.AllowedDomains {
		if !blocked[strings.ToLower(d)] {
			result = append(result, d)
		}
	}
	return result
}

// EgressProxyName returns the proxy container name for an agent.
func EgressProxyName(agentName string) string {
	return "conga-egress-" + agentName
}

// envoyConfigData holds the pre-processed data for the Envoy config template.
type envoyConfigData struct {
	HasDomains   bool
	ExactDomains []string // pre-escaped, lowercased exact domains
	Suffixes     []string // pre-escaped, lowercased suffixes (without *. prefix)
}

var envoyConfigTmpl = template.Must(template.New("envoy-config").Parse(templates.EnvoyConfig))

// GenerateProxyConf generates an Envoy config for an egress proxy with domain filtering.
// Envoy handles HTTP CONNECT tunneling via its dynamic forward proxy filter.
// Domain filtering uses a Lua filter that inspects :authority before routing.
// When domains is non-empty, only listed domains pass through (allowlist mode).
// When domains is nil/empty, all traffic passes through (passthrough mode).
//
// NOTE: The bash reimplementation in terraform/user-data.sh.tftpl generates the same
// config format — keep both implementations and templates/envoy-config.yaml.tmpl in sync.
func GenerateProxyConf(domains []string) string {
	data := envoyConfigData{HasDomains: len(domains) > 0}

	for _, d := range domains {
		d = strings.ToLower(d)
		if base, ok := strings.CutPrefix(d, "*."); ok {
			data.Suffixes = append(data.Suffixes, luaEscapeString(base))
		} else {
			data.ExactDomains = append(data.ExactDomains, luaEscapeString(d))
		}
	}

	var b strings.Builder
	if err := envoyConfigTmpl.Execute(&b, data); err != nil {
		panic(fmt.Sprintf("executing envoy config template: %v", err))
	}
	return b.String()
}

// GenerateProxyEntrypoint returns a shell entrypoint script for the egress proxy container.
func GenerateProxyEntrypoint() string {
	return templates.ProxyEntrypoint
}

// ProxyBootstrapJS returns a Node.js script that patches fetch() and https.globalAgent
// to route all HTTP(S) traffic through the egress proxy. Without this, Node.js ignores
// the HTTPS_PROXY env var — fetch() doesn't honor it, and axios's built-in proxy
// support uses regular HTTP requests instead of CONNECT tunneling.
//
// The bootstrap script:
//  1. Sets undici's EnvHttpProxyAgent as the global fetch dispatcher
//  2. Replaces https.globalAgent with a CONNECT tunnel agent (pure built-in modules)
//  3. Saves the proxy URL in __CONGA_PROXY_URL so child processes can re-discover it
//
// Injected via NODE_OPTIONS="--require /opt/proxy-bootstrap.js" in the container.
// Assumes undici is installed at /app/node_modules/undici (OpenClaw image layout).
func ProxyBootstrapJS() string {
	return templates.ProxyBootstrapJS
}
