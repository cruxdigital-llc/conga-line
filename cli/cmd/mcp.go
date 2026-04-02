package cmd

import (
	"fmt"
	"os"

	awsutil "github.com/cruxdigital-llc/conga-line/cli/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/mcpserver"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/ui"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server for AI agent integration",
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP server on stdio",
	Long:  "Start an MCP (Model Context Protocol) server on stdio. This exposes Conga Line operations as MCP tools for AI agents like Claude Code.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Suppress all terminal UI (spinners, prompts) — stdout is the MCP transport.
		ui.OutputJSON = true

		// Load config (same as PersistentPreRunE but using env var overrides).
		cfg, _ := provider.LoadConfig(provider.DefaultConfigPath())

		// Env var overrides for non-interactive MCP context.
		if v := os.Getenv("CONGA_PROVIDER"); v != "" {
			cfg.Provider = v
		}
		if cfg.Provider == "" {
			cfg.Provider = "local"
		}

		// AWS-specific env vars.
		if cfg.Provider == "aws" {
			if v := os.Getenv("CONGA_PROFILE"); v != "" {
				cfg.Profile = v
			} else if v := os.Getenv("AWS_PROFILE"); v != "" {
				cfg.Profile = v
			}
			if v := os.Getenv("CONGA_REGION"); v != "" {
				cfg.Region = v
			} else if v := os.Getenv("AWS_REGION"); v != "" {
				cfg.Region = v
			}
			// Resolve region from the AWS profile if not set via env vars.
			if cfg.Region == "" && cfg.Profile != "" {
				if info := awsutil.GetProfileInfo(cfg.Profile); info != nil && info.Region != "" {
					cfg.Region = info.Region
				}
			}
		}

		// Remote provider env vars.
		if cfg.Provider == "remote" {
			if v := os.Getenv("CONGA_SSH_HOST"); v != "" {
				cfg.SSHHost = v
			}
			if v := os.Getenv("CONGA_SSH_USER"); v != "" {
				cfg.SSHUser = v
			}
			if v := os.Getenv("CONGA_SSH_KEY_PATH"); v != "" {
				cfg.SSHKeyPath = v
			}
		}

		prov, err := provider.Get(cfg.Provider, cfg)
		if err != nil {
			return fmt.Errorf("initializing %s provider: %w", cfg.Provider, err)
		}

		srv := mcpserver.NewServer(prov, Version)
		return srv.Serve()
	},
}

func init() {
	mcpCmd.AddCommand(mcpServeCmd)
	rootCmd.AddCommand(mcpCmd)
}
