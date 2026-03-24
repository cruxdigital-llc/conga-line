package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

func adminTeardownRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	if !adminForce && !ui.JSONInputActive {
		if !ui.Confirm("This will remove ALL agents, containers, networks, and local config. Continue?") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := prov.Teardown(ctx); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]string{"status": "ok"})
	}
	return nil
}
