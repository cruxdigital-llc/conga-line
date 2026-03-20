package cmd

import (
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
		profileName := resolvedProfile
		if profileName == "" {
			profileName = "your-profile"
		}
		fmt.Println("To authenticate, run:")
		fmt.Println()
		fmt.Printf("  aws sso login --profile %s\n", profileName)

		if resolvedProfileInfo != nil {
			fmt.Println()
			fmt.Println("Your profile is configured with:")
			if resolvedProfileInfo.SSOStartURL != "" {
				fmt.Printf("  SSO start URL:  %s\n", resolvedProfileInfo.SSOStartURL)
			}
			if resolvedProfileInfo.SSORegion != "" {
				fmt.Printf("  SSO region:     %s\n", resolvedProfileInfo.SSORegion)
			}
			if resolvedProfileInfo.SSOAccountID != "" {
				fmt.Printf("  Account ID:     %s\n", resolvedProfileInfo.SSOAccountID)
			}
			if resolvedProfileInfo.SSORoleName != "" {
				fmt.Printf("  Role name:      %s\n", resolvedProfileInfo.SSORoleName)
			}
		}

		fmt.Println()
		fmt.Println("If you haven't configured an SSO profile yet, run:")
		fmt.Printf("  aws configure sso --profile %s\n", profileName)
		fmt.Println()
		fmt.Println("Tip: set AWS_PROFILE to skip --profile on subsequent commands:")
		fmt.Printf("  export AWS_PROFILE=%s\n", profileName)
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current AWS identity and agent mapping",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := commandContext()
		defer cancel()
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
		if err == nil && identity.AgentName != "" {
			fmt.Printf("Agent:           %s\n", identity.AgentName)
		} else {
			fmt.Println("Agent:           (not mapped — use --agent or ask admin)")
		}

		return nil
	},
}
