package runtime_test

import (
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"

	// Register runtimes
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/hermes"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"

	// Register channels (needed for config generation)
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
)

// testRuntimeContract runs shared tests that every Runtime must pass.
func testRuntimeContract(t *testing.T, rt runtime.Runtime) {
	t.Helper()

	t.Run("Name is non-empty", func(t *testing.T) {
		if rt.Name() == "" {
			t.Fatal("Name() must return a non-empty string")
		}
	})

	t.Run("ConfigFileName is non-empty", func(t *testing.T) {
		if rt.ConfigFileName() == "" {
			t.Fatal("ConfigFileName() must return a non-empty string")
		}
	})

	t.Run("GenerateConfig returns valid bytes", func(t *testing.T) {
		params := runtime.ConfigParams{
			Agent: provider.AgentConfig{
				Name:        "test-agent",
				Type:        "user",
				GatewayPort: 18789,
			},
			Secrets:      provider.SharedSecrets{Values: map[string]string{}},
			GatewayToken: "test-token-123",
		}
		data, err := rt.GenerateConfig(params)
		if err != nil {
			t.Fatalf("GenerateConfig() error: %v", err)
		}
		if len(data) == 0 {
			t.Fatal("GenerateConfig() returned empty bytes")
		}
	})

	t.Run("GenerateEnvFile returns bytes", func(t *testing.T) {
		params := runtime.EnvParams{
			Agent: provider.AgentConfig{
				Name: "test-agent",
				Type: "user",
			},
			Secrets:  provider.SharedSecrets{Values: map[string]string{}},
			PerAgent: map[string]string{"anthropic-api-key": "sk-test"},
		}
		data := rt.GenerateEnvFile(params)
		if len(data) == 0 {
			t.Fatal("GenerateEnvFile() returned empty bytes")
		}
		// Should contain the per-agent secret
		if got := string(data); !strings.Contains(got, "ANTHROPIC_API_KEY=sk-test") {
			t.Fatalf("GenerateEnvFile() missing per-agent secret; got:\n%s", got)
		}
	})

	t.Run("ContainerSpec has valid port", func(t *testing.T) {
		spec := rt.ContainerSpec(provider.AgentConfig{Name: "test", GatewayPort: 18789})
		if spec.ContainerPort <= 0 {
			t.Fatalf("ContainerSpec.ContainerPort must be > 0, got %d", spec.ContainerPort)
		}
	})

	t.Run("ContainerSpec has valid user", func(t *testing.T) {
		spec := rt.ContainerSpec(provider.AgentConfig{Name: "test", GatewayPort: 18789})
		if spec.User == "" {
			t.Fatal("ContainerSpec.User must not be empty")
		}
	})

	t.Run("ContainerSpec has valid memory", func(t *testing.T) {
		spec := rt.ContainerSpec(provider.AgentConfig{Name: "test", GatewayPort: 18789})
		if spec.Memory == "" {
			t.Fatal("ContainerSpec.Memory must not be empty")
		}
	})

	t.Run("CreateDirectories creates expected structure", func(t *testing.T) {
		dir := t.TempDir()
		if err := rt.CreateDirectories(dir); err != nil {
			t.Fatalf("CreateDirectories() error: %v", err)
		}
	})

	t.Run("ContainerDataPath is non-empty", func(t *testing.T) {
		if rt.ContainerDataPath() == "" {
			t.Fatal("ContainerDataPath() must not be empty")
		}
	})

	t.Run("WorkspacePath is non-empty", func(t *testing.T) {
		if rt.WorkspacePath() == "" {
			t.Fatal("WorkspacePath() must not be empty")
		}
	})

	t.Run("DetectReady returns valid phase", func(t *testing.T) {
		phase := rt.DetectReady("", false)
		if phase.Phase == "" {
			t.Fatal("DetectReady() must return a non-empty Phase")
		}
	})

	t.Run("HealthEndpoint returns empty or valid path", func(t *testing.T) {
		ep := rt.HealthEndpoint()
		if ep != "" && ep[0] != '/' {
			t.Fatalf("HealthEndpoint must start with / or be empty, got %q", ep)
		}
	})

	t.Run("ReadGatewayToken round-trips with GenerateConfig", func(t *testing.T) {
		params := runtime.ConfigParams{
			Agent: provider.AgentConfig{
				Name:        "test",
				Type:        "user",
				GatewayPort: 18789,
			},
			Secrets:      provider.SharedSecrets{Values: map[string]string{}},
			GatewayToken: "round-trip-token-abc",
		}
		data, err := rt.GenerateConfig(params)
		if err != nil {
			t.Fatalf("GenerateConfig() error: %v", err)
		}
		got := rt.ReadGatewayToken(data)
		if got != "round-trip-token-abc" {
			t.Fatalf("ReadGatewayToken() = %q, want %q", got, "round-trip-token-abc")
		}
	})

	t.Run("WebhookPath returns valid path for slack", func(t *testing.T) {
		path := rt.WebhookPath("slack")
		if path == "" {
			t.Fatal("WebhookPath(\"slack\") must return a non-empty path")
		}
		if path[0] != '/' {
			t.Fatalf("WebhookPath must start with /, got %q", path)
		}
	})
}

