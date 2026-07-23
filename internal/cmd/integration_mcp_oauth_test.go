//go:build integration

package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// TestMCPOAuthRestoreOnRefresh verifies the Phase 2 durability path end-to-end on
// a real local container: a persisted mcp-oauth/ credential blob is materialized
// into the container's data dir on refresh (so an agent comes back with its token
// after a data-dir loss), never leaks into the environment, and is cold-only
// (never overwrites the runtime's authoritative on-disk copy).
func TestMCPOAuthRestoreOnRefresh(t *testing.T) {
	requireDocker(t)
	dataDir, agentName := setupTestEnv(t)
	base := baseArgs(dataDir)
	parent := t

	// Path inside the container where OpenClaw keeps MCP OAuth blobs.
	const blobPath = "/home/node/.openclaw/mcp-oauth/linear-test.json"
	// Synthetic blob (not a real credential). Sentinels let us assert content
	// and prove no leak into the environment.
	const blob = `{"tokens":{"access_token":"ACCESS-SENTINEL","refresh_token":"REFRESH-SENTINEL"}}`
	const secretName = "mcp-oauth/linear-test.json"

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(`{"image":%q}`, testImage)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-user", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	// Fresh agent has no mcp-oauth/ dir on disk — a genuine "cold" slot, as after
	// a fresh provision or data-dir loss.
	t.Run("blob-absent-before-restore", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cName := "conga-" + agentName
		if out, err := dockerExec(t, cName, "cat", blobPath); err == nil {
			t.Fatalf("expected no blob before restore, but found: %s", out)
		}
	})

	t.Run("store-oauth-secret", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "secrets", "set", secretName, "--value", blob, "--agent", agentName)...)
	})

	t.Run("refresh-restores-blob-into-container", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)

		cName := "conga-" + agentName
		got, err := dockerExec(t, cName, "cat", blobPath)
		if err != nil {
			t.Fatalf("blob not restored into container at %s: %v", blobPath, err)
		}
		if strings.TrimSpace(got) != blob {
			t.Errorf("restored blob mismatch:\n got: %s\nwant: %s", got, blob)
		}
	})

	t.Run("blob-never-leaks-into-env", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cName := "conga-" + agentName
		env, err := dockerExec(t, cName, "env")
		if err != nil {
			t.Fatalf("could not read container env: %v", err)
		}
		if strings.Contains(env, "SENTINEL") || strings.Contains(env, "mcp-oauth") {
			t.Errorf("MCP OAuth blob leaked into the container environment:\n%s", env)
		}
	})

	// Cold-only: once the runtime owns an on-disk copy (kept fresh as the token
	// refreshes), a subsequent refresh must NOT overwrite it from the (older)
	// stored secret.
	t.Run("refresh-does-not-clobber-authoritative-on-disk-copy", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cName := "conga-" + agentName

		// Change the STORED secret so it differs from the on-disk copy, then
		// refresh. Cold-only means the existing on-disk file (the runtime's
		// authoritative copy) must NOT be overwritten from the changed secret —
		// so the container must still hold the ORIGINAL blob, not the new value.
		// (Modifying the secret is ownership-independent, unlike writing the
		// 0600/0644 blob file which is owned by uid 1000 in a cap-dropped
		// container.)
		mustRunCLI(t, append(base, "secrets", "set", secretName,
			"--value", `{"tokens":{"refresh_token":"STORED-CHANGED"}}`, "--agent", agentName)...)

		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)

		got, err := dockerExec(t, cName, "cat", blobPath)
		if err != nil {
			t.Fatalf("blob missing after refresh: %v", err)
		}
		if strings.TrimSpace(got) != blob {
			t.Errorf("cold-only violated: refresh overwrote the authoritative on-disk copy from the changed secret.\n got: %s\nwant (original): %s", got, blob)
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}
