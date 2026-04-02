package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conga-policy.yaml")

	original := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress: &EgressPolicy{
			AllowedDomains: []string{"api.anthropic.com", "*.slack.com"},
			Mode:           EgressModeEnforce,
		},
	}

	if err := Save(original, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.APIVersion != CurrentAPIVersion {
		t.Errorf("apiVersion = %q, want %q", loaded.APIVersion, CurrentAPIVersion)
	}
	if loaded.Egress == nil {
		t.Fatal("egress is nil after round-trip")
	}
	if len(loaded.Egress.AllowedDomains) != 2 {
		t.Errorf("allowed_domains len = %d, want 2", len(loaded.Egress.AllowedDomains))
	}
	if loaded.Egress.Mode != EgressModeEnforce {
		t.Errorf("mode = %q, want %q", loaded.Egress.Mode, "enforce")
	}
}

func TestSaveCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "conga-policy.yaml")

	pf := &PolicyFile{APIVersion: CurrentAPIVersion}
	if err := Save(pf, path); err != nil {
		t.Fatalf("Save with nested dir: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.APIVersion != CurrentAPIVersion {
		t.Errorf("apiVersion = %q, want %q", loaded.APIVersion, CurrentAPIVersion)
	}
}

func TestSetEgressGlobal(t *testing.T) {
	pf := &PolicyFile{APIVersion: CurrentAPIVersion}
	patch := &EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com"},
		Mode:           EgressModeEnforce,
	}

	SetEgress(pf, "", patch)

	if pf.Egress == nil {
		t.Fatal("global egress is nil")
	}
	if len(pf.Egress.AllowedDomains) != 1 {
		t.Errorf("allowed_domains len = %d, want 1", len(pf.Egress.AllowedDomains))
	}
	if pf.Egress.Mode != EgressModeEnforce {
		t.Errorf("mode = %q, want %q", pf.Egress.Mode, "enforce")
	}
}

func TestSetEgressAgent(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress: &EgressPolicy{
			AllowedDomains: []string{"api.anthropic.com"},
		},
	}
	patch := &EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "*.github.com"},
		Mode:           EgressModeEnforce,
	}

	SetEgress(pf, "myagent", patch)

	if pf.Agents == nil {
		t.Fatal("Agents map is nil")
	}
	override, ok := pf.Agents["myagent"]
	if !ok {
		t.Fatal("myagent override not found")
	}
	if override.Egress == nil {
		t.Fatal("myagent egress is nil")
	}
	if len(override.Egress.AllowedDomains) != 2 {
		t.Errorf("agent allowed_domains len = %d, want 2", len(override.Egress.AllowedDomains))
	}
	// Global should be unchanged
	if len(pf.Egress.AllowedDomains) != 1 {
		t.Errorf("global allowed_domains len = %d, want 1", len(pf.Egress.AllowedDomains))
	}
}

func TestSetEgressAgentCreatesMap(t *testing.T) {
	pf := &PolicyFile{APIVersion: CurrentAPIVersion}
	patch := &EgressPolicy{AllowedDomains: []string{"example.com"}}

	SetEgress(pf, "agent1", patch)

	if pf.Agents == nil {
		t.Fatal("Agents map is nil")
	}
	if pf.Agents["agent1"] == nil || pf.Agents["agent1"].Egress == nil {
		t.Fatal("agent1 egress not created")
	}
}

func TestSetRoutingGlobal(t *testing.T) {
	pf := &PolicyFile{APIVersion: CurrentAPIVersion}
	patch := &RoutingPolicy{
		DefaultModel:  "claude-sonnet-4-6",
		FallbackChain: []string{"claude-haiku-4-5-20251001"},
		CostLimits: &CostLimits{
			DailyPerAgent: 5.0,
			MonthlyGlobal: 100.0,
		},
	}

	SetRouting(pf, "", patch)

	if pf.Routing == nil {
		t.Fatal("global routing is nil")
	}
	if pf.Routing.DefaultModel != "claude-sonnet-4-6" {
		t.Errorf("default_model = %q, want %q", pf.Routing.DefaultModel, "claude-sonnet-4-6")
	}
	if pf.Routing.CostLimits.DailyPerAgent != 5.0 {
		t.Errorf("daily_per_agent = %f, want 5.0", pf.Routing.CostLimits.DailyPerAgent)
	}
}

