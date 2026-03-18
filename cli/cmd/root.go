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
		return fmt.Errorf("invalid member ID %q: must be uppercase alphanumeric (e.g., UEXAMPLE01)", id)
	}
	return nil
}

func validateChannelID(id string) error {
	if !validChannelPattern.MatchString(id) {
		return fmt.Errorf("invalid channel ID %q: must be uppercase alphanumeric (e.g., CEXAMPLE01)", id)
	}
	return nil
}

func resolveUserID(ctx context.Context) (string, error) {
	if flagUser != "" {
		if err := validateMemberID(flagUser); err != nil {
			return "", err
		}
		return flagUser, nil
	}

	identity, err := discovery.ResolveIdentity(ctx, clients.STS, clients.SSM)
	if err != nil {
		return "", err
	}
	if identity.MemberID == "" {
		return "", fmt.Errorf("your IAM identity (%s) is not mapped to an OpenClaw user.\nUse --user <member_id> or ask admin to update the mapping", identity.SessionName)
	}
	return identity.MemberID, nil
}

func findInstance(ctx context.Context) (string, error) {
	return discovery.FindInstance(ctx, clients.EC2, cfg.InstanceTag)
}
