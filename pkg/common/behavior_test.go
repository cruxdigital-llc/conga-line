package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/hermes"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

// setupBehaviorDir creates a temp behavior directory with the runtime+type structure:
//
//	default/openclaw/team/{SOUL.md, AGENTS.md, USER.md.tmpl}
//	default/openclaw/user/{SOUL.md, AGENTS.md, USER.md.tmpl}
func setupBehaviorDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "default", "openclaw", "team"), 0755)
	os.MkdirAll(filepath.Join(dir, "default", "openclaw", "user"), 0755)
	os.WriteFile(filepath.Join(dir, "default", "openclaw", "team", "SOUL.md"), []byte("team soul"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "openclaw", "team", "AGENTS.md"), []byte("team agents"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "openclaw", "team", "USER.md.tmpl"), []byte("team user: {{AGENT_NAME}}"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "openclaw", "user", "SOUL.md"), []byte("user soul"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "openclaw", "user", "AGENTS.md"), []byte("user agents"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "openclaw", "user", "USER.md.tmpl"), []byte("dm user: {{AGENT_NAME}}"), 0644)

	return dir
}

// setupHermesBehaviorDir creates a temp behavior directory with Hermes runtime files.
func setupHermesBehaviorDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "default", "hermes", "team"), 0755)
	os.MkdirAll(filepath.Join(dir, "default", "hermes", "user"), 0755)
	os.WriteFile(filepath.Join(dir, "default", "hermes", "team", "SOUL.md"), []byte("hermes team soul"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "hermes", "team", "AGENTS.md"), []byte("hermes team agents"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "hermes", "team", "USER.md.tmpl"), []byte("hermes team: {{AGENT_NAME}}"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "hermes", "user", "SOUL.md"), []byte("hermes user soul"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "hermes", "user", "AGENTS.md"), []byte("hermes user agents"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "hermes", "user", "USER.md.tmpl"), []byte("hermes dm: {{AGENT_NAME}}"), 0644)

	return dir
}

func TestComposeAgentWorkspaceFiles_DefaultsOnly(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	files, toDelete, manifest, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	soul := string(files["SOUL.md"].Content)
	if soul != "team soul" {
		t.Errorf("SOUL.md = %q, want 'team soul'", soul)
	}
	if files["SOUL.md"].Source != "default" {
		t.Errorf("SOUL.md source = %q, want default", files["SOUL.md"].Source)
	}

	agents := string(files["AGENTS.md"].Content)
	if agents != "team agents" {
		t.Errorf("AGENTS.md = %q, want 'team agents'", agents)
	}

	user := string(files["USER.md"].Content)
	if user != "team user: acme" {
		t.Errorf("USER.md = %q, want 'team user: acme'", user)
	}

	if len(toDelete) != 0 {
		t.Errorf("toDelete = %v, want empty", toDelete)
	}
	if manifest.Version != ManifestVersion {
		t.Errorf("manifest version = %d", manifest.Version)
	}
}

func TestComposeAgentWorkspaceFiles_UserAgentDefaults(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "bob", Type: provider.AgentTypeUser}

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	soul := string(files["SOUL.md"].Content)
	if soul != "user soul" {
		t.Errorf("SOUL.md = %q, want 'user soul'", soul)
	}

	agents := string(files["AGENTS.md"].Content)
	if agents != "user agents" {
		t.Errorf("AGENTS.md = %q, want 'user agents'", agents)
	}

	user := string(files["USER.md"].Content)
	if user != "dm user: bob" {
		t.Errorf("USER.md = %q, want 'dm user: bob'", user)
	}
}

func TestComposeAgentWorkspaceFiles_AgentOverridesDefault(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	agentDir := filepath.Join(dir, "agents", "acme")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "SOUL.md"), []byte("custom soul"), 0644)

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	soul := string(files["SOUL.md"].Content)
	if soul != "custom soul" {
		t.Errorf("SOUL.md = %q, want 'custom soul'", soul)
	}
	if files["SOUL.md"].Source != "agent" {
		t.Errorf("SOUL.md source = %q, want agent", files["SOUL.md"].Source)
	}

	// AGENTS.md should still come from default
	agents := string(files["AGENTS.md"].Content)
	if agents != "team agents" {
		t.Errorf("AGENTS.md = %q, want 'team agents' (should fall back)", agents)
	}
}

