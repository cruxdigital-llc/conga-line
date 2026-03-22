package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/spf13/cobra"
)

var connectNoPairing bool

func init() {
	connectCmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to OpenClaw web UI",
		RunE:  connectRun,
	}
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

	if cfg, err := prov.GetAgent(setupCtx, agentName); err == nil && cfg != nil && cfg.Paused {
		return fmt.Errorf("agent %s is paused. Use `conga admin unpause %s` first", agentName, agentName)
	}

	info, err := prov.Connect(setupCtx, agentName, 0)
	if err != nil {
		return err
	}

	fmt.Printf("\nOpen in your browser:\n  %s\n\n", info.URL)
	fmt.Println("Press Ctrl+C to disconnect.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()

	// Start device pairing poll in background (works on both providers)
	if !connectNoPairing {
		go pollDevicePairing(sessionCtx, prov, agentName)
	}

	if info.Waiter != nil {
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
		<-sigCh
		fmt.Println("\nDisconnected.")
	}

	return nil
}

// pollDevicePairing watches for pending device pairing requests and auto-approves them.
// Uses the Provider's ContainerExec so it works on both AWS (via SSM) and local (via docker exec).
func pollDevicePairing(ctx context.Context, p provider.Provider, agentName string) {
	fmt.Println("Watching for device pairing requests...")

	for i := 0; i < 60; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}

		output, err := p.ContainerExec(ctx, agentName, []string{"npx", "openclaw", "devices", "list", "--json"})
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

		output, err = p.ContainerExec(ctx, agentName, []string{"npx", "openclaw", "devices", "approve", "--latest"})
		if err != nil {
			continue
		}

		if strings.Contains(output, "Approved") {
			fmt.Println("Device paired! Refresh your browser if needed.")
			return
		}
	}
}
