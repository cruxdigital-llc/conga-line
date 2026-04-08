//go:build integration

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

// setupPolicyTestEnv creates an isolated test environment for tests that
// don't need Docker (e.g. policy validation). Only creates a temp data dir.
func setupPolicyTestEnv(t *testing.T) (dataDir string) {
	t.Helper()
	dataDir = filepath.Join(t.TempDir(), ".conga")
	t.Cleanup(func() {
		runCLI(t, "--provider", "local", "--data-dir", dataDir,
			"admin", "teardown", "--force")
	})
	return dataDir
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
	safeURL := strings.ReplaceAll(url, "'", "\\'")
	script := fmt.Sprintf(`
		setTimeout(() => { process.stderr.write('timeout'); process.exit(1); }, 5000);
		const https = require('https');
		const req = https.get('%s', {timeout: 3000}, (res) => {
			process.stdout.write(String(res.statusCode));
			process.exit(0);
		});
		req.on('timeout', () => { req.destroy(); process.stderr.write('timeout'); process.exit(1); });
		req.on('error', (e) => { process.stderr.write(e.message); process.exit(1); });
	`, safeURL)
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

// --- Remote provider test helpers ---

const (
	sshContainerName = "conga-test-sshd"
	sshImageName     = "conga-test-sshd"
)

var buildSSHImageOnce sync.Once

// buildSSHImage builds the test SSH container image (once per process).
func buildSSHImage(t *testing.T) {
	t.Helper()
	buildSSHImageOnce.Do(func() {
		dockerfilePath := filepath.Join("internal", "cmd", "testdata", "sshd")
		// Try relative to repo root if cwd is different
		if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
			dockerfilePath = filepath.Join(repoRoot(t), "internal", "cmd", "testdata", "sshd")
		}
		out, err := exec.Command("docker", "build", "-t", sshImageName, dockerfilePath).CombinedOutput()
		if err != nil {
			t.Fatalf("failed to build SSH image: %v\n%s", err, out)
		}
	})
}

// generateSSHKey creates an ephemeral ed25519 key pair in a temp directory.
// Returns the directory containing id_test (private) and id_test.pub (public).
func generateSSHKey(t *testing.T) string {
	t.Helper()
	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "id_test")
	out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "").CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen failed: %v\n%s", err, out)
	}
	return keyDir
}

// startSSHContainer starts the test SSH container with Docker socket and
// authorized_keys mounted. Also creates a shared host directory for /opt/conga/
// so that Docker mounts from the remote provider work (Docker Desktop resolves
// -v paths relative to the host, not the SSH container).
func startSSHContainer(t *testing.T, keyDir, hash string) (port int, remoteDir string) {
	t.Helper()
	// Remove any stale container
	exec.Command("docker", "rm", "-f", sshContainerName).Run()

	// The remote provider uses a configurable base directory on the "remote host".
	// Docker -v mounts resolve against the HOST filesystem (not the SSH container).
	// We use a temp dir under /tmp/ (shared by Docker Desktop) so both the SSH
	// container and Docker daemon see the same files. The remote_dir is passed
	// via --json to admin setup.
	remoteDir = filepath.Join(os.TempDir(), fmt.Sprintf("conga-rtest-%s", hash))
	os.MkdirAll(remoteDir, 0755)
	t.Cleanup(func() { os.RemoveAll(remoteDir) })

	pubKeyPath := filepath.Join(keyDir, "id_test.pub")
	out, err := exec.Command("docker", "run", "-d",
		"--name", sshContainerName,
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", pubKeyPath+":/root/.ssh/authorized_keys:ro",
		"-v", remoteDir+":"+remoteDir,
		"-p", "0:22",
		sshImageName,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("failed to start SSH container: %v\n%s", err, out)
	}

	// Extract the assigned host port
	portOut, err := exec.Command("docker", "port", sshContainerName, "22").Output()
	if err != nil {
		t.Fatalf("failed to get SSH container port: %v", err)
	}
	// Output format: "0.0.0.0:12345\n" or "[::]:12345\n"
	portStr := strings.TrimSpace(string(portOut))
	parts := strings.Split(portStr, ":")
	port, parseErr := strconv.Atoi(parts[len(parts)-1])
	if parseErr != nil {
		t.Fatalf("failed to parse SSH port from %q: %v", portStr, parseErr)
	}
	return port, remoteDir
}

// waitForSSH polls until the SSH port is accepting connections, then adds
// the host key to ~/.ssh/known_hosts so the SSH client doesn't reject it.
func waitForSSH(t *testing.T, port int) {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < 20; i++ {
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err == nil {
			conn.Close()
			// Scan and add host key to known_hosts
			addSSHHostKey(t, port)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("SSH container not reachable at %s after 10s", addr)
}

// addSSHHostKey scans the SSH container's host key and adds it to known_hosts.
func addSSHHostKey(t *testing.T, port int) {
	t.Helper()
	out, err := exec.Command("ssh-keyscan", "-p", strconv.Itoa(port), "127.0.0.1").Output()
	if err != nil || len(out) == 0 {
		t.Logf("WARNING: ssh-keyscan failed (host key verification may fail): %v", err)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")

	// Read existing content to check for duplicates and to restore later
	existing, _ := os.ReadFile(knownHostsPath)

	// Append the new key
	f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Logf("WARNING: cannot write known_hosts: %v", err)
		return
	}
	f.Write(out)
	f.Close()

	// Restore original known_hosts on cleanup
	t.Cleanup(func() {
		os.WriteFile(knownHostsPath, existing, 0600)
	})
}

// stopSSHContainer removes the test SSH container.
func stopSSHContainer() {
	exec.Command("docker", "rm", "-f", sshContainerName).Run()
}

// setupRemoteTestEnv creates a remote test environment: builds SSH image,
// generates keys, starts SSH container, creates isolated data dir.
func setupRemoteTestEnv(t *testing.T) (dataDir, agentName string, sshPort int, keyPath, remoteDir string) {
	t.Helper()
	requireDocker(t)

	hash := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(t.Name())))
	if len(hash) > 8 {
		hash = hash[:8]
	}

	buildSSHImage(t)
	keyDir := generateSSHKey(t)
	sshPort, remoteDir = startSSHContainer(t, keyDir, hash)
	waitForSSH(t, sshPort)

	dataDir = filepath.Join(t.TempDir(), ".conga")
	agentName = "rtest-" + hash
	keyPath = filepath.Join(keyDir, "id_test")

	// Cleanup in LIFO order: teardown first (needs SSH), then containers, then SSH.
	t.Cleanup(func() { stopSSHContainer() })
	t.Cleanup(func() { cleanupTestContainers(agentName) })
	t.Cleanup(func() {
		if prov != nil {
			runCLI(t, "--provider", "remote", "--data-dir", dataDir,
				"admin", "teardown", "--force")
		}
	})

	return dataDir, agentName, sshPort, keyPath, remoteDir
}

// remoteBaseArgs returns the common CLI args for remote provider commands.
func remoteBaseArgs(dataDir string) []string {
	return []string{"--provider", "remote", "--data-dir", dataDir}
}
