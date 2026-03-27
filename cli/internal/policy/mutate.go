package policy

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Save marshals the PolicyFile to YAML and writes it atomically.
// The parent directory is created if it does not exist.
func Save(pf *PolicyFile, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating policy directory: %w", err)
	}

	data, err := yaml.Marshal(pf)
	if err != nil {
		return fmt.Errorf("marshaling policy YAML: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("writing temporary policy file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replacing policy file: %w", err)
	}

	return nil
}

// SetEgress sets the egress section on a PolicyFile. When agentName is empty,
// the global section is replaced. When non-empty, a per-agent override is
// created or updated. The patch replaces the entire section (shallow-replace).
func SetEgress(pf *PolicyFile, agentName string, patch *EgressPolicy) {
	if agentName == "" {
		pf.Egress = patch
		return
	}
	ensureAgentOverride(pf, agentName).Egress = patch
}

// SetRouting sets the routing section. Same semantics as SetEgress.
func SetRouting(pf *PolicyFile, agentName string, patch *RoutingPolicy) {
	if agentName == "" {
		pf.Routing = patch
		return
	}
	ensureAgentOverride(pf, agentName).Routing = patch
}

// SetPosture sets the posture section. Same semantics as SetEgress.
func SetPosture(pf *PolicyFile, agentName string, patch *PostureDeclarations) {
	if agentName == "" {
		pf.Posture = patch
		return
	}
	ensureAgentOverride(pf, agentName).Posture = patch
}

// ensureAgentOverride returns the AgentOverride for agentName, creating the
// Agents map and/or the entry if needed.
func ensureAgentOverride(pf *PolicyFile, agentName string) *AgentOverride {
	if pf.Agents == nil {
		pf.Agents = make(map[string]*AgentOverride)
	}
	if pf.Agents[agentName] == nil {
		pf.Agents[agentName] = &AgentOverride{}
	}
	return pf.Agents[agentName]
}