func TestSetRoutingAgent(t *testing.T) {
	pf := &PolicyFile{APIVersion: CurrentAPIVersion}
	patch := &RoutingPolicy{DefaultModel: "claude-haiku-4-5-20251001"}

	SetRouting(pf, "cheapagent", patch)

	if pf.Agents["cheapagent"].Routing == nil {
		t.Fatal("agent routing is nil")
	}
	if pf.Agents["cheapagent"].Routing.DefaultModel != "claude-haiku-4-5-20251001" {
		t.Errorf("got %q", pf.Agents["cheapagent"].Routing.DefaultModel)
	}
}

func TestSetPostureGlobal(t *testing.T) {
	pf := &PolicyFile{APIVersion: CurrentAPIVersion}
	patch := &PostureDeclarations{
		IsolationLevel:       "standard",
		SecretsBackend:       "file",
		Monitoring:           "basic",
		ComplianceFrameworks: []string{"SOC2"},
	}

	SetPosture(pf, "", patch)

	if pf.Posture == nil {
		t.Fatal("global posture is nil")
	}
	if pf.Posture.IsolationLevel != "standard" {
		t.Errorf("isolation_level = %q", pf.Posture.IsolationLevel)
	}
	if len(pf.Posture.ComplianceFrameworks) != 1 {
		t.Errorf("compliance_frameworks len = %d, want 1", len(pf.Posture.ComplianceFrameworks))
	}
}

func TestSetPostureAgent(t *testing.T) {
	pf := &PolicyFile{APIVersion: CurrentAPIVersion}
	patch := &PostureDeclarations{Monitoring: "full"}

	SetPosture(pf, "prodagent", patch)

	if pf.Agents["prodagent"].Posture == nil {
		t.Fatal("agent posture is nil")
	}
	if pf.Agents["prodagent"].Posture.Monitoring != "full" {
		t.Errorf("monitoring = %q", pf.Agents["prodagent"].Posture.Monitoring)
	}
}

func TestSetPreservesOtherSections(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Routing:    &RoutingPolicy{DefaultModel: "claude-sonnet-4-6"},
		Posture:    &PostureDeclarations{Monitoring: "basic"},
	}

	SetEgress(pf, "", &EgressPolicy{AllowedDomains: []string{"example.com"}})

	if pf.Routing == nil || pf.Routing.DefaultModel != "claude-sonnet-4-6" {
		t.Error("routing was clobbered by SetEgress")
	}
	if pf.Posture == nil || pf.Posture.Monitoring != "basic" {
		t.Error("posture was clobbered by SetEgress")
	}
}

func TestSaveFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conga-policy.yaml")

	pf := &PolicyFile{APIVersion: CurrentAPIVersion}
	if err := Save(pf, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0600 {
		t.Errorf("policy file mode = %04o, want 0600", mode)
	}
}

func TestSetAgentPreservesOtherOverrides(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Agents: map[string]*AgentOverride{
			"agentA": {Egress: &EgressPolicy{AllowedDomains: []string{"a.com"}}},
		},
	}

	SetEgress(pf, "agentB", &EgressPolicy{AllowedDomains: []string{"b.com"}})

	if pf.Agents["agentA"].Egress == nil || pf.Agents["agentA"].Egress.AllowedDomains[0] != "a.com" {
		t.Error("agentA override was clobbered")
	}
	if pf.Agents["agentB"].Egress == nil || pf.Agents["agentB"].Egress.AllowedDomains[0] != "b.com" {
		t.Error("agentB override was not set")
	}
}
