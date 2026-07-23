//go:build integration

package cmd

import (
	"fmt"
	"strings"
	"testing"
)

// TestMCPOAuthRestoreOnRefreshRemote verifies the Phase 2 durability path through
// the remote provider's SSH+SFTP code paths: a persisted mcp-oauth/ blob is
// materialized into the container's data dir on refresh, never leaks into env,
// and restore is cold-only. Mirrors TestMCPOAuthRestoreOnRefresh (local).
func TestMCPOAuthRestoreOnRefreshRemote(t *testing.T) {
	dataDir, agentName, sshPort, keyPath, remoteDir := setupRemoteTestEnv(t)
	base := remoteBaseArgs(dataDir)
	root := repoRoot(t)
	parent := t

	const blobPath = "/home/node/.openclaw/mcp-oauth/linear-test.json"
	const blob = `{"tokens":{"access_token":"ACCESS-SENTINEL","refresh_token":"REFRESH-SENTINEL"}}`
	const secretName = "mcp-oauth/linear-test.json"

	t.Run("setup", func(t *testing.T) {
		cfg := fmt.Sprintf(
			`{"ssh_host":"127.0.0.1","ssh_port":%d,"ssh_user":"root","ssh_key_path":%q,"image":%q,"repo_path":%q,"remote_dir":%q}`,
			sshPort, keyPath, testImage, root, remoteDir)
		mustRunCLI(t, append(base, "admin", "setup", "--json", cfg)...)
	})

	t.Run("add-user", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		mustRunCLI(t, append(base, "admin", "add-user", agentName)...)
		assertContainerRunning(t, agentName)
	})

	t.Run("blob-absent-before-restore", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		if out, err := dockerExec(t, "conga-"+agentName, "cat", blobPath); err == nil {
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

		got, err := dockerExec(t, "conga-"+agentName, "cat", blobPath)
		if err != nil {
			t.Fatalf("blob not restored into container at %s: %v", blobPath, err)
		}
		if strings.TrimSpace(got) != blob {
			t.Errorf("restored blob mismatch:\n got: %s\nwant: %s", got, blob)
		}
	})

	t.Run("blob-never-leaks-into-env", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		env, err := dockerExec(t, "conga-"+agentName, "env")
		if err != nil {
			t.Fatalf("could not read container env: %v", err)
		}
		if strings.Contains(env, "SENTINEL") || strings.Contains(env, "mcp-oauth") {
			t.Errorf("MCP OAuth blob leaked into the container environment:\n%s", env)
		}
	})

	t.Run("refresh-does-not-clobber-authoritative-on-disk-copy", func(t *testing.T) {
		skipIfPriorFailed(t, parent)
		cName := "conga-" + agentName

		// Change the STORED secret so it differs from the on-disk copy, then
		// refresh. Cold-only means the existing on-disk file (authoritative) must
		// NOT be overwritten from the changed secret — the container must still
		// hold the ORIGINAL blob. (Ownership-independent: modifies the secret, not
		// the uid-1000-owned blob file in a cap-dropped container.)
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