func TestComposeAgentWorkspaceFiles_AgentOverridesUSERmd(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	agentDir := filepath.Join(dir, "agents", "acme")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "USER.md"), []byte("custom user file"), 0644)

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	user := string(files["USER.md"].Content)
	if user != "custom user file" {
		t.Errorf("USER.md = %q, want 'custom user file'", user)
	}
	if files["USER.md"].Source != "agent" {
		t.Errorf("USER.md source = %q, want agent", files["USER.md"].Source)
	}
}

func TestComposeAgentWorkspaceFiles_OnlyKnownFilesRead(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	// Even if extra files exist in agents/acme/, only SOUL.md/AGENTS.md/USER.md are read.
	// MEMORY.md in the agent dir is simply ignored (not loaded, not deployed).
	agentDir := filepath.Join(dir, "agents", "acme")
	os.MkdirAll(agentDir, 0755)
	os.WriteFile(filepath.Join(agentDir, "MEMORY.md"), []byte("should be ignored"), 0644)
	os.WriteFile(filepath.Join(agentDir, "SOUL.md"), []byte("custom soul"), 0644)

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SOUL.md should be the agent version
	if string(files["SOUL.md"].Content) != "custom soul" {
		t.Errorf("SOUL.md = %q, want 'custom soul'", string(files["SOUL.md"].Content))
	}

	// MEMORY.md should NOT be in the output (it's not one of the known files)
	if _, ok := files["MEMORY.md"]; ok {
		t.Error("MEMORY.md should not be in files — only SOUL.md, AGENTS.md, USER.md are read")
	}
}

func TestComposeAgentWorkspaceFiles_NoAgentDir(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	// No agents/ directory — should produce default files
	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := files["SOUL.md"]; !ok {
		t.Error("SOUL.md should be present from defaults")
	}
}

