package cmd

import (
	"fmt"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

var logLines int

func init() {
	logsCmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail your container logs",
		RunE:  logsRun,
	}
	logsCmd.Flags().IntVarP(&logLines, "lines", "n", 50, "Number of log lines")
	rootCmd.AddCommand(logsCmd)
}

func logsRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	output, err := prov.GetLogs(ctx, agentName, logLines)
	if err != nil {
		return err
	}

	if ui.OutputJSON {
		lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = []string{}
		}
		ui.EmitJSON(struct {
			Agent string   `json:"agent"`
			Lines []string `json:"lines"`
		}{
			Agent: agentName,
			Lines: lines,
		})
		return nil
	}

	fmt.Print(output)
	return nil
}
