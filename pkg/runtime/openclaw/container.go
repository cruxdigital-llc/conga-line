package openclaw

import (
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// ContainerPort is the gateway port inside OpenClaw containers.
const ContainerPort = 18789

func (r *Runtime) ContainerSpec(agent provider.AgentConfig) runtime.ContainerSpec {
	return runtime.ContainerSpec{
		ContainerPort: ContainerPort,
		User:          "1000:1000",
		Memory:        "2g",
		CPUs:          "0.75",
		PIDsLimit:     "256",
		EnvVars:       map[string]string{"NODE_OPTIONS": "--max-old-space-size=1536"},
	}
}

func (r *Runtime) DefaultImage() string {
	return "ghcr.io/openclaw/openclaw:latest"
}

func (r *Runtime) ContainerDataPath() string {
	return "/home/node/.openclaw"
}

func (r *Runtime) WorkspacePath() string {
	return "data/workspace"
}

func (r *Runtime) SupportsNodeProxy() bool { return true }
