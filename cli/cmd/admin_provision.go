package cmd

import (
	"context"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

func adminAddUserRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	if err := validateAgentName(agentName); err != nil {
		return err
	}

	// Slack member ID is optional (gateway-only mode without it)
	var slackMemberID string
	if len(args) >= 2 {
		slackMemberID = args[1]
	} else if s, ok := ui.GetString("slack_member_id"); ok {
		slackMemberID = s
	}
	if slackMemberID != "" {
		if err := validateMemberID(slackMemberID); err != nil {
			return err
		}
	}

	// Gateway port: flag > JSON > auto-assign
	if adminGatewayPort == 0 {
		if p, ok := ui.GetInt("gateway_port"); ok {
			adminGatewayPort = p
		}
	}
	gatewayPort, err := resolveGatewayPort(ctx)
	if err != nil {
		return err
	}

	// Get IAM identity: flag > JSON > prompt (AWS only)
	iamIdentity := adminIAMIdentity
	if iamIdentity == "" {
		if s, ok := ui.GetString("iam_identity"); ok {
			iamIdentity = s
		}
	}
	if iamIdentity == "" && prov.Name() == "aws" && !ui.JSONInputActive {
		defaultIdentity := ""
		identity, err := prov.WhoAmI(ctx)
		if err == nil && identity.Name != "" {
			defaultIdentity = identity.Name
		}
		iamIdentity, err = ui.TextPromptWithDefault("SSO username/email of the user to add", defaultIdentity)
		if err != nil {
			return err
		}
	}

	cfg := provider.AgentConfig{
		Name:          agentName,
		Type:          provider.AgentTypeUser,
		SlackMemberID: slackMemberID,
		GatewayPort:   gatewayPort,
		IAMIdentity:   iamIdentity,
	}

	if err := prov.ProvisionAgent(ctx, cfg); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Agent       string `json:"agent"`
			Type        string `json:"type"`
			GatewayPort int    `json:"gateway_port"`
			Status      string `json:"status"`
		}{
			Agent:       agentName,
			Type:        string(provider.AgentTypeUser),
			GatewayPort: gatewayPort,
			Status:      "provisioned",
		})
		return nil
	}

	fmt.Printf("\nAgent %s provisioned successfully!\n\n", agentName)
	fmt.Println("Next steps:")
	fmt.Printf("  1. conga secrets set anthropic-api-key --agent %s\n", agentName)
	fmt.Printf("  2. conga refresh --agent %s\n", agentName)
	fmt.Printf("  3. conga connect --agent %s\n", agentName)
	return nil
}

func adminAddTeamRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	if err := validateAgentName(agentName); err != nil {
		return err
	}

	// Slack channel is optional (gateway-only mode without it)
	var slackChannel string
	if len(args) >= 2 {
		slackChannel = args[1]
	} else if s, ok := ui.GetString("slack_channel"); ok {
		slackChannel = s
	}
	if slackChannel != "" {
		if err := validateChannelID(slackChannel); err != nil {
			return err
		}
	}

	// Gateway port: flag > JSON > auto-assign
	if adminGatewayPort == 0 {
		if p, ok := ui.GetInt("gateway_port"); ok {
			adminGatewayPort = p
		}
	}
	gatewayPort, err := resolveGatewayPort(ctx)
	if err != nil {
		return err
	}

	cfg := provider.AgentConfig{
		Name:         agentName,
		Type:         provider.AgentTypeTeam,
		SlackChannel: slackChannel,
		GatewayPort:  gatewayPort,
	}

	if err := prov.ProvisionAgent(ctx, cfg); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Agent       string `json:"agent"`
			Type        string `json:"type"`
			GatewayPort int    `json:"gateway_port"`
			Status      string `json:"status"`
		}{
			Agent:       agentName,
			Type:        string(provider.AgentTypeTeam),
			GatewayPort: gatewayPort,
			Status:      "provisioned",
		})
		return nil
	}

	fmt.Printf("\nTeam agent %s provisioned successfully!\n", agentName)
	if slackChannel != "" {
		fmt.Printf("Channel: %s\n", slackChannel)
	} else {
		fmt.Println("Mode: gateway-only (no Slack)")
	}
	fmt.Printf("Gateway port: %d\n", gatewayPort)
	return nil
}

func resolveGatewayPort(ctx context.Context) (int, error) {
	if adminGatewayPort != 0 {
		return adminGatewayPort, nil
	}

	agents, err := prov.ListAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to query agents for port assignment: %w", err)
	}

	port := common.NextAvailablePort(agents)
	fmt.Printf("Auto-assigned gateway port: %d\n", port)
	return port, nil
}
