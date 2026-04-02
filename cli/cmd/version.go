package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		if ui.OutputJSON {
			ui.EmitJSON(map[string]string{
				"version": Version,
				"commit":  Commit,
				"date":    Date,
			})
			return
		}
		fmt.Printf("conga %s (commit: %s, built: %s)\n", Version, Commit, Date)
	},
}
