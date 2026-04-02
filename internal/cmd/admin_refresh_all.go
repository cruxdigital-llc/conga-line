package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

func adminRefreshAllRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agents, err := prov.ListAgents(ctx)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	if !adminForce && !ui.JSONInputActive {
		fmt.Printf("This will restart all %d agent(s). Active sessions will be interrupted.\n", len(agents))
		if !ui.Confirm("Continue?") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := prov.RefreshAll(ctx); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			AgentsRefreshed int    `json:"agents_refreshed"`
			Status          string `json:"status"`
		}{AgentsRefreshed: len(agents), Status: "ok"})
		return nil
	}

	fmt.Printf("All %d agent(s) refreshed.\n", len(agents))
	return nil
}
