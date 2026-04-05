package hermes

import (
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// ContainerPort is the API server port inside Hermes containers.
const ContainerPort = 8642

func (r *Runtime) ContainerSpec(agent provider.AgentConfig) runtime.ContainerSpec {
	return runtime.ContainerSpec{
		ContainerPort: ContainerPort,
		// Hermes container runs as root — the image installs system packages
		// (Python, Node.js, Playwright/Chromium) that require root access.
		// Security is enforced via cap-drop ALL, no-new-privileges, and
		// network isolation (same as all other containers).
		User:       "0:0",
		Memory:     "2g",
		CPUs:       "0.75",
		PIDsLimit:  "256",
		EnvVars:    map[string]string{},
		Entrypoint: []string{"hermes", "gateway", "run"},
	}
}

func (r *Runtime) DefaultImage() string {
	return "nousresearch/hermes-agent:latest"
}

func (r *Runtime) ContainerDataPath() string {
	return "/opt/data"
}

func (r *Runtime) WorkspacePath() string {
	return "workspace"
}

func (r *Runtime) SupportsNodeProxy() bool { return false }
