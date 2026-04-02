// Package remoteprovider implements the Provider interface for SSH-accessible hosts.
package remoteprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	posixpath "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHClient wraps an *ssh.Client with convenience methods.
// Stores connection parameters so it can transparently reconnect on stale connections.
//
// Thread safety: SSHClient is NOT safe for concurrent use. All callers (MCP tool calls)
// must be serialized. If concurrent access is needed in the future, mu must be held
// around session()/sftpClient()/reconnect() calls.
type SSHClient struct {
	mu     sync.Mutex // guards client replacement during reconnect
	client *ssh.Client
	config *ssh.ClientConfig // stored for reconnection
	host   string
	port   int
	user   string
}

// SSHTunnel represents an active SSH port forwarding tunnel.
type SSHTunnel struct {
	listener net.Listener
	done     chan error
	cancel   context.CancelFunc
}

// Wait blocks until the tunnel closes.
func (t *SSHTunnel) Wait() error {
	return <-t.done
}

// Stop closes the tunnel.
func (t *SSHTunnel) Stop() {
	t.cancel()
	t.listener.Close()
}

// SSHConnect establishes an SSH connection to the remote host.
// Key resolution order: explicit keyPath > SSH agent > ~/.ssh/id_ed25519 > ~/.ssh/id_rsa
func SSHConnect(host string, port int, user, keyPath string) (*SSHClient, error) {
	if port == 0 {
		port = 22
	}
	if user == "" {
		user = "root"
	}

	// Collect all available signers, then present them as a single auth method.
	// This avoids the issue where Go's SSH library treats a rejected key from
	// one auth method as a definitive failure instead of trying the next.
	var signers []ssh.Signer

	// 1. Explicit key path (highest priority)
	if keyPath != "" {
		if signer, err := keyFileSigner(keyPath); err == nil {
			signers = append(signers, signer)
		} else {
			return nil, fmt.Errorf("failed to read SSH key %s: %w", keyPath, err)
		}
	}

	// 2. Default key paths (ed25519 preferred over rsa)
	if keyPath == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			for _, name := range []string{"id_ed25519", "id_rsa"} {
				p := filepath.Join(home, ".ssh", name)
				if signer, err := keyFileSigner(p); err == nil {
					signers = append(signers, signer)
				}
			}
		}
	}

	// 3. SSH agent (adds any additional keys not already covered by files)
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			if agentSigners, err := agent.NewClient(conn).Signers(); err == nil {
				signers = append(signers, agentSigners...)
			}
		}
	}

	var authMethods []ssh.AuthMethod
	if len(signers) > 0 {
		authMethods = append(authMethods, ssh.PublicKeys(signers...))
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no SSH authentication methods available. Provide --ssh-key or start an ssh-agent")
	}

	// Host key verification
	hostKeyCallback, err := hostKeyVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to load known_hosts: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s@%s: %w", user, addr, err)
	}

	// Start SSH keepalive to prevent idle disconnects from NAT/firewalls.
	go sshKeepalive(client)

	return &SSHClient{
		client: client,
		config: config,
		host:   host,
		port:   port,
		user:   user,
	}, nil
}

// Run executes a command on the remote host and returns stdout.
func (c *SSHClient) Run(ctx context.Context, cmd string) (string, error) {
	stdout, stderr, err := c.RunWithStderr(ctx, cmd)
	if err != nil {
		errMsg := strings.TrimSpace(stderr)
		if errMsg != "" {
			return "", fmt.Errorf("%s (%w)", errMsg, err)
		}
		return "", err
	}
	return stdout, nil
}

