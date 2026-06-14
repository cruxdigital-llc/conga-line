package awsprovider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmTypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	awsutil "github.com/cruxdigital-llc/conga-line/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

func TestParseKeyValues(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect map[string]string
	}{
		{"basic", "KEY=value\nFOO=bar", map[string]string{"KEY": "value", "FOO": "bar"}},
		{"empty value", "KEY=", map[string]string{"KEY": ""}},
		{"equals in value", "KEY=a=b", map[string]string{"KEY": "a=b"}},
		{"empty input", "", map[string]string{}},
		{"trailing newline", "KEY=val\n", map[string]string{"KEY": "val"}},
		{"no equals", "NOEQ", map[string]string{}},
		{"mixed", "KEY=val\nBAD\nFOO=bar", map[string]string{"KEY": "val", "FOO": "bar"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseKeyValues(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("parseKeyValues(%q) returned %d entries, want %d", tt.input, len(got), len(tt.expect))
				return
			}
			for k, want := range tt.expect {
				if got[k] != want {
					t.Errorf("parseKeyValues(%q)[%q] = %q, want %q", tt.input, k, got[k], want)
				}
			}
		})
	}
}

func TestBuildAgentStatus_NotFound(t *testing.T) {
	kv := map[string]string{"CONTAINER_STATUS": "not found"}
	status := buildAgentStatus("test", kv)
	if status.Container.State != "not found" {
		t.Errorf("expected 'not found', got %q", status.Container.State)
	}
}

func TestBuildAgentStatus_Ready(t *testing.T) {
	kv := map[string]string{
		"SERVICE_STATE":       "active",
		"CONTAINER_STATUS":    "running",
		"CONTAINER_STARTED":   "2026-03-21T10:00:00Z",
		"BOOT_GATEWAY":        "1",
		"BOOT_SLACK_START":    "1",
		"BOOT_SLACK_HTTP":     "1",
		"BOOT_SLACK_CHANNELS": "1",
		"BOOT_ERROR":          "0",
		"CONTAINER_STATS":     "1.5%|256MiB / 2GiB|12",
	}
	status := buildAgentStatus("test", kv)

	if status.ReadyPhase != "ready" {
		t.Errorf("expected 'ready', got %q", status.ReadyPhase)
	}
	if status.Container.CPUPercent != "1.5%" {
		t.Errorf("expected CPU '1.5%%', got %q", status.Container.CPUPercent)
	}
	if status.Container.MemoryUsage != "256MiB / 2GiB" {
		t.Errorf("expected mem '256MiB / 2GiB', got %q", status.Container.MemoryUsage)
	}
}

// mockSSM is a minimal mock for testing setAgentPaused.
type mockSSM struct {
	awsutil.SSMClient
	stored map[string]string
}

func (m *mockSSM) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	val := m.stored[aws.ToString(params.Name)]
	return &ssm.GetParameterOutput{
		Parameter: &ssmTypes.Parameter{Value: aws.String(val)},
	}, nil
}

func (m *mockSSM) PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	m.stored[aws.ToString(params.Name)] = aws.ToString(params.Value)
	return &ssm.PutParameterOutput{}, nil
}

func TestSetAgentPaused_PreservesUnknownFields(t *testing.T) {
	// SSM contains fields that aren't in the AgentConfig struct
	original := `{"type":"user","channels":[{"platform":"slack","id":"U123"}],"gateway_port":18790,"custom_field":"preserve_me","nested":{"key":"value"}}`

	mock := &mockSSM{stored: map[string]string{
		"/conga/agents/testuser": original,
	}}
	p := &AWSProvider{clients: &awsutil.Clients{SSM: mock}}
	agent := &provider.AgentConfig{
		Name:        "testuser",
		Type:        "user",
		Channels:    []channels.ChannelBinding{{Platform: "slack", ID: "U123"}},
		GatewayPort: 18790,
	}

	// Pause: should add "paused":true and keep unknown fields
	if err := p.setAgentPaused(context.Background(), "testuser", agent, true); err != nil {
		t.Fatalf("setAgentPaused(true) error: %v", err)
	}

	var paused map[string]interface{}
	if err := json.Unmarshal([]byte(mock.stored["/conga/agents/testuser"]), &paused); err != nil {
		t.Fatalf("failed to parse paused JSON: %v", err)
	}
	if paused["paused"] != true {
		t.Error("expected paused=true")
	}
	if paused["custom_field"] != "preserve_me" {
		t.Errorf("custom_field lost: got %v", paused["custom_field"])
	}
	nested, ok := paused["nested"].(map[string]interface{})
	if !ok || nested["key"] != "value" {
		t.Errorf("nested field lost: got %v", paused["nested"])
	}

	// Unpause: should remove "paused" and keep unknown fields
	if err := p.setAgentPaused(context.Background(), "testuser", agent, false); err != nil {
		t.Fatalf("setAgentPaused(false) error: %v", err)
	}

	var unpaused map[string]interface{}
	if err := json.Unmarshal([]byte(mock.stored["/conga/agents/testuser"]), &unpaused); err != nil {
		t.Fatalf("failed to parse unpaused JSON: %v", err)
	}
	if _, exists := unpaused["paused"]; exists {
		t.Error("expected paused field to be removed after unpause")
	}
	if unpaused["custom_field"] != "preserve_me" {
		t.Errorf("custom_field lost after unpause: got %v", unpaused["custom_field"])
	}
}

