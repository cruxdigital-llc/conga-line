// Package provider defines the Provider interface and shared types for
// pluggable deployment targets (AWS, local Docker, etc.).
package provider

import (
	"context"
	"time"
)

// AgentType distinguishes user (DM-only) from team (channel-based) agents.
type AgentType string

const (
	AgentTypeUser AgentType = "user"
	AgentTypeTeam AgentType = "team"
)

// AgentConfig is the provider-agnostic representation of an agent.
type AgentConfig struct {
	Name          string    `json:"name"`
	Type          AgentType `json:"type"`
	SlackMemberID string    `json:"slack_member_id,omitempty"`
	SlackChannel  string    `json:"slack_channel,omitempty"`
	GatewayPort   int       `json:"gateway_port"`
	IAMIdentity   string    `json:"iam_identity,omitempty"`
	Paused        bool      `json:"paused,omitempty"`
}

// AgentStatus is returned by GetStatus.
type AgentStatus struct {
	AgentName    string
	ServiceState string // "running", "stopped", "not-found"
	Container    ContainerStatus
	ReadyPhase   string // "starting", "gateway up", "slack loading", "ready"
	Errors       []string
}

// ContainerStatus holds Docker container state and resource usage.
type ContainerStatus struct {
	State        string // "running", "exited", "created", "not found"
	Uptime       time.Duration
	StartedAt    string
	RestartCount int
	MemoryUsage  string
	CPUPercent   string
	PIDs         int
}

// SecretEntry represents a stored secret.
type SecretEntry struct {
	Name        string
	EnvVar      string
	Path        string
	LastChanged time.Time
}

// Identity represents the resolved caller identity.
type Identity struct {
	Name      string // username or IAM session name
	AccountID string // AWS account ID (empty for local)
	ARN       string // AWS ARN (empty for local)
	AgentName string // mapped agent name (empty if unmapped)
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

	// --- Connectivity ---

	// Connect establishes a connection to the agent's web UI.
	Connect(ctx context.Context, agentName string, localPort int) (*ConnectInfo, error)

	// --- Environment Management ---

	// Setup runs the initial environment setup wizard.
	Setup(ctx context.Context) error

	// CycleHost restarts the entire deployment environment.
	CycleHost(ctx context.Context) error

	// Teardown removes the entire deployment environment.
	// On local: removes all containers, networks, and config.
	// On AWS: not supported (use terraform destroy).
	Teardown(ctx context.Context) error
}
