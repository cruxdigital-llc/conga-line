package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/discovery"
	"github.com/spf13/cobra"
)

const defaultInstanceTag = "openclaw-host"

var validIDPattern = regexp.MustCompile(`^[A-Z0-9]+$`)
var validChannelPattern = regexp.MustCompile(`^[A-Z0-9]+$`)

var (
	flagRegion  string
	flagProfile string
	flagAgent   string
	flagVerbose bool

	clients *awsutil.Clients

	// Resolved at startup from AWS profile auto-detection.
	resolvedProfile     string
	resolvedProfileInfo *awsutil.AWSProfileInfo
	resolvedRegion      string
)

var rootCmd = &cobra.Command{
	Use:   "cruxclaw",
	Short: "CruxClaw — manage your OpenClaw deployment",
	Long:  "Cross-platform CLI for managing OpenClaw containers on AWS via SSM.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		resolvedProfile, resolvedProfileInfo = resolveProfile()

		// Region priority: --region flag > AWS profile > empty (SDK default)
		if flagRegion != "" {
			resolvedRegion = flagRegion
		} else if resolvedProfileInfo != nil && resolvedProfileInfo.Region != "" {
			resolvedRegion = resolvedProfileInfo.Region
		}

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagRegion, "region", "", "AWS region (default: from AWS profile)")
	rootCmd.PersistentFlags().StringVar(&flagProfile, "profile", "", "AWS CLI profile name (default: auto-detect from active SSO session)")
	rootCmd.PersistentFlags().StringVar(&flagAgent, "agent", "", "Agent name (auto-detected from IAM if omitted)")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Verbose output")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func ensureClients(ctx context.Context) error {
	if clients != nil {
		return nil
	}
	c, err := awsutil.NewClients(ctx, resolvedRegion, resolvedProfile)
	if err != nil {
		if resolvedProfile != "" {
			return fmt.Errorf("failed to initialize AWS session (profile=%q): %w\nRun `aws sso login --profile %s` to authenticate", resolvedProfile, err, resolvedProfile)
		}
		return fmt.Errorf("failed to initialize AWS session: %w\nRun `aws sso login --profile <your-profile>` to authenticate", err)
	}
	clients = c
	return nil
}

// resolveProfile returns the AWS profile to use and its parsed info. Priority:
//  1. --profile flag
//  2. AWS_PROFILE env var (we return "" to let the SDK read it, but still parse info)
//  3. auto-detect from active SSO session in ~/.aws/config
//  4. empty → SDK default chain
func resolveProfile() (string, *awsutil.AWSProfileInfo) {
	if flagProfile != "" {
		info := awsutil.GetProfileInfo(flagProfile)
		return flagProfile, info
	}
	if envProfile := os.Getenv("AWS_PROFILE"); envProfile != "" {
		info := awsutil.GetProfileInfo(envProfile)
		return "", info // let the SDK read AWS_PROFILE directly
	}
	if info := awsutil.DetectSSOProfileInfo(); info != nil {
		return info.Name, info
	}
	return "", nil
}

func validateMemberID(id string) error {
	if !validIDPattern.MatchString(id) {
		return fmt.Errorf("invalid member ID %q: must be uppercase alphanumeric (e.g., UXXXXXXXXXX)", id)
	}
	return nil
}

func validateChannelID(id string) error {
	if !validChannelPattern.MatchString(id) {
		return fmt.Errorf("invalid channel ID %q: must be uppercase alphanumeric (e.g., CXXXXXXXXXX)", id)
	}
	return nil
}

// resolveAgentName returns the caller's agent name. If allowOverride is true,
// the --agent flag can specify a different agent (for admin commands).
// User-facing commands should pass allowOverride=false to prevent
// cross-user operations.
func resolveAgentName(ctx context.Context) (string, error) {
	return resolveAgentNameWithOverride(ctx, false)
}

func resolveAgentNameAdmin(ctx context.Context) (string, error) {
	return resolveAgentNameWithOverride(ctx, true)
}

func resolveAgentNameWithOverride(ctx context.Context, allowOverride bool) (string, error) {
	identity, err := discovery.ResolveIdentity(ctx, clients.STS, clients.SSM)
	if err != nil {
		return "", err
	}

	if flagAgent != "" {
		if !allowOverride && identity.AgentName != "" && flagAgent != identity.AgentName {
			return "", fmt.Errorf("cannot operate on another user's resources. Your agent name is %s", identity.AgentName)
		}
		return flagAgent, nil
	}

	if identity.AgentName == "" {
		return "", fmt.Errorf("your IAM identity (%s) is not mapped to an agent.\nUse --agent <name> or ask admin to run `cruxclaw admin add-user`", identity.SessionName)
	}
	return identity.AgentName, nil
}

func findInstance(ctx context.Context) (string, error) {
	return discovery.FindInstance(ctx, clients.EC2, defaultInstanceTag)
}
