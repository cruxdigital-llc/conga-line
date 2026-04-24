package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/internal/mcpserver"
	"github.com/cruxdigital-llc/conga-line/pkg/policy"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcptest"
)

// policyTestEnv sets up a test environment with a mock provider, MCP test
// server, and optional policy file on disk.
type policyTestEnv struct {
	mock       *mockProvider
	policyPath string
	policyDir  string
}

func newPolicyTestEnv(t *testing.T, policyYAML string) (*policyTestEnv, *client.Client) {
	t.Helper()

	dir := t.TempDir()
	congaDir := filepath.Join(dir, ".conga")
	os.MkdirAll(congaDir, 0755)
	policyPath := filepath.Join(congaDir, "conga-policy.yaml")

	if policyYAML != "" {
		if err := os.WriteFile(policyPath, []byte(policyYAML), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("HOME", dir)

	mock := &mockProvider{
		name: "local",
		agents: []provider.AgentConfig{
			{Name: "agent1", Type: provider.AgentTypeUser, GatewayPort: 18789},
			{Name: "agent2", Type: provider.AgentTypeTeam, GatewayPort: 18790},
		},
		agent: &provider.AgentConfig{
			Name: "agent1", Type: provider.AgentTypeUser, GatewayPort: 18789,
		},
	}

	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testSrv.Close() })

	return &policyTestEnv{
		mock:       mock,
		policyPath: policyPath,
		policyDir:  congaDir,
	}, testSrv.Client()
}

// --- Read-only tool tests ---

func TestPolicyGetNoFile(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_get", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var pf policy.PolicyFile
	if err := json.Unmarshal([]byte(textContent(t, result)), &pf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pf.APIVersion != policy.CurrentAPIVersion {
		t.Errorf("apiVersion = %q, want %q", pf.APIVersion, policy.CurrentAPIVersion)
	}
}

func TestPolicyGetExistingFile(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	_, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_get", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var pf policy.PolicyFile
	if err := json.Unmarshal([]byte(textContent(t, result)), &pf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pf.Egress == nil {
		t.Fatal("egress is nil")
	}
	if len(pf.Egress.AllowedDomains) != 1 {
		t.Errorf("allowed_domains len = %d, want 1", len(pf.Egress.AllowedDomains))
	}
	if pf.Egress.Mode != policy.EgressModeEnforce {
		t.Errorf("mode = %q, want %q", pf.Egress.Mode, "enforce")
	}
}

func TestPolicyValidateValid(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	_, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_validate", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var vr struct {
		Valid  bool                `json:"valid"`
		Report []policy.RuleReport `json:"enforcement_report"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &vr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !vr.Valid {
		t.Error("expected valid=true")
	}
	if len(vr.Report) == 0 {
		t.Error("expected non-empty enforcement report")
	}
}

func TestPolicyValidateInvalid(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  mode: invalid_mode
`
	_, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_validate", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var vr struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &vr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if vr.Valid {
		t.Error("expected valid=false for invalid mode")
	}
	if vr.Error == "" {
		t.Error("expected non-empty error")
	}
}

func TestPolicyValidateWithAgent(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
agents:
  myagent:
    egress:
      allowed_domains:
        - api.anthropic.com
        - "*.github.com"
      mode: enforce
`
	_, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_validate", map[string]any{"agent": "myagent"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var vr struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &vr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !vr.Valid {
		t.Error("expected valid=true")
	}
}

func TestPolicyGetAgent(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
agents:
  myagent:
    egress:
      allowed_domains:
        - api.anthropic.com
        - "*.github.com"
`
	_, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_get_agent", map[string]any{"agent": "myagent"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var pf policy.PolicyFile
	if err := json.Unmarshal([]byte(textContent(t, result)), &pf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pf.Egress == nil {
		t.Fatal("egress is nil")
	}
	if len(pf.Egress.AllowedDomains) != 2 {
		t.Errorf("allowed_domains len = %d, want 2 (agent override)", len(pf.Egress.AllowedDomains))
	}
	// Agents map should not be present in merged output
	if len(pf.Agents) != 0 {
		t.Errorf("expected no agents map in merged output, got %d entries", len(pf.Agents))
	}
}

func TestPolicyGetAgentNoOverride(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
`
	_, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_get_agent", map[string]any{"agent": "nonexistent"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var pf policy.PolicyFile
	if err := json.Unmarshal([]byte(textContent(t, result)), &pf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Should return global policy
	if pf.Egress == nil || len(pf.Egress.AllowedDomains) != 1 {
		t.Error("expected global policy when no agent override")
	}
}

// --- Mutation tool tests ---

func TestPolicySetEgress(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_set_egress", map[string]any{
		"allowed_domains": []any{"api.anthropic.com", "*.slack.com"},
		"mode":            "enforce",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var resp struct {
		Policy         *policy.PolicyFile `json:"policy"`
		DeployRequired bool               `json:"deploy_required"`
		DeployHint     string             `json:"deploy_hint"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.DeployRequired {
		t.Error("deploy_required should be true after a set without deploy=true")
	}
	if resp.DeployHint == "" {
		t.Error("deploy_hint should be populated when deploy_required is true")
	}
	if resp.Policy == nil || resp.Policy.Egress == nil {
		t.Fatal("policy.egress is nil")
	}
	if len(resp.Policy.Egress.AllowedDomains) != 2 {
		t.Errorf("allowed_domains len = %d, want 2", len(resp.Policy.Egress.AllowedDomains))
	}
	if resp.Policy.Egress.Mode != policy.EgressModeEnforce {
		t.Errorf("mode = %q, want %q", resp.Policy.Egress.Mode, "enforce")
	}
}

func TestPolicySetEgressValidationError(t *testing.T) {
	env, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_set_egress", map[string]any{
		"allowed_domains": []any{"*bad.com"},
	})
	if !result.IsError {
		t.Fatal("expected validation error for invalid domain")
	}

	// File should not have been created
	if _, err := os.Stat(env.policyPath); !os.IsNotExist(err) {
		t.Error("policy file should not exist after validation failure")
	}
}

