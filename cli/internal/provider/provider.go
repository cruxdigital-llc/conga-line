// Package provider defines the Provider interface and shared types for
// pluggable deployment targets (AWS, local Docker, etc.).
package provider

import (
	"context"
	"time"

	"github.com/cruxdigital-llc/conga-line/cli/internal/channels"
)

// AgentType distinguishes user (DM-only) from team (channel-based) agents.
type AgentType string

const (
	AgentTypeUser AgentType = "user"
	AgentTypeTeam AgentType = "team"
)

// AgentConfig is the provider-agnostic representation of an agent.
type AgentConfig struct {
	Name        string                    `json:"name"`
	Type        AgentType                 `json:"type"`
	Channels    []channels.ChannelBinding `json:"channels,omitempty"`
	GatewayPort int                       `json:"gateway_port"`
	IAMIdentity string                    `json:"iam_identity,omitempty"`
	Paused      bool                      `json:"paused,omitempty"`
}

// ChannelBinding returns the first binding for the given platform, or nil.
func (a *AgentConfig) ChannelBinding(platform string) *channels.ChannelBinding {
	for i := range a.Channels {
		if a.Channels[i].Platform == platform {
			return &a.Channels[i]
		}
	}
	return nil
}

// AgentStatus is returned by GetStatus.
type AgentStatus struct {
	AgentName    string          `json:"agent_name"`
	ServiceState string          `json:"service_state"` // "running", "stopped", "not-found"
	Container    ContainerStatus `json:"container"`
	ReadyPhase   string          `json:"ready_phase"` // "starting", "gateway up", "slack loading", "ready"
	Errors       []string        `json:"errors,omitempty"`
}

// ContainerStatus holds Docker container state and resource usage.
type ContainerStatus struct {
	State        string        `json:"state"` // "running", "exited", "created", "not found"
	Uptime       time.Duration `json:"uptime"`
	StartedAt    string        `json:"started_at,omitempty"`
	RestartCount int           `json:"restart_count"`
	MemoryUsage  string        `json:"memory_usage,omitempty"`
	CPUPercent   string        `json:"cpu_percent,omitempty"`
	PIDs         int           `json:"pids"`
}

// SecretEntry represents a stored secret.
type SecretEntry struct {
	Name        string    `json:"name"`
	EnvVar      string    `json:"env_var"`
	Path        string    `json:"path,omitempty"`
	LastChanged time.Time `json:"last_changed"`
}

// Identity represents the resolved caller identity.
type Identity struct {
	Name      string `json:"name"`                 // username or IAM session name
	AccountID string `json:"account_id,omitempty"` // AWS account ID (empty for local)
	ARN       string `json:"arn,omitempty"`        // AWS ARN (empty for local)
	AgentName string `json:"agent_name,omitempty"` // mapped agent name (empty if unmapped)
}

// ChannelStatus reports the state of a configured channel platform.
type ChannelStatus struct {
	Platform      string   `json:"platform"`       // "slack"
	Configured    bool     `json:"configured"`      // shared secrets present
	RouterRunning bool     `json:"router_running"`  // router container is running
	BoundAgents   []string `json:"bound_agents"`    // agent names with this channel binding
}

// ConnectInfo is returned by Connect for display to the user.
type ConnectInfo struct {
	URL       string
	LocalPort int
	Token     string
	// Waiter is a channel that blocks until the connection ends.
	// Nil for providers that don't maintain a persistent connection (e.g. local).
	Waiter <-chan error
}

// Provider is the core abstraction. Each deployment target implements this.
type Provider interface {
	// Name returns the provider identifier ("aws", "local").
	Name() string

	// --- Identity & Discovery ---

	// WhoAmI returns the current caller's identity.
	WhoAmI(ctx context.Context) (*Identity, error)

	// ListAgents returns all configured agents.
	ListAgents(ctx context.Context) ([]AgentConfig, error)

	// GetAgent returns a single agent by name, or error if not found.
	GetAgent(ctx context.Context, name string) (*AgentConfig, error)

	// ResolveAgentByIdentity finds the agent mapped to the current caller.
	// Returns nil, nil if no mapping exists.
	ResolveAgentByIdentity(ctx context.Context) (*AgentConfig, error)

	// --- Agent Lifecycle ---

	// ProvisionAgent creates a new agent.
	ProvisionAgent(ctx context.Context, cfg AgentConfig) error

	// RemoveAgent stops the container, removes network, cleans config.
	RemoveAgent(ctx context.Context, name string, deleteSecrets bool) error

	// PauseAgent stops an agent's container and removes it from routing.
	// All configuration, secrets, and data are preserved.
	PauseAgent(ctx context.Context, name string) error

	// UnpauseAgent restarts a paused agent and restores routing.
	UnpauseAgent(ctx context.Context, name string) error

	// --- Container Operations ---

	// GetStatus returns the current container status and health.
	GetStatus(ctx context.Context, agentName string) (*AgentStatus, error)

	// GetLogs returns the last N lines of container logs.
	GetLogs(ctx context.Context, agentName string, lines int) (string, error)

	// RefreshAgent restarts the agent container with fresh secrets/config.
	RefreshAgent(ctx context.Context, agentName string) error

	// RefreshAll restarts all agent containers.
	RefreshAll(ctx context.Context) error

	// ContainerExec runs a command inside the agent's container and returns stdout.
	// On AWS this uses SSM RunCommand; on local this uses docker exec.
	ContainerExec(ctx context.Context, agentName string, command []string) (string, error)

	// --- Secrets ---

	// SetSecret creates or updates a secret for the given agent.
	SetSecret(ctx context.Context, agentName, secretName, value string) error

	// ListSecrets returns all secrets for the given agent.
	ListSecrets(ctx context.Context, agentName string) ([]SecretEntry, error)

	// DeleteSecret removes a secret.
	DeleteSecret(ctx context.Context, agentName, secretName string) error

	// --- Channel Management ---

	// AddChannel configures a messaging channel platform (e.g. "slack") by storing
	// its shared secrets and starting the router. Idempotent: re-adding updates secrets.
	AddChannel(ctx context.Context, platform string, secrets map[string]string) error

	// RemoveChannel removes a channel platform: stops the router, strips bindings
	// from all agents, deletes shared secrets.
	RemoveChannel(ctx context.Context, platform string) error

	// ListChannels returns the status of all registered channel platforms.
	ListChannels(ctx context.Context) ([]ChannelStatus, error)

	// BindChannel adds a channel binding to an existing agent.
	BindChannel(ctx context.Context, agentName string, binding channels.ChannelBinding) error

	// UnbindChannel removes a channel binding from an agent.
	UnbindChannel(ctx context.Context, agentName string, platform string) error

	// --- Connectivity ---

	// Connect establishes a connection to the agent's web UI.
	Connect(ctx context.Context, agentName string, localPort int) (*ConnectInfo, error)

	// --- Environment Management ---

	// Setup runs the initial environment setup wizard.
	// When cfg is non-nil, values from it are used instead of interactive prompts.
	Setup(ctx context.Context, cfg *SetupConfig) error

	// CycleHost restarts the entire deployment environment.
	CycleHost(ctx context.Context) error

	// Teardown removes the entire deployment environment.
	// On local: removes all containers, networks, and config.
	// On AWS: not supported (use terraform destroy).
	Teardown(ctx context.Context) error
}
