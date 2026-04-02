package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidManifest(t *testing.T) {
	content := `
apiVersion: conga.dev/v1alpha1
kind: Environment
setup:
  image: "ghcr.io/openclaw/openclaw:2026.3.11"
  ssh_host: "demo.example.com"
  ssh_user: "ubuntu"
  install_docker: true
agents:
  - name: aaron
    type: user
    secrets:
      anthropic-api-key: "sk-test"
  - name: team
    type: team
channels:
  - platform: slack
    secrets:
      slack-bot-token: "xoxb-test"
    bindings:
      - agent: aaron
        id: "U0123456789"
      - agent: team
        id: "C0123456789"
policy:
  egress:
    mode: enforce
    allowed_domains:
      - "api.anthropic.com"
`
	path := writeTempFile(t, "manifest.yaml", content)
	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if m.APIVersion != "conga.dev/v1alpha1" {
		t.Errorf("APIVersion = %q, want conga.dev/v1alpha1", m.APIVersion)
	}
	if m.Kind != "Environment" {
		t.Errorf("Kind = %q, want Environment", m.Kind)
	}
	if m.Setup == nil {
		t.Fatal("Setup is nil")
	}
	if m.Setup.Image != "ghcr.io/openclaw/openclaw:2026.3.11" {
		t.Errorf("Setup.Image = %q", m.Setup.Image)
	}
	if m.Setup.SSHHost != "demo.example.com" {
		t.Errorf("Setup.SSHHost = %q", m.Setup.SSHHost)
	}
	if !m.Setup.InstallDocker {
		t.Error("Setup.InstallDocker = false")
	}
	if len(m.Agents) != 2 {
		t.Fatalf("len(Agents) = %d, want 2", len(m.Agents))
	}
	if m.Agents[0].Name != "aaron" || m.Agents[0].Type != "user" {
		t.Errorf("Agent[0] = %+v", m.Agents[0])
	}
	if m.Agents[0].Secrets["anthropic-api-key"] != "sk-test" {
		t.Errorf("Agent[0].Secrets = %v", m.Agents[0].Secrets)
	}
	if len(m.Channels) != 1 {
		t.Fatalf("len(Channels) = %d, want 1", len(m.Channels))
	}
	if len(m.Channels[0].Bindings) != 2 {
		t.Fatalf("len(Bindings) = %d, want 2", len(m.Channels[0].Bindings))
	}
	if m.Policy == nil || m.Policy.Egress == nil {
		t.Fatal("Policy.Egress is nil")
	}
	if len(m.Policy.Egress.AllowedDomains) != 1 {
		t.Errorf("AllowedDomains = %v", m.Policy.Egress.AllowedDomains)
	}
}

func TestLoad_MinimalManifest(t *testing.T) {
	content := `
apiVersion: conga.dev/v1alpha1
kind: Environment
`
	path := writeTempFile(t, "minimal.yaml", content)
	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if m.Setup != nil {
		t.Error("Setup should be nil")
	}
	if len(m.Agents) != 0 {
		t.Errorf("Agents should be empty, got %d", len(m.Agents))
	}
	if len(m.Channels) != 0 {
		t.Errorf("Channels should be empty, got %d", len(m.Channels))
	}
	if m.Policy != nil {
		t.Error("Policy should be nil")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/manifest.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeTempFile(t, "bad.yaml", "{{not yaml}}")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidate_BadAPIVersion(t *testing.T) {
	m := &Manifest{APIVersion: "v2", Kind: "Environment"}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected error for bad apiVersion")
	}
	assertContains(t, err.Error(), "unsupported apiVersion")
}

func TestValidate_BadKind(t *testing.T) {
	m := &Manifest{APIVersion: supportedAPIVersion, Kind: "Cluster"}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected error for bad kind")
	}
	assertContains(t, err.Error(), "unsupported kind")
}

func TestValidate_InvalidAgentName(t *testing.T) {
	m := &Manifest{
		APIVersion: supportedAPIVersion,
		Kind:       supportedKind,
		Agents:     []ManifestAgent{{Name: "Bad Name!", Type: "user"}},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected error for invalid agent name")
	}
}

