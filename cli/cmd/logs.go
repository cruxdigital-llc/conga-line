package cmd

import (
	"context"
	"fmt"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/spf13/cobra"
)

var logLines int

func init() {
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail your container logs",
		RunE:  logsRun,
	}
	logsCmd.Flags().IntVarP(&logLines, "lines", "n", 50, "Number of log lines")
	rootCmd.AddCommand(logsCmd)
}

func logsRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	script := fmt.Sprintf("docker logs openclaw-%s --tail %d 2>&1", agentName, logLines)

	spin := ui.NewSpinner("Fetching logs...")
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, script, 30*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	fmt.Print(result.Stdout)
	return nil
}
