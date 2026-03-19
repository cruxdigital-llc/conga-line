package cmd

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/discovery"
	"github.com/spf13/cobra"
)

func init() {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage AWS SSO authentication",
	}

	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate via AWS SSO",
	Long:  "Opens your browser to complete AWS SSO login. Credentials are cached for future commands.",
	RunE: func(cmd *cobra.Command, args []string) error {
		profile := resolveProfile()
		if profile == "" {
			profile = "cruxclaw"
		}
		fmt.Println("To authenticate, run:")
		fmt.Println()
		fmt.Printf("  aws configure sso --profile %s\n", profile)
		fmt.Println()
		fmt.Println("Use the following settings:")
		fmt.Printf("  SSO start URL:  %s\n", cfg.SSOStartURL)
		fmt.Printf("  SSO region:     %s\n", cfg.Region)
		fmt.Printf("  Account ID:     %s\n", cfg.SSOAccountID)
		fmt.Printf("  Role name:      %s\n", cfg.SSORoleName)
		fmt.Println()
		fmt.Println("Then run:")
		fmt.Printf("  aws sso login --profile %s\n", profile)
		fmt.Println()
		fmt.Println("Tip: set AWS_PROFILE to skip --profile on subsequent commands:")
		fmt.Printf("  export AWS_PROFILE=%s\n", profile)
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current AWS identity and Slack Member ID",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		if err := ensureClients(ctx); err != nil {
			return err
		}

		out, err := clients.STS.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			return fmt.Errorf("session expired or invalid. Run `cruxclaw auth login` to authenticate.\n%w", err)
		}

		fmt.Printf("Identity:        %s\n", aws.ToString(out.Arn))
		fmt.Printf("Account:         %s\n", aws.ToString(out.Account))

		identity, err := discovery.ResolveIdentity(ctx, clients.STS, clients.SSM)
		if err == nil && identity.MemberID != "" {
			fmt.Printf("Slack Member ID: %s\n", identity.MemberID)
		} else {
			fmt.Println("Slack Member ID: (not mapped — use --user or ask admin)")
		}

		return nil
	},
}
