package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
		Use:   "add-user <slack_member_id>",
		Short: "Provision a new individual (DM-only) agent for a user",
		Args:  cobra.ExactArgs(1),
		RunE:  adminAddUserRun,
	}
	addUserCmd.Flags().IntVar(&adminGatewayPort, "gateway-port", 0, "Gateway port (auto-assigned if 0)")
	addUserCmd.Flags().StringVar(&adminIAMIdentity, "iam-identity", "", "IAM identity (SSO username/email)")

	addTeamCmd := &cobra.Command{
		Use:   "add-team <team_name> <slack_channel>",
		Short: "Provision a new team (channel-based) agent",
		Args:  cobra.ExactArgs(2),
		RunE:  adminAddTeamRun,
	}
	addTeamCmd.Flags().IntVar(&adminGatewayPort, "gateway-port", 0, "Gateway port (auto-assigned if 0)")

	listAgentsCmd := &cobra.Command{
		Use:   "list-agents",
		Short: "List all provisioned agents (users and teams)",
		RunE:  adminListAgentsRun,
	}

	removeUserCmd := &cobra.Command{
		Use:   "remove-user <slack_member_id>",
		Short: "Remove a user agent from the instance",
		Args:  cobra.ExactArgs(1),
		RunE:  adminRemoveUserRun,
	}
	removeUserCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")
	removeUserCmd.Flags().BoolVar(&adminDeleteSecrets, "delete-secrets", false, "Also delete user secrets")

	removeTeamCmd := &cobra.Command{
		Use:   "remove-team <team_name>",
		Short: "Remove a team agent from the instance",
		Args:  cobra.ExactArgs(1),
		RunE:  adminRemoveTeamRun,
	}
	removeTeamCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

	mapUserCmd := &cobra.Command{
		Use:   "map-user <slack_member_id> <iam_identity>",
		Short: "Create or update the IAM-to-user mapping so auth status resolves correctly",
		Args:  cobra.ExactArgs(2),
		RunE:  adminMapUserRun,
	}

	cycleHostCmd := &cobra.Command{
		Use:   "cycle-host",
		Short: "Stop and restart the EC2 instance (re-bootstraps all containers)",
		RunE:  adminCycleHostRun,
	}
	cycleHostCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")

	adminCmd.AddCommand(addUserCmd, addTeamCmd, listAgentsCmd, removeUserCmd, removeTeamCmd, mapUserCmd, cycleHostCmd)
	rootCmd.AddCommand(adminCmd)
}

func adminAddUserRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	memberID := args[0]

	if err := validateMemberID(memberID); err != nil {
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

	// Create SSM parameters
	userConfigJSON, _ := json.Marshal(map[string]interface{}{
		"agent_name":   memberID,
		"gateway_port": gatewayPort,
	})

	fmt.Println("Creating SSM parameters...")
	if err := awsutil.PutParameter(ctx, clients.SSM, fmt.Sprintf("/openclaw/users/%s", memberID), string(userConfigJSON)); err != nil {
		return fmt.Errorf("failed to create user config parameter: %w", err)
	}
	if iamIdentity != "" {
		if err := awsutil.PutParameter(ctx, clients.SSM, fmt.Sprintf("/openclaw/users/by-iam/%s", iamIdentity), memberID); err != nil {
			return fmt.Errorf("failed to create IAM mapping parameter: %w", err)
		}
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
		MemberID      string
		AWSRegion     string
		GatewayPort   int
		OpenClawImage string
	}{
		MemberID:      memberID,
		AWSRegion:     cfg.Region,
		GatewayPort:   gatewayPort,
		OpenClawImage: cfg.OpenClawImage,
	})
	if err != nil {
		return fmt.Errorf("failed to render add-user script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Provisioning user %s...", memberID))
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 180*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status == "Success" {
		fmt.Printf("\nUser %s provisioned successfully!\n\n", memberID)
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

	teamName := args[0]
	slackChannel := args[1]

	if err := validateTeamName(teamName); err != nil {
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
	teamConfigJSON, _ := json.Marshal(map[string]interface{}{
		"slack_channel": slackChannel,
		"gateway_port":  gatewayPort,
	})

	fmt.Println("Creating SSM parameter...")
	if err := awsutil.PutParameter(ctx, clients.SSM, fmt.Sprintf("/openclaw/teams/%s", teamName), string(teamConfigJSON)); err != nil {
		return fmt.Errorf("failed to create team config parameter: %w", err)
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
		TeamName      string
		SlackChannel  string
		AWSRegion     string
		GatewayPort   int
		OpenClawImage string
	}{
		TeamName:      teamName,
		SlackChannel:  slackChannel,
		AWSRegion:     cfg.Region,
		GatewayPort:   gatewayPort,
		OpenClawImage: cfg.OpenClawImage,
	})
	if err != nil {
		return fmt.Errorf("failed to render add-team script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Provisioning team agent %s...", teamName))
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 180*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status == "Success" {
		fmt.Printf("\nTeam agent %s provisioned successfully!\n", teamName)
		fmt.Printf("Channel: %s\n", slackChannel)
		fmt.Printf("Gateway port: %d\n", gatewayPort)
	} else {
		fmt.Printf("Setup failed:\n%s\n%s\n", result.Stdout, result.Stderr)
	}
	return nil
}

func adminMapUserRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	memberID := args[0]
	iamIdentity := args[1]

	if err := validateMemberID(memberID); err != nil {
		return err
	}

	paramName := fmt.Sprintf("/openclaw/users/by-iam/%s", iamIdentity)
	if err := awsutil.PutParameter(ctx, clients.SSM, paramName, memberID); err != nil {
		return fmt.Errorf("failed to create IAM mapping: %w", err)
	}

	fmt.Printf("Mapped IAM identity '%s' → %s\n", iamIdentity, memberID)
	fmt.Println("The user can now run `cruxclaw auth status` to verify.")
	return nil
}

func adminListAgentsRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	userEntries, userErr := awsutil.GetParametersByPath(ctx, clients.SSM, "/openclaw/users/")
	teamEntries, teamErr := awsutil.GetParametersByPath(ctx, clients.SSM, "/openclaw/teams/")

	if userErr != nil && teamErr != nil {
		return fmt.Errorf("failed to query agents: %w", userErr)
	}

	headers := []string{"NAME", "TYPE", "MEMBER ID / CHANNEL", "GATEWAY PORT"}
	var rows [][]string

	// User agents
	for _, e := range userEntries {
		parts := strings.Split(e.Name, "/")
		memberID := parts[len(parts)-1]

		var cfg struct {
			AgentName   string `json:"agent_name"`
			GatewayPort int    `json:"gateway_port"`
		}
		if json.Unmarshal([]byte(e.Value), &cfg) != nil {
			continue
		}
		name := cfg.AgentName
		if name == "" {
			name = memberID
		}
		rows = append(rows, []string{name, "user", memberID, strconv.Itoa(cfg.GatewayPort)})
	}

	// Team agents
	for _, e := range teamEntries {
		parts := strings.Split(e.Name, "/")
		teamName := parts[len(parts)-1]

		var cfg struct {
			SlackChannel string `json:"slack_channel"`
			GatewayPort  int    `json:"gateway_port"`
		}
		if json.Unmarshal([]byte(e.Value), &cfg) != nil {
			continue
		}
		rows = append(rows, []string{teamName, "team", cfg.SlackChannel, strconv.Itoa(cfg.GatewayPort)})
	}

	if len(rows) == 0 {
		fmt.Println("No agents found.")
		return nil
	}

	ui.PrintTable(headers, rows)
	return nil
}

func adminRemoveUserRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	memberID := args[0]
	if err := validateMemberID(memberID); err != nil {
		return err
	}

	if !adminForce {
		if !ui.Confirm(fmt.Sprintf("Remove user %s? This will stop their container and delete config.", memberID)) {
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
	err = tmpl.Execute(&buf, struct{ ContainerID string }{ContainerID: memberID})
	if err != nil {
		return fmt.Errorf("failed to render remove-user script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Removing user %s...", memberID))
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 60*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		fmt.Printf("Warning: instance cleanup may have partially failed:\n%s\n", result.Stderr)
	}

	// Delete SSM parameters
	awsutil.DeleteParameter(ctx, clients.SSM, fmt.Sprintf("/openclaw/users/%s", memberID))

	// Try to find and delete IAM mapping
	allParams, _ := awsutil.GetParametersByPath(ctx, clients.SSM, "/openclaw/users/by-iam/")
	// GetParametersByPath skips by-iam by default, so query directly
	// Just attempt cleanup — not critical if it fails

	if adminDeleteSecrets {
		secrets, err := awsutil.ListSecrets(ctx, clients.SecretsManager, fmt.Sprintf("openclaw/%s/", memberID))
		if err == nil {
			for _, s := range secrets {
				awsutil.DeleteSecret(ctx, clients.SecretsManager, fmt.Sprintf("openclaw/%s/%s", memberID, s.Name))
			}
		}
	}

	_ = allParams
	fmt.Printf("User %s removed.\n", memberID)
	return nil
}

func adminRemoveTeamRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	teamName := args[0]
	if err := validateTeamName(teamName); err != nil {
		return err
	}

	if !adminForce {
		if !ui.Confirm(fmt.Sprintf("Remove team agent %s? This will stop the container and delete config.", teamName)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	// Run remove script on instance — team containers use team name as container ID
	tmpl, err := template.New("removeagent").Parse(scripts.RemoveAgentScript)
	if err != nil {
		return fmt.Errorf("failed to parse remove-agent template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, struct{ ContainerID string }{ContainerID: teamName})
	if err != nil {
		return fmt.Errorf("failed to render remove template: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Removing team agent %s...", teamName))
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 60*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	if result.Status != "Success" {
		fmt.Printf("Warning: instance cleanup may have partially failed:\n%s\n", result.Stderr)
	}

	// Delete SSM parameter
	awsutil.DeleteParameter(ctx, clients.SSM, fmt.Sprintf("/openclaw/teams/%s", teamName))

	fmt.Printf("Team agent %s removed.\n", teamName)
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

	// Check user agents
	userEntries, userErr := awsutil.GetParametersByPath(ctx, clients.SSM, "/openclaw/users/")
	if userErr != nil {
		return 0, fmt.Errorf("failed to query user agents for port assignment: %w", userErr)
	}
	for _, e := range userEntries {
		var cfg struct {
			GatewayPort int `json:"gateway_port"`
		}
		if json.Unmarshal([]byte(e.Value), &cfg) == nil && cfg.GatewayPort > maxPort {
			maxPort = cfg.GatewayPort
		}
	}

	// Check team agents
	teamEntries, teamErr := awsutil.GetParametersByPath(ctx, clients.SSM, "/openclaw/teams/")
	if teamErr != nil {
		return 0, fmt.Errorf("failed to query team agents for port assignment: %w", teamErr)
	}
	for _, e := range teamEntries {
		var cfg struct {
			GatewayPort int `json:"gateway_port"`
		}
		if json.Unmarshal([]byte(e.Value), &cfg) == nil && cfg.GatewayPort > maxPort {
			maxPort = cfg.GatewayPort
		}
	}

	port := maxPort + 1
	fmt.Printf("Auto-assigned gateway port: %d\n", port)
	return port, nil
}

// validateTeamName checks that a team name is a valid lowercase alphanumeric + hyphen identifier.
func validateTeamName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("team name must not be empty")
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("invalid team name %q: must be lowercase alphanumeric with hyphens (e.g., \"devops\", \"ml-team\")", name)
		}
	}
	return nil
}
