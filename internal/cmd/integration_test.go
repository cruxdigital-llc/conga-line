//go:build integration

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// skipIfPriorFailed returns true (and skips) when a prior subtest has
// already failed, preventing noisy cascading failures in sequential tests.
func skipIfPriorFailed(t *testing.T, parent *testing.T) {
	t.Helper()
	if parent.Failed() {
		t.Skip("skipped due to prior subtest failure")
	}
}

// TestAgentLifecycle exercises the full user-agent lifecycle: setup, provision,
// secrets, refresh, logs, pause/unpause, removal, teardown. Each subtest
// depends on the previous — if one fails, later ones are skipped.
func TestAgentLifecycle(t *testing.T) {
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)
	parent := t

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q}`, testImage)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)

		if _, err := os.Stat(filepath.Join(dataDir, "local-config.json")); err != nil {
			t.Fatalf("local-config.json not created: %v", err)
		}
	})

	t.Run("add-user", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("list-agents", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out := mustRunCLI(t, append(base, "admin", "list-agents", "--output", "json")...)
		if !strings.Contains(out, agentName) {
			t.Errorf("list-agents output does not contain %q:\n%s", agentName, out)
		}
	})

	t.Run("status", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out := mustRunCLI(t, append(base, "status", "--agent", agentName, "--output", "json")...)
		if !strings.Contains(out, `"running"`) {
			t.Errorf("status does not show running:\n%s", out)
		}
	})

	t.Run("secrets-set", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "secrets", "set", "test-key", "--value", "dummy123", "--agent", agentName)...)
	})

	t.Run("secrets-list", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		out := mustRunCLI(t, append(base, "secrets", "list", "--agent", agentName, "--output", "json")...)
		if !strings.Contains(out, "test-key") {
			t.Errorf("secrets list does not contain test-key:\n%s", out)
		}
	})

	t.Run("secrets-not-in-env-before-refresh", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertNoEnvVar(t, agentName, "TEST_KEY")
	})

	t.Run("refresh", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("secrets-in-env-after-refresh", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertEnvVar(t, agentName, "TEST_KEY", "dummy123")
	})

	t.Run("secrets-delete", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "secrets", "delete", "test-key", "--agent", agentName, "--force")...)
		out := mustRunCLI(t, append(base, "secrets", "list", "--agent", agentName, "--output", "json")...)
		if strings.Contains(out, "test-key") {
			t.Errorf("secret test-key still in list after delete:\n%s", out)
		}
	})

	t.Run("refresh-after-delete", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("secrets-gone-from-env", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertNoEnvVar(t, agentName, "TEST_KEY")
	})

	t.Run("logs", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Use docker logs directly — the CLI pipes through fmt.Print which
		// our stdout capture handles, but the container may need a moment
		// to produce output after restart.
		cName := "conga-" + agentName
		var out string
		for i := 0; i < 5; i++ {
			raw, _ := exec.Command("docker", "logs", "--tail", "10", cName).CombinedOutput()
			out = string(raw)
			if len(strings.TrimSpace(out)) > 0 {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if len(strings.TrimSpace(out)) == 0 {
			t.Error("docker logs output is empty after 10s")
		}
	})

	t.Run("pause", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "pause", agentName)...)
		assertContainerStopped(t, agentName)
	})

	t.Run("unpause", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "unpause", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("remove-agent", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "remove-agent", agentName, "--force", "--delete-secrets")...)
		assertContainerNotExists(t, agentName)
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestTeamAgentWithBehavior tests per-agent behavior file deployment and
// manifest reconciliation using a team agent with custom behavior files.
func TestTeamAgentWithBehavior(t *testing.T) {
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)
	root := repoRoot(t)
	parent := t

	workspacePath := "/home/node/.openclaw/data/workspace"

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q,"repo_path":%q}`, testImage, root)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("create-agent-behavior", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		agentBehaviorDir := filepath.Join(dataDir, "behavior", "agents", agentName)
		if err := os.MkdirAll(agentBehaviorDir, 0755); err != nil {
			t.Fatalf("failed to create agent behavior dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(agentBehaviorDir, "SOUL.md"),
			[]byte("# Test Soul\n\nThis is a test-specific SOUL.md."), 0644); err != nil {
			t.Fatalf("failed to write test SOUL.md: %v", err)
		}
	})

	t.Run("add-team", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-team", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("verify-soul-in-container", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertFileContent(t, agentName, workspacePath+"/SOUL.md", "Test Soul")
	})

	t.Run("verify-agents-default", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// AGENTS.md should come from default (not agent-specific)
		assertFileContent(t, agentName, workspacePath+"/AGENTS.md", "Your Workspace")
	})

	t.Run("verify-memory-pristine", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cName := "conga-" + agentName
		out, err := dockerExec(t, cName, "cat", workspacePath+"/MEMORY.md")
		if err != nil {
			t.Fatalf("failed to read MEMORY.md: %v", err)
		}
		if strings.TrimSpace(out) != "# Memory" {
			t.Errorf("MEMORY.md is not pristine: %q", out)
		}
	})

	t.Run("add-agents-md-override", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Write an agent-specific AGENTS.md (overriding the default)
		content := []byte("# Custom AGENTS.md\n\nAdded by integration test.")
		agentDir := filepath.Join(dataDir, "behavior", "agents", agentName)
		if err := os.WriteFile(filepath.Join(agentDir, "AGENTS.md"), content, 0644); err != nil {
			t.Fatalf("failed to write AGENTS.md: %v", err)
		}
	})

	t.Run("refresh-for-behavior", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("verify-agents-md-overridden", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		assertFileContent(t, agentName, workspacePath+"/AGENTS.md", "Custom AGENTS.md")
	})

	t.Run("remove-agents-md-override", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		os.Remove(filepath.Join(dataDir, "behavior", "agents", agentName, "AGENTS.md"))
	})

	t.Run("refresh-after-rm", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
	})

	t.Run("verify-agents-md-reverted", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Should revert to the default AGENTS.md
		assertFileContent(t, agentName, workspacePath+"/AGENTS.md", "Your Workspace")
	})

	t.Run("verify-memory-still-pristine", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cName := "conga-" + agentName
		out, err := dockerExec(t, cName, "cat", workspacePath+"/MEMORY.md")
		if err != nil {
			t.Fatalf("failed to read MEMORY.md: %v", err)
		}
		if strings.TrimSpace(out) != "# Memory" {
			t.Errorf("MEMORY.md was modified: %q", out)
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestPolicyValidate tests the policy validation command without Docker containers.
func TestPolicyValidate(t *testing.T) {
	dataDir := setupPolicyTestEnv(t)
	base := baseArgs(dataDir)

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q}`, testImage)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("write-valid-policy", func(t *testing.T) {
		writePolicyFile(t, dataDir, `apiVersion: conga.dev/v1alpha1
egress:
  mode: enforce
  allowed_domains:
    - api.anthropic.com
`)
	})

	t.Run("validate-passes", func(t *testing.T) {
		_, _, err := runCLI(t, append(base, "policy", "validate")...)
		if err != nil {
			t.Errorf("policy validate failed for valid policy: %v", err)
		}
	})

	t.Run("write-invalid-policy", func(t *testing.T) {
		writePolicyFile(t, dataDir, `egress:
  mode: enforce
`)
	})

	t.Run("validate-fails", func(t *testing.T) {
		_, stderr, err := runCLI(t, append(base, "policy", "validate")...)
		if err == nil {
			t.Fatal("policy validate should fail for missing apiVersion")
		}
		combined := stderr + err.Error()
		if !strings.Contains(strings.ToLower(combined), "apiversion") {
			t.Errorf("error should mention apiVersion, got: %s", combined)
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}

// TestEgressPolicyEnforcement verifies that the egress proxy actually controls
// outbound traffic from inside the container across all three policy modes.
func TestEgressPolicyEnforcement(t *testing.T) {
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)
	parent := t

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q}`, testImage)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-user", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("no-policy-blocks", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Default: no policy file → egress proxy deny-all
		_, err := makeHTTPRequest(t, agentName, "https://api.anthropic.com")
		if err == nil {
			t.Error("expected HTTP request to be blocked with no policy (deny-all)")
		}
	})

	t.Run("write-validate-policy", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		writePolicyFile(t, dataDir, `apiVersion: conga.dev/v1alpha1
egress:
  mode: validate
  allowed_domains:
    - api.anthropic.com
`)
	})

	t.Run("refresh-validate", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("validate-allows", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Validate mode: proxy logs but allows traffic
		code, err := makeHTTPRequest(t, agentName, "https://api.anthropic.com")
		if err != nil {
			t.Errorf("expected request to succeed in validate mode, got error: %v", err)
		} else {
			t.Logf("validate mode: api.anthropic.com returned HTTP %d", code)
		}
	})

	t.Run("write-enforce-policy", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		writePolicyFile(t, dataDir, `apiVersion: conga.dev/v1alpha1
egress:
  mode: enforce
  allowed_domains:
    - api.anthropic.com
`)
	})

	t.Run("refresh-enforce", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("enforce-allowed", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Enforce mode: allowed domain should get through
		code, err := makeHTTPRequest(t, agentName, "https://api.anthropic.com")
		if err != nil {
			t.Errorf("expected request to api.anthropic.com to succeed in enforce mode, got error: %v", err)
		} else {
			t.Logf("enforce mode: api.anthropic.com returned HTTP %d", code)
		}
	})

	t.Run("enforce-blocked", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		// Enforce mode: non-allowed domain should be blocked
		_, err := makeHTTPRequest(t, agentName, "https://example.com")
		if err == nil {
			t.Error("expected request to example.com to be blocked in enforce mode")
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}
