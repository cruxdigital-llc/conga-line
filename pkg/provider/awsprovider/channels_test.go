package awsprovider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmTypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	awsutil "github.com/cruxdigital-llc/conga-line/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"

	// Register slack channel so channels.Get("slack") works
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
)

// mockSecretsManager implements awsutil.SecretsManagerClient for testing.
type mockSecretsManager struct {
	awsutil.SecretsManagerClient
	secrets map[string]string
}

func (m *mockSecretsManager) GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	val, ok := m.secrets[aws.ToString(params.SecretId)]
	if !ok {
		return nil, &smtypes.ResourceNotFoundException{Message: aws.String("not found")}
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(val)}, nil
}

func (m *mockSecretsManager) PutSecretValue(ctx context.Context, params *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	m.secrets[aws.ToString(params.SecretId)] = aws.ToString(params.SecretString)
	return &secretsmanager.PutSecretValueOutput{}, nil
}

func (m *mockSecretsManager) CreateSecret(ctx context.Context, params *secretsmanager.CreateSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error) {
	m.secrets[aws.ToString(params.Name)] = aws.ToString(params.SecretString)
	return &secretsmanager.CreateSecretOutput{}, nil
}

func (m *mockSecretsManager) DeleteSecret(ctx context.Context, params *secretsmanager.DeleteSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error) {
	delete(m.secrets, aws.ToString(params.SecretId))
	return &secretsmanager.DeleteSecretOutput{}, nil
}

func (m *mockSecretsManager) ListSecrets(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	var entries []smtypes.SecretListEntry
	for name := range m.secrets {
		entries = append(entries, smtypes.SecretListEntry{Name: aws.String(name)})
	}
	return &secretsmanager.ListSecretsOutput{SecretList: entries}, nil
}

// mockSSMForChannels extends mockSSM with GetParametersByPath for ListAgents.
type mockSSMForChannels struct {
	awsutil.SSMClient
	stored map[string]string
}

func (m *mockSSMForChannels) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	val, ok := m.stored[aws.ToString(params.Name)]
	if !ok {
		return nil, &ssmTypes.ParameterNotFound{}
	}
	return &ssm.GetParameterOutput{
		Parameter: &ssmTypes.Parameter{Value: aws.String(val)},
	}, nil
}

func (m *mockSSMForChannels) PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	m.stored[aws.ToString(params.Name)] = aws.ToString(params.Value)
	return &ssm.PutParameterOutput{}, nil
}

func (m *mockSSMForChannels) GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	prefix := aws.ToString(params.Path)
	var parameters []ssmTypes.Parameter
	for name, val := range m.stored {
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			parameters = append(parameters, ssmTypes.Parameter{
				Name:  aws.String(name),
				Value: aws.String(val),
			})
		}
	}
	return &ssm.GetParametersByPathOutput{Parameters: parameters}, nil
}

func TestSaveAgentToSSM_WithChannels(t *testing.T) {
	mock := &mockSSMForChannels{stored: make(map[string]string)}
	p := &AWSProvider{clients: &awsutil.Clients{SSM: mock}}

	agent := provider.AgentConfig{
		Name:        "testuser",
		Type:        "user",
		Channels:    []channels.ChannelBinding{{Platform: "slack", ID: "U123"}},
		GatewayPort: 18789,
		IAMIdentity: "testiam",
	}

	if err := p.saveAgentToSSM(context.Background(), agent); err != nil {
		t.Fatalf("saveAgentToSSM error: %v", err)
	}

	stored, ok := mock.stored["/conga/agents/testuser"]
	if !ok {
		t.Fatal("expected agent config in SSM")
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(stored), &parsed); err != nil {
		t.Fatalf("failed to parse stored JSON: %v", err)
	}

	if parsed["type"] != "user" {
		t.Errorf("expected type=user, got %v", parsed["type"])
	}

	chans, ok := parsed["channels"].([]any)
	if !ok || len(chans) != 1 {
		t.Fatalf("expected 1 channel, got %v", parsed["channels"])
	}

	ch := chans[0].(map[string]any)
	if ch["platform"] != "slack" || ch["id"] != "U123" {
		t.Errorf("unexpected channel: %v", ch)
	}
}

