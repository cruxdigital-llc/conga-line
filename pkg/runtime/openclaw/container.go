package openclaw

import (
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// ContainerPort is the gateway port inside OpenClaw containers.
const ContainerPort = 18789

// AgentCustomConfigFile is the admin-owned OpenClaw config file that Conga
// references from the managed openclaw.json via "$include". Conga generates the
// managed root wholesale but never reads or overwrites this file (except
// `conga agent rebaseline`, which backs it up and resets it to "{}"). OpenClaw
// deep-merges it under the root, with the root winning on conflicting scalar
// keys. It lives next to openclaw.json in the agent data dir. See
// specs/2026-06-09_feature_infrastructure-only-simplification/spec.md.
const AgentCustomConfigFile = "agent-custom.json"

// FleetCustomConfigFile and AgentManagedCustomConfigFile are the Conga-DEPLOYED
// declarative custom-config layers (feature #31). Conga writes them into each
// agent's data dir from committed sources — fleet-custom.json from
// agents/_defaults/<runtime>/fleet-custom.json (applies to all agents), and
// agent-managed-custom.json from agents/<name>/custom.json (per-agent) — and
// references all three via the "$include" array. OpenClaw deep-merges in array
// order (later wins), and the managed root wins over every include (verified).
// Effective precedence: root > agent-custom (admin drift) > agent-managed (per-agent) > fleet.
// See specs/2026-06-10_feature_fleet-baseline-configuration/spec.md.
const (
	FleetCustomConfigFile        = "fleet-custom.json"
	AgentManagedCustomConfigFile = "agent-managed-custom.json"
)

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

// OAuthStateDir is where OpenClaw persists remote-MCP OAuth credential blobs
// (<server>-<hash>.json), relative to ContainerDataPath().
func (r *Runtime) OAuthStateDir() string {
	return "mcp-oauth"
}

func (r *Runtime) WorkspacePath() string {
	return "data/workspace"
}

func (r *Runtime) SupportsNodeProxy() bool { return true }

// PluginsToInstall returns the external OpenClaw plugin packages required
// for the agent's configured channels. Starting with v2026.5.x, Slack is
// shipped as an external plugin (@openclaw/slack) rather than bundled in
// the image, so any agent with a slack channel binding needs it installed
// into the data dir before the gateway starts.
//
// Platform matching is case- and whitespace-insensitive so a hand-edited
// agent JSON with "Slack" or " slack " still triggers the install. The
// canonical platform name registered in pkg/channels/slack is "slack".
func (r *Runtime) PluginsToInstall(agent provider.AgentConfig) []string {
	var plugins []string
	for _, ch := range agent.Channels {
		if strings.ToLower(strings.TrimSpace(ch.Platform)) == "slack" {
			plugins = append(plugins, "@openclaw/slack")
			break
		}
	}
	return plugins
}

// PluginInstallCommand returns the in-container command that installs an
// OpenClaw plugin into ~/.openclaw/npm. The install errors out with exit 1
// if the plugin is already on disk ("plugin already exists; delete it
// first"), which providers tolerate via best-effort wrapping — the
// already-installed plugin is the desired end state. Use `--force` when
// an explicit reinstall is needed (out of scope for the auto-bootstrap).
//
// NB: `--yes` is NOT a valid flag on OpenClaw v2026.5.18+ — passing it
// makes the command exit non-zero before any work happens. Keep the
// command minimal.
func (r *Runtime) PluginInstallCommand(spec string) []string {
	return []string{"openclaw", "plugins", "install", spec}
}
