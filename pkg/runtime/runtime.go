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

	// CustomConfigFileName returns the admin-owned customization file that the
	// generated config references (e.g. via "$include"), or "" if the runtime
	// has no such file. Providers must ensure this file exists (as "{}") next to
	// the config on every write — a missing target can invalidate the config.
	CustomConfigFileName() string

	// ManagedCustomConfigFiles returns the Conga-DEPLOYED declarative custom-config
	// layers referenced from the generated config (feature #31): the fleet baseline
	// and the per-agent file. Conga owns these (re-synced from committed sources on
	// every write), so unlike CustomConfigFileName (admin drift) they are both
	// reserved-key-guarded AND hash-verified against their deployed baseline.
	// Returns nil for runtimes without $include layering.
	ManagedCustomConfigFiles() []string

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

	// OAuthStateDir returns the directory, relative to ContainerDataPath(),
	// where the runtime persists remote-MCP OAuth credential blobs — or "" if
	// the runtime has no such state. OpenClaw stores one JSON blob per server at
	// <ContainerDataPath>/mcp-oauth/<server>-<hash>.json; returns "mcp-oauth".
	// Runtimes without remote-MCP OAuth (e.g. Hermes) return "", and all
	// capture/restore logic no-ops for them.
	OAuthStateDir() string

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

	// WebhookPort returns the container-internal port for receiving channel
	// events from the router. Returns 0 to use the same port as the gateway
	// (ContainerSpec.ContainerPort). Hermes uses a separate webhook adapter
	// on port 8644 while its API server is on 8642.
	WebhookPort() int

	// --- Egress Proxy ---

	// SupportsNodeProxy returns true if this runtime needs the
	// proxy-bootstrap.js --require injection for Node.js.
	SupportsNodeProxy() bool

	// --- Plugin Bootstrap ---

	// PluginsToInstall returns external plugin specs (e.g. "@openclaw/slack")
	// that must be installed inside the container's data directory before the
	// runtime can serve the agent's configured channels. Providers run these
	// as transient docker containers with the data dir bind-mounted, so the
	// install persists across container restarts.
	//
	// Idempotent: providers run this on every provision/refresh; the runtime's
	// own install command must handle "already installed" without error.
	// Returns nil if no external plugins are needed (e.g. no Slack channel
	// configured, or runtime ships everything bundled).
	PluginsToInstall(agent provider.AgentConfig) []string

	// PluginInstallCommand returns the in-container command that installs the
	// given plugin spec. The provider runs this with the data directory bind-
	// mounted at ContainerDataPath() so install state persists.
	// Returns nil if this runtime doesn't support runtime plugin installs.
	PluginInstallCommand(spec string) []string
}

// ConfigParams holds all inputs needed to generate a runtime config file.
type ConfigParams struct {
	Agent        provider.AgentConfig
	Secrets      provider.SharedSecrets
	GatewayToken string
	Model        string // LLM model identifier (e.g., "anthropic/claude-sonnet-4-20250514").
	// Consumed by the Hermes runtime today (see pkg/runtime/hermes/config.go);
	// OpenClaw uses the richer Overlay below. A future spec will migrate Hermes
	// to consume Overlay too, at which point this field can be removed. Until
	// then, both fields coexist; new runtimes should prefer Overlay.

	// Overlay is the optional, provider-agnostic per-agent runtime overlay
	// loaded from agents/<name>/agent.yaml. Runtime config generators
	// translate it into their native config shape. nil = no overlay applied.
	Overlay *AgentOverlay

	// RuntimeDefaults is the optional, operator-editable runtime baseline
	// config read from disk (agents/_defaults/<runtime>/openclaw-defaults.json,
	// feature #31's de-embed). When set and valid it replaces the binary's
	// embedded defaults as the generation base, letting operators edit the
	// fleet runtime baseline without a binary/provider release. nil or invalid
	// bytes fall back to the embedded copy (first-boot / air-gap / tamper-safe).
	// Resolve with common.ResolveRuntimeDefaults.
	RuntimeDefaults []byte
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

// MCPOAuthSecretPrefix namespaces per-agent secrets that hold a remote-MCP OAuth
// credential blob (value = the runtime's <server>-<hash>.json verbatim). Secrets
// under this prefix are materialized as files into the runtime's OAuthStateDir at
// provision/refresh, NOT injected as environment variables.
const MCPOAuthSecretPrefix = "mcp-oauth/"

// IsMCPOAuthSecret reports whether a per-agent secret name holds an MCP OAuth
// blob (and therefore must be excluded from env-file generation).
func IsMCPOAuthSecret(name string) bool {
	return strings.HasPrefix(name, MCPOAuthSecretPrefix)
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