// RunWithStderr executes a command and returns stdout and stderr separately.
func (c *SSHClient) RunWithStderr(ctx context.Context, cmd string) (string, string, error) {
	session, err := c.session()
	if err != nil {
		return "", "", fmt.Errorf("ssh session failed: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf strings.Builder
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Handle context cancellation
	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case err := <-done:
		return stdoutBuf.String(), stderrBuf.String(), err
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		return "", "", ctx.Err()
	}
}

// Upload writes content to a remote path with the specified permissions using SFTP.
// Uses atomic write (temp + rename) for files with restrictive permissions.
// TODO: Cache the SFTP client on SSHClient to avoid per-operation handshakes during setup.
func (c *SSHClient) Upload(path string, content []byte, perm os.FileMode) error {
	sftpClient, err := c.sftpClient()
	if err != nil {
		return c.uploadViaShell(path, content, perm)
	}
	defer sftpClient.Close()

	dir := posixpath.Dir(path)
	sftpClient.MkdirAll(dir)

	// Atomic write: temp file with unique suffix + rename.
	// Unique suffix prevents races when multiple uploads target the same path concurrently.
	var suffix [4]byte
	rand.Read(suffix[:])
	tmpPath := path + ".tmp." + hex.EncodeToString(suffix[:])
	f, err := sftpClient.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create remote file %s: %w", tmpPath, err)
	}

	if _, err := f.Write(content); err != nil {
		f.Close()
		sftpClient.Remove(tmpPath)
		return fmt.Errorf("failed to write remote file %s: %w", tmpPath, err)
	}
	f.Close()

	if err := sftpClient.Chmod(tmpPath, perm); err != nil {
		sftpClient.Remove(tmpPath)
		return fmt.Errorf("failed to chmod remote file %s: %w", tmpPath, err)
	}

	if err := sftpClient.Rename(tmpPath, path); err != nil {
		// Rename may fail if crossing filesystems; try Posix rename
		if err2 := sftpClient.PosixRename(tmpPath, path); err2 != nil {
			sftpClient.Remove(tmpPath)
			return fmt.Errorf("failed to rename remote file: %w", err)
		}
	}

	return nil
}

// uploadViaShell is a fallback when SFTP is not available.
func (c *SSHClient) uploadViaShell(path string, content []byte, perm os.FileMode) error {
	dir := posixpath.Dir(path)
	permStr := fmt.Sprintf("%04o", perm)

	session, err := c.session()
	if err != nil {
		return fmt.Errorf("ssh session failed: %w", err)
	}
	defer session.Close()

	// Pipe content to cat via stdin
	session.Stdin = strings.NewReader(string(content))
	cmd := fmt.Sprintf("mkdir -p %s && cat > %s && chmod %s %s",
		shellQuote(dir), shellQuote(path), permStr, shellQuote(path))
	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("failed to upload %s via shell: %w", path, err)
	}
	return nil
}

// Download reads a remote file's content.
func (c *SSHClient) Download(path string) ([]byte, error) {
	sftpClient, err := c.sftpClient()
	if err != nil {
		// Fallback to cat
		out, err := c.Run(context.Background(), "cat "+shellQuote(path))
		if err != nil {
			return nil, err
		}
		return []byte(out), nil
	}
	defer sftpClient.Close()

	f, err := sftpClient.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open remote file %s: %w", path, err)
	}
	defer f.Close()

	return io.ReadAll(f)
}

// UploadDir recursively uploads a local directory to a remote path.
func (c *SSHClient) UploadDir(localDir, remotePath string) error {
	sftpClient, err := c.sftpClient()
	if err != nil {
		return fmt.Errorf("SFTP not available: %w", err)
	}
	defer sftpClient.Close()

	return filepath.WalkDir(localDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		dstPath := posixpath.Join(remotePath, relPath)

		// Skip node_modules
		if strings.Contains(relPath, "node_modules") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return sftpClient.MkdirAll(dstPath)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		f, err := sftpClient.Create(dstPath)
		if err != nil {
			return fmt.Errorf("failed to create remote file %s: %w", dstPath, err)
		}
		defer f.Close()

		_, err = f.Write(data)
		return err
	})
}

// MkdirAll creates directories recursively on the remote host.
func (c *SSHClient) MkdirAll(path string, perm os.FileMode) error {
	permStr := fmt.Sprintf("%04o", perm)
	_, err := c.Run(context.Background(), fmt.Sprintf("mkdir -p %s && chmod %s %s",
		shellQuote(path), permStr, shellQuote(path)))
	return err
}

// ForwardPort creates an SSH tunnel (local port -> remote port).
// Note: ForwardPort intentionally does NOT use the reconnect wrapper. Tunnels are
// long-lived and reconnecting the SSH client mid-tunnel would invalidate the
// listener's connection state. If the SSH connection dies during a tunnel, the user
// re-runs the connect command, which creates a fresh tunnel on a reconnected client.
func (c *SSHClient) ForwardPort(ctx context.Context, localPort, remotePort int) (*SSHTunnel, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on local port %d: %w", localPort, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)

	tunnel := &SSHTunnel{
		listener: listener,
		done:     done,
		cancel:   cancel,
	}

	go func() {
		defer close(done)
		for {
			localConn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					done <- err
					return
				}
			}

			remoteAddr := fmt.Sprintf("localhost:%d", remotePort)
			remoteConn, err := c.client.Dial("tcp", remoteAddr)
			if err != nil {
				localConn.Close()
				continue
			}

			go tunnelCopy(ctx, localConn, remoteConn)
		}
	}()

	return tunnel, nil
}