func TestValidate_DuplicateAgentNames(t *testing.T) {
	m := &Manifest{
		APIVersion: supportedAPIVersion,
		Kind:       supportedKind,
		Agents: []ManifestAgent{
			{Name: "aaron", Type: "user"},
			{Name: "aaron", Type: "team"},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected error for duplicate agent names")
	}
	assertContains(t, err.Error(), "duplicate agent name")
}

func TestValidate_InvalidAgentType(t *testing.T) {
	m := &Manifest{
		APIVersion: supportedAPIVersion,
		Kind:       supportedKind,
		Agents:     []ManifestAgent{{Name: "test", Type: "admin"}},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected error for invalid agent type")
	}
	assertContains(t, err.Error(), "invalid type")
}

func TestValidate_DuplicatePlatform(t *testing.T) {
	m := &Manifest{
		APIVersion: supportedAPIVersion,
		Kind:       supportedKind,
		Channels: []ManifestChannel{
			{Platform: "slack"},
			{Platform: "slack"},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected error for duplicate platform")
	}
	assertContains(t, err.Error(), "duplicate channel platform")
}

func TestValidate_BindingMissingAgent(t *testing.T) {
	m := &Manifest{
		APIVersion: supportedAPIVersion,
		Kind:       supportedKind,
		Agents:     []ManifestAgent{{Name: "aaron", Type: "user"}},
		Channels: []ManifestChannel{
			{
				Platform: "slack",
				Bindings: []ManifestBinding{{Agent: "bob", ID: "U0123"}},
			},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected error for binding referencing missing agent")
	}
	assertContains(t, err.Error(), "not in agents list")
}

func TestValidate_BindingMissingID(t *testing.T) {
	m := &Manifest{
		APIVersion: supportedAPIVersion,
		Kind:       supportedKind,
		Agents:     []ManifestAgent{{Name: "aaron", Type: "user"}},
		Channels: []ManifestChannel{
			{
				Platform: "slack",
				Bindings: []ManifestBinding{{Agent: "aaron", ID: ""}},
			},
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected error for binding with empty ID")
	}
	assertContains(t, err.Error(), "id is required")
}

func TestValidate_EmptyManifest(t *testing.T) {
	m := &Manifest{APIVersion: supportedAPIVersion, Kind: supportedKind}
	if err := Validate(m); err != nil {
		t.Fatalf("empty manifest should be valid: %v", err)
	}
}

func TestValidate_EmptyPlatform(t *testing.T) {
	m := &Manifest{
		APIVersion: supportedAPIVersion,
		Kind:       supportedKind,
		Channels:   []ManifestChannel{{Platform: ""}},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected error for empty platform")
	}
	assertContains(t, err.Error(), "must not be empty")
}

func TestExpandSecrets_EnvVar(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-secret-123")
	m := &Manifest{
		Agents: []ManifestAgent{
			{Name: "a", Secrets: map[string]string{"api-key": "$TEST_API_KEY"}},
		},
	}
	if err := ExpandSecrets(m); err != nil {
		t.Fatalf("ExpandSecrets failed: %v", err)
	}
	if m.Agents[0].Secrets["api-key"] != "sk-secret-123" {
		t.Errorf("got %q, want sk-secret-123", m.Agents[0].Secrets["api-key"])
	}
}

func TestExpandSecrets_BracketSyntax(t *testing.T) {
	t.Setenv("TEST_BRACKET", "bracket-value")
	m := &Manifest{
		Agents: []ManifestAgent{
			{Name: "a", Secrets: map[string]string{"key": "${TEST_BRACKET}"}},
		},
	}
	if err := ExpandSecrets(m); err != nil {
		t.Fatalf("ExpandSecrets failed: %v", err)
	}
	if m.Agents[0].Secrets["key"] != "bracket-value" {
		t.Errorf("got %q, want bracket-value", m.Agents[0].Secrets["key"])
	}
}

func TestExpandSecrets_MissingVar(t *testing.T) {
	// Ensure the var is not set
	os.Unsetenv("MISSING_VAR_12345")
	m := &Manifest{
		Agents: []ManifestAgent{
			{Name: "a", Secrets: map[string]string{"key": "$MISSING_VAR_12345"}},
		},
	}
	err := ExpandSecrets(m)
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
	assertContains(t, err.Error(), "MISSING_VAR_12345")
	assertContains(t, err.Error(), "not set")
}

func TestExpandSecrets_LiteralValue(t *testing.T) {
	m := &Manifest{
		Agents: []ManifestAgent{
			{Name: "a", Secrets: map[string]string{"key": "literal-value"}},
		},
	}
	if err := ExpandSecrets(m); err != nil {
		t.Fatalf("ExpandSecrets failed: %v", err)
	}
	if m.Agents[0].Secrets["key"] != "literal-value" {
		t.Errorf("got %q, want literal-value", m.Agents[0].Secrets["key"])
	}
}

func TestExpandSecrets_MultipleVars(t *testing.T) {
	t.Setenv("TEST_KEY_A", "val-a")
	t.Setenv("TEST_KEY_B", "val-b")
	m := &Manifest{
		Agents: []ManifestAgent{
			{Name: "a", Secrets: map[string]string{
				"key-a": "$TEST_KEY_A",
				"key-b": "$TEST_KEY_B",
			}},
		},
	}
	if err := ExpandSecrets(m); err != nil {
		t.Fatalf("ExpandSecrets failed: %v", err)
	}
	if m.Agents[0].Secrets["key-a"] != "val-a" {
		t.Errorf("key-a = %q", m.Agents[0].Secrets["key-a"])
	}
	if m.Agents[0].Secrets["key-b"] != "val-b" {
		t.Errorf("key-b = %q", m.Agents[0].Secrets["key-b"])
	}
}

func TestExpandSecrets_ChannelSecrets(t *testing.T) {
	t.Setenv("TEST_BOT_TOKEN", "xoxb-test")
	m := &Manifest{
		Channels: []ManifestChannel{
			{Platform: "slack", Secrets: map[string]string{"bot-token": "$TEST_BOT_TOKEN"}},
		},
	}
	if err := ExpandSecrets(m); err != nil {
		t.Fatalf("ExpandSecrets failed: %v", err)
	}
	if m.Channels[0].Secrets["bot-token"] != "xoxb-test" {
		t.Errorf("got %q", m.Channels[0].Secrets["bot-token"])
	}
}

func TestExpandSecrets_MidStringVar(t *testing.T) {
	t.Setenv("TEST_TOKEN", "abc123")
	m := &Manifest{
		Agents: []ManifestAgent{
			{Name: "a", Secrets: map[string]string{"key": "Bearer $TEST_TOKEN"}},
		},
	}
	if err := ExpandSecrets(m); err != nil {
		t.Fatalf("ExpandSecrets failed: %v", err)
	}
	if m.Agents[0].Secrets["key"] != "Bearer abc123" {
		t.Errorf("got %q, want \"Bearer abc123\"", m.Agents[0].Secrets["key"])
	}
}

func TestExpandSecrets_NoAgentsOrChannels(t *testing.T) {
	m := &Manifest{}
	if err := ExpandSecrets(m); err != nil {
		t.Fatalf("ExpandSecrets failed on empty manifest: %v", err)
	}
}

func TestLoadEnvFile_Valid(t *testing.T) {
	content := "KEY1=value1\nKEY2=value2\n# comment\n\nKEY3=value3"
	path := writeTempFile(t, "test.env", content)
	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile failed: %v", err)
	}
	if v := os.Getenv("KEY1"); v != "value1" {
		t.Errorf("KEY1 = %q", v)
	}
	if v := os.Getenv("KEY2"); v != "value2" {
		t.Errorf("KEY2 = %q", v)
	}
	if v := os.Getenv("KEY3"); v != "value3" {
		t.Errorf("KEY3 = %q", v)
	}
}

func TestLoadEnvFile_MalformedLine(t *testing.T) {
	content := "GOOD=value\nBAD LINE NO EQUALS"
	path := writeTempFile(t, "bad.env", content)
	err := LoadEnvFile(path)
	if err == nil {
		t.Fatal("expected error for malformed line")
	}
	assertContains(t, err.Error(), "missing '='")
}

func TestLoadEnvFile_EmptyKey(t *testing.T) {
	content := "=value"
	path := writeTempFile(t, "emptykey.env", content)
	err := LoadEnvFile(path)
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	assertContains(t, err.Error(), "empty key")
}

func TestLoadEnvFile_QuotedValues(t *testing.T) {
	content := "DQ=\"double quoted\"\nSQ='single quoted'\nNQ=no quotes"
	path := writeTempFile(t, "quoted.env", content)
	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile failed: %v", err)
	}
	if v := os.Getenv("DQ"); v != "double quoted" {
		t.Errorf("DQ = %q, want \"double quoted\"", v)
	}
	if v := os.Getenv("SQ"); v != "single quoted" {
		t.Errorf("SQ = %q, want \"single quoted\"", v)
	}
	if v := os.Getenv("NQ"); v != "no quotes" {
		t.Errorf("NQ = %q, want \"no quotes\"", v)
	}
}

func TestLoadEnvFile_FileNotFound(t *testing.T) {
	err := LoadEnvFile("/nonexistent/path/test.env")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// --- helpers ---

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !contains(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
