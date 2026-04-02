package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	awsutil "github.com/cruxdigital-llc/conga-line/cli/pkg/aws"
)

func CheckPlugin() error {
	_, err := exec.LookPath("session-manager-plugin")
	if err != nil {
		return fmt.Errorf("session-manager-plugin not found on PATH.\n\n%s", InstallHint())
	}
	return nil
}

func InstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Install with: brew install --cask session-manager-plugin"
	case "linux":
		return "Install from: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html"
	case "windows":
		return "Download MSI from: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html"
	default:
		return "Install the AWS Session Manager Plugin for your platform."
	}
}

type Tunnel struct {
	cmd       *exec.Cmd
	SessionID string
}

func StartTunnel(ctx context.Context, ssmClient awsutil.SSMClient, instanceID string, remotePort, localPort int, region, profile string) (*Tunnel, error) {
	input := &ssm.StartSessionInput{
		Target:       aws.String(instanceID),
		DocumentName: aws.String("AWS-StartPortForwardingSession"),
		Parameters: map[string][]string{
			"portNumber":      {strconv.Itoa(remotePort)},
			"localPortNumber": {strconv.Itoa(localPort)},
		},
	}

	out, err := ssmClient.StartSession(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to start SSM session: %w", err)
	}

	sessionJSON, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session response: %w", err)
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session input: %w", err)
	}

	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", region)

	cmd := exec.CommandContext(ctx, "session-manager-plugin",
		string(sessionJSON),
		region,
		"StartSession",
		profile,
		string(inputJSON),
		endpoint,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start session-manager-plugin: %w", err)
	}

	return &Tunnel{
		cmd:       cmd,
		SessionID: aws.ToString(out.SessionId),
	}, nil
}

func (t *Tunnel) Wait() error {
	return t.cmd.Wait()
}

func (t *Tunnel) Stop() error {
	if t.cmd.Process == nil {
		return nil
	}
	// Try graceful SIGTERM first so session-manager-plugin can clean up the SSM session
	t.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- t.cmd.Wait() }()
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return t.cmd.Process.Kill()
	}
}
