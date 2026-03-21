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

	awsutil "github.com/cruxdigital-llc/conga-line/cli/internal/aws"
	"github.com/cruxdigital-llc/conga-line/cli/internal/discovery"
	"github.com/cruxdigital-llc/conga-line/cli/internal/tunnel"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

var (
	connectLocalPort int
	connectNoPairing bool
)

func init() {
	connectCmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect to OpenClaw web UI via SSM tunnel",
		RunE:  connectRun,
	}
	connectCmd.Flags().IntVar(&connectLocalPort, "local-port", 0, "Local port for tunnel (default: agent's gateway port)")
	connectCmd.Flags().BoolVar(&connectNoPairing, "no-pairing", false, "Skip device pairing poll")
	rootCmd.AddCommand(connectCmd)
}

func connectRun(cmd *cobra.Command, args []string) error {
	// Use a bounded context for setup (client init, agent resolution, token fetch),
	// then switch to an unbounded context for the long-lived tunnel session.
	setupCtx, setupCancel := commandContext()
	defer setupCancel()

	if err := tunnel.CheckPlugin(); err != nil {
		return err
	}

	if err := ensureClients(setupCtx); err != nil {
		return err
	}

	agentName, err := resolveAgentName(setupCtx)
	if err != nil {
		return err
	}

	agentCfg, err := discovery.ResolveAgent(setupCtx, clients.SSM, agentName)
	if err != nil {
		return err
	}

	instanceID, err := findInstance(setupCtx)
	if err != nil {
		return err
	}

	// Fetch gateway token
	tokenScript := fmt.Sprintf(`python3 -c "import json; c=json.load(open('/opt/conga/data/%s/openclaw.json')); print(c.get('gateway',{}).get('auth',{}).get('token','NOT_FOUND'))"`, agentName)

	spin := ui.NewSpinner("Fetching gateway token...")
	result, err := awsutil.RunCommand(setupCtx, clients.SSM, instanceID, tokenScript, 30*time.Second)
	spin.Stop()
	if err != nil {
		return fmt.Errorf("failed to fetch gateway token: %w", err)
	}

	token := strings.TrimSpace(result.Stdout)
	if token == "" || token == "NOT_FOUND" {
		return fmt.Errorf("gateway token not found. Container may not have started yet.\nTry: conga status")
	}

	// Default local port to the agent's gateway port for stable per-agent URLs
	localPort := connectLocalPort
	if localPort == 0 {
		localPort = agentCfg.GatewayPort
	}

	// Switch to unbounded context for long-lived tunnel
	tunnelCtx, tunnelCancel := context.WithCancel(context.Background())
	defer tunnelCancel()

	// Start tunnel
	fmt.Printf("Starting tunnel: localhost:%d → instance:%d\n", localPort, agentCfg.GatewayPort)

	tun, err := tunnel.StartTunnel(tunnelCtx, clients.SSM, instanceID, agentCfg.GatewayPort, localPort, resolvedRegion, resolvedProfile)
	if err != nil {
		return err
	}

	dashboardURL := fmt.Sprintf("http://localhost:%d#token=%s", localPort, token)
	fmt.Printf("\nOpen in your browser:\n  %s\n\n", dashboardURL)

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Device pairing poll (background)
	if !connectNoPairing {
		go pollDevicePairing(tunnelCtx, instanceID, agentName, flagVerbose)
	}

	// Wait for tunnel exit or signal
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- tun.Wait()
	}()

	select {
	case <-sigCh:
		fmt.Println("\nClosing tunnel...")
		tunnelCancel()
		tun.Stop()
	case err := <-doneCh:
		if err != nil {
			return fmt.Errorf("tunnel exited: %w", err)
		}
	}

	return nil
}

func pollDevicePairing(ctx context.Context, instanceID, agentName string, verbose bool) {
	fmt.Println("Watching for device pairing requests...")

	for i := 0; i < 30; i++ {
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}

		listScript := fmt.Sprintf("docker exec conga-%s npx openclaw devices list --json 2>&1", agentName)
		result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, listScript, 30*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "[verbose] device pairing poll error: %v\n", err)
			}
			continue
		}

		var devList struct {
			Pending []json.RawMessage `json:"pending"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &devList); err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "[verbose] device list parse error: %v\n", err)
			}
			continue
		}
		if len(devList.Pending) == 0 {
			continue
		}

		approveScript := fmt.Sprintf("docker exec conga-%s npx openclaw devices approve --latest 2>&1", agentName)
		result, err = awsutil.RunCommand(ctx, clients.SSM, instanceID, approveScript, 30*time.Second)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "[verbose] device pairing approve error: %v\n", err)
			}
			continue
		}

		if !strings.Contains(result.Stdout, "Approved") {
			if verbose {
				fmt.Fprintf(os.Stderr, "[verbose] unexpected approve output: %s\n", result.Stdout)
			}
			continue
		}

		fmt.Printf("Device paired! Refresh your browser.\n")
		return
	}
}
