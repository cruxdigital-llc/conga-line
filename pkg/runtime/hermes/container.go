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
		EnvVars:    map[string]string{"HERMES_HOME": "/opt/data"},
		Entrypoint: []string{"hermes", "gateway", "run"},
	}
}

func (r *Runtime) DefaultImage() string {
	return "nousresearch/hermes-agent:latest"
}

func (r *Runtime) ContainerDataPath() string {
	return "/opt/data"
}

// OAuthStateDir returns "" — Hermes has no remote-MCP OAuth credential state, so
// MCP OAuth capture/restore no-ops for it.
func (r *Runtime) OAuthStateDir() string {
	return ""
}

func (r *Runtime) WorkspacePath() string {
	return "workspace"
}

func (r *Runtime) SupportsNodeProxy() bool { return false }

// PluginsToInstall — Hermes ships its channel adapters bundled in the image,
// so no external runtime plugins are needed at provision time.
func (r *Runtime) PluginsToInstall(provider.AgentConfig) []string { return nil }

// PluginInstallCommand — Hermes has no plugin install command; the runtime
// loader picks up adapters from the image at startup.
func (r *Runtime) PluginInstallCommand(string) []string { return nil }
