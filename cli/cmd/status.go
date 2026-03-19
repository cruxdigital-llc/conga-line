package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/spf13/cobra"
)

func parseKeyValues(output string) map[string]string {
	kv := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if i := strings.IndexByte(line, '='); i > 0 {
			kv[line[:i]] = strings.TrimSpace(line[i+1:])
		}
	}
	return kv
}

func splitStats(s string) []string {
	parts := strings.SplitN(s, "|", 3)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

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

		agentName, err := resolveAgentName(ctx)
		if err != nil {
			return err
		}

		instanceID, err := findInstance(ctx)
		if err != nil {
			return err
		}

		script := fmt.Sprintf(`
SVC=openclaw-%s

# Service state
SVC_STATE=$(systemctl is-active $SVC 2>/dev/null || echo "inactive")
echo "SERVICE_STATE=$SVC_STATE"

# Container details
if docker inspect $SVC >/dev/null 2>&1; then
  echo "CONTAINER_STATUS=$(docker inspect --format '{{.State.Status}}' $SVC)"
  echo "CONTAINER_STARTED=$(docker inspect --format '{{.State.StartedAt}}' $SVC)"
  echo "CONTAINER_RESTARTS=$(docker inspect --format '{{.RestartCount}}' $SVC)"
  STATS=$(docker stats --no-stream --format '{{.CPUPerc}}|{{.MemUsage}}|{{.PIDs}}' $SVC 2>/dev/null)
  echo "CONTAINER_STATS=$STATS"

  # Boot phase: check last 50 log lines for startup markers (newest last)
  LOGS=$(docker logs $SVC --tail 50 2>&1)
  echo "BOOT_GATEWAY=$(echo "$LOGS" | grep -c '\[gateway\] listening on')"
  echo "BOOT_SLACK_START=$(echo "$LOGS" | grep -c '\[slack\].*starting provider')"
  echo "BOOT_SLACK_HTTP=$(echo "$LOGS" | grep -c '\[slack\] http mode listening')"
  echo "BOOT_SLACK_CHANNELS=$(echo "$LOGS" | grep -c '\[slack\] channels resolved')"
  echo "BOOT_ERROR=$(echo "$LOGS" | grep -ci 'error\|fatal\|panic' || true)"
else
  echo "CONTAINER_STATUS=not found"
fi
`, agentName)

		spin := ui.NewSpinner("Checking status...")
		result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, script, 30*time.Second)
		spin.Stop()
		if err != nil {
			return err
		}

		kv := parseKeyValues(result.Stdout)

		svcState := kv["SERVICE_STATE"]
		containerStatus := kv["CONTAINER_STATUS"]

		if containerStatus == "not found" || containerStatus == "" {
			fmt.Println("Container:  not found")
			fmt.Printf("Service:    %s\n", svcState)
			return nil
		}

		// Determine readiness phase
		phase := "starting"
		if kv["BOOT_GATEWAY"] != "0" {
			phase = "gateway up, waiting for plugins"
		}
		if kv["BOOT_SLACK_START"] != "0" {
			phase = "slack plugin loading"
		}
		if kv["BOOT_SLACK_HTTP"] != "0" {
			phase = "slack endpoint ready, resolving channels"
		}
		if kv["BOOT_SLACK_CHANNELS"] != "0" {
			phase = "ready"
		}
		if kv["BOOT_ERROR"] != "0" && kv["BOOT_ERROR"] != "" {
			phase += " (errors in logs — check `cruxclaw logs`)"
		}

		fmt.Printf("Container:  %s\n", containerStatus)
		fmt.Printf("Service:    %s\n", svcState)
		fmt.Printf("Readiness:  %s\n", phase)
		if started := kv["CONTAINER_STARTED"]; started != "" {
			fmt.Printf("Started:    %s\n", started)
		}
		if restarts := kv["CONTAINER_RESTARTS"]; restarts != "" && restarts != "0" {
			fmt.Printf("Restarts:   %s\n", restarts)
		}
		if stats := kv["CONTAINER_STATS"]; stats != "" {
			parts := splitStats(stats)
			if len(parts) == 3 {
				fmt.Printf("CPU:        %s\n", parts[0])
				fmt.Printf("Memory:     %s\n", parts[1])
				fmt.Printf("PIDs:       %s\n", parts[2])
			}
		}
		return nil
	},
}
