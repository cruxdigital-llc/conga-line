package cmd

import (
	"context"
	"fmt"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show your container status",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		if err := ensureClients(ctx); err != nil {
			return err
		}

		memberID, err := resolveUserID(ctx)
		if err != nil {
			return err
		}

		instanceID, err := findInstance(ctx)
		if err != nil {
			return err
		}

		script := fmt.Sprintf(`echo "=== Service ==="
systemctl is-active openclaw-%s 2>/dev/null || echo "inactive"
echo "=== Container ==="
docker inspect --format '{{.State.Status}} | Started: {{.State.StartedAt}} | Restarts: {{.RestartCount}}' openclaw-%s 2>/dev/null || echo "not found"
echo "=== Resources ==="
docker stats --no-stream --format 'CPU: {{.CPUPerc}} | Mem: {{.MemUsage}} | PIDs: {{.PIDs}}' openclaw-%s 2>/dev/null || echo "N/A"`, memberID, memberID, memberID)

		spin := ui.NewSpinner("Checking status...")
		result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, script, 30*time.Second)
		spin.Stop()
		if err != nil {
			return err
		}

		fmt.Println(result.Stdout)
		return nil
	},
}