// TestLoadRefreshPolicy_* exercise the refresh-time policy loader that
// backs `redeployEgressDuringRefresh`. The critical invariant is the
// missing-vs-malformed split: a typo in conga-policy.yaml MUST surface
// as an error so the refresh aborts before the proxy gets a deny-all
// config; a genuinely missing file MUST fall back to deny-all + warn
// (this is the bootstrap-time state before any `conga policy deploy`).
//
// Without this split, a typo silently regressed every refreshed agent —
// the original silent-failure documented in PR #53's review (CRIT-1).
func TestLoadRefreshPolicy_MissingFile_DenyAllWithWarning(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "conga-policy.yaml")

	sink := &common.WarningSink{}
	ctx := common.WithWarningSink(context.Background(), sink)

	pf, content, err := loadRefreshPolicy(ctx, missing)
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	if pf != nil {
		t.Errorf("missing file should return nil policy, got %+v", pf)
	}
	if content != "" {
		t.Errorf("missing file should return empty content, got %q", content)
	}
	warnings := sink.Drain()
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "no conga-policy.yaml") {
		t.Errorf("warning should mention missing policy file, got %q", warnings[0])
	}
	if !strings.Contains(warnings[0], "deny-all") {
		t.Errorf("warning should mention deny-all fallback, got %q", warnings[0])
	}
}

func TestLoadRefreshPolicy_MalformedYAML_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "conga-policy.yaml")
	// Intentionally malformed: tab indent + unclosed mapping.
	if err := os.WriteFile(broken, []byte("egress:\n\tallowed_domains: ["), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	sink := &common.WarningSink{}
	ctx := common.WithWarningSink(context.Background(), sink)

	pf, content, err := loadRefreshPolicy(ctx, broken)
	if err == nil {
		t.Fatal("malformed YAML should return error, got nil — this is the silent failure CRIT-1 was meant to fix")
	}
	if pf != nil || content != "" {
		t.Errorf("error path should return zero values, got pf=%v content=%q", pf, content)
	}
	// Error message context — either "read policy" or "parse policy"
	// depending on whether the failure was I/O or YAML; both name the
	// path so the operator can fix it.
	if !strings.Contains(err.Error(), "policy") {
		t.Errorf("error should mention the policy path context, got %q", err.Error())
	}
	if warnings := sink.Drain(); len(warnings) != 0 {
		t.Errorf("malformed YAML should not emit deny-all warning, got %v", warnings)
	}
}

// TestLoadRefreshPolicy_EmptyFile_ReturnsError covers the "file exists
// but is empty / whitespace-only" branch — a third state distinct from
// missing and malformed. policy.LoadFromBytes refuses empty input
// (deliberately, so an interrupted `conga policy set-egress` mid-write
// doesn't pass as a no-op deny-all). loadRefreshPolicy must propagate
// that as an error rather than collapse into deny-all.
func TestLoadRefreshPolicy_EmptyFile_ReturnsError(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content []byte
	}{
		{"zero bytes", []byte{}},
		{"whitespace only", []byte("   \n\n\t\n")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "conga-policy.yaml")
			if err := os.WriteFile(path, tt.content, 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			sink := &common.WarningSink{}
			ctx := common.WithWarningSink(context.Background(), sink)

			pf, content, err := loadRefreshPolicy(ctx, path)
			if err == nil {
				t.Fatal("empty / whitespace-only policy should return error, got nil — would silently regress to deny-all")
			}
			if pf != nil || content != "" {
				t.Errorf("error path should return zero values, got pf=%v content=%q", pf, content)
			}
			if warnings := sink.Drain(); len(warnings) != 0 {
				t.Errorf("empty file should not emit deny-all warning (that's for the missing-file branch only), got %v", warnings)
			}
		})
	}
}