func TestSaveAgentToSSM_WithoutChannels(t *testing.T) {
	mock := &mockSSMForChannels{stored: make(map[string]string)}
	p := &AWSProvider{clients: &awsutil.Clients{SSM: mock}}

	agent := provider.AgentConfig{
		Name:        "solo",
		Type:        "user",
		GatewayPort: 18789,
	}

	if err := p.saveAgentToSSM(context.Background(), agent); err != nil {
		t.Fatalf("saveAgentToSSM error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(mock.stored["/conga/agents/solo"]), &parsed); err != nil {
		t.Fatalf("failed to parse stored JSON: %v", err)
	}

	// Channels should be null/empty, not missing (SSM agent format expects the field)
	if parsed["type"] != "user" {
		t.Errorf("expected type=user, got %v", parsed["type"])
	}
}

func TestReadSharedSecrets_ReadsFromSecretsManager(t *testing.T) {
	mock := &mockSecretsManager{secrets: map[string]string{
		"conga/shared/slack-bot-token":      "xoxb-test",
		"conga/shared/slack-signing-secret": "test-signing",
		"conga/shared/slack-app-token":      "xapp-test",
		"conga/shared/google-client-id":     "google-id",
		"conga/shared/google-client-secret": "google-secret",
	}}
	p := &AWSProvider{clients: &awsutil.Clients{SecretsManager: mock}}

	shared, err := p.readSharedSecrets(context.Background())
	if err != nil {
		t.Fatalf("readSharedSecrets error: %v", err)
	}

	if shared.Values["slack-bot-token"] != "xoxb-test" {
		t.Errorf("expected slack-bot-token=xoxb-test, got %s", shared.Values["slack-bot-token"])
	}
	if shared.Values["slack-signing-secret"] != "test-signing" {
		t.Errorf("expected slack-signing-secret=test-signing, got %s", shared.Values["slack-signing-secret"])
	}
	if shared.GoogleClientID != "google-id" {
		t.Errorf("expected google-client-id=google-id, got %s", shared.GoogleClientID)
	}
}

func TestReadSharedSecrets_IgnoresReplaceMe(t *testing.T) {
	mock := &mockSecretsManager{secrets: map[string]string{
		"conga/shared/slack-bot-token": "REPLACE_ME",
	}}
	p := &AWSProvider{clients: &awsutil.Clients{SecretsManager: mock}}

	shared, err := p.readSharedSecrets(context.Background())
	if err != nil {
		t.Fatalf("readSharedSecrets error: %v", err)
	}

	if _, ok := shared.Values["slack-bot-token"]; ok {
		t.Error("expected REPLACE_ME values to be excluded")
	}
}

func TestReadSharedSecrets_IgnoresMissing(t *testing.T) {
	mock := &mockSecretsManager{secrets: map[string]string{}}
	p := &AWSProvider{clients: &awsutil.Clients{SecretsManager: mock}}

	shared, err := p.readSharedSecrets(context.Background())
	if err != nil {
		t.Fatalf("readSharedSecrets error: %v", err)
	}

	if len(shared.Values) != 0 {
		t.Errorf("expected empty values, got %d", len(shared.Values))
	}
}

func TestReadAgentSecrets_ReadsFromSecretsManager(t *testing.T) {
	mock := &mockSecretsManager{secrets: map[string]string{
		"conga/agents/user-a/anthropic-api-key": "sk-test",
		"conga/agents/user-a/trello-api-key":    "trello-key",
	}}
	p := &AWSProvider{clients: &awsutil.Clients{SecretsManager: mock}}

	secrets, err := p.readAgentSecrets(context.Background(), "user-a")
	if err != nil {
		t.Fatalf("readAgentSecrets error: %v", err)
	}

	if secrets["anthropic-api-key"] != "sk-test" {
		t.Errorf("expected anthropic-api-key=sk-test, got %s", secrets["anthropic-api-key"])
	}
	if secrets["trello-api-key"] != "trello-key" {
		t.Errorf("expected trello-api-key=trello-key, got %s", secrets["trello-api-key"])
	}
}

// TestRouterRestartScriptUsesSlackPath is a regression guard. restartRouterOnInstance
// once ran `npm install` against /opt/conga/router (which has no package.json after
// the router source moved to .../slack in the slack/telegram split), so the install
// failed under `set -e` *after* the router was already stopped — silently breaking
// the router on every agent refresh / channel bind. The dep-check, the npm-install
// mount, and the run-step mount must all target /opt/conga/router/slack.
func TestRouterRestartScriptUsesSlackPath(t *testing.T) {
	s := routerRestartScript
	if strings.Contains(s, "-v /opt/conga/router:/app") {
		t.Error("router script mounts parent /opt/conga/router (pre-split path); must use /opt/conga/router/slack")
	}
	for _, want := range []string{
		"[ ! -d /opt/conga/router/slack/node_modules ]",
		"-v /opt/conga/router/slack:/app -w /app node:22-alpine npm install",
		"-v /opt/conga/router/slack:/app:ro",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("router script missing expected fragment: %q", want)
		}
	}

	// Host-networking topology (specs/2026-06-11_bugfix_router-host-networking):
	// the router runs --network host and reaches each agent via its published
	// 127.0.0.1:<hostPort>. It must NOT attach to per-agent bridge networks —
	// that attach is impossible on Docker 25 + kernel 6.1.174 (route conflict).
	if !strings.Contains(s, "--network host") {
		t.Error("router script must run the router with --network host (loopback delivery topology)")
	}
	if strings.Contains(s, "docker network connect") {
		t.Error("router script must NOT attach the router to per-agent bridge networks; delivery is via published loopback ports")
	}
}

