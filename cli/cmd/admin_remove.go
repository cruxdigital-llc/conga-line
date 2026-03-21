package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

func adminRemoveAgentRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]

	// Look up the agent to confirm it exists and get its type
	agent, err := prov.GetAgent(ctx, agentName)
	if err != nil {
		return err
	}

	if !adminForce {
		if !ui.Confirm(fmt.Sprintf("Remove agent %s (type: %s)? This will stop the container and delete config.", agentName, agent.Type)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := prov.RemoveAgent(ctx, agentName, adminDeleteSecrets); err != nil {
		return err
	}

	fmt.Printf("Agent %s removed.\n", agentName)
	return nil
}
