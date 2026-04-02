package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/pkg/ui"
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

	// Kill any stale `conga connect` tunnels before tearing down
	if agents, err := prov.ListAgents(ctx); err == nil {
		var ports []int
		for _, a := range agents {
			if a.GatewayPort != 0 {
				ports = append(ports, a.GatewayPort)
			}
		}
		killStaleTunnels(ports)
	}

	if err := prov.Teardown(ctx); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]string{"status": "ok"})
	}
	return nil
}