// TestDeleteSecretUsesAgentScopedPath locks the per-agent secret path contract used
// to purge a credential, and that it touches only the named secret. NOTE: removing a
// secret from tfvars currently leaves it orphaned in Secrets Manager because the
// conga_secret resource destroy doesn't call this — that wiring + its acceptance test
// live in terraform-provider-conga (tracked in ROADMAP.md).
func TestDeleteSecretUsesAgentScopedPath(t *testing.T) {
	const target = "conga/agents/team-a/linear-api-key"
	const keep = "conga/agents/team-a/anthropic-api-key"
	mock := &mockSecretsManager{secrets: map[string]string{
		target: "lin_api_xxx",
		keep:   "sk-keep",
	}}
	p := &AWSProvider{clients: &awsutil.Clients{SecretsManager: mock}}

	if err := p.DeleteSecret(context.Background(), "team-a", "linear-api-key"); err != nil {
		t.Fatalf("DeleteSecret error: %v", err)
	}
	if _, ok := mock.secrets[target]; ok {
		t.Errorf("expected %s deleted from Secrets Manager", target)
	}
	if _, ok := mock.secrets[keep]; !ok {
		t.Error("DeleteSecret must not touch the agent's other secrets")
	}
}

func TestResolveAWSBehaviorDir(t *testing.T) {
	// All scenarios use a sandboxed cwd so the real repo's agents/ dir
	// (visible at the test runner's cwd) doesn't pollute results.

	t.Run("cwd has agents/ dir but no go.mod — does NOT resolve", func(t *testing.T) {
		// This case used to silently resolve to "./agents" (cwd-relative
		// shortcut). That behavior caused a silent-wrong deployment when
		// the MCP server's cwd was a git worktree whose agents/ dir
		// contained only the committed _defaults/ — the loader treated
		// per-agent overlays as missing and produced defaults-only
		// config. The fix drops the shortcut: resolution now requires a
		// conga-line go.mod up the parent chain, ensuring we always
		// land on the canonical repo's agents/ regardless of which dir
		// the operator happens to be in.
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "agents"), 0700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		withCwd(t, dir, func() {
			if got := resolveAWSBehaviorDir(); got != "" {
				t.Fatalf("want empty (no go.mod found up the parent chain), got %q", got)
			}
		})
	})

	t.Run("walks up to repo root", func(t *testing.T) {
		// Construct a fake repo: <root>/go.mod with the right module marker,
		// <root>/agents/, and <root>/sub1/sub2/ as the cwd.
		root := t.TempDir()
		goMod := []byte("module github.com/cruxdigital-llc/conga-line\n\ngo 1.25\n")
		if err := os.WriteFile(filepath.Join(root, "go.mod"), goMod, 0600); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "agents"), 0700); err != nil {
			t.Fatalf("mkdir agents: %v", err)
		}
		sub := filepath.Join(root, "sub1", "sub2")
		if err := os.MkdirAll(sub, 0700); err != nil {
			t.Fatalf("mkdir sub: %v", err)
		}
		// macOS resolves t.TempDir() through /var → /private/var; compare
		// against the resolved root so the assertion is portable.
		resolvedRoot, _ := filepath.EvalSymlinks(root)
		withCwd(t, sub, func() {
			got := resolveAWSBehaviorDir()
			gotResolved, _ := filepath.EvalSymlinks(got)
			want := filepath.Join(resolvedRoot, "agents")
			if gotResolved != want {
				t.Fatalf("want %q, got %q", want, gotResolved)
			}
		})
	})

	t.Run("stops at foreign go.mod without ascending past it", func(t *testing.T) {
		// A go.mod from a different module shouldn't be picked up, even if
		// the parent has a real agents/ dir. The walk stops at the first
		// go.mod it sees.
		root := t.TempDir()
		foreign := filepath.Join(root, "other-repo")
		if err := os.MkdirAll(foreign, 0700); err != nil {
			t.Fatalf("mkdir foreign: %v", err)
		}
		if err := os.WriteFile(filepath.Join(foreign, "go.mod"),
			[]byte("module example.com/other\n"), 0600); err != nil {
			t.Fatalf("write foreign go.mod: %v", err)
		}
		// Place a agents/ dir at the OUTER root — we must NOT find it.
		if err := os.MkdirAll(filepath.Join(root, "agents"), 0700); err != nil {
			t.Fatalf("mkdir outer agents: %v", err)
		}
		withCwd(t, foreign, func() {
			if got := resolveAWSBehaviorDir(); got != "" {
				t.Fatalf("want empty (foreign repo), got %q", got)
			}
		})
	})

	t.Run("returns empty when nothing matches", func(t *testing.T) {
		dir := t.TempDir() // no go.mod, no agents/
		withCwd(t, dir, func() {
			if got := resolveAWSBehaviorDir(); got != "" {
				t.Fatalf("want empty, got %q", got)
			}
		})
	})
}

