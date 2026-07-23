package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/internal/mcpoauth"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

var doctorLines int

func init() {
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the fleet for remote-MCP OAuth servers that need re-authentication",
		Long: `Scan agents' container logs for remote-MCP servers stuck needing OAuth
authorization — after a token expired or the credential was lost — and print the
exact 'conga mcp login' command to fix each one.

Scope defaults to the whole fleet; use --agent to check one agent.

Caveat: this scans the last N log lines (--lines). A server that has not logged
the error within that window is reported clean; this is not a positive health
assertion. Exit status is non-zero if any agent needs attention (scriptable).`,
		// We print our own report and use the return error only for the exit code.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          doctorRun,
	}
	doctorCmd.Flags().IntVarP(&doctorLines, "lines", "n", 200, "Log lines to scan per agent")
	rootCmd.AddCommand(doctorCmd)
}

func doctorRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	// Determine the set of agents to scan.
	var agents []provider.AgentConfig
	if flagAgent != "" {
		ag, err := prov.GetAgent(ctx, flagAgent)
		if err != nil {
			return err
		}
		agents = []provider.AgentConfig{*ag}
	} else {
		var err error
		agents, err = prov.ListAgents(ctx)
		if err != nil {
			return err
		}
	}

	findings := make([]mcpoauth.AgentFinding, 0, len(agents))
	unhealthy := 0
	for _, ag := range agents {
		logs, err := prov.GetLogs(ctx, ag.Name, doctorLines)
		if err != nil {
			// A paused/absent container isn't an OAuth problem — record and move on.
			findings = append(findings, mcpoauth.AgentFinding{Agent: ag.Name, Error: err.Error()})
			continue
		}
		f := mcpoauth.BuildFinding(ag.Name, logs)
		if len(f.Servers) > 0 {
			unhealthy++
		}
		findings = append(findings, f)
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Healthy   bool                    `json:"healthy"`
			Unhealthy int                     `json:"agents_needing_oauth"`
			Findings  []mcpoauth.AgentFinding `json:"findings"`
		}{Healthy: unhealthy == 0, Unhealthy: unhealthy, Findings: findings})
		return nil
	}

	// Human report.
	unreadable := func() {
		for _, f := range findings {
			if f.Error != "" {
				fmt.Printf("  (note: could not read %s logs: %s)\n", f.Agent, f.Error)
			}
		}
	}
	if unhealthy == 0 {
		fmt.Printf("✓ No MCP OAuth issues found in the last %d log lines across %d agent(s).\n", doctorLines, len(agents))
		unreadable()
		return nil
	}

	fmt.Printf("%d agent(s) have MCP servers needing OAuth re-authentication:\n\n", unhealthy)
	for _, f := range findings {
		for _, need := range f.Servers {
			seen := ""
			if need.LastSeen != "" {
				seen = fmt.Sprintf(" (last error %s)", need.LastSeen)
			}
			fmt.Printf("  %s / %s%s\n      fix: conga mcp login %s --agent %s\n", f.Agent, need.Server, seen, need.Server, f.Agent)
		}
	}
	unreadable()
	fmt.Println("Note: a credential re-authed more recently than its last-error time is already fixed —")
	fmt.Printf("the stale error ages out of the %d-line log window on the agent's next turns.\n\n", doctorLines)
	// Non-zero exit for scripting; SilenceErrors keeps this from re-printing.
	return fmt.Errorf("%d agent(s) need MCP OAuth re-authentication", unhealthy)
}
