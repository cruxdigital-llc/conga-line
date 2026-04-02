package awsprovider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmTypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	awsutil "github.com/cruxdigital-llc/conga-line/cli/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/discovery"
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
	agent := &discovery.AgentConfig{
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
