//go:build integration

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/pflag"
)

const testImage = "ghcr.io/openclaw/openclaw:2026.3.11"

// requireDocker skips the test if Docker is not available.
func requireDocker(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker not available, skipping integration test")
	}
}

// resetCLIState zeros all package-level flag variables, ui globals, and
// cobra's per-flag Changed bits so state doesn't leak between Execute() calls.
func resetCLIState() {
	flagProvider = ""
	flagDataDir = ""
	flagAgent = ""
	flagJSON = ""
	flagOutput = "text"
	flagVerbose = false
	flagRuntime = ""
	flagRegion = ""
	flagProfile = ""
	flagTimeout = 5 * time.Minute
	prov = nil
	ui.OutputJSON = false
	ui.JSONInputActive = false
	secretValue = ""
	secretForce = false
	adminForce = false
	adminDeleteSecrets = false
	behaviorAsName = ""

	// Reset cobra's "Changed" bits on all persistent flags.
	// Without this, flags set in one test leak into the next.
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
	})
}

// runCLI resets state, executes the CLI with the given args, and returns
// captured stdout/stderr. Captures real os.Stdout/os.Stderr since the CLI
// uses fmt.Printf (not cmd.OutOrStdout).
func runCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	resetCLIState()

	// Capture real stdout
	oldStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	// Capture real stderr
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	rootCmd.SetArgs(args)
	err = rootCmd.Execute()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var outBuf, errBuf bytes.Buffer
	outBuf.ReadFrom(rOut)
	errBuf.ReadFrom(rErr)

	return outBuf.String(), errBuf.String(), err
}

// mustRunCLI calls runCLI and fatals on error.
func mustRunCLI(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, err := runCLI(t, args...)
	if err != nil {
		t.Fatalf("CLI command failed: %v\nargs: %v\nstdout: %s\nstderr: %s", err, args, stdout, stderr)
	}
	return stdout
}

// setupTestEnv creates an isolated test environment with a temp data dir
// and unique agent name. Registers cleanup to teardown and remove containers.
func setupTestEnv(t *testing.T) (dataDir, agentName string) {
	t.Helper()
	requireDocker(t)

	dataDir = filepath.Join(t.TempDir(), ".conga")
	hash := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(t.Name())))
	if len(hash) > 8 {
		hash = hash[:8]
	}
	agentName = "itest-" + hash

	t.Cleanup(func() {
		// Graceful teardown
		runCLI(t, "--provider", "local", "--data-dir", dataDir,
			"admin", "teardown", "--force")
		// Belt-and-suspenders: kill leaked containers/networks
		cleanupTestContainers(agentName)
	})

	return dataDir, agentName
}

