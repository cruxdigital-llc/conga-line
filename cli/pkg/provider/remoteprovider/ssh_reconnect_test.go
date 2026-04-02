package remoteprovider

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testSSHServer starts a minimal SSH server on a random port.
// The handler receives each exec command and returns (stdout, exit code).
// Returns the port and a stop function that shuts down the server and all active connections.
func testSSHServer(t *testing.T, handler func(cmd string) (string, int)) (int, func()) {
	t.Helper()

	// Generate an ephemeral host key
	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatalf("signer from host key: %v", err)
	}

	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	var mu sync.Mutex
	var conns []net.Conn

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			go handleTestConn(conn, config, handler)
		}
	}()

	stop := func() {
		listener.Close()
		<-done
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		conns = nil
		mu.Unlock()
	}

	return port, stop
}

func handleTestConn(conn net.Conn, config *ssh.ServerConfig, handler func(string) (string, int)) {
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}

		ch, requests, err := newChan.Accept()
		if err != nil {
			continue
		}

		go func() {
			defer ch.Close()
			for req := range requests {
				if req.Type == "exec" {
					// Parse the command from the exec payload
					cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
					cmd := string(req.Payload[4 : 4+cmdLen])

					stdout, exitCode := handler(cmd)
					ch.Write([]byte(stdout))
					req.Reply(true, nil)

					// Send exit status
					exitMsg := ssh.Marshal(struct{ Status uint32 }{uint32(exitCode)})
					ch.SendRequest("exit-status", false, exitMsg)
					return
				}
				if req.Type == "keepalive@openssh.com" {
					req.Reply(true, nil)
					continue
				}
				if req.WantReply {
					req.Reply(false, nil)
				}
			}
		}()
	}
}

// connectToTestServer creates an SSHClient connected to a test server on the given port.
// Uses InsecureIgnoreHostKey because the test server generates ephemeral host keys.
func connectToTestServer(t *testing.T, port int) *SSHClient {
	t.Helper()

	config := &ssh.ClientConfig{
		User:            "test",
		Auth:            []ssh.AuthMethod{ssh.Password("")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // OK: ephemeral test server
		Timeout:         5 * time.Second,
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("dial test server: %v", err)
	}

	return &SSHClient{
		client: client,
		config: config,
		host:   "127.0.0.1",
		port:   port,
		user:   "test",
	}
}

// testSSHServerOnPort starts a test SSH server on a specific port. Retries briefly
// in case the OS hasn't fully released the port yet.
func testSSHServerOnPort(t *testing.T, port int, handler func(cmd string) (string, int)) func() {
	t.Helper()

	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatalf("signer from host key: %v", err)
	}

	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)

	var listener net.Listener
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for range 20 {
		listener, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("listen on port %d: %v", port, err)
	}

	var mu sync.Mutex
	var conns []net.Conn

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
			go handleTestConn(conn, config, handler)
		}
	}()

	return func() {
		listener.Close()
		<-done
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		conns = nil
		mu.Unlock()
	}
}

func TestSessionReconnectsOnStaleConnection(t *testing.T) {
	port, stop := testSSHServer(t, func(cmd string) (string, int) {
		return "pong1", 0
	})

	c := connectToTestServer(t, port)
	defer c.Close()

	// First command works
	out, err := c.Run(context.Background(), "ping")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if strings.TrimSpace(out) != "pong1" {
		t.Fatalf("first run: got %q, want %q", out, "pong1")
	}

	// Kill the server (simulates connection death)
	stop()

	// Start a new server on the same port with different response
	stop2 := testSSHServerOnPort(t, port, func(cmd string) (string, int) {
		return "pong2", 0
	})
	defer stop2()

	// Next command should fail on the dead connection, reconnect, and succeed
	out, err = c.Run(context.Background(), "ping")
	if err != nil {
		t.Fatalf("run after reconnect: %v", err)
	}
	if strings.TrimSpace(out) != "pong2" {
		t.Fatalf("run after reconnect: got %q, want %q", out, "pong2")
	}
}

