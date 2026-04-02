package cmd

import (
	"context"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

func adminAddUserRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	if err := validateAgentName(agentName); err != nil {
		return err
	}

	// Channel binding: flag > JSON > none (gateway-only)
	bindings, err := resolveChannelBinding("user")
	if err != nil {
		return err
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
		Name:        agentName,
		Type:        provider.AgentTypeUser,
		Channels:    bindings,
		GatewayPort: gatewayPort,
		IAMIdentity: iamIdentity,
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

	// Channel binding: flag > JSON > none (gateway-only)
	bindings, err := resolveChannelBinding("team")
	if err != nil {
		return err
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
		Name:        agentName,
		Type:        provider.AgentTypeTeam,
		Channels:    bindings,
		GatewayPort: gatewayPort,
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
	if len(bindings) > 0 {
		fmt.Printf("Channel: %s:%s\n", bindings[0].Platform, bindings[0].ID)
	} else {
		fmt.Println("Mode: gateway-only (no channel)")
	}
	fmt.Printf("Gateway port: %d\n", gatewayPort)
	return nil
}

// resolveChannelBinding parses the --channel flag or JSON input into a binding slice.
func resolveChannelBinding(agentType string) ([]channels.ChannelBinding, error) {
	chStr := adminChannel
	if chStr == "" {
		if s, ok := ui.GetString("channel"); ok {
			chStr = s
		}
	}
	if chStr == "" {
		return nil, nil // gateway-only
	}

	binding, err := channels.ParseBinding(chStr)
	if err != nil {
		return nil, err
	}
	ch, ok := channels.Get(binding.Platform)
	if !ok {
		return nil, fmt.Errorf("unknown channel platform %q", binding.Platform)
	}
	if err := ch.ValidateBinding(agentType, binding.ID); err != nil {
		return nil, err
	}
	return []channels.ChannelBinding{binding}, nil
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
