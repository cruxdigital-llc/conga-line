package cmd

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/discovery"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/cruxdigital-llc/openclaw-template/cli/scripts"
	"github.com/spf13/cobra"
)

func adminRefreshAllRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agents, err := discovery.ListAgents(ctx, clients.SSM)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	if !adminForce {
		fmt.Printf("This will restart all %d agent(s). Active sessions will be interrupted.\n", len(agents))
		if !ui.Confirm("Continue?") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	tmpl, err := template.New("refresh-all").Parse(scripts.RefreshAllScript)
	if err != nil {
		return fmt.Errorf("failed to parse refresh-all template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, struct {
		Agents    []discovery.AgentConfig
		AWSRegion string
	}{
		Agents:    agents,
		AWSRegion: resolvedRegion,
	})
	if err != nil {
		return fmt.Errorf("failed to render refresh-all script: %w", err)
	}

	spin := ui.NewSpinner("Refreshing all agents...")
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 300*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		fmt.Fprintf(os.Stderr, "Output:\n%s\n%s\n", result.Stdout, result.Stderr)
		return fmt.Errorf("refresh-all failed on instance")
	}

	fmt.Printf("All %d agent(s) refreshed.\n", len(agents))
	return nil
}