func TestSessionFailsWhenServerTrulyDown(t *testing.T) {
	port, stop := testSSHServer(t, func(cmd string) (string, int) {
		return "pong", 0
	})

	c := connectToTestServer(t, port)
	defer c.Close()

	// First command works
	_, err := c.Run(context.Background(), "ping")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Kill the server, do NOT restart
	stop()

	// Next command should fail with both session and reconnect errors
	_, err = c.Run(context.Background(), "ping")
	if err == nil {
		t.Fatal("expected error when server is down, got nil")
	}
	if !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("error should mention reconnect failure, got: %v", err)
	}
}

func TestReconnectPreservesParameters(t *testing.T) {
	port, stop := testSSHServer(t, func(cmd string) (string, int) {
		return "ok", 0
	})

	c := connectToTestServer(t, port)
	defer c.Close()

	origHost := c.host
	origPort := c.port
	origUser := c.user
	origConfig := c.config

	// Kill and restart
	stop()
	stop2 := testSSHServerOnPort(t, port, func(cmd string) (string, int) {
		return "ok", 0
	})
	defer stop2()

	// Trigger reconnect
	_, err := c.Run(context.Background(), "check")
	if err != nil {
		t.Fatalf("run after reconnect: %v", err)
	}

	if c.host != origHost || c.port != origPort || c.user != origUser || c.config != origConfig {
		t.Fatalf("parameters changed: host=%q port=%d user=%q",
			c.host, c.port, c.user)
	}
}

func TestRunSucceedsWithoutReconnect(t *testing.T) {
	callCount := 0
	port, stop := testSSHServer(t, func(cmd string) (string, int) {
		callCount++
		return fmt.Sprintf("response-%d", callCount), 0
	})
	defer stop()

	c := connectToTestServer(t, port)
	defer c.Close()

	// Save the original client pointer to detect reconnects
	origClient := c.client

	for i := 1; i <= 5; i++ {
		out, err := c.Run(context.Background(), "test")
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		expected := fmt.Sprintf("response-%d", i)
		if strings.TrimSpace(out) != expected {
			t.Fatalf("run %d: got %q, want %q", i, out, expected)
		}
	}

	// Verify no reconnect happened (same client pointer)
	if c.client != origClient {
		t.Fatal("client was replaced — reconnect happened on healthy connection")
	}
}

func TestSftpClientReconnectsOnStaleConnection(t *testing.T) {
	// The SFTP handshake opens a subsystem channel on the SSH connection.
	// When the connection is dead, sftp.NewClient fails with a connection error.
	// sftpClient() should reconnect and retry.
	port, stop := testSSHServer(t, func(cmd string) (string, int) {
		return "ok", 0
	})

	c := connectToTestServer(t, port)
	defer c.Close()

	// Verify the connection works (via a session, since our test server
	// doesn't support the SFTP subsystem — the important thing is that
	// sftpClient() triggers a reconnect on the dead connection)
	_, err := c.Run(context.Background(), "ping")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Kill the server
	stop()

	// Start a new server on the same port
	stop2 := testSSHServerOnPort(t, port, func(cmd string) (string, int) {
		return "ok", 0
	})
	defer stop2()

	// sftpClient() should fail on the dead connection, reconnect, then
	// fail again because the test server doesn't support SFTP subsystem —
	// but the client pointer should have been replaced (proving reconnect happened)
	origClient := c.client
	_, sftpErr := c.sftpClient()
	// SFTP will fail (no subsystem support in test server), but reconnect should have fired
	if sftpErr == nil {
		t.Fatal("expected SFTP error (test server has no SFTP subsystem), got nil")
	}
	if c.client == origClient {
		t.Fatal("client was not replaced — reconnect did not fire for SFTP")
	}

	// Verify the reconnected connection is healthy by running a command
	out, err := c.Run(context.Background(), "verify")
	if err != nil {
		t.Fatalf("run after SFTP reconnect: %v", err)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("run after SFTP reconnect: got %q, want %q", out, "ok")
	}
}
