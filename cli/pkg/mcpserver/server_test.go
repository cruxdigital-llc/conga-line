package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cruxdigital-llc/conga-line/cli/pkg/channels"
	_ "github.com/cruxdigital-llc/conga-line/cli/pkg/channels/slack"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/mcpserver"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/provider"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
)

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	name string

	// Return values for each method. Set these per-test.
	identity    *provider.Identity
	agents      []provider.AgentConfig
	agent       *provider.AgentConfig
	status      *provider.AgentStatus
	logs        string
	secrets     []provider.SecretEntry
	execOutput  string
	connectInfo *provider.ConnectInfo

	// Capture call args.
	lastAgentName      string
	lastSecretName     string
	lastSecretValue    string
	lastCommand        []string
	lastProvisionCfg   provider.AgentConfig
	lastDeleteSecret   bool
	lastSetupCfg       *provider.SetupConfig
	lastLogLines       int
	lastPlatform       string
	lastChannelSecrets map[string]string

	// Error to return.
	err error
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) WhoAmI(ctx context.Context) (*provider.Identity, error) {
	return m.identity, m.err
}
func (m *mockProvider) ListAgents(ctx context.Context) ([]provider.AgentConfig, error) {
	return m.agents, m.err
}
func (m *mockProvider) GetAgent(ctx context.Context, name string) (*provider.AgentConfig, error) {
	m.lastAgentName = name
	return m.agent, m.err
}
func (m *mockProvider) ResolveAgentByIdentity(ctx context.Context) (*provider.AgentConfig, error) {
	return m.agent, m.err
}
func (m *mockProvider) ProvisionAgent(ctx context.Context, cfg provider.AgentConfig) error {
	m.lastProvisionCfg = cfg
	return m.err
}
func (m *mockProvider) RemoveAgent(ctx context.Context, name string, deleteSecrets bool) error {
	m.lastAgentName = name
	m.lastDeleteSecret = deleteSecrets
	return m.err
}
func (m *mockProvider) PauseAgent(ctx context.Context, name string) error {
	m.lastAgentName = name
	return m.err
}
func (m *mockProvider) UnpauseAgent(ctx context.Context, name string) error {
	m.lastAgentName = name
	return m.err
}
func (m *mockProvider) GetStatus(ctx context.Context, agentName string) (*provider.AgentStatus, error) {
	m.lastAgentName = agentName
	return m.status, m.err
}
func (m *mockProvider) GetLogs(ctx context.Context, agentName string, lines int) (string, error) {
	m.lastAgentName = agentName
	m.lastLogLines = lines
	return m.logs, m.err
}
func (m *mockProvider) RefreshAgent(ctx context.Context, agentName string) error {
	m.lastAgentName = agentName
	return m.err
}
func (m *mockProvider) RefreshAll(ctx context.Context) error { return m.err }
func (m *mockProvider) ContainerExec(ctx context.Context, agentName string, command []string) (string, error) {
	m.lastAgentName = agentName
	m.lastCommand = command
	return m.execOutput, m.err
}
func (m *mockProvider) SetSecret(ctx context.Context, agentName, secretName, value string) error {
	m.lastAgentName = agentName
	m.lastSecretName = secretName
	m.lastSecretValue = value
	return m.err
}
func (m *mockProvider) ListSecrets(ctx context.Context, agentName string) ([]provider.SecretEntry, error) {
	m.lastAgentName = agentName
	return m.secrets, m.err
}
func (m *mockProvider) DeleteSecret(ctx context.Context, agentName, secretName string) error {
	m.lastAgentName = agentName
	m.lastSecretName = secretName
	return m.err
}
func (m *mockProvider) Connect(ctx context.Context, agentName string, localPort int) (*provider.ConnectInfo, error) {
	return m.connectInfo, m.err
}
func (m *mockProvider) Setup(ctx context.Context, cfg *provider.SetupConfig) error {
	m.lastSetupCfg = cfg
	return m.err
}
func (m *mockProvider) AddChannel(ctx context.Context, platform string, secrets map[string]string) error {
	m.lastPlatform = platform
	m.lastChannelSecrets = secrets
	return m.err
}
func (m *mockProvider) RemoveChannel(ctx context.Context, platform string) error {
	m.lastPlatform = platform
	return m.err
}
func (m *mockProvider) ListChannels(ctx context.Context) ([]provider.ChannelStatus, error) {
	if m.err != nil {
		return nil, m.err
	}
	return []provider.ChannelStatus{
		{Platform: "slack", Configured: true, RouterRunning: true, BoundAgents: []string{"agent1"}},
	}, nil
}
func (m *mockProvider) BindChannel(ctx context.Context, agentName string, binding channels.ChannelBinding) error {
	m.lastAgentName = agentName
	return m.err
}
func (m *mockProvider) UnbindChannel(ctx context.Context, agentName string, platform string) error {
	m.lastAgentName = agentName
	return m.err
}
func (m *mockProvider) CycleHost(ctx context.Context) error { return m.err }
func (m *mockProvider) Teardown(ctx context.Context) error  { return m.err }

