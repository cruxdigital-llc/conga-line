// Package manifest defines the YAML manifest schema for declarative
// environment provisioning via `conga bootstrap`.
package manifest

import (
	"fmt"
	"os"
	"strings"

	"github.com/cruxdigital-llc/conga-line/cli/pkg/common"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/policy"
	"gopkg.in/yaml.v3"
)

const (
	supportedAPIVersion = "conga.dev/v1alpha1"
	supportedKind       = "Environment"
)

// Manifest is the top-level structure of a conga bootstrap manifest.
type Manifest struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Provider   string            `yaml:"provider,omitempty"`
	Setup      *ManifestSetup    `yaml:"setup,omitempty"`
	Agents     []ManifestAgent   `yaml:"agents,omitempty"`
	Channels   []ManifestChannel `yaml:"channels,omitempty"`
	Policy     *ManifestPolicy   `yaml:"policy,omitempty"`
}

// ManifestSetup maps to provider.SetupConfig for server bootstrap.
type ManifestSetup struct {
	Image         string `yaml:"image,omitempty"`
	RepoPath      string `yaml:"repo_path,omitempty"`
	SSHHost       string `yaml:"ssh_host,omitempty"`
	SSHPort       int    `yaml:"ssh_port,omitempty"`
	SSHUser       string `yaml:"ssh_user,omitempty"`
	SSHKeyPath    string `yaml:"ssh_key_path,omitempty"`
	InstallDocker bool   `yaml:"install_docker,omitempty"`
}

// ManifestAgent defines an agent to provision.
type ManifestAgent struct {
	Name        string            `yaml:"name"`
	Type        string            `yaml:"type"`
	IAMIdentity string            `yaml:"iam_identity,omitempty"`
	Secrets     map[string]string `yaml:"secrets,omitempty"`
}

// ManifestChannel defines a messaging platform and its bindings.
type ManifestChannel struct {
	Platform string            `yaml:"platform"`
	Secrets  map[string]string `yaml:"secrets,omitempty"`
	Bindings []ManifestBinding `yaml:"bindings,omitempty"`
}

// ManifestBinding links an agent to a channel endpoint.
type ManifestBinding struct {
	Agent string `yaml:"agent"`
	ID    string `yaml:"id"`
	Label string `yaml:"label,omitempty"`
}

// ManifestPolicy defines inline policy using the same types as conga-policy.yaml.
type ManifestPolicy struct {
	Egress  *policy.EgressPolicy             `yaml:"egress,omitempty"`
	Routing *policy.RoutingPolicy            `yaml:"routing,omitempty"`
	Posture *policy.PostureDeclarations      `yaml:"posture,omitempty"`
	Agents  map[string]*policy.AgentOverride `yaml:"agents,omitempty"`
}

// Load reads a manifest YAML file from disk.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest YAML: %w", err)
	}
	return &m, nil
}

// Validate checks structural correctness without making provider calls.
func Validate(m *Manifest) error {
	if m.APIVersion != supportedAPIVersion {
		return fmt.Errorf("unsupported apiVersion %q (expected %q)", m.APIVersion, supportedAPIVersion)
	}
	if m.Kind != supportedKind {
		return fmt.Errorf("unsupported kind %q (expected %q)", m.Kind, supportedKind)
	}

	// Validate provider
	if m.Provider != "" {
		switch m.Provider {
		case "local", "remote", "aws":
			// ok
		default:
			return fmt.Errorf("unsupported provider %q (must be \"local\", \"remote\", or \"aws\")", m.Provider)
		}
	}

	// Validate agents
	agentNames := make(map[string]bool)
	for _, a := range m.Agents {
		if err := common.ValidateAgentName(a.Name); err != nil {
			return err
		}
		if agentNames[a.Name] {
			return fmt.Errorf("duplicate agent name %q", a.Name)
		}
		agentNames[a.Name] = true

		if a.Type != "user" && a.Type != "team" {
			return fmt.Errorf("agent %q: invalid type %q (must be \"user\" or \"team\")", a.Name, a.Type)
		}
	}

	// Validate channels
	platforms := make(map[string]bool)
	for _, ch := range m.Channels {
		if ch.Platform == "" {
			return fmt.Errorf("channel platform must not be empty")
		}
		if platforms[ch.Platform] {
			return fmt.Errorf("duplicate channel platform %q", ch.Platform)
		}
		platforms[ch.Platform] = true

		for _, b := range ch.Bindings {
			if !agentNames[b.Agent] {
				return fmt.Errorf("channel %q binding: agent %q not in agents list", ch.Platform, b.Agent)
			}
			if b.ID == "" {
				return fmt.Errorf("channel %q binding for agent %q: id is required", ch.Platform, b.Agent)
			}
		}
	}

	// Validate inline policy
	if m.Policy != nil {
		pf := &policy.PolicyFile{
			APIVersion: policy.CurrentAPIVersion,
			Egress:     m.Policy.Egress,
			Routing:    m.Policy.Routing,
			Posture:    m.Policy.Posture,
			Agents:     m.Policy.Agents,
		}
		if err := pf.Validate(); err != nil {
			return fmt.Errorf("policy: %w", err)
		}
	}

	return nil
}

// ExpandSecrets expands $VAR and ${VAR} references in agent and channel
// secret values from the process environment. Returns an error if any
// referenced variable is not set.
func ExpandSecrets(m *Manifest) error {
	for i := range m.Agents {
		if err := expandMap(m.Agents[i].Secrets, "agent", m.Agents[i].Name); err != nil {
			return err
		}
	}
	for i := range m.Channels {
		if err := expandMap(m.Channels[i].Secrets, "channel", m.Channels[i].Platform); err != nil {
			return err
		}
	}
	return nil
}

// LoadEnvFile reads a KEY=VALUE env file and sets each variable in the
// process environment. Lines starting with # and empty lines are skipped.
// Returns an error for malformed lines (no '=' sign) or empty keys.
// Surrounding double or single quotes on values are stripped.
func LoadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading env file: %w", err)
	}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("env file line %d: missing '=' in %q", i+1, line)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("env file line %d: empty key in %q", i+1, line)
		}
		val := strings.TrimSpace(value)
		// Strip matching surrounding quotes
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		os.Setenv(key, val)
	}
	return nil
}

func expandMap(secrets map[string]string, kind, name string) error {
	for k, v := range secrets {
		if !strings.Contains(v, "$") {
			continue
		}
		var missing []string
		expanded := os.Expand(v, func(key string) string {
			val, ok := os.LookupEnv(key)
			if !ok {
				missing = append(missing, key)
			}
			return val
		})
		if len(missing) > 0 {
			return fmt.Errorf("expanding secrets for %s %q: environment variable(s) not set: %s", kind, name, strings.Join(missing, ", "))
		}
		secrets[k] = expanded
	}
	return nil
}
