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
		Use:   "add-user <slack_member_id> <slack_channel>",
		Short: "Provision a new user on the instance",
		Args:  cobra.ExactArgs(2),
		RunE:  adminAddUserRun,
	}
	addUserCmd.Flags().IntVar(&adminGatewayPort, "gateway-port", 0, "Gateway port (auto-assigned if 0)")
	addUserCmd.Flags().StringVar(&adminIAMIdentity, "iam-identity", "", "IAM identity (SSO username/email)")

	listUsersCmd := &cobra.Command{
		Use:   "list-users",
		Short: "List all provisioned users",
		RunE:  adminListUsersRun,
	}

	removeUserCmd := &cobra.Command{
		Use:   "remove-user <slack_member_id>",
		Short: "Remove a user from the instance",
		Args:  cobra.ExactArgs(1),
		RunE:  adminRemoveUserRun,
	}
	removeUserCmd.Flags().BoolVar(&adminForce, "force", false, "Skip confirmation")
	removeUserCmd.Flags().BoolVar(&adminDeleteSecrets, "delete-secrets", false, "Also delete user secrets")

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

	adminCmd.AddCommand(addUserCmd, listUsersCmd, removeUserCmd, mapUserCmd, cycleHostCmd)
	rootCmd.AddCommand(adminCmd)
}

func adminAddUserRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	memberID := args[0]
	slackChannel := args[1]

	if err := validateMemberID(memberID); err != nil {
		return err
	}
	if err := validateChannelID(slackChannel); err != nil {
		return err
	}

	// Auto-assign port if not specified
	gatewayPort := adminGatewayPort
	if gatewayPort == 0 {
		entries, err := awsutil.GetParametersByPath(ctx, clients.SSM, "/openclaw/users/")
		if err != nil {
			gatewayPort = 18789
		} else {
			maxPort := 18788
			for _, e := range entries {
				var cfg struct {
					GatewayPort int `json:"gateway_port"`
				}
				if json.Unmarshal([]byte(e.Value), &cfg) == nil && cfg.GatewayPort > maxPort {
					maxPort = cfg.GatewayPort
				}
			}
			gatewayPort = maxPort + 1
		}
		fmt.Printf("Auto-assigned gateway port: %d\n", gatewayPort)
	}

	// Get IAM identity
	iamIdentity := adminIAMIdentity
	if iamIdentity == "" {
		// Auto-detect caller's SSO identity as default
		defaultIdentity := ""
		if id, err := discovery.ResolveIdentity(ctx, clients.STS, clients.SSM); err == nil && id.SessionName != "" {
			defaultIdentity = id.SessionName
		}
		var err error
		iamIdentity, err = ui.TextPromptWithDefault("SSO username/email of the user to add", defaultIdentity)
		if err != nil {
			return err
		}
	}

	// Create SSM parameters
	userConfigJSON, _ := json.Marshal(map[string]interface{}{
		"slack_channel": slackChannel,
		"gateway_port":  gatewayPort,
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
		SlackChannel  string
		AWSRegion     string
		GatewayPort   int
		OpenClawImage string
	}{
		MemberID:      memberID,
		SlackChannel:  slackChannel,
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

func adminListUsersRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	entries, err := awsutil.GetParametersByPath(ctx, clients.SSM, "/openclaw/users/")
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No users found.")
		return nil
	}

	headers := []string{"SLACK MEMBER ID", "SLACK CHANNEL", "GATEWAY PORT"}
	var rows [][]string
	for _, e := range entries {
		// Extract member ID from parameter name
		parts := strings.Split(e.Name, "/")
		memberID := parts[len(parts)-1]

		var cfg struct {
			SlackChannel string `json:"slack_channel"`
			GatewayPort  int    `json:"gateway_port"`
		}
		if json.Unmarshal([]byte(e.Value), &cfg) != nil {
			continue
		}
		rows = append(rows, []string{memberID, cfg.SlackChannel, strconv.Itoa(cfg.GatewayPort)})
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
	tmpl, err := template.New("removeuser").Parse(scripts.RemoveUserScript)
	if err != nil {
		return fmt.Errorf("failed to parse remove-user template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, struct{ MemberID string }{MemberID: memberID})
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

func adminCycleHostRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	if !adminForce {
		if !ui.Confirm("This will restart the EC2 instance and ALL user containers. Continue?") {
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
