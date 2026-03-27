package cmd

import (
	"fmt"
	"strings"

	"github.com/cruxdigital-llc/conga-line/cli/internal/channels"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

var (
	channelRemoveForce bool
	channelUnbindForce bool
)

func init() {
	channelsCmd := &cobra.Command{
		Use:   "channels",
		Short: "Manage messaging channel integrations",
		Long:  "Add, remove, and manage messaging channel integrations (e.g. Slack) and agent-channel bindings.",
	}

	addCmd := &cobra.Command{
		Use:   "add <platform>",
		Short: "Add a messaging channel integration",
		Long: `Configure a messaging channel platform (e.g. Slack) by providing its credentials.
This stores the shared secrets and starts the router.

Example:
  conga channels add slack`,
		Args: cobra.ExactArgs(1),
		RunE: channelsAddRun,
	}

	removeCmd := &cobra.Command{
		Use:   "remove <platform>",
		Short: "Remove a messaging channel integration",
		Long: `Remove a channel platform. This stops the router, removes all agent bindings
for this platform, and deletes the shared credentials.`,
		Args: cobra.ExactArgs(1),
		RunE: channelsRemoveRun,
	}
	removeCmd.Flags().BoolVar(&channelRemoveForce, "force", false, "Skip confirmation")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured channels and their status",
		RunE:  channelsListRun,
	}

	bindCmd := &cobra.Command{
		Use:   "bind <agent> <platform:id>",
		Short: "Bind an agent to a channel",
		Long: `Add a channel binding to an existing agent.

Example:
  conga channels bind aaron slack:U0123456789
  conga channels bind leadership slack:C0123456789`,
		Args: cobra.ExactArgs(2),
		RunE: channelsBindRun,
	}

	unbindCmd := &cobra.Command{
		Use:   "unbind <agent> <platform>",
		Short: "Remove a channel binding from an agent",
		Args:  cobra.ExactArgs(2),
		RunE:  channelsUnbindRun,
	}
	unbindCmd.Flags().BoolVar(&channelUnbindForce, "force", false, "Skip confirmation")

	channelsCmd.AddCommand(addCmd, removeCmd, listCmd, bindCmd, unbindCmd)
	rootCmd.AddCommand(channelsCmd)
}

func channelsAddRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	platform := args[0]
	ch, ok := channels.Get(platform)
	if !ok {
		return fmt.Errorf("unknown channel platform %q", platform)
	}

	// Collect secrets
	secrets := map[string]string{}
	for _, def := range ch.SharedSecrets() {
		var value string
		var err error

		// Check JSON input first
		if ui.JSONInputActive {
			value, _ = ui.GetString(def.Name)
		}

		if value == "" && !ui.JSONInputActive {
			label := def.Prompt
			if !def.Required {
				label += " (optional)"
			}
			value, err = ui.SecretPrompt(fmt.Sprintf("  %s", label))
			if err != nil {
				return err
			}
		}

		if value == "" {
			if def.Required {
				return fmt.Errorf("missing required secret %q", def.Name)
			}
			continue
		}
		secrets[def.Name] = value
	}

	if err := prov.AddChannel(ctx, platform, secrets); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]any{
			"platform":       platform,
			"status":         "configured",
			"router_started": true,
		})
	} else {
		fmt.Printf("Channel %s configured. Router started.\n", platform)
	}
	return nil
}

func channelsRemoveRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	platform := args[0]

	// Confirmation
	if !channelRemoveForce && !ui.JSONInputActive {
		statuses, err := prov.ListChannels(ctx)
		if err != nil {
			return err
		}
		var boundAgents []string
		for _, s := range statuses {
			if s.Platform == platform {
				boundAgents = s.BoundAgents
				break
			}
		}

		msg := fmt.Sprintf("This will remove the %s channel", platform)
		if len(boundAgents) > 0 {
			msg += fmt.Sprintf(", unbind agents (%s)", strings.Join(boundAgents, ", "))
		}
		msg += ", and delete credentials. Continue?"
		if !ui.Confirm(msg) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := prov.RemoveChannel(ctx, platform); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]any{
			"platform": platform,
			"status":   "removed",
		})
	} else {
		fmt.Printf("Channel %s removed.\n", platform)
	}
	return nil
}

func channelsListRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	statuses, err := prov.ListChannels(ctx)
	if err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(statuses)
		return nil
	}

	if len(statuses) == 0 {
		fmt.Println("No channel platforms registered.")
		return nil
	}

	headers := []string{"PLATFORM", "STATUS", "ROUTER", "BOUND AGENTS"}
	var rows [][]string
	for _, s := range statuses {
		status := "not configured"
		if s.Configured {
			status = "configured"
		}
		router := "-"
		if s.RouterRunning {
			router = "running"
		} else if s.Configured {
			router = "stopped"
		}
		agents := "-"
		if len(s.BoundAgents) > 0 {
			agents = strings.Join(s.BoundAgents, ", ")
		}
		rows = append(rows, []string{s.Platform, status, router, agents})
	}

	ui.PrintTable(headers, rows)
	return nil
}

func channelsBindRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	if err := validateAgentName(agentName); err != nil {
		return err
	}

	binding, err := channels.ParseBinding(args[1])
	if err != nil {
		return err
	}

	if err := prov.BindChannel(ctx, agentName, binding); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]any{
			"agent":    agentName,
			"platform": binding.Platform,
			"id":       binding.ID,
			"status":   "bound",
		})
	} else {
		fmt.Printf("Agent %s bound to %s:%s.\n", agentName, binding.Platform, binding.ID)
	}
	return nil
}

func channelsUnbindRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	platform := args[1]

	if !channelUnbindForce && !ui.JSONInputActive {
		if !ui.Confirm(fmt.Sprintf("Remove %s binding from agent %s?", platform, agentName)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := prov.UnbindChannel(ctx, agentName, platform); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(map[string]any{
			"agent":    agentName,
			"platform": platform,
			"status":   "unbound",
		})
	} else {
		fmt.Printf("Agent %s unbound from %s.\n", agentName, platform)
	}
	return nil
}