// repoRoot returns the congaline repo root.
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("failed to find repo root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// cleanupTestContainers force-removes containers and networks for a test agent.
func cleanupTestContainers(agentName string) {
	for _, name := range []string{
		"conga-" + agentName,
		"conga-egress-" + agentName,
	} {
		exec.Command("docker", "rm", "-f", name).Run()
	}
	exec.Command("docker", "network", "rm", "conga-"+agentName).Run()
}

// baseArgs returns the common CLI args for all commands in a test.
func baseArgs(dataDir string) []string {
	return []string{"--provider", "local", "--data-dir", dataDir}
}

// --- Docker assertion helpers ---

// dockerExec runs a command inside a container with a 10s timeout.
func dockerExec(t *testing.T, containerName string, cmd ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := append([]string{"exec", containerName}, cmd...)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// assertContainerRunning asserts the agent's container is running, with retries.
func assertContainerRunning(t *testing.T, agentName string) {
	t.Helper()
	cName := "conga-" + agentName
	for i := 0; i < 5; i++ {
		out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", cName).Output()
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("container %s is not running after 10s", cName)
}

// assertContainerNotExists asserts no container exists for the agent.
func assertContainerNotExists(t *testing.T, agentName string) {
	t.Helper()
	cName := "conga-" + agentName
	err := exec.Command("docker", "inspect", cName).Run()
	if err == nil {
		t.Fatalf("container %s still exists", cName)
	}
}

// assertContainerStopped asserts the agent's container exists but is not running.
func assertContainerStopped(t *testing.T, agentName string) {
	t.Helper()
	cName := "conga-" + agentName
	// Container may have been removed entirely by pause, or just stopped
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", cName).Output()
	if err != nil {
		// Container doesn't exist — that counts as stopped
		return
	}
	if strings.TrimSpace(string(out)) == "true" {
		t.Fatalf("container %s is still running (expected stopped)", cName)
	}
}

// assertEnvVar asserts an environment variable is set inside the container.
func assertEnvVar(t *testing.T, agentName, key, value string) {
	t.Helper()
	cName := "conga-" + agentName
	out, err := dockerExec(t, cName, "env")
	if err != nil {
		t.Fatalf("docker exec env failed: %v", err)
	}
	expected := key + "=" + value
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == expected {
			return
		}
	}
	t.Errorf("env var %s=%s not found in container %s", key, value, cName)
}

// assertNoEnvVar asserts an environment variable is NOT set inside the container.
func assertNoEnvVar(t *testing.T, agentName, key string) {
	t.Helper()
	cName := "conga-" + agentName
	out, err := dockerExec(t, cName, "env")
	if err != nil {
		// Container may not be ready yet — not fatal for a "not exists" check
		return
	}
	prefix := key + "="
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			t.Errorf("env var %s should NOT be in container %s, but found: %s", key, cName, line)
			return
		}
	}
}

// assertFileContent asserts a file inside the container contains the given string.
func assertFileContent(t *testing.T, agentName, path, contains string) {
	t.Helper()
	cName := "conga-" + agentName
	out, err := dockerExec(t, cName, "cat", path)
	if err != nil {
		t.Fatalf("cat %s in %s failed: %v\noutput: %s", path, cName, err, out)
	}
	if !strings.Contains(out, contains) {
		t.Errorf("file %s in %s does not contain %q\ngot: %s", path, cName, contains, out)
	}
}

// assertFileNotExists asserts a file does NOT exist inside the container.
func assertFileNotExists(t *testing.T, agentName, path string) {
	t.Helper()
	cName := "conga-" + agentName
	_, err := dockerExec(t, cName, "cat", path)
	if err == nil {
		t.Errorf("file %s should NOT exist in %s, but it does", path, cName)
	}
}

// makeHTTPRequest runs a node script inside the container to make an HTTP
// request through the egress proxy. Returns the status code or an error.
// Uses a 5s hard timeout via setTimeout to prevent hangs when the proxy
// drops connections silently.
func makeHTTPRequest(t *testing.T, agentName, url string) (int, error) {
	t.Helper()
	// The setTimeout ensures the process exits even if the HTTPS CONNECT
	// hangs at the socket level (which timeout: on the request won't catch).
	script := fmt.Sprintf(`
		setTimeout(() => { process.stderr.write('timeout'); process.exit(1); }, 5000);
		const https = require('https');
		const req = https.get('%s', {timeout: 3000}, (res) => {
			process.stdout.write(String(res.statusCode));
			process.exit(0);
		});
		req.on('timeout', () => { req.destroy(); process.stderr.write('timeout'); process.exit(1); });
		req.on('error', (e) => { process.stderr.write(e.message); process.exit(1); });
	`, url)
	cName := "conga-" + agentName
	out, err := dockerExec(t, cName, "node", "-e", script)
	if err != nil {
		return 0, fmt.Errorf("HTTP request failed: %v (output: %s)", err, out)
	}
	code, parseErr := strconv.Atoi(strings.TrimSpace(out))
	if parseErr != nil {
		return 0, fmt.Errorf("unexpected response: %q", out)
	}
	return code, nil
}

// writePolicyFile writes a conga-policy.yaml to the data directory.
func writePolicyFile(t *testing.T, dataDir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dataDir, "conga-policy.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write policy file: %v", err)
	}
}
