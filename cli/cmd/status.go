package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// formatUptime parses an ISO timestamp and returns a human-readable duration.
func formatUptime(started string) string {
	t, err := time.Parse(time.RFC3339Nano, started)
	if err != nil {
		t, err = time.Parse(time.RFC3339, started)
		if err != nil {
			return ""
		}
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		return fmt.Sprintf("%dd %dh", days, hours)
	}
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show your container status",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := commandContext()
		defer cancel()

		agentName, err := resolveAgentName(ctx)
		if err != nil {
			return err
		}

		status, err := prov.GetStatus(ctx, agentName)
		if err != nil {
			return err
		}

		if status.Container.State == "not found" || status.Container.State == "" {
			fmt.Println("Container:  not found")
			fmt.Printf("Service:    %s\n", status.ServiceState)
			return nil
		}

		fmt.Printf("Container:  %s\n", status.Container.State)
		fmt.Printf("Service:    %s\n", status.ServiceState)
		fmt.Printf("Readiness:  %s\n", status.ReadyPhase)

		if started := status.Container.StartedAt; started != "" {
			if up := formatUptime(started); up != "" {
				fmt.Printf("Started:    %s (up %s)\n", started, up)
			} else {
				fmt.Printf("Started:    %s\n", started)
			}
		}
		if status.Container.RestartCount > 0 {
			fmt.Printf("Restarts:   %d\n", status.Container.RestartCount)
		}
		if status.Container.CPUPercent != "" {
			fmt.Printf("CPU:        %s\n", status.Container.CPUPercent)
		}
		if status.Container.MemoryUsage != "" {
			fmt.Printf("Memory:     %s\n", status.Container.MemoryUsage)
		}
		if status.Container.PIDs > 0 {
			fmt.Printf("PIDs:       %d\n", status.Container.PIDs)
		}
		return nil
	},
}