// tunnelCopy bidirectionally copies between two connections.
// When either direction finishes, both connections are closed immediately
// so the other direction unblocks. This prevents WebSocket and HTTP
// connections from hanging when one side closes.
func tunnelCopy(ctx context.Context, local, remote net.Conn) {
	done := make(chan struct{}, 1)

	go func() {
		io.Copy(remote, local)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(local, remote)
		done <- struct{}{}
	}()

	// As soon as one direction finishes, close both connections
	// to unblock the other direction.
	select {
	case <-done:
	case <-ctx.Done():
	}

	local.Close()
	remote.Close()
}

// Close closes the SSH connection.
func (c *SSHClient) Close() error {
	return c.client.Close()
}

// reconnect closes the dead connection and establishes a new one using stored parameters.
func (c *SSHClient) reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.client.Close(); err != nil {
		// Log but don't fail — the connection is likely already dead.
		fmt.Fprintf(os.Stderr, "ssh: closing stale connection: %v\n", err)
	}
	addr := fmt.Sprintf("%s:%d", c.host, c.port)
	client, err := ssh.Dial("tcp", addr, c.config)
	if err != nil {
		return fmt.Errorf("ssh reconnect failed: %w", err)
	}
	c.client = client
	go sshKeepalive(client)
	return nil
}

// isConnectionError returns true if the error likely indicates a dead or broken SSH
// connection. The SSH library doesn't expose a clean error type hierarchy, so we
// default to true (attempt reconnect) and only return false for errors that are
// clearly channel-level rejections where the connection itself is fine.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Channel open rejections mean the server is alive but refused this specific request.
	// Reconnecting won't help — the server will reject again.
	if strings.Contains(msg, "administratively prohibited") ||
		strings.Contains(msg, "too many open sessions") {
		return false
	}
	// Default: assume the connection is broken and attempt reconnect.
	// This is safe because we only retry once — worst case we reconnect unnecessarily.
	return true
}

// session creates a new SSH session, reconnecting once if the connection is stale.
func (c *SSHClient) session() (*ssh.Session, error) {
	session, err := c.client.NewSession()
	if err == nil {
		return session, nil
	}
	// Only reconnect for connection-level errors, not transient channel errors
	if !isConnectionError(err) {
		return nil, err
	}
	if reconnErr := c.reconnect(); reconnErr != nil {
		return nil, fmt.Errorf("ssh session failed (reconnect also failed: %v): %w", reconnErr, err)
	}
	return c.client.NewSession()
}

// sftpClient creates a new SFTP client, reconnecting once if the connection is stale.
func (c *SSHClient) sftpClient() (*sftp.Client, error) {
	sc, err := sftp.NewClient(c.client)
	if err == nil {
		return sc, nil
	}
	// Only reconnect for connection-level errors, not transient channel errors
	if !isConnectionError(err) {
		return nil, err
	}
	if reconnErr := c.reconnect(); reconnErr != nil {
		return nil, fmt.Errorf("sftp failed (reconnect also failed: %v): %w", reconnErr, err)
	}
	return sftp.NewClient(c.client)
}

// sshKeepalive sends periodic keepalive requests to prevent idle disconnects.
// Stops automatically when the connection is closed.
func sshKeepalive(client *ssh.Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		if err != nil {
			return
		}
	}
}

// --- helpers ---

// keyFileSigner parses a private key file and returns a signer.
func keyFileSigner(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

// hostKeyVerifier returns a host key callback that checks ~/.ssh/known_hosts.
// If the file doesn't exist, it falls back to accepting all keys with a warning.
func hostKeyVerifier() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: cannot determine home directory — host key verification disabled. Connection may be insecure.\n")
		return ssh.InsecureIgnoreHostKey(), nil
	}

	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "WARNING: %s not found — host key verification disabled. Connection may be insecure.\n", knownHostsPath)
		return ssh.InsecureIgnoreHostKey(), nil
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, err
	}
	return callback, nil
}

// shellQuote quotes a string for safe use in a POSIX shell command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// shelljoin quotes and joins arguments for a shell command.
func shelljoin(args ...string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		// Don't quote simple safe strings (optimization for readability in logs)
		if isSafeArg(arg) {
			quoted[i] = arg
		} else {
			quoted[i] = shellQuote(arg)
		}
	}
	return strings.Join(quoted, " ")
}

// isSafeArg returns true if the string contains only safe shell characters.
// '=' is included because Docker -e flags use KEY=VALUE (e.g. NODE_OPTIONS=--max-old-space-size=1536).
func isSafeArg(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '/' || c == ':' || c == '=' || c == ',') {
			return false
		}
	}
	return true
}