func TestComposeAgentWorkspaceFiles_DeletionReconciliation(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	// Agent had SOUL.md override previously, now removed (no agents/acme/ dir)
	oldContent := []byte("old agent soul")
	prev := &OverlayManifest{
		Version: ManifestVersion,
		Files: []OverlayEntry{
			{Path: "SOUL.md", SHA256: HashFileContent(oldContent), Source: "agent"},
		},
	}

	// Workspace still has the old agent SOUL.md
	hashWorkspaceFile := func(rel string) (string, error) {
		if rel == "SOUL.md" {
			return HashFileContent(oldContent), nil
		}
		return "", os.ErrNotExist
	}

	_, toDelete, _, err := ComposeAgentWorkspaceFiles(dir, agent, prev, hashWorkspaceFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SOUL.md is still in the new file set (from defaults), so it should NOT be deleted
	// — it just switches from agent to default source
	for _, d := range toDelete {
		if d == "SOUL.md" {
			t.Error("SOUL.md should not be in toDelete — it's still present from defaults")
		}
	}
}

func TestComposeAgentWorkspaceFiles_DeletionPreservesModified(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	prev := &OverlayManifest{
		Version: ManifestVersion,
		Files: []OverlayEntry{
			{Path: "CUSTOM.md", SHA256: HashFileContent([]byte("original")), Source: "agent"},
		},
	}

	hashWorkspaceFile := func(rel string) (string, error) {
		return HashFileContent([]byte("agent edited this")), nil
	}

	_, toDelete, _, err := ComposeAgentWorkspaceFiles(dir, agent, prev, hashWorkspaceFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toDelete) != 0 {
		t.Errorf("toDelete = %v, want empty (modified file should be preserved)", toDelete)
	}
}

func TestComposeAgentWorkspaceFiles_BackwardsCompatOldManifest(t *testing.T) {
	dir := setupBehaviorDir(t)
	agent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}

	// Old manifest has Source: "overlay" — should still be recognized for deletion
	oldContent := []byte("old overlay content")
	prev := &OverlayManifest{
		Version: ManifestVersion,
		Files: []OverlayEntry{
			{Path: "REMOVED.md", SHA256: HashFileContent(oldContent), Source: "overlay"},
		},
	}

	hashWorkspaceFile := func(rel string) (string, error) {
		if rel == "REMOVED.md" {
			return HashFileContent(oldContent), nil
		}
		return "", os.ErrNotExist
	}

	_, toDelete, _, err := ComposeAgentWorkspaceFiles(dir, agent, prev, hashWorkspaceFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(toDelete) != 1 || toDelete[0] != "REMOVED.md" {
		t.Errorf("toDelete = %v, want [REMOVED.md] (backwards compat with 'overlay' source)", toDelete)
	}
}

func TestComposeAgentWorkspaceFiles_HermesDefaults(t *testing.T) {
	dir := setupHermesBehaviorDir(t)
	agent := provider.AgentConfig{Name: "atlas", Type: provider.AgentTypeTeam, Runtime: "hermes"}

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	soul := string(files["SOUL.md"].Content)
	if soul != "hermes team soul" {
		t.Errorf("SOUL.md = %q, want 'hermes team soul'", soul)
	}

	agents := string(files["AGENTS.md"].Content)
	if agents != "hermes team agents" {
		t.Errorf("AGENTS.md = %q, want 'hermes team agents'", agents)
	}

	user := string(files["USER.md"].Content)
	if user != "hermes team: atlas" {
		t.Errorf("USER.md = %q, want 'hermes team: atlas'", user)
	}
}

func TestComposeAgentWorkspaceFiles_HermesUserDefaults(t *testing.T) {
	dir := setupHermesBehaviorDir(t)
	agent := provider.AgentConfig{Name: "jarvis", Type: provider.AgentTypeUser, Runtime: "hermes"}

	files, _, _, err := ComposeAgentWorkspaceFiles(dir, agent, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	soul := string(files["SOUL.md"].Content)
	if soul != "hermes user soul" {
		t.Errorf("SOUL.md = %q, want 'hermes user soul'", soul)
	}

	agents := string(files["AGENTS.md"].Content)
	if agents != "hermes user agents" {
		t.Errorf("AGENTS.md = %q, want 'hermes user agents'", agents)
	}

	user := string(files["USER.md"].Content)
	if user != "hermes dm: jarvis" {
		t.Errorf("USER.md = %q, want 'hermes dm: jarvis'", user)
	}
}

func TestComposeAgentWorkspaceFiles_RuntimeIsolation(t *testing.T) {
	// Create a dir with both openclaw and hermes defaults
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "default", "openclaw", "team"), 0755)
	os.WriteFile(filepath.Join(dir, "default", "openclaw", "team", "SOUL.md"), []byte("openclaw team soul"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "openclaw", "team", "AGENTS.md"), []byte("openclaw team agents"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "openclaw", "team", "USER.md.tmpl"), []byte("openclaw team: {{AGENT_NAME}}"), 0644)

	os.MkdirAll(filepath.Join(dir, "default", "hermes", "team"), 0755)
	os.WriteFile(filepath.Join(dir, "default", "hermes", "team", "SOUL.md"), []byte("hermes team soul"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "hermes", "team", "AGENTS.md"), []byte("hermes team agents"), 0644)
	os.WriteFile(filepath.Join(dir, "default", "hermes", "team", "USER.md.tmpl"), []byte("hermes team: {{AGENT_NAME}}"), 0644)

	// OpenClaw agent gets openclaw files
	ocAgent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam}
	ocFiles, _, _, err := ComposeAgentWorkspaceFiles(dir, ocAgent, nil, nil)
	if err != nil {
		t.Fatalf("openclaw: unexpected error: %v", err)
	}
	if string(ocFiles["SOUL.md"].Content) != "openclaw team soul" {
		t.Errorf("openclaw SOUL.md = %q, want 'openclaw team soul'", string(ocFiles["SOUL.md"].Content))
	}

	// Hermes agent gets hermes files
	hAgent := provider.AgentConfig{Name: "acme", Type: provider.AgentTypeTeam, Runtime: "hermes"}
	hFiles, _, _, err := ComposeAgentWorkspaceFiles(dir, hAgent, nil, nil)
	if err != nil {
		t.Fatalf("hermes: unexpected error: %v", err)
	}
	if string(hFiles["SOUL.md"].Content) != "hermes team soul" {
		t.Errorf("hermes SOUL.md = %q, want 'hermes team soul'", string(hFiles["SOUL.md"].Content))
	}
}