func TestOpenClaw_RuntimeContract(t *testing.T) {
	rt, err := runtime.Get(runtime.RuntimeOpenClaw)
	if err != nil {
		t.Fatalf("Get(RuntimeOpenClaw) error: %v", err)
	}
	testRuntimeContract(t, rt)
}

func TestHermes_RuntimeContract(t *testing.T) {
	rt, err := runtime.Get(runtime.RuntimeHermes)
	if err != nil {
		t.Fatalf("Get(RuntimeHermes) error: %v", err)
	}
	testRuntimeContract(t, rt)
}

func TestResolveRuntime(t *testing.T) {
	tests := []struct {
		agent, global string
		want          runtime.RuntimeName
	}{
		{"hermes", "", runtime.RuntimeHermes},
		{"", "hermes", runtime.RuntimeHermes},
		{"openclaw", "hermes", runtime.RuntimeOpenClaw}, // agent takes precedence
		{"", "", runtime.RuntimeOpenClaw},               // default
	}
	for _, tt := range tests {
		got := runtime.ResolveRuntime(tt.agent, tt.global)
		if got != tt.want {
			t.Errorf("ResolveRuntime(%q, %q) = %q, want %q", tt.agent, tt.global, got, tt.want)
		}
	}
}

func TestRegistry(t *testing.T) {
	names := runtime.Names()
	if len(names) < 2 {
		t.Fatalf("Expected at least 2 registered runtimes, got %d: %v", len(names), names)
	}

	// Verify both are registered
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["openclaw"] {
		t.Error("openclaw runtime not registered")
	}
	if !found["hermes"] {
		t.Error("hermes runtime not registered")
	}
}

func TestOpenClaw_Specifics(t *testing.T) {
	rt, _ := runtime.Get(runtime.RuntimeOpenClaw)

	t.Run("DefaultImage returns openclaw image", func(t *testing.T) {
		if got := rt.DefaultImage(); !strings.Contains(got, "openclaw") {
			t.Fatalf("DefaultImage() = %q, want to contain 'openclaw'", got)
		}
	})

	t.Run("SupportsNodeProxy is true", func(t *testing.T) {
		if !rt.SupportsNodeProxy() {
			t.Fatal("OpenClaw should support Node proxy")
		}
	})

	t.Run("ContainerPort is 18789", func(t *testing.T) {
		spec := rt.ContainerSpec(provider.AgentConfig{Name: "test", GatewayPort: 18789})
		if spec.ContainerPort != 18789 {
			t.Fatalf("ContainerPort = %d, want 18789", spec.ContainerPort)
		}
	})

	t.Run("WebhookPath is /slack/events", func(t *testing.T) {
		if got := rt.WebhookPath("slack"); got != "/slack/events" {
			t.Fatalf("WebhookPath(slack) = %q, want /slack/events", got)
		}
	})

	t.Run("ConfigFileName is openclaw.json", func(t *testing.T) {
		if got := rt.ConfigFileName(); got != "openclaw.json" {
			t.Fatalf("ConfigFileName() = %q, want openclaw.json", got)
		}
	})

	t.Run("Config includes Slack channel config when credentials present", func(t *testing.T) {
		params := runtime.ConfigParams{
			Agent: provider.AgentConfig{
				Name:        "test",
				Type:        "user",
				GatewayPort: 18789,
				Channels: []channels.ChannelBinding{
					{Platform: "slack", ID: "U0123456789"},
				},
			},
			Secrets: provider.SharedSecrets{
				Values: map[string]string{
					"slack-bot-token":      "xoxb-test",
					"slack-signing-secret": "test-secret",
				},
			},
		}
		data, err := rt.GenerateConfig(params)
		if err != nil {
			t.Fatalf("GenerateConfig() error: %v", err)
		}
		cfg := string(data)
		if !strings.Contains(cfg, "slack") {
			t.Fatal("Config should contain slack channel config")
		}
		if !strings.Contains(cfg, "allowFrom") {
			t.Fatal("Config should contain allowFrom for user agent")
		}
	})
}

