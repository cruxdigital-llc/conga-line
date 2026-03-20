package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/template"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/discovery"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/cruxdigital-llc/openclaw-template/cli/scripts"
	"github.com/spf13/cobra"
)

func adminAddUserRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agentName := args[0]
	slackMemberID := args[1]

	if err := validateAgentName(agentName); err != nil {
		return err
	}
	if err := validateMemberID(slackMemberID); err != nil {
		return err
	}

	// Auto-assign port if not specified
	gatewayPort, err := resolveGatewayPort(ctx)
	if err != nil {
		return err
	}

	// Get IAM identity
	iamIdentity := adminIAMIdentity
	if iamIdentity == "" {
		// Auto-detect caller's SSO identity as default
		defaultIdentity := ""
		if id, err := discovery.ResolveIdentity(ctx, clients.STS, clients.SSM); err == nil && id.SessionName != "" {
			defaultIdentity = id.SessionName
		}
		iamIdentity, err = ui.TextPromptWithDefault("SSO username/email of the user to add", defaultIdentity)
		if err != nil {
			return err
		}
	}

	// Create SSM parameter
	agentConfigJSON, err := json.Marshal(map[string]interface{}{
		"type":            "user",
		"slack_member_id": slackMemberID,
		"gateway_port":    gatewayPort,
		"iam_identity":    iamIdentity,
	})
	if err != nil {
		return fmt.Errorf("failed to serialize agent config: %w", err)
	}

	fmt.Println("Creating SSM parameter...")
	if err := awsutil.PutParameter(ctx, clients.SSM, fmt.Sprintf("/openclaw/agents/%s", agentName), string(agentConfigJSON)); err != nil {
		return fmt.Errorf("failed to create agent config parameter: %w", err)
	}

	// Find instance and run setup script
	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	tmpl, err := template.New("adduser").Parse(scripts.AddUserScript)
	if err != nil {
		return fmt.Errorf("failed to parse add-user template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, struct {
		AgentName     string
		SlackMemberID string
		AWSRegion     string
		GatewayPort   int
	}{
		AgentName:     agentName,
		SlackMemberID: slackMemberID,
		AWSRegion:     resolvedRegion,
		GatewayPort:   gatewayPort,
	})
	if err != nil {
		return fmt.Errorf("failed to render add-user script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Provisioning agent %s...", agentName))
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 180*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		fmt.Fprintf(os.Stderr, "Setup output:\n%s\n%s\n", result.Stdout, result.Stderr)
		return fmt.Errorf("provisioning agent %s failed on instance", agentName)
	}

	fmt.Printf("\nAgent %s provisioned successfully!\n\n", agentName)
	fmt.Println("Next steps for the user:")
	fmt.Printf("  1. cruxclaw secrets set anthropic-api-key --agent %s\n", agentName)
	fmt.Printf("  2. cruxclaw refresh --agent %s\n", agentName)
	fmt.Printf("  3. cruxclaw connect --agent %s\n", agentName)
	return nil
}

func adminAddTeamRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agentName := args[0]
	slackChannel := args[1]

	if err := validateAgentName(agentName); err != nil {
		return err
	}
	if err := validateChannelID(slackChannel); err != nil {
		return err
	}

	// Auto-assign port if not specified
	gatewayPort, err := resolveGatewayPort(ctx)
	if err != nil {
		return err
	}

	// Create SSM parameter
	agentConfigJSON, err := json.Marshal(map[string]interface{}{
		"type":          "team",
		"slack_channel": slackChannel,
		"gateway_port":  gatewayPort,
	})
	if err != nil {
		return fmt.Errorf("failed to serialize agent config: %w", err)
	}

	fmt.Println("Creating SSM parameter...")
	if err := awsutil.PutParameter(ctx, clients.SSM, fmt.Sprintf("/openclaw/agents/%s", agentName), string(agentConfigJSON)); err != nil {
		return fmt.Errorf("failed to create agent config parameter: %w", err)
	}

	// Find instance and run setup script
	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	tmpl, err := template.New("addteam").Parse(scripts.AddTeamScript)
	if err != nil {
		return fmt.Errorf("failed to parse add-team template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, struct {
		AgentName    string
		SlackChannel string
		AWSRegion    string
		GatewayPort  int
	}{
		AgentName:    agentName,
		SlackChannel: slackChannel,
		AWSRegion:    resolvedRegion,
		GatewayPort:  gatewayPort,
	})
	if err != nil {
		return fmt.Errorf("failed to render add-team script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Provisioning team agent %s...", agentName))
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 180*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		fmt.Fprintf(os.Stderr, "Setup output:\n%s\n%s\n", result.Stdout, result.Stderr)
		return fmt.Errorf("provisioning team agent %s failed on instance", agentName)
	}

	fmt.Printf("\nTeam agent %s provisioned successfully!\n", agentName)
	fmt.Printf("Channel: %s\n", slackChannel)
	fmt.Printf("Gateway port: %d\n", gatewayPort)
	return nil
}

// resolveGatewayPort returns the gateway port to use, auto-assigning from
// the next available port if adminGatewayPort is 0.
func resolveGatewayPort(ctx context.Context) (int, error) {
	if adminGatewayPort != 0 {
		return adminGatewayPort, nil
	}

	maxPort := 18788

	agents, err := discovery.ListAgents(ctx, clients.SSM)
	if err != nil {
		return 0, fmt.Errorf("failed to query agents for port assignment: %w", err)
	}
	for _, a := range agents {
		if a.GatewayPort > maxPort {
			maxPort = a.GatewayPort
		}
	}

	port := maxPort + 1
	fmt.Printf("Auto-assigned gateway port: %d\n", port)
	return port, nil
}

// validateAgentName checks that an agent name is a valid lowercase alphanumeric + hyphen identifier.
func validateAgentName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("agent name must not be empty")
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("invalid agent name %q: must be lowercase alphanumeric with hyphens (e.g., \"myagent\", \"ml-team\")", name)
		}
	}
	return nil
}
