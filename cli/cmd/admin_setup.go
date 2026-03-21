package cmd

import "github.com/spf13/cobra"

func init() {
	// setupCmd is registered in admin.go init()
}

func adminSetupRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	return prov.Setup(ctx)
}