func TestHermes_Specifics(t *testing.T) {
	rt, _ := runtime.Get(runtime.RuntimeHermes)

	t.Run("DefaultImage is hermes-agent", func(t *testing.T) {
		if got := rt.DefaultImage(); !strings.Contains(got, "hermes-agent") {
			t.Fatalf("DefaultImage() = %q, want to contain 'hermes-agent'", got)
		}
	})

	t.Run("SupportsNodeProxy is false", func(t *testing.T) {
		if rt.SupportsNodeProxy() {
			t.Fatal("Hermes should not support Node proxy")
		}
	})

	t.Run("ContainerPort is 8642", func(t *testing.T) {
		spec := rt.ContainerSpec(provider.AgentConfig{Name: "test", GatewayPort: 18789})
		if spec.ContainerPort != 8642 {
			t.Fatalf("ContainerPort = %d, want 8642", spec.ContainerPort)
		}
	})

	t.Run("WebhookPath is /webhooks/slack", func(t *testing.T) {
		if got := rt.WebhookPath("slack"); got != "/webhooks/slack" {
			t.Fatalf("WebhookPath(slack) = %q, want /webhooks/slack", got)
		}
	})

	t.Run("ConfigFileName is config.yaml", func(t *testing.T) {
		if got := rt.ConfigFileName(); got != "config.yaml" {
			t.Fatalf("ConfigFileName() = %q, want config.yaml", got)
		}
	})

	t.Run("Config is valid YAML with api_server", func(t *testing.T) {
		params := runtime.ConfigParams{
			Agent: provider.AgentConfig{
				Name:        "test",
				Type:        "user",
				GatewayPort: 18789,
			},
			Secrets:      provider.SharedSecrets{Values: map[string]string{}},
			GatewayToken: "test-token",
		}
		data, err := rt.GenerateConfig(params)
		if err != nil {
			t.Fatalf("GenerateConfig() error: %v", err)
		}
		cfg := string(data)
		if !strings.Contains(cfg, "api_server") {
			t.Fatal("Config should contain api_server section")
		}
		if !strings.Contains(cfg, "test-token") {
			t.Fatal("Config should contain the gateway token")
		}
		// Model should NOT be in config when not provided in params
		if strings.Contains(cfg, "claude") {
			t.Fatal("Config should not include a model when params.Model is empty")
		}
	})

	t.Run("EnvFile excludes NODE_OPTIONS", func(t *testing.T) {
		params := runtime.EnvParams{
			Agent:    provider.AgentConfig{Name: "test"},
			Secrets:  provider.SharedSecrets{Values: map[string]string{}},
			PerAgent: map[string]string{"anthropic-api-key": "sk-test"},
		}
		data := rt.GenerateEnvFile(params)
		if strings.Contains(string(data), "NODE_OPTIONS") {
			t.Fatal("Hermes env file should NOT contain NODE_OPTIONS")
		}
	})
}