func TestPolicySetEgressAgent(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
`
	_, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_set_egress", map[string]any{
		"allowed_domains": []any{"api.anthropic.com", "*.github.com"},
		"agent":           "myagent",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var resp struct {
		Policy *policy.PolicyFile `json:"policy"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	pf := resp.Policy
	if pf == nil {
		t.Fatal("policy is nil")
	}
	// Global should be unchanged
	if len(pf.Egress.AllowedDomains) != 1 {
		t.Errorf("global allowed_domains = %d, want 1", len(pf.Egress.AllowedDomains))
	}
	// Agent override should exist
	if pf.Agents == nil || pf.Agents["myagent"] == nil || pf.Agents["myagent"].Egress == nil {
		t.Fatal("agent override not created")
	}
	if len(pf.Agents["myagent"].Egress.AllowedDomains) != 2 {
		t.Errorf("agent allowed_domains = %d, want 2", len(pf.Agents["myagent"].Egress.AllowedDomains))
	}
}

func TestPolicySetRouting(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_set_routing", map[string]any{
		"default_model":  "claude-sonnet-4-6",
		"fallback_chain": []any{"claude-haiku-4-5-20251001"},
		"cost_limits": map[string]any{
			"daily_per_agent": 5.0,
			"monthly_global":  100.0,
		},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var pf policy.PolicyFile
	if err := json.Unmarshal([]byte(textContent(t, result)), &pf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pf.Routing == nil {
		t.Fatal("routing is nil")
	}
	if pf.Routing.DefaultModel != "claude-sonnet-4-6" {
		t.Errorf("default_model = %q", pf.Routing.DefaultModel)
	}
	if len(pf.Routing.FallbackChain) != 1 {
		t.Errorf("fallback_chain len = %d, want 1", len(pf.Routing.FallbackChain))
	}
	if pf.Routing.CostLimits == nil {
		t.Fatal("cost_limits is nil")
	}
	if pf.Routing.CostLimits.DailyPerAgent != 5.0 {
		t.Errorf("daily_per_agent = %f, want 5.0", pf.Routing.CostLimits.DailyPerAgent)
	}
}

func TestPolicySetPosture(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_set_posture", map[string]any{
		"isolation_level":       "standard",
		"secrets_backend":       "file",
		"monitoring":            "basic",
		"compliance_frameworks": []any{"SOC2"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var pf policy.PolicyFile
	if err := json.Unmarshal([]byte(textContent(t, result)), &pf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pf.Posture == nil {
		t.Fatal("posture is nil")
	}
	if pf.Posture.IsolationLevel != "standard" {
		t.Errorf("isolation_level = %q", pf.Posture.IsolationLevel)
	}
	if len(pf.Posture.ComplianceFrameworks) != 1 || pf.Posture.ComplianceFrameworks[0] != "SOC2" {
		t.Errorf("compliance_frameworks = %v", pf.Posture.ComplianceFrameworks)
	}
}

// --- Input validation tests ---

func TestPolicySetEgressRejectsNonStringArrayElement(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_set_egress", map[string]any{
		"allowed_domains": []any{"api.anthropic.com", 42},
	})
	if !result.IsError {
		t.Fatal("expected error for non-string array element")
	}
	text := textContent(t, result)
	if !strings.Contains(text, "must be a string") {
		t.Errorf("error = %q, want it to mention 'must be a string'", text)
	}
}

func TestPolicySetRoutingRejectsBadCostLimits(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_set_routing", map[string]any{
		"cost_limits": "not-an-object",
	})
	if !result.IsError {
		t.Fatal("expected error for non-object cost_limits")
	}
	text := textContent(t, result)
	if !strings.Contains(text, "must be an object") {
		t.Errorf("error = %q, want it to mention 'must be an object'", text)
	}
}