// helper to call a tool and return the text result.
func callTool(t *testing.T, client interface {
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args

	result, err := client.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return result
}

func textContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("empty result content")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func TestToolsViaStdio(t *testing.T) {
	mock := &mockProvider{
		name: "mock",
		identity: &provider.Identity{
			Name:      "testuser",
			AccountID: "123456789",
			AgentName: "myagent",
		},
		agents: []provider.AgentConfig{
			{Name: "agent1", Type: provider.AgentTypeUser, GatewayPort: 18789},
			{Name: "agent2", Type: provider.AgentTypeTeam, GatewayPort: 18790, Paused: true},
		},
		agent: &provider.AgentConfig{
			Name: "agent1", Type: provider.AgentTypeUser, GatewayPort: 18789,
		},
		status: &provider.AgentStatus{
			AgentName:    "agent1",
			ServiceState: "running",
			ReadyPhase:   "ready",
			Container: provider.ContainerStatus{
				State:       "running",
				Uptime:      2 * time.Hour,
				StartedAt:   "2026-03-24T10:00:00Z",
				MemoryUsage: "512MiB",
				CPUPercent:  "2.5%",
				PIDs:        12,
			},
		},
		logs: "line1\nline2\nline3",
		secrets: []provider.SecretEntry{
			{Name: "anthropic-api-key", EnvVar: "ANTHROPIC_API_KEY", LastChanged: time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)},
		},
		execOutput: "hello world",
	}

	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	defer testSrv.Close()
	client := testSrv.Client()

	t.Run("conga_whoami", func(t *testing.T) {
		result := callTool(t, client, "conga_whoami", nil)
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		text := textContent(t, result)
		var id provider.Identity
		if err := json.Unmarshal([]byte(text), &id); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if id.Name != "testuser" {
			t.Errorf("got name %q, want %q", id.Name, "testuser")
		}
		if id.AgentName != "myagent" {
			t.Errorf("got agent_name %q, want %q", id.AgentName, "myagent")
		}
	})

	t.Run("conga_list_agents", func(t *testing.T) {
		result := callTool(t, client, "conga_list_agents", nil)
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		var agents []provider.AgentConfig
		if err := json.Unmarshal([]byte(textContent(t, result)), &agents); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(agents) != 2 {
			t.Errorf("got %d agents, want 2", len(agents))
		}
	})

	t.Run("conga_get_agent", func(t *testing.T) {
		result := callTool(t, client, "conga_get_agent", map[string]any{"agent_name": "agent1"})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if mock.lastAgentName != "agent1" {
			t.Errorf("provider received agent_name %q, want %q", mock.lastAgentName, "agent1")
		}
	})

	t.Run("conga_get_agent_missing_param", func(t *testing.T) {
		result := callTool(t, client, "conga_get_agent", nil)
		if !result.IsError {
			t.Fatal("expected error for missing agent_name")
		}
	})

	t.Run("conga_get_status", func(t *testing.T) {
		result := callTool(t, client, "conga_get_status", map[string]any{"agent_name": "agent1"})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		var status provider.AgentStatus
		if err := json.Unmarshal([]byte(textContent(t, result)), &status); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if status.ServiceState != "running" {
			t.Errorf("got service_state %q, want %q", status.ServiceState, "running")
		}
		if status.Container.MemoryUsage != "512MiB" {
			t.Errorf("got memory %q, want %q", status.Container.MemoryUsage, "512MiB")
		}
	})

	t.Run("conga_get_logs", func(t *testing.T) {
		result := callTool(t, client, "conga_get_logs", map[string]any{"agent_name": "agent1", "lines": 10})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if textContent(t, result) != "line1\nline2\nline3" {
			t.Errorf("unexpected logs: %q", textContent(t, result))
		}
		if mock.lastLogLines != 10 {
			t.Errorf("got lines %d, want 10", mock.lastLogLines)
		}
	})

	t.Run("conga_get_logs_default_lines", func(t *testing.T) {
		callTool(t, client, "conga_get_logs", map[string]any{"agent_name": "agent1"})
		if mock.lastLogLines != 50 {
			t.Errorf("default lines: got %d, want 50", mock.lastLogLines)
		}
	})

	t.Run("conga_provision_agent", func(t *testing.T) {
		result := callTool(t, client, "conga_provision_agent", map[string]any{
			"agent_name":   "newagent",
			"type":         "user",
			"channel":      "slack:U0123456789",
			"gateway_port": 18800,
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if mock.lastProvisionCfg.Name != "newagent" {
			t.Errorf("got name %q, want %q", mock.lastProvisionCfg.Name, "newagent")
		}
		if mock.lastProvisionCfg.Type != provider.AgentTypeUser {
			t.Errorf("got type %q, want %q", mock.lastProvisionCfg.Type, provider.AgentTypeUser)
		}
		if len(mock.lastProvisionCfg.Channels) != 1 || mock.lastProvisionCfg.Channels[0].ID != "U0123456789" {
			t.Errorf("got channels %v, want [{slack U0123456789}]", mock.lastProvisionCfg.Channels)
		}
		if mock.lastProvisionCfg.GatewayPort != 18800 {
			t.Errorf("got gateway_port %d, want %d", mock.lastProvisionCfg.GatewayPort, 18800)
		}
	})

	t.Run("conga_provision_agent_invalid_type", func(t *testing.T) {
		result := callTool(t, client, "conga_provision_agent", map[string]any{
			"agent_name": "bad",
			"type":       "invalid",
		})
		if !result.IsError {
			t.Fatal("expected error for invalid type")
		}
	})

	t.Run("conga_remove_agent", func(t *testing.T) {
		result := callTool(t, client, "conga_remove_agent", map[string]any{
			"agent_name":     "agent1",
			"delete_secrets": true,
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if mock.lastAgentName != "agent1" {
			t.Errorf("got name %q, want %q", mock.lastAgentName, "agent1")
		}
		if !mock.lastDeleteSecret {
			t.Error("expected delete_secrets=true")
		}
	})

	t.Run("conga_pause_agent", func(t *testing.T) {
		result := callTool(t, client, "conga_pause_agent", map[string]any{"agent_name": "agent1"})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if mock.lastAgentName != "agent1" {
			t.Errorf("got name %q, want %q", mock.lastAgentName, "agent1")
		}
	})

	t.Run("conga_unpause_agent", func(t *testing.T) {
		result := callTool(t, client, "conga_unpause_agent", map[string]any{"agent_name": "agent1"})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
	})

	t.Run("conga_refresh_agent", func(t *testing.T) {
		result := callTool(t, client, "conga_refresh_agent", map[string]any{"agent_name": "agent1"})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
	})

	t.Run("conga_refresh_all", func(t *testing.T) {
		result := callTool(t, client, "conga_refresh_all", nil)
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
	})

	t.Run("conga_container_exec", func(t *testing.T) {
		result := callTool(t, client, "conga_container_exec", map[string]any{
			"agent_name": "agent1",
			"command":    []any{"echo", "hello"},
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if textContent(t, result) != "hello world" {
			t.Errorf("got %q, want %q", textContent(t, result), "hello world")
		}
	})

	t.Run("conga_set_secret", func(t *testing.T) {
		result := callTool(t, client, "conga_set_secret", map[string]any{
			"agent_name":  "agent1",
			"secret_name": "my-key",
			"value":       "secret123",
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if mock.lastSecretName != "my-key" {
			t.Errorf("got secret_name %q, want %q", mock.lastSecretName, "my-key")
		}
		if mock.lastSecretValue != "secret123" {
			t.Errorf("got value %q, want %q", mock.lastSecretValue, "secret123")
		}
	})

	t.Run("conga_set_secret_value_file", func(t *testing.T) {
		// Write secret to a temp file.
		tmpFile := t.TempDir() + "/secret.txt"
		if err := os.WriteFile(tmpFile, []byte("file-secret-456\n"), 0600); err != nil {
			t.Fatal(err)
		}
		result := callTool(t, client, "conga_set_secret", map[string]any{
			"agent_name":  "agent1",
			"secret_name": "file-key",
			"value_file":  tmpFile,
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if mock.lastSecretValue != "file-secret-456" {
			t.Errorf("got value %q, want %q", mock.lastSecretValue, "file-secret-456")
		}
		// Temp file should be cleaned up.
		if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
			t.Error("expected temp file to be deleted")
		}
	})

	t.Run("conga_set_secret_missing_value", func(t *testing.T) {
		result := callTool(t, client, "conga_set_secret", map[string]any{
			"agent_name":  "agent1",
			"secret_name": "no-value",
		})
		if !result.IsError {
			t.Fatal("expected error when neither value nor value_file provided")
		}
	})

	t.Run("conga_list_secrets", func(t *testing.T) {
		result := callTool(t, client, "conga_list_secrets", map[string]any{"agent_name": "agent1"})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		var secrets []provider.SecretEntry
		if err := json.Unmarshal([]byte(textContent(t, result)), &secrets); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(secrets) != 1 {
			t.Fatalf("got %d secrets, want 1", len(secrets))
		}
		if secrets[0].Name != "anthropic-api-key" {
			t.Errorf("got name %q, want %q", secrets[0].Name, "anthropic-api-key")
		}
	})

	t.Run("conga_delete_secret", func(t *testing.T) {
		result := callTool(t, client, "conga_delete_secret", map[string]any{
			"agent_name":  "agent1",
			"secret_name": "old-key",
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if mock.lastSecretName != "old-key" {
			t.Errorf("got secret_name %q, want %q", mock.lastSecretName, "old-key")
		}
	})

	t.Run("conga_setup", func(t *testing.T) {
		result := callTool(t, client, "conga_setup", map[string]any{
			"image":           "ghcr.io/openclaw/openclaw:latest",
			"slack_bot_token": "xoxb-test",
		})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		if mock.lastSetupCfg.Image != "ghcr.io/openclaw/openclaw:latest" {
			t.Errorf("got image %q", mock.lastSetupCfg.Image)
		}
		if mock.lastSetupCfg.SecretValue("slack-bot-token") != "xoxb-test" {
			t.Errorf("got slack-bot-token %q", mock.lastSetupCfg.SecretValue("slack-bot-token"))
		}
	})

	t.Run("conga_cycle_host", func(t *testing.T) {
		result := callTool(t, client, "conga_cycle_host", nil)
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
	})

	t.Run("conga_teardown", func(t *testing.T) {
		result := callTool(t, client, "conga_teardown", nil)
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
	})

	t.Run("conga_connect_help", func(t *testing.T) {
		result := callTool(t, client, "conga_connect_help", map[string]any{"agent_name": "agent1"})
		if result.IsError {
			t.Fatalf("unexpected error: %s", textContent(t, result))
		}
		text := textContent(t, result)
		if !strings.Contains(text, "conga connect --agent agent1") {
			t.Errorf("expected command with agent name, got: %s", text)
		}
	})
}

func TestToolsErrorPropagation(t *testing.T) {
	mock := &mockProvider{
		name: "mock",
		err:  errors.New("provider failure"),
	}

	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	defer testSrv.Close()
	client := testSrv.Client()

	tools := []struct {
		name string
		args map[string]any
	}{
		{"conga_whoami", nil},
		{"conga_list_agents", nil},
		{"conga_get_agent", map[string]any{"agent_name": "x"}},
		{"conga_get_status", map[string]any{"agent_name": "x"}},
		{"conga_get_logs", map[string]any{"agent_name": "x"}},
		{"conga_get_proxy_logs", map[string]any{"agent_name": "x"}},
		{"conga_refresh_agent", map[string]any{"agent_name": "x"}},
		{"conga_refresh_all", nil},
		{"conga_container_exec", map[string]any{"agent_name": "x", "command": []any{"ls"}}},
		{"conga_provision_agent", map[string]any{"agent_name": "x", "type": "user"}},
		{"conga_remove_agent", map[string]any{"agent_name": "x"}},
		{"conga_pause_agent", map[string]any{"agent_name": "x"}},
		{"conga_unpause_agent", map[string]any{"agent_name": "x"}},
		{"conga_set_secret", map[string]any{"agent_name": "x", "secret_name": "k", "value": "v"}},
		{"conga_list_secrets", map[string]any{"agent_name": "x"}},
		{"conga_delete_secret", map[string]any{"agent_name": "x", "secret_name": "k"}},
		{"conga_setup", nil},
		{"conga_cycle_host", nil},
		{"conga_teardown", nil},
		{"conga_channels_add", map[string]any{"platform": "slack", "slack_bot_token": "x", "slack_signing_secret": "x"}},
		{"conga_channels_remove", map[string]any{"platform": "slack"}},
		{"conga_channels_list", nil},
		{"conga_channels_bind", map[string]any{"agent_name": "x", "channel": "slack:U0123456789"}},
		{"conga_channels_unbind", map[string]any{"agent_name": "x", "platform": "slack"}},
	}

	for _, tt := range tools {
		t.Run(tt.name, func(t *testing.T) {
			result := callTool(t, client, tt.name, tt.args)
			if !result.IsError {
				t.Errorf("expected error result for %s", tt.name)
			}
			text := textContent(t, result)
			if !strings.Contains(text, "provider failure") {
				t.Errorf("got error %q, want it to contain %q", text, "provider failure")
			}
		})
	}
}