// regenerateAgentConfigOnInstance must fail closed when the agents/ overlay
// directory cannot be resolved. The previous behavior — emit a warning and
// proceed — silently stripped per-agent model overrides (e.g. agent.yaml
// pointing at a self-hosted LLM) the next time the operator refreshed from
// outside the repo (notably via MCP, where stderr is invisible).
//
// We construct an AWSProvider with NIL clients on purpose: if the overlay
// check passes through, the next line (readSharedSecrets) will dereference
// p.clients.SecretsManager and panic. The test therefore proves both that
// the function returns an error AND that it short-circuits before any AWS
// call.
func TestRegenerateAgentConfigOnInstance_FailsClosedWhenOverlayUnresolvable(t *testing.T) {
	dir := t.TempDir() // no go.mod, no agents/
	withCwd(t, dir, func() {
		p := &AWSProvider{} // nil clients — would panic if AWS path is entered
		err := p.regenerateAgentConfigOnInstance(
			context.Background(),
			"i-doesnt-matter",
			provider.AgentConfig{Name: "test-agent"},
		)
		if err == nil {
			t.Fatal("expected error when agents/ dir cannot be resolved, got nil")
		}
		// Sanity-check the error wording so a future refactor doesn't quietly
		// downgrade this to a warning again.
		want := "cannot locate the congaline agents/ overlay directory"
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err, want)
		}
		if !strings.Contains(err.Error(), "test-agent") {
			t.Fatalf("error %q should name the agent that would have been refreshed", err)
		}
	})
}

// withCwd chdir's into dir for the duration of fn, restoring on return.
func withCwd(t *testing.T, dir string, fn func()) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer os.Chdir(orig)
	fn()
}