func TestPolicySetEgressRejectsNonArrayDomains(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_set_egress", map[string]any{
		"allowed_domains": "api.anthropic.com",
	})
	if !result.IsError {
		t.Fatal("expected error for non-array allowed_domains")
	}
	text := textContent(t, result)
	if !strings.Contains(text, "must be an array") {
		t.Errorf("error = %q, want it to mention 'must be an array'", text)
	}
}

func TestPolicySetRoutingRejectsNonNumericCostLimitField(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_set_routing", map[string]any{
		"cost_limits": map[string]any{
			"daily_per_agent": "five",
		},
	})
	if !result.IsError {
		t.Fatal("expected error for non-numeric cost_limits field")
	}
	text := textContent(t, result)
	if !strings.Contains(text, "must be a number") {
		t.Errorf("error = %q, want it to mention 'must be a number'", text)
	}
}

// --- Deploy tool tests ---

func TestPolicyDeployNoFile(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	result := callTool(t, client, "conga_policy_deploy", nil)
	if !result.IsError {
		t.Fatal("expected error when no policy file exists")
	}
	text := textContent(t, result)
	if !strings.Contains(text, "no policy file found") {
		t.Errorf("error = %q, want it to mention missing policy file", text)
	}
}

func TestPolicyDeployValidatesFirst(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  mode: invalid_mode
`
	env, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_deploy", nil)
	if !result.IsError {
		t.Fatal("expected validation error")
	}
	// RefreshAll should NOT have been called
	if env.mock.lastAgentName != "" {
		t.Error("provider should not have been called for invalid policy")
	}
}

func TestPolicyDeploySingleAgent(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	env, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_deploy", map[string]any{
		"agent": "agent1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	if env.mock.lastAgentName != "agent1" {
		t.Errorf("RefreshAgent called with %q, want %q", env.mock.lastAgentName, "agent1")
	}

	var dr struct {
		Validated bool     `json:"validated"`
		Deployed  []string `json:"deployed"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &dr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !dr.Validated {
		t.Error("expected validated=true")
	}
	if len(dr.Deployed) != 1 || dr.Deployed[0] != "agent1" {
		t.Errorf("deployed = %v, want [agent1]", dr.Deployed)
	}
}

