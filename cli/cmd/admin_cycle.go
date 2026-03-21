package cmd

import (
	"context"
	"fmt"

	awsutil "github.com/cruxdigital-llc/conga-line/cli/internal/aws"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

func adminCycleHostRun(cmd *cobra.Command, args []string) error {
	// Use commandContext for setup, then an unbounded context for the
	// stop/wait/start/wait cycle which routinely takes 3-8 minutes.
	setupCtx, setupCancel := commandContext()
	defer setupCancel()

	if err := ensureClients(setupCtx); err != nil {
		return err
	}

	if !adminForce {
		if !ui.Confirm("This will restart the EC2 instance and ALL agent containers. Continue?") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	instanceID, err := findInstance(setupCtx)
	if err != nil {
		return err
	}

	// Switch to unbounded context for the long-running cycle operation
	cycleCtx, cycleCancel := context.WithCancel(context.Background())
	defer cycleCancel()

	// Stop
	fmt.Printf("Stopping instance %s...\n", instanceID)
	if err := awsutil.StopInstance(cycleCtx, clients.EC2, instanceID); err != nil {
		return fmt.Errorf("failed to stop instance: %w", err)
	}

	spin := ui.NewSpinner("Waiting for instance to stop...")
	err = awsutil.WaitForState(cycleCtx, clients.EC2, instanceID, "stopped")
	spin.Stop()
	if err != nil {
		return fmt.Errorf("instance failed to stop: %w", err)
	}
	fmt.Println("Instance stopped.")

	// Start
	fmt.Printf("Starting instance %s...\n", instanceID)
	if err := awsutil.StartInstance(cycleCtx, clients.EC2, instanceID); err != nil {
		return fmt.Errorf("failed to start instance: %w", err)
	}

	spin = ui.NewSpinner("Waiting for instance to start...")
	err = awsutil.WaitForState(cycleCtx, clients.EC2, instanceID, "running")
	spin.Stop()
	if err != nil {
		return fmt.Errorf("instance failed to start: %w", err)
	}

	fmt.Println("Instance running. SSM agent may take 1-2 minutes to reconnect.")
	fmt.Println("Use `conga status` to verify your container is healthy.")
	return nil
}
