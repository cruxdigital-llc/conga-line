package runtime_test

import (
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"

	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/hermes"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

func TestIsMCPOAuthSecret(t *testing.T) {
	cases := map[string]bool{
		"mcp-oauth/linear-4cca6302a658efcc.json": true,
		"mcp-oauth/github-336ff6f3750dcf7c.json": true,
		"anthropic-api-key":                      false,
		"trello-token":                           false,
		"mcp-oauth":                              false, // prefix requires the trailing slash
		"":                                       false,
	}
	for name, want := range cases {
		if got := runtime.IsMCPOAuthSecret(name); got != want {
			t.Errorf("IsMCPOAuthSecret(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestOAuthStateDir_PerRuntime(t *testing.T) {
	cases := []struct {
		runtime runtime.RuntimeName
		want    string
	}{
		{runtime.RuntimeOpenClaw, "mcp-oauth"},
		{runtime.RuntimeHermes, ""},
	}
	for _, tc := range cases {
		rt, err := runtime.Get(tc.runtime)
		if err != nil {
			t.Fatalf("Get(%q): %v", tc.runtime, err)
		}
		if got := rt.OAuthStateDir(); got != tc.want {
			t.Errorf("%s OAuthStateDir() = %q, want %q", tc.runtime, got, tc.want)
		}
	}
}

// TestGenerateEnvFile_SkipsMCPOAuthSecrets is the key S1 guarantee: an MCP OAuth
// blob stored as a per-agent secret must NEVER be emitted into the env file
// (it's a file to materialize, and its value is a live token blob).
func TestGenerateEnvFile_SkipsMCPOAuthSecrets(t *testing.T) {
	for _, rtName := range []runtime.RuntimeName{runtime.RuntimeOpenClaw, runtime.RuntimeHermes} {
		rt, err := runtime.Get(rtName)
		if err != nil {
			t.Fatalf("Get(%q): %v", rtName, err)
		}
		blob := `{"tokens":{"access_token":"SECRET-DO-NOT-LEAK"}}`
		env := string(rt.GenerateEnvFile(runtime.EnvParams{
			Agent: provider.AgentConfig{Name: "team-a", Type: "team", GatewayPort: 18789},
			PerAgent: map[string]string{
				"anthropic-api-key":                      "sk-normal",
				"mcp-oauth/linear-4cca6302a658efcc.json": blob,
			},
		}))
		if !strings.Contains(env, "ANTHROPIC_API_KEY=sk-normal") {
			t.Errorf("%s: expected normal secret in env file, got:\n%s", rtName, env)
		}
		if strings.Contains(env, "SECRET-DO-NOT-LEAK") || strings.Contains(env, "mcp-oauth") || strings.Contains(env, "MCP_OAUTH") {
			t.Errorf("%s: MCP OAuth blob leaked into env file:\n%s", rtName, env)
		}
	}
}
