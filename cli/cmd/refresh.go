package cmd

import (
	"fmt"

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

		fmt.Printf("Secrets refreshed and container restarted for %s.\n", agentName)
		return nil
	},
}
