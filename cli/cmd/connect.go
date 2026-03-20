package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/discovery"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/tunnel"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
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
	connectCmd.Flags().IntVar(&connectLocalPort, "local-port", 18789, "Local port for tunnel")
	connectCmd.Flags().BoolVar(&connectNoPairing, "no-pairing", false, "Skip device pairing poll")
	rootCmd.AddCommand(connectCmd)
}

func connectRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tunnel.CheckPlugin(); err != nil {
		return err
	}

	if err := ensureClients(ctx); err != nil {
		return err
	}

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	agentCfg, err := discovery.ResolveAgent(ctx, clients.SSM, agentName)
	if err != nil {
		return err
	}

	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	// Fetch gateway token
	tokenScript := fmt.Sprintf(`python3 -c "import json; c=json.load(open('/opt/openclaw/data/%s/openclaw.json')); print(c.get('gateway',{}).get('auth',{}).get('token','NOT_FOUND'))"`, agentName)

	spin := ui.NewSpinner("Fetching gateway token...")
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, tokenScript, 30*time.Second)
	spin.Stop()
	if err != nil {
		return fmt.Errorf("failed to fetch gateway token: %w", err)
	}

	token := strings.TrimSpace(result.Stdout)
	if token == "" || token == "NOT_FOUND" {
		return fmt.Errorf("gateway token not found. Container may not have started yet.\nTry: cruxclaw status")
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════")
	fmt.Println("  Gateway Token (paste into browser):")
	fmt.Printf("  %s\n", token)
	fmt.Println("════════════════════════════════════════")
	fmt.Println()

	// Start tunnel
	fmt.Printf("Starting tunnel: localhost:%d → instance:%d\n", connectLocalPort, agentCfg.GatewayPort)

	tun, err := tunnel.StartTunnel(ctx, clients.SSM, instanceID, agentCfg.GatewayPort, connectLocalPort, resolvedRegion, resolvedProfile)
	if err != nil {
		return err
	}

	fmt.Printf("Open http://localhost:%d in your browser\n\n", connectLocalPort)

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Device pairing poll (background)
	if !connectNoPairing {
		go pollDevicePairing(ctx, instanceID, agentName)
	}

	// Wait for tunnel exit or signal
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- tun.Wait()
	}()

	select {
	case <-sigCh:
		fmt.Println("\nClosing tunnel...")
		tun.Stop()
	case err := <-doneCh:
		if err != nil {
			return fmt.Errorf("tunnel exited: %w", err)
		}
	}

	return nil
}

func pollDevicePairing(ctx context.Context, instanceID, agentName string) {
	fmt.Println("Watching for device pairing requests...")

	for i := 0; i < 30; i++ {
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return
		}

		listScript := fmt.Sprintf("docker exec openclaw-%s npx openclaw devices list --json 2>&1", agentName)
		result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, listScript, 30*time.Second)
		if err != nil {
			continue
		}

		if !strings.Contains(result.Stdout, "pending") && !strings.Contains(result.Stdout, "Pending") {
			continue
		}

		approveScript := fmt.Sprintf("docker exec openclaw-%s npx openclaw devices approve --latest 2>&1", agentName)
		result, err = awsutil.RunCommand(ctx, clients.SSM, instanceID, approveScript, 30*time.Second)
		if err != nil {
			continue
		}

		fmt.Printf("Device paired! Refresh your browser.\n")
		return
	}
}