func TestPolicyDeployAll(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	_, client := newPolicyTestEnv(t, yaml)

	result := callTool(t, client, "conga_policy_deploy", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var dr struct {
		Validated bool     `json:"validated"`
		Deployed  []string `json:"deployed"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &dr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !dr.Validated {
		t.Error("expected validated=true")
	}
	if len(dr.Deployed) != 2 {
		t.Errorf("deployed len = %d, want 2", len(dr.Deployed))
	}
}

// --- egressDeployer dispatch tests ---

// mockEgressProvider embeds mockProvider and implements DeployEgress.
type mockEgressProvider struct {
	mockProvider
	deployedAgents    []string
	deployedConfigs   map[string]string            // agent name -> envoy config
	deployedModes     map[string]policy.EgressMode // agent name -> mode
	deployedManifests map[string]string            // agent name -> manifest JSON
	deployErr         map[string]error             // agent name -> error
}

func (m *mockEgressProvider) DeployEgress(ctx context.Context, agentName, policyContent, envoyConfig, manifestJSON string, mode policy.EgressMode) error {
	if err, ok := m.deployErr[agentName]; ok && err != nil {
		return err
	}
	m.deployedAgents = append(m.deployedAgents, agentName)
	if m.deployedConfigs == nil {
		m.deployedConfigs = make(map[string]string)
	}
	if m.deployedModes == nil {
		m.deployedModes = make(map[string]policy.EgressMode)
	}
	if m.deployedManifests == nil {
		m.deployedManifests = make(map[string]string)
	}
	m.deployedConfigs[agentName] = envoyConfig
	m.deployedModes[agentName] = mode
	m.deployedManifests[agentName] = manifestJSON
	return nil
}

func newEgressDeployerTestEnv(t *testing.T, policyYAML string, mock *mockEgressProvider) *client.Client {
	t.Helper()

	dir := t.TempDir()
	congaDir := filepath.Join(dir, ".conga")
	os.MkdirAll(congaDir, 0755)
	policyPath := filepath.Join(congaDir, "conga-policy.yaml")

	if policyYAML != "" {
		if err := os.WriteFile(policyPath, []byte(policyYAML), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("HOME", dir)

	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testSrv.Close() })
	return testSrv.Client()
}

func TestPolicyDeployViaEgressDeployer(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	mock := &mockEgressProvider{
		mockProvider: mockProvider{
			name: "aws",
			agents: []provider.AgentConfig{
				{Name: "agent1", Type: provider.AgentTypeUser},
				{Name: "agent2", Type: provider.AgentTypeTeam},
			},
			agent: &provider.AgentConfig{
				Name: "agent1", Type: provider.AgentTypeUser,
			},
		},
	}
	client := newEgressDeployerTestEnv(t, yaml, mock)

	result := callTool(t, client, "conga_policy_deploy", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var dr struct {
		Validated      bool     `json:"validated"`
		Deployed       []string `json:"deployed"`
		Errors         []string `json:"errors"`
		PartialFailure bool     `json:"partial_failure"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &dr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !dr.Validated {
		t.Error("expected validated=true")
	}
	if len(dr.Deployed) != 2 {
		t.Errorf("deployed = %v, want 2 agents", dr.Deployed)
	}
	if len(dr.Errors) != 0 {
		t.Errorf("errors = %v, want none", dr.Errors)
	}
	if dr.PartialFailure {
		t.Error("expected partial_failure=false")
	}
	if len(mock.deployedAgents) != 2 {
		t.Errorf("DeployEgress called %d times, want 2", len(mock.deployedAgents))
	}
}

func TestPolicyDeployViaEgressDeployerPartialFailure(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	mock := &mockEgressProvider{
		mockProvider: mockProvider{
			name: "aws",
			agents: []provider.AgentConfig{
				{Name: "agent1", Type: provider.AgentTypeUser},
				{Name: "agent2", Type: provider.AgentTypeTeam},
			},
			agent: &provider.AgentConfig{
				Name: "agent1", Type: provider.AgentTypeUser,
			},
		},
		deployErr: map[string]error{
			"agent2": errors.New("SSM timeout"),
		},
	}
	client := newEgressDeployerTestEnv(t, yaml, mock)

	result := callTool(t, client, "conga_policy_deploy", nil)
	if result.IsError {
		t.Fatal("partial failure should not return error result")
	}

	var dr struct {
		Validated      bool     `json:"validated"`
		Deployed       []string `json:"deployed"`
		Errors         []string `json:"errors"`
		PartialFailure bool     `json:"partial_failure"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &dr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(dr.Deployed) != 1 || dr.Deployed[0] != "agent1" {
		t.Errorf("deployed = %v, want [agent1]", dr.Deployed)
	}
	if len(dr.Errors) != 1 {
		t.Errorf("errors = %v, want 1 error", dr.Errors)
	}
	if !dr.PartialFailure {
		t.Error("expected partial_failure=true")
	}
}

func TestPolicyDeployViaEgressDeployerSingleAgent(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	mock := &mockEgressProvider{
		mockProvider: mockProvider{
			name: "aws",
			agents: []provider.AgentConfig{
				{Name: "agent1", Type: provider.AgentTypeUser},
				{Name: "agent2", Type: provider.AgentTypeTeam},
			},
			agent: &provider.AgentConfig{
				Name: "agent1", Type: provider.AgentTypeUser,
			},
		},
	}
	client := newEgressDeployerTestEnv(t, yaml, mock)

	result := callTool(t, client, "conga_policy_deploy", map[string]any{
		"agent": "agent1",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	if len(mock.deployedAgents) != 1 || mock.deployedAgents[0] != "agent1" {
		t.Errorf("DeployEgress called for %v, want [agent1]", mock.deployedAgents)
	}

	var dr struct {
		Deployed []string `json:"deployed"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &dr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(dr.Deployed) != 1 || dr.Deployed[0] != "agent1" {
		t.Errorf("deployed = %v, want [agent1]", dr.Deployed)
	}
}

func TestPolicyDeployViaEgressDeployerDeploysEmptyDomains(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
agents:
  agent2:
    egress:
      allowed_domains: []
`
	mock := &mockEgressProvider{
		mockProvider: mockProvider{
			name: "aws",
			agents: []provider.AgentConfig{
				{Name: "agent1", Type: provider.AgentTypeUser},
				{Name: "agent2", Type: provider.AgentTypeTeam},
			},
			agent: &provider.AgentConfig{
				Name: "agent1", Type: provider.AgentTypeUser,
			},
		},
	}
	client := newEgressDeployerTestEnv(t, yaml, mock)

	result := callTool(t, client, "conga_policy_deploy", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	// Both agents should be deployed — agent2 gets deny-all (empty domains)
	if len(mock.deployedAgents) != 2 {
		t.Errorf("DeployEgress called for %v, want [agent1 agent2] (both should be deployed)", mock.deployedAgents)
	}

	// agent2 has empty domains — should get deny-all config with Lua filter
	if conf, ok := mock.deployedConfigs["agent2"]; ok {
		if !strings.Contains(conf, "envoy.filters.http.lua") {
			t.Error("agent2 should get Lua filter in deny-all config")
		}
		if !strings.Contains(conf, `egress denied:`) {
			t.Error("agent2 deny-all config should contain enforce-mode 403 response")
		}
	}
	// agent2 mode should be enforce (default for empty domains)
	if mode, ok := mock.deployedModes["agent2"]; ok {
		if mode != policy.EgressModeEnforce {
			t.Errorf("agent2 mode = %q, want %q", mode, policy.EgressModeEnforce)
		}
	}
}

func TestPolicyDeployViaEgressDeployerTotalFailure(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	mock := &mockEgressProvider{
		mockProvider: mockProvider{
			name: "aws",
			agents: []provider.AgentConfig{
				{Name: "agent1", Type: provider.AgentTypeUser},
			},
			agent: &provider.AgentConfig{
				Name: "agent1", Type: provider.AgentTypeUser,
			},
		},
		deployErr: map[string]error{
			"agent1": errors.New("instance unreachable"),
		},
	}
	client := newEgressDeployerTestEnv(t, yaml, mock)

	result := callTool(t, client, "conga_policy_deploy", nil)
	if !result.IsError {
		t.Fatal("expected error when all agents fail")
	}
	text := textContent(t, result)
	if !strings.Contains(text, "deploy failed for all agents") {
		t.Errorf("error = %q, want it to mention all agents failed", text)
	}
}

func TestPolicySetEgressDefaultMode(t *testing.T) {
	_, client := newPolicyTestEnv(t, "")

	// Call set_egress WITHOUT specifying mode — should default to "enforce"
	result := callTool(t, client, "conga_policy_set_egress", map[string]any{
		"allowed_domains": []any{"api.anthropic.com"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	var resp struct {
		Policy *policy.PolicyFile `json:"policy"`
	}
	if err := json.Unmarshal([]byte(textContent(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	pf := resp.Policy
	if pf == nil || pf.Egress == nil {
		t.Fatal("policy.egress is nil")
	}
	if pf.Egress.Mode != policy.EgressModeEnforce {
		t.Errorf("mode = %q, want %q (default when omitted)", pf.Egress.Mode, policy.EgressModeEnforce)
	}
}

func TestPolicyDeploySkipsPausedAgents(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	mock := &mockEgressProvider{
		mockProvider: mockProvider{
			name: "aws",
			agents: []provider.AgentConfig{
				{Name: "agent1", Type: provider.AgentTypeUser},
				{Name: "agent2", Type: provider.AgentTypeTeam, Paused: true},
				{Name: "agent3", Type: provider.AgentTypeUser},
			},
			agent: &provider.AgentConfig{
				Name: "agent1", Type: provider.AgentTypeUser,
			},
		},
	}
	client := newEgressDeployerTestEnv(t, yaml, mock)

	result := callTool(t, client, "conga_policy_deploy", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}

	// agent2 is paused — should be skipped
	if len(mock.deployedAgents) != 2 {
		t.Errorf("DeployEgress called %d times, want 2 (paused agent should be skipped)", len(mock.deployedAgents))
	}
	for _, name := range mock.deployedAgents {
		if name == "agent2" {
			t.Error("DeployEgress should not have been called for paused agent2")
		}
	}
}

func TestPolicyDeployAllPausedReturnsError(t *testing.T) {
	yaml := `apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
  mode: enforce
`
	mock := &mockEgressProvider{
		mockProvider: mockProvider{
			name: "aws",
			agents: []provider.AgentConfig{
				{Name: "agent1", Type: provider.AgentTypeUser, Paused: true},
				{Name: "agent2", Type: provider.AgentTypeTeam, Paused: true},
			},
			agent: &provider.AgentConfig{
				Name: "agent1", Type: provider.AgentTypeUser,
			},
		},
	}
	client := newEgressDeployerTestEnv(t, yaml, mock)

	result := callTool(t, client, "conga_policy_deploy", nil)
	if !result.IsError {
		t.Fatal("expected error when all agents are paused")
	}
	text := textContent(t, result)
	if !strings.Contains(text, "no active agents") {
		t.Errorf("error = %q, want it to mention no active agents", text)
	}
}