func TestLoadRefreshPolicy_ValidYAML_ReturnsParsedPlusContent(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "conga-policy.yaml")
	src := "apiVersion: conga.dev/v1alpha1\negress:\n  mode: validate\n  allowed_domains:\n    - api.anthropic.com\n"
	if err := os.WriteFile(valid, []byte(src), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	sink := &common.WarningSink{}
	ctx := common.WithWarningSink(context.Background(), sink)

	pf, content, err := loadRefreshPolicy(ctx, valid)
	if err != nil {
		t.Fatalf("valid YAML should not error, got: %v", err)
	}
	if pf == nil {
		t.Fatal("valid YAML should return non-nil policy")
	}
	if pf.Egress == nil || pf.Egress.Mode != "validate" {
		t.Errorf("expected egress.mode=validate, got %+v", pf.Egress)
	}
	if content != src {
		t.Errorf("content should be the raw file bytes, got %q", content)
	}
	if warnings := sink.Drain(); len(warnings) != 0 {
		t.Errorf("valid policy should not emit warnings, got %v", warnings)
	}
}

// TestRefreshAgent_StepsDocumented is a structural regression guard:
// the AWS RefreshAgent flow has four steps (config regen → refresh
// script → routing reconcile → egress redeploy). Steps 3 and 4 were
// added in PR #53 to fix followup items #6 and #9. If a future
// refactor drops either step the warning sink wiring or the helper
// calls below will go missing — this test catches that by scanning
// the live source code for the helper invocations.
//
// Not a substitute for an end-to-end mock test, but it does ensure no
// one can silently delete the routing or egress steps from RefreshAgent
// without a test failure.
func TestRefreshAgent_StepsDocumented(t *testing.T) {
	src, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("read provider.go: %v", err)
	}
	body := string(src)

	// Locate the RefreshAgent body.
	const startMarker = "func (p *AWSProvider) RefreshAgent(ctx"
	const endMarker = "func (p *AWSProvider) redeployEgressDuringRefresh"
	start := strings.Index(body, startMarker)
	end := strings.Index(body, endMarker)
	if start < 0 || end < 0 || end <= start {
		t.Fatal("could not locate RefreshAgent body; markers shifted — update this test")
	}
	refreshBody := body[start:end]

	required := []struct {
		name    string
		snippet string
	}{
		{"step 1 (config regen)", "regenerateAgentConfigOnInstance"},
		{"step 2 (Go unit build + start)", "defineAndStartAgentService"},
		{"step 3a (routing.json reconcile)", "regenerateRoutingOnInstance"},
		{"step 3b (router restart)", "restartRouterOnInstance"},
		{"step 4 (egress redeploy)", "redeployEgressDuringRefresh"},
	}
	for _, r := range required {
		if !strings.Contains(refreshBody, r.snippet) {
			t.Errorf("RefreshAgent missing %s — %q not found in body. Either restore the step or update this test if the contract changed.", r.name, r.snippet)
		}
	}
}

func TestValidateHeredocSafety(t *testing.T) {
	tests := []struct {
		name    string
		values  map[string]string
		wantErr bool
	}{
		{
			"clean inputs",
			map[string]string{
				"PolicyContent": "egress:\n  allowed_domains:\n    - api.anthropic.com",
				"EnvoyConfig":   "static_resources:\n  listeners: []",
			},
			false,
		},
		{
			"POLICYEOF in policy content",
			map[string]string{
				"PolicyContent": "line1\nPOLICYEOF\nline3",
			},
			true,
		},
		{
			"ENVOYEOF in envoy config",
			map[string]string{
				"EnvoyConfig": "bad\nENVOYEOF\ninjection",
			},
			true,
		},
		{
			"BOOTSTRAPEOF in bootstrap",
			map[string]string{
				"ProxyBootstrapJS": "// BOOTSTRAPEOF",
			},
			true,
		},
		{
			"PROXYDF in any value",
			map[string]string{
				"PolicyContent": "FROM envoyproxy/envoy\nPROXYDF\n",
			},
			true,
		},
		{
			"delimiter as substring",
			map[string]string{
				"PolicyContent": "contains POLICYEOF inside",
			},
			true,
		},
		{
			"similar but not matching",
			map[string]string{
				"PolicyContent": "POLICY_EOF is fine",
				"EnvoyConfig":   "ENVOY_EOF is fine",
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHeredocSafety(tt.values)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateHeredocSafety() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
