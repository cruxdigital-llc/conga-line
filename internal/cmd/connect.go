package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
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
	// Use the timeout context only for short setup operations.
	setupCtx, setupCancel := commandContext()
	defer setupCancel()

	agentName, err := resolveAgentName(setupCtx)
	if err != nil {
		return err
	}

	if cfg, err := prov.GetAgent(setupCtx, agentName); err == nil && cfg != nil && cfg.Paused {
		return fmt.Errorf("agent %s is paused. Use `conga admin unpause %s` first", agentName, agentName)
	}

	// Use a signal-driven context for the long-lived connection so it isn't
	// killed by the global command timeout.
	sessionCtx, sessionCancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer sessionCancel()

	info, err := prov.Connect(sessionCtx, agentName, 0)
	if err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Agent string `json:"agent"`
			URL   string `json:"url"`
			Port  int    `json:"port"`
			Token string `json:"token,omitempty"`
		}{
			Agent: agentName,
			URL:   info.URL,
			Port:  info.LocalPort,
			Token: info.Token,
		})
		return nil
	}

	fmt.Printf("\nOpen in your browser:\n  %s\n\n", info.URL)
	fmt.Println("Press Ctrl+C to disconnect.")

	// Start device pairing poll in background
	if !connectNoPairing {
		go pollDevicePairing(sessionCtx, prov, agentName)
	}

	if info.Waiter != nil {
		select {
		case <-sessionCtx.Done():
			fmt.Println("\nClosing connection...")
		case err := <-info.Waiter:
			if err != nil {
				return fmt.Errorf("connection exited: %w", err)
			}
		}
	} else {
		<-sessionCtx.Done()
		fmt.Println("\nDisconnected.")
	}

	return nil
}

// pollDevicePairing watches for pending device pairing requests and auto-approves them.
// Uses the Provider's ContainerExec so it works on both AWS (via SSM) and local (via docker exec).
// Polls aggressively (500ms) for the first 10 seconds, then backs off to 3s.
func pollDevicePairing(ctx context.Context, p provider.Provider, agentName string) {
	fmt.Println("Watching for device pairing requests...")

	const (
		fastInterval      = 500 * time.Millisecond
		slowInterval      = 3 * time.Second
		fastPhaseDuration = 10 * time.Second
		totalTimeout      = 3 * time.Minute
	)

	start := time.Now()
	timer := time.NewTimer(0) // fire immediately on first iteration
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		if time.Since(start) > totalTimeout {
			return
		}

		if tryApproveDevice(ctx, p, agentName) {
			return
		}

		interval := slowInterval
		if time.Since(start) < fastPhaseDuration {
			interval = fastInterval
		}
		timer.Reset(interval)
	}
}

func tryApproveDevice(ctx context.Context, p provider.Provider, agentName string) bool {
	output, err := p.ContainerExec(ctx, agentName, []string{"npx", "openclaw", "devices", "list", "--json"})
	if err != nil {
		return false
	}

	var devList struct {
		Pending []json.RawMessage `json:"pending"`
	}
	if err := json.Unmarshal([]byte(output), &devList); err != nil {
		return false
	}
	if len(devList.Pending) == 0 {
		return false
	}

	output, err = p.ContainerExec(ctx, agentName, []string{"npx", "openclaw", "devices", "approve", "--latest"})
	if err != nil {
		return false
	}

	if strings.Contains(output, "Approved") {
		fmt.Println("Device paired! Refresh your browser if needed.")
		return true
	}
	return false
}
