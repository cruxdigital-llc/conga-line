package cmd

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/cruxdigital-llc/openclaw-template/cli/scripts"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(refreshCmd)
}

var refreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Restart your container with fresh secrets",
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

		tmpl, err := template.New("refresh").Parse(scripts.RefreshUserScript)
		if err != nil {
			return fmt.Errorf("failed to parse refresh template: %w", err)
		}

		var buf bytes.Buffer
		err = tmpl.Execute(&buf, struct {
			AgentName string
			AWSRegion string
		}{
			AgentName: agentName,
			AWSRegion: cfg.Region,
		})
		if err != nil {
			return fmt.Errorf("failed to render refresh script: %w", err)
		}

		spin := ui.NewSpinner(fmt.Sprintf("Refreshing secrets for %s...", agentName))
		result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 120*time.Second)
		spin.Stop()
		if err != nil {
			return err
		}

		if result.Status == "Success" {
			fmt.Printf("Secrets refreshed and container restarted for %s.\n", agentName)
		} else {
			fmt.Printf("Command failed:\n%s\n%s\n", result.Stdout, result.Stderr)
		}
		return nil
	},
}
