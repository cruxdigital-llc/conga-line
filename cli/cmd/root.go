package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/discovery"
	"github.com/spf13/cobra"
)

const defaultInstanceTag = "openclaw-host"

var validMemberIDPattern = regexp.MustCompile(`^U[A-Z0-9]{10}$`)
var validChannelIDPattern = regexp.MustCompile(`^C[A-Z0-9]{10}$`)

var (
	flagRegion  string
	flagProfile string
	flagAgent   string
	flagVerbose bool
	flagTimeout time.Duration

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
	rootCmd.PersistentFlags().DurationVar(&flagTimeout, "timeout", 5*time.Minute, "Global timeout for AWS operations")
}

// commandContext returns a context with the global timeout applied.
func commandContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), flagTimeout)
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
	if !validMemberIDPattern.MatchString(id) {
		return fmt.Errorf("invalid Slack member ID %q: must start with 'U' followed by 10 alphanumeric characters (e.g., U0123456789)", id)
	}
	return nil
}

func validateChannelID(id string) error {
	if !validChannelIDPattern.MatchString(id) {
		return fmt.Errorf("invalid Slack channel ID %q: must start with 'C' followed by 10 alphanumeric characters (e.g., C0123456789)", id)
	}
	return nil
}

// resolveAgentName returns the agent name to operate on.
// Uses --agent flag if provided, otherwise auto-detects from IAM identity.
// Access control is enforced by IAM, not the CLI.
func resolveAgentName(ctx context.Context) (string, error) {
	if flagAgent != "" {
		return flagAgent, nil
	}

	identity, err := discovery.ResolveIdentity(ctx, clients.STS, clients.SSM)
	if err != nil {
		return "", err
	}

	if identity.AgentName == "" {
		return "", fmt.Errorf("your IAM identity (%s) is not mapped to an agent.\nUse --agent <name> or ask admin to run `cruxclaw admin add-user`", identity.SessionName)
	}
	return identity.AgentName, nil
}

func findInstance(ctx context.Context) (string, error) {
	return discovery.FindInstance(ctx, clients.EC2, defaultInstanceTag)
}
