package cmd

import (
	"fmt"
	"strconv"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

var (
	adminGatewayPort   int
	adminIAMIdentity   string
	adminChannel       string
	adminForce         bool
	adminDeleteSecrets bool
)

func init() {
	adminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Admin operations (requires elevated permissions)",
	}

	addUserCmd := &cobra.Command{
		Use:   "add-user <name>",
		Short: "Provision a new user agent",
		Args:  cobra.ExactArgs(1),
		RunE:  adminAddUserRun,
	}
	addUserCmd.Flags().IntVar(&adminGatewayPort, "gateway-port", 0, "Gateway port (auto-assigned if 0)")
	addUserCmd.Flags().StringVar(&adminIAMIdentity, "iam-identity", "", "IAM identity (SSO username/email)")
	addUserCmd.Flags().StringVar(&adminChannel, "channel", "", "Channel binding (platform:id, e.g., slack:U0123456789)")

	addTeamCmd := &cobra.Command{
		Use:   "add-team <name>",
		Short: "Provision a new team agent",
		Args:  cobra.ExactArgs(1),
		RunE:  adminAddTeamRun,
	}
	addTeamCmd.Flags().IntVar(&adminGatewayPort, "gateway-port", 0, "Gateway port (auto-assigned if 0)")
	addTeamCmd.Flags().StringVar(&adminChannel, "channel", "", "Channel binding (platform:id, e.g., slack:C0123456789)")

	listAgentsCmd := &cobra.Command{
		Use:   "list-agents",
		Short: "List all provisioned agents",
		RunE:  adminListAgentsRun,
	}

	removeAgentCmd := &cobra.Command{
		Use:   "remove-agent <name>",
		Short: "Remove an agent",
		Args:  cobra.ExactArgs(1),
		RunE:  adminRemoveAgentRun,
	}
	removeAgentCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")
	removeAgentCmd.Flags().BoolVar(&adminDeleteSecrets, "delete-secrets", false, "Also delete agent secrets")

	cycleHostCmd := &cobra.Command{
		Use:   "cycle-host",
		Short: "Restart the deployment environment (re-bootstraps all containers)",
		RunE:  adminCycleHostRun,
	}
	cycleHostCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure shared secrets and settings",
		RunE:  adminSetupRun,
	}
	setupCmd.Flags().StringVar(&adminSetupConfig, "config", "", "JSON config (inline or file path) for non-interactive setup")

	refreshAllCmd := &cobra.Command{
		Use:   "refresh-all",
		Short: "Restart all agent containers (picks up latest behavior, config, secrets)",
		RunE:  adminRefreshAllRun,
	}
	refreshAllCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

	teardownCmd := &cobra.Command{
		Use:   "teardown",
		Short: "Remove the entire deployment (all agents, containers, config)",
		RunE:  adminTeardownRun,
	}
	teardownCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

	pauseCmd := &cobra.Command{
		Use:   "pause <name>",
		Short: "Temporarily stop an agent (preserves all data)",
		Args:  cobra.ExactArgs(1),
		RunE:  adminPauseRun,
	}

	unpauseCmd := &cobra.Command{
		Use:   "unpause <name>",
		Short: "Resume a paused agent",
		Args:  cobra.ExactArgs(1),
		RunE:  adminUnpauseRun,
	}

	adminCmd.AddCommand(setupCmd, addUserCmd, addTeamCmd, listAgentsCmd, removeAgentCmd, cycleHostCmd, refreshAllCmd, teardownCmd, pauseCmd, unpauseCmd)
	rootCmd.AddCommand(adminCmd)
}

func adminListAgentsRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agents, err := prov.ListAgents(ctx)
	if err != nil {
		return err
	}

	if ui.OutputJSON {
		if agents == nil {
			agents = []provider.AgentConfig{}
		}
		ui.EmitJSON(agents)
		return nil
	}

	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	headers := []string{"NAME", "TYPE", "STATUS", "CHANNEL", "GATEWAY PORT"}
	var rows [][]string
	for _, a := range agents {
		channel := "(gateway-only)"
		if len(a.Channels) > 0 {
			channel = a.Channels[0].Platform + ":" + a.Channels[0].ID
		}
		status := "active"
		if a.Paused {
			status = "paused"
		}
		rows = append(rows, []string{a.Name, string(a.Type), status, channel, strconv.Itoa(a.GatewayPort)})
	}

	ui.PrintTable(headers, rows)
	return nil
}
