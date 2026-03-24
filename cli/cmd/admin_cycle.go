package cmd

import (
	"context"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

func adminCycleHostRun(cmd *cobra.Command, args []string) error {
	if !adminForce && !ui.JSONInputActive {
		if !ui.Confirm("This will restart the deployment environment and ALL agent containers. Continue?") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// Use unbounded context for long-running cycle operation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := prov.CycleHost(ctx); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]string{"status": "ok"})
	}
	return nil
}
