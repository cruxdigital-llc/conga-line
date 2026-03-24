package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(refreshCmd)
}

var refreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Restart your container with fresh secrets",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := commandContext()
		defer cancel()

		agentName, err := resolveAgentName(ctx)
		if err != nil {
			return err
		}

		if err := prov.RefreshAgent(ctx, agentName); err != nil {
			return err
		}

		if ui.OutputJSON {
			ui.EmitJSON(struct {
				Agent  string `json:"agent"`
				Status string `json:"status"`
			}{Agent: agentName, Status: "refreshed"})
			return nil
		}

		fmt.Printf("Secrets refreshed and container restarted for %s.\n", agentName)
		return nil
	},
}
