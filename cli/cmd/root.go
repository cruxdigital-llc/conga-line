package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/config"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/discovery"
	"github.com/spf13/cobra"
)

var validIDPattern = regexp.MustCompile(`^[A-Z0-9]+$`)
var validChannelPattern = regexp.MustCompile(`^[A-Z0-9]+$`)

var (
	flagRegion  string
	flagProfile string
	flagUser    string
	flagVerbose bool

	cfg     *config.Config
	clients *awsutil.Clients
)

var rootCmd = &cobra.Command{
	Use:   "cruxclaw",
	Short: "CruxClaw — manage your OpenClaw deployment",
	Long:  "Cross-platform CLI for managing OpenClaw containers on AWS via SSM.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cfg = config.Load()
		if flagRegion != "" {
			cfg.Region = flagRegion
		}
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagRegion, "region", "", "AWS region (default: from config)")
	rootCmd.PersistentFlags().StringVar(&flagProfile, "profile", "", "AWS CLI profile name")
	rootCmd.PersistentFlags().StringVar(&flagUser, "user", "", "OpenClaw member ID (auto-detected from IAM if omitted)")
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
	profile := flagProfile
	region := cfg.Region
	c, err := awsutil.NewClients(ctx, region, profile)
	if err != nil {
		return fmt.Errorf("failed to initialize AWS session: %w\nRun `cruxclaw auth login` to authenticate", err)
	}
	clients = c
	return nil
}

func validateMemberID(id string) error {
	if !validIDPattern.MatchString(id) {
		return fmt.Errorf("invalid member ID %q: must be uppercase alphanumeric (e.g., UA13HEGTS)", id)
	}
	return nil
}

func validateChannelID(id string) error {
	if !validChannelPattern.MatchString(id) {
		return fmt.Errorf("invalid channel ID %q: must be uppercase alphanumeric (e.g., C0ALL272SV8)", id)
	}
	return nil
}

// resolveUserID returns the caller's member ID. If allowOverride is true,
// the --user flag can specify a different user (for admin commands).
// User-facing commands should pass allowOverride=false to prevent
// cross-user operations.
func resolveUserID(ctx context.Context) (string, error) {
	return resolveUserIDWithOverride(ctx, false)
}

func resolveUserIDAdmin(ctx context.Context) (string, error) {
	return resolveUserIDWithOverride(ctx, true)
}

func resolveUserIDWithOverride(ctx context.Context, allowOverride bool) (string, error) {
	identity, err := discovery.ResolveIdentity(ctx, clients.STS, clients.SSM)
	if err != nil {
		return "", err
	}

	if flagUser != "" {
		if err := validateMemberID(flagUser); err != nil {
			return "", err
		}
		if !allowOverride && identity.MemberID != "" && flagUser != identity.MemberID {
			return "", fmt.Errorf("cannot operate on another user's resources. You are %s", identity.MemberID)
		}
		return flagUser, nil
	}

	if identity.MemberID == "" {
		return "", fmt.Errorf("your IAM identity (%s) is not mapped to an OpenClaw user.\nUse --user <member_id> or ask admin to update the mapping", identity.SessionName)
	}
	return identity.MemberID, nil
}

func findInstance(ctx context.Context) (string, error) {
	return discovery.FindInstance(ctx, clients.EC2, cfg.InstanceTag)
}
