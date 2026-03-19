package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"text/template"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/discovery"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/cruxdigital-llc/openclaw-template/cli/scripts"
	"github.com/spf13/cobra"
)

var (
	adminGatewayPort   int
	adminIAMIdentity   string
	adminForce         bool
	adminDeleteSecrets bool
)

func init() {
	adminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Admin operations (requires elevated permissions)",
	}

	addUserCmd := &cobra.Command{
		Use:   "add-user <name> <slack_member_id>",
		Short: "Provision a new individual (DM-only) agent for a user",
		Args:  cobra.ExactArgs(2),
		RunE:  adminAddUserRun,
	}
	addUserCmd.Flags().IntVar(&adminGatewayPort, "gateway-port", 0, "Gateway port (auto-assigned if 0)")
	addUserCmd.Flags().StringVar(&adminIAMIdentity, "iam-identity", "", "IAM identity (SSO username/email)")

	addTeamCmd := &cobra.Command{
		Use:   "add-team <name> <slack_channel>",
		Short: "Provision a new team (channel-based) agent",
		Args:  cobra.ExactArgs(2),
		RunE:  adminAddTeamRun,
	}
	addTeamCmd.Flags().IntVar(&adminGatewayPort, "gateway-port", 0, "Gateway port (auto-assigned if 0)")

	listAgentsCmd := &cobra.Command{
		Use:   "list-agents",
		Short: "List all provisioned agents",
		RunE:  adminListAgentsRun,
	}

	removeAgentCmd := &cobra.Command{
		Use:   "remove-agent <name>",
		Short: "Remove an agent from the instance",
		Args:  cobra.ExactArgs(1),
		RunE:  adminRemoveAgentRun,
	}
	removeAgentCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")
	removeAgentCmd.Flags().BoolVar(&adminDeleteSecrets, "delete-secrets", false, "Also delete agent secrets")

	cycleHostCmd := &cobra.Command{
		Use:   "cycle-host",
		Short: "Stop and restart the EC2 instance (re-bootstraps all containers)",
		RunE:  adminCycleHostRun,
	}
	cycleHostCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

	setupCmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure shared secrets and settings from the deployment manifest",
		RunE:  adminSetupRun,
	}

	adminCmd.AddCommand(setupCmd, addUserCmd, addTeamCmd, listAgentsCmd, removeAgentCmd, cycleHostCmd)
	rootCmd.AddCommand(adminCmd)
}

func adminSetupRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	// Read the setup manifest from SSM
	manifestJSON, err := awsutil.GetParameter(ctx, clients.SSM, "/openclaw/config/setup-manifest")
	if err != nil {
		return fmt.Errorf("setup manifest not found in SSM. Run `terraform apply` first to create infrastructure")
	}

	var manifest struct {
		Config  map[string]string `json:"config"`
		Secrets map[string]string `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return fmt.Errorf("failed to parse setup manifest: %w", err)
	}

	fmt.Println("Reading setup manifest...")
	changed := 0

	// Process config values (stored in SSM)
	for key, description := range manifest.Config {
		paramName := fmt.Sprintf("/openclaw/config/%s", key)
		current, _ := awsutil.GetParameter(ctx, clients.SSM, paramName)

		status := "set"
		if current == "" {
			status = "not set"
		}
		fmt.Printf("\n[config] %s — %s (%s)\n", key, description, status)

		if current != "" {
			if !ui.Confirm("  Update this value?") {
				continue
			}
		}

		value, err := ui.TextPrompt(fmt.Sprintf("  Enter value for %s", key))
		if err != nil {
			return err
		}
		if value == "" {
			fmt.Println("  Skipped (empty value)")
			continue
		}

		if err := awsutil.PutParameter(ctx, clients.SSM, paramName, value); err != nil {
			return fmt.Errorf("failed to set config %s: %w", key, err)
		}
		fmt.Printf("  Saved to SSM: %s\n", paramName)
		changed++
	}

	// Process secrets (stored in Secrets Manager)
	for path, description := range manifest.Secrets {
		current, _ := awsutil.GetSecretValue(ctx, clients.SecretsManager, path)

		status := "set"
		if current == "" || current == "REPLACE_ME" {
			status = "not set"
		}
		fmt.Printf("\n[secret] %s — %s (%s)\n", path, description, status)

		if current != "" && current != "REPLACE_ME" {
			if !ui.Confirm("  Update this value?") {
				continue
			}
		}

		value, err := ui.SecretPrompt(fmt.Sprintf("  Enter value for %s", path))
		if err != nil {
			return err
		}
		if value == "" {
			fmt.Println("  Skipped (empty value)")
			continue
		}

		if err := awsutil.SetSecret(ctx, clients.SecretsManager, path, value); err != nil {
			return fmt.Errorf("failed to set secret %s: %w", path, err)
		}
		fmt.Printf("  Saved to Secrets Manager\n")
		changed++
	}

	if changed > 0 {
		fmt.Printf("\n%d value(s) updated. Run `cruxclaw admin cycle-host` to apply.\n", changed)
	} else {
		fmt.Println("\nAll values already configured. No changes needed.")
	}
	return nil
}

func adminAddUserRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
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
	agentConfigJSON, _ := json.Marshal(map[string]interface{}{
		"type":            "user",
		"slack_member_id": slackMemberID,
		"gateway_port":    gatewayPort,
		"iam_identity":    iamIdentity,
	})

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
		AWSRegion:     cfg.Region,
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

	if result.Status == "Success" {
		fmt.Printf("\nAgent %s provisioned successfully!\n\n", agentName)
		fmt.Println("Next steps for the user:")
		fmt.Printf("  1. cruxclaw secrets set anthropic-api-key\n")
		fmt.Printf("  2. cruxclaw refresh\n")
		fmt.Printf("  3. cruxclaw connect\n")
	} else {
		fmt.Printf("Setup failed:\n%s\n%s\n", result.Stdout, result.Stderr)
	}
	return nil
}

func adminAddTeamRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
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
	agentConfigJSON, _ := json.Marshal(map[string]interface{}{
		"type":          "team",
		"slack_channel": slackChannel,
		"gateway_port":  gatewayPort,
	})

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
		AWSRegion:    cfg.Region,
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

	if result.Status == "Success" {
		fmt.Printf("\nTeam agent %s provisioned successfully!\n", agentName)
		fmt.Printf("Channel: %s\n", slackChannel)
		fmt.Printf("Gateway port: %d\n", gatewayPort)
	} else {
		fmt.Printf("Setup failed:\n%s\n%s\n", result.Stdout, result.Stderr)
	}
	return nil
}

func adminListAgentsRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agents, err := discovery.ListAgents(ctx, clients.SSM)
	if err != nil {
		return err
	}

	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	headers := []string{"NAME", "TYPE", "IDENTIFIER", "GATEWAY PORT"}
	var rows [][]string
	for _, a := range agents {
		identifier := a.SlackMemberID
		if a.Type == "team" {
			identifier = a.SlackChannel
		}
		rows = append(rows, []string{a.Name, a.Type, identifier, strconv.Itoa(a.GatewayPort)})
	}

	ui.PrintTable(headers, rows)
	return nil
}

func adminRemoveAgentRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agentName := args[0]

	// Look up the agent to confirm it exists and get its type
	agent, err := discovery.ResolveAgent(ctx, clients.SSM, agentName)
	if err != nil {
		return err
	}

	if !adminForce {
		if !ui.Confirm(fmt.Sprintf("Remove agent %s (type: %s)? This will stop the container and delete config.", agentName, agent.Type)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	// Run remove script on instance
	tmpl, err := template.New("removeagent").Parse(scripts.RemoveAgentScript)
	if err != nil {
		return fmt.Errorf("failed to parse remove-agent template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, struct{ ContainerID string }{ContainerID: agentName})
	if err != nil {
		return fmt.Errorf("failed to render remove-agent script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Removing agent %s...", agentName))
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 60*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		fmt.Printf("Warning: instance cleanup may have partially failed:\n%s\n", result.Stderr)
	}

	// Delete SSM parameter
	awsutil.DeleteParameter(ctx, clients.SSM, fmt.Sprintf("/openclaw/agents/%s", agentName))

	// Delete secrets if requested
	if adminDeleteSecrets {
		secretPrefix := fmt.Sprintf("openclaw/agents/%s/", agentName)
		secrets, err := awsutil.ListSecrets(ctx, clients.SecretsManager, secretPrefix)
		if err == nil {
			for _, s := range secrets {
				awsutil.DeleteSecret(ctx, clients.SecretsManager, fmt.Sprintf("openclaw/agents/%s/%s", agentName, s.Name))
			}
		}
	}

	fmt.Printf("Agent %s removed.\n", agentName)
	return nil
}

func adminCycleHostRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	if !adminForce {
		if !ui.Confirm("This will restart the EC2 instance and ALL agent containers. Continue?") {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	// Stop
	fmt.Printf("Stopping instance %s...\n", instanceID)
	if err := awsutil.StopInstance(ctx, clients.EC2, instanceID); err != nil {
		return fmt.Errorf("failed to stop instance: %w", err)
	}

	spin := ui.NewSpinner("Waiting for instance to stop...")
	err = awsutil.WaitForState(ctx, clients.EC2, instanceID, "stopped")
	spin.Stop()
	if err != nil {
		return fmt.Errorf("instance failed to stop: %w", err)
	}
	fmt.Println("Instance stopped.")

	// Start
	fmt.Printf("Starting instance %s...\n", instanceID)
	if err := awsutil.StartInstance(ctx, clients.EC2, instanceID); err != nil {
		return fmt.Errorf("failed to start instance: %w", err)
	}

	spin = ui.NewSpinner("Waiting for instance to start...")
	err = awsutil.WaitForState(ctx, clients.EC2, instanceID, "running")
	spin.Stop()
	if err != nil {
		return fmt.Errorf("instance failed to start: %w", err)
	}

	fmt.Println("Instance running. SSM agent may take 1-2 minutes to reconnect.")
	fmt.Println("Use `cruxclaw status` to verify your container is healthy.")
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
