//go:build integration

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

		// Modify the on-disk copy from the HOST (via the bind-mount source), not
		// from inside the container: the restored blob is owned by the host user
		// (a non-root host can't chown it to uid 1000), so the container user
		// can't write it. Simulating a runtime token refresh host-side is
		// ownership-independent and works on both Linux CI and Docker Desktop.
		out, err := exec.Command("docker", "inspect", "-f",
			`{{range .Mounts}}{{if eq .Destination "/home/node/.openclaw"}}{{.Source}}{{end}}{{end}}`, cName).Output()
		if err != nil {
			t.Fatalf("docker inspect mount source: %v", err)
		}
		hostBlob := filepath.Join(strings.TrimSpace(string(out)), "mcp-oauth", "linear-test.json")
		const newer = `{"tokens":{"refresh_token":"ONDISK-NEWER"}}`
		if err := os.WriteFile(hostBlob, []byte(newer), 0o644); err != nil {
			t.Fatalf("could not overwrite on-disk blob at %s: %v", hostBlob, err)
		}

		mustRunCLI(t, append(base, "refresh", "--agent", agentName)...)
		assertContainerRunning(t, agentName)

		got, err := dockerExec(t, cName, "cat", blobPath)
		if err != nil {
			t.Fatalf("blob missing after refresh: %v", err)
		}
		if strings.TrimSpace(got) != newer {
			t.Errorf("cold-only violated: refresh overwrote the authoritative on-disk copy.\n got: %s\nwant: %s", got, newer)
		}
	})

	t.Run("teardown", func(t *testing.T) {
		mustRunCLI(t, append(base, "admin", "teardown", "--force")...)
	})
}
