// Package runtime defines the Runtime interface for pluggable agent runtimes
// (OpenClaw, Hermes, etc.). Each runtime implementation lives in its own
// sub-package and self-registers via init().
package runtime

import (
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// RuntimeName identifies an agent runtime.
type RuntimeName string

const (
	RuntimeOpenClaw RuntimeName = "openclaw"
	RuntimeHermes   RuntimeName = "hermes"
)

// Runtime encapsulates all agent-runtime-specific behavior.
type Runtime interface {
	// Name returns the runtime identifier.
	Name() RuntimeName

	// --- Config Generation ---

	// GenerateConfig produces the runtime's native config file bytes.
	GenerateConfig(params ConfigParams) ([]byte, error)

	// ConfigFileName returns the config file name written to the data directory.
	ConfigFileName() string

	// GenerateEnvFile produces the .env file content for the agent container.
	GenerateEnvFile(params EnvParams) []byte

	// --- Container Specification ---

	// ContainerSpec returns Docker container parameters.
	ContainerSpec(agent provider.AgentConfig) ContainerSpec

	// DefaultImage returns the default Docker image for this runtime.
	DefaultImage() string

	// --- Directory Layout ---

	// CreateDirectories creates the runtime-specific directory structure
	// inside the agent's host-side data directory.
	CreateDirectories(dataDir string) error

	// ContainerDataPath returns the path inside the container where the
	// data directory is mounted.
	ContainerDataPath() string

	// WorkspacePath returns the relative path within the data directory
	// to the agent's workspace (for behavior file deployment).
	WorkspacePath() string

	// --- Health Detection ---

	// DetectReady parses container log output and returns the readiness phase.
	DetectReady(logOutput string, hasSlack bool) ReadyPhase

	// HealthEndpoint returns an HTTP path for health checks (e.g., "/health").
	// Returns "" if the runtime doesn't expose a health endpoint.
	// The provider calls this on localhost:{hostPort} when log-based detection
	// is inconclusive (e.g., runtime logs to files instead of stdout).
	HealthEndpoint() string

	// --- Gateway Token ---

	// ReadGatewayToken extracts the gateway auth token from the config
	// file bytes on disk.
	ReadGatewayToken(configData []byte) string

	// GatewayTokenDockerExec returns arguments for docker exec to extract
	// the gateway token from inside a running container.
	// Returns nil if the runtime doesn't support in-container extraction.
	GatewayTokenDockerExec() []string

	// --- Channel Integration ---

	// ChannelConfig produces the runtime-native channel configuration for
	// embedding in the runtime's config file.
	ChannelConfig(agentType string, binding channels.ChannelBinding, secretValues map[string]string) (map[string]any, error)

	// PluginConfig produces runtime-native plugin/adapter enable/disable config.
	// Returns nil if this runtime doesn't have a plugin system.
	PluginConfig(platform string, enabled bool) map[string]any

	// WebhookPath returns the HTTP path where the router should deliver
	// channel events to this runtime's container.
	WebhookPath(platform string) string

	// --- Egress Proxy ---

	// SupportsNodeProxy returns true if this runtime needs the
	// proxy-bootstrap.js --require injection for Node.js.
	SupportsNodeProxy() bool
}

// ConfigParams holds all inputs needed to generate a runtime config file.
type ConfigParams struct {
	Agent        provider.AgentConfig
	Secrets      provider.SharedSecrets
	GatewayToken string
	Model        string // LLM model identifier (e.g., "anthropic/claude-sonnet-4-20250514")
}

// EnvParams holds all inputs needed to generate an env file.
type EnvParams struct {
	Agent    provider.AgentConfig
	Secrets  provider.SharedSecrets
	PerAgent map[string]string // per-agent secret name→value
}

// ContainerSpec defines Docker container parameters.
type ContainerSpec struct {
	ContainerPort int               // Port inside the container
	User          string            // "--user" value, e.g. "1000:1000"
	Memory        string            // "--memory" value, e.g. "2g"
	CPUs          string            // "--cpus" value, e.g. "0.75"
	PIDsLimit     string            // "--pids-limit" value
	EnvVars       map[string]string // Runtime-specific env vars
	Entrypoint    []string          // Override entrypoint (nil = use image default)
}

// ReadyPhase describes the container's readiness state.
type ReadyPhase struct {
	Phase    string // "starting", "gateway_up", "loading", "ready", "error"
	Message  string // Human-readable description
	IsReady  bool   // true when the agent is fully operational
	HasError bool   // true when errors detected in logs
}

// SecretNameToEnvVar converts a kebab-case secret name to SCREAMING_SNAKE_CASE.
// Example: "anthropic-api-key" -> "ANTHROPIC_API_KEY"
func SecretNameToEnvVar(name string) string {
	return strings.NewReplacer("-", "_").Replace(strings.ToUpper(name))
}

// ResolveRuntime returns the effective runtime name for an agent.
// Falls back to the global default, then to "openclaw".
func ResolveRuntime(agentRuntime, globalDefault string) RuntimeName {
	if agentRuntime != "" {
		return RuntimeName(agentRuntime)
	}
	if globalDefault != "" {
		return RuntimeName(globalDefault)
	}
	return RuntimeOpenClaw
}
