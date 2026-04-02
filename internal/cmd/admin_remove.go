package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/ui"
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

	if !adminForce && !ui.JSONInputActive {
		if !ui.Confirm(fmt.Sprintf("Remove agent %s (type: %s)? This will stop the container and delete config.", agentName, agent.Type)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Kill any stale `conga connect` tunnel for this agent
	if agent.GatewayPort != 0 {
		killStaleTunnels([]int{agent.GatewayPort})
	}

	if err := prov.RemoveAgent(ctx, agentName, adminDeleteSecrets); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Agent  string `json:"agent"`
			Status string `json:"status"`
		}{Agent: agentName, Status: "removed"})
		return nil
	}

	fmt.Printf("Agent %s removed.\n", agentName)
	return nil
}
