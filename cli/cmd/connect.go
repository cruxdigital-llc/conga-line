package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var (
	connectLocalPort int
	connectNoPairing bool
)

func init() {
	connectCmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to OpenClaw web UI",
		RunE:  connectRun,
	}
	connectCmd.Flags().IntVar(&connectLocalPort, "local-port", 0, "Local port (default: agent's gateway port)")
	connectCmd.Flags().BoolVar(&connectNoPairing, "no-pairing", false, "Skip device pairing poll")
	rootCmd.AddCommand(connectCmd)
}

func connectRun(cmd *cobra.Command, args []string) error {
	setupCtx, setupCancel := commandContext()
	defer setupCancel()

	agentName, err := resolveAgentName(setupCtx)
	if err != nil {
		return err
	}

	info, err := prov.Connect(setupCtx, agentName, connectLocalPort)
	if err != nil {
		return err
	}

	fmt.Printf("\nOpen in your browser:\n  %s\n\n", info.URL)
	fmt.Println("Press Ctrl+C to disconnect.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()

	// Start device pairing poll in background
	if !connectNoPairing {
		go pollDevicePairing(sessionCtx, agentName)
	}

	if info.Waiter != nil {
		// AWS: block on tunnel
		select {
		case <-sigCh:
			fmt.Println("\nClosing connection...")
			sessionCancel()
		case err := <-info.Waiter:
			if err != nil {
				return fmt.Errorf("connection exited: %w", err)
			}
		}
	} else {
		// Local: stay alive for pairing and UX
		<-sigCh
		fmt.Println("\nDisconnected.")
	}

	return nil
}

// pollDevicePairing watches for pending device pairing requests and auto-approves them.
func pollDevicePairing(ctx context.Context, agentName string) {
	container := "conga-" + agentName
	fmt.Println("Watching for device pairing requests...")

	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}

		// List pending devices
		output, err := dockerExec(ctx, container, "npx", "openclaw", "devices", "list", "--json")
		if err != nil {
			continue
		}

		var devList struct {
			Pending []json.RawMessage `json:"pending"`
		}
		if err := json.Unmarshal([]byte(output), &devList); err != nil {
			continue
		}
		if len(devList.Pending) == 0 {
			continue
		}

		// Approve latest pending device
		output, err = dockerExec(ctx, container, "npx", "openclaw", "devices", "approve", "--latest")
		if err != nil {
			continue
		}

		if strings.Contains(output, "Approved") {
			fmt.Println("Device paired! Refresh your browser if needed.")
			return
		}
	}
}

// dockerExec runs a command inside a Docker container and returns stdout.
func dockerExec(ctx context.Context, container string, args ...string) (string, error) {
	cmdArgs := append([]string{"exec", container}, args...)
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker exec: %s (%w)", strings.TrimSpace(stderr.String()), err)
	}
	return stdout.String(), nil
}
