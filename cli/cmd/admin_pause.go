package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func adminPauseRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	name := args[0]
	if err := prov.PauseAgent(ctx, name); err != nil {
		return err
	}

	fmt.Printf("Agent %s paused.\n", name)
	fmt.Printf("To resume: conga admin unpause %s\n", name)
	return nil
}

func adminUnpauseRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	name := args[0]
	if err := prov.UnpauseAgent(ctx, name); err != nil {
		return err
	}

	fmt.Printf("Agent %s unpaused and running.\n", name)
	return nil
}
