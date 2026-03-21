package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

func adminTeardownRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	if !adminForce {
		if !ui.Confirm("This will remove ALL agents, containers, networks, and local config. Continue?") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	return prov.Teardown(ctx)
}
