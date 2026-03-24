package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	awsutil "github.com/cruxdigital-llc/conga-line/cli/internal/aws"
	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"

	// Register providers via init()
	_ "github.com/cruxdigital-llc/conga-line/cli/internal/provider/awsprovider"
	_ "github.com/cruxdigital-llc/conga-line/cli/internal/provider/localprovider"
	_ "github.com/cruxdigital-llc/conga-line/cli/internal/provider/remoteprovider"
)

var (
	flagRegion   string
	flagProfile  string
	flagAgent    string
	flagVerbose  bool
	flagTimeout  time.Duration
	flagProvider string
	flagDataDir  string
	flagJSON     string
	flagOutput   string

	// prov is the active provider, initialized in PersistentPreRunE.
	prov provider.Provider

	// Kept for auth login display (AWS-specific, no provider interaction).
	resolvedProfile     string
	resolvedProfileInfo *awsutil.AWSProfileInfo
	resolvedRegion      string
)

var rootCmd = &cobra.Command{
	Use:   "conga",
	Short: "Conga Line — manage your OpenClaw deployment",
	Long:  "Cross-platform CLI for managing OpenClaw containers via pluggable providers (AWS, local Docker, remote SSH).",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize JSON mode early so errors can be emitted as JSON.
		// Set OutputJSON before parsing so that even parse failures are JSON-formatted.
		if flagJSON != "" {
			if flagOutput == "text" && cmd.Flags().Changed("output") {
				return fmt.Errorf("--json implies --output json; cannot use --output text with --json")
			}
			ui.OutputJSON = true
			if err := ui.SetJSONMode(flagJSON); err != nil {
				return err
			}
		}
		if flagOutput == "json" {
			ui.OutputJSON = true
		}

		// Skip provider init for commands that don't need it
		if cmd.Name() == "version" || cmd.Name() == "help" || cmd.Name() == "json-schema" || cmd.Name() == "serve" {
			return nil
		}

		// Load persisted config
		cfg, _ := provider.LoadConfig(provider.DefaultConfigPath())

		// Override with flags
		if flagProvider != "" {
			cfg.Provider = flagProvider
		}
		if flagDataDir != "" {
			cfg.DataDir = flagDataDir
		}

		// Default to local (works without cloud credentials)
		if cfg.Provider == "" {
			cfg.Provider = "local"
		}

		// AWS-specific: resolve profile and region for provider init
		if cfg.Provider == "aws" {
			resolvedProfile, resolvedProfileInfo = resolveProfile()
			if flagRegion != "" {
				resolvedRegion = flagRegion
			} else if resolvedProfileInfo != nil && resolvedProfileInfo.Region != "" {
				resolvedRegion = resolvedProfileInfo.Region
			}
			cfg.Region = resolvedRegion
			cfg.Profile = resolvedProfile
		}

		// Initialize provider
		var err error
		prov, err = provider.Get(cfg.Provider, cfg)
		if err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagRegion, "region", "", "AWS region (default: from AWS profile)")
	rootCmd.PersistentFlags().StringVar(&flagProfile, "profile", "", "AWS CLI profile name (default: auto-detect from active SSO session)")
	rootCmd.PersistentFlags().StringVar(&flagAgent, "agent", "", "Agent name (auto-detected from identity if omitted)")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().DurationVar(&flagTimeout, "timeout", 5*time.Minute, "Global timeout for operations")
	rootCmd.PersistentFlags().StringVar(&flagProvider, "provider", "", "Deployment provider: aws, local, remote (default: local)")
	rootCmd.PersistentFlags().StringVar(&flagDataDir, "data-dir", "", "Data directory for local provider (default: ~/.conga/)")
	rootCmd.PersistentFlags().StringVar(&flagJSON, "json", "", "JSON input (inline or @file.json); implies --output json")
	rootCmd.PersistentFlags().StringVar(&flagOutput, "output", "text", "Output format: text, json")
}

// commandContext returns a context with the global timeout applied.
func commandContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), flagTimeout)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if ui.OutputJSON {
			ui.EmitError(err)
		}
		os.Exit(1)
	}
}

// resolveAgentName returns the agent name to operate on.
func resolveAgentName(ctx context.Context) (string, error) {
	if flagAgent != "" {
		return flagAgent, nil
	}

	agent, err := prov.ResolveAgentByIdentity(ctx)
	if err != nil {
		return "", err
	}
	if agent == nil {
		return "", fmt.Errorf("could not determine agent name; use --agent flag")
	}
	return agent.Name, nil
}

// resolveProfile returns the AWS profile to use and its parsed info.
func resolveProfile() (string, *awsutil.AWSProfileInfo) {
	if flagProfile != "" {
		info := awsutil.GetProfileInfo(flagProfile)
		return flagProfile, info
	}
	if envProfile := os.Getenv("AWS_PROFILE"); envProfile != "" {
		info := awsutil.GetProfileInfo(envProfile)
		return "", info
	}
	if info := awsutil.DetectSSOProfileInfo(); info != nil {
		return info.Name, info
	}
	return "", nil
}

// Validation helpers delegate to common package.
func validateMemberID(id string) error    { return common.ValidateMemberID(id) }
func validateChannelID(id string) error   { return common.ValidateChannelID(id) }
func validateAgentName(name string) error { return common.ValidateAgentName(name) }
