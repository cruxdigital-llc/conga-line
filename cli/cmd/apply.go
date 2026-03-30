package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/cruxdigital-llc/conga-line/cli/internal/manifest"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

var (
	applyFile    string
	applyEnvFile string
)

func init() {
	applyCmd := &cobra.Command{
		Use:   "apply [manifest.yaml]",
		Short: "Apply a manifest to provision an environment",
		Long: `Read a YAML manifest and execute all provisioning steps:
setup, agents, secrets, channels, bindings, policy, and refresh.

Each step is idempotent — re-running skips completed work.
Secrets use $VAR references expanded from environment variables.

Example:
  conga apply demo.yaml --env demo.env`,
		Args: cobra.MaximumNArgs(1),
		RunE: applyRun,
	}
	applyCmd.Flags().StringVarP(&applyFile, "file", "f", "", "Path to manifest file")
	applyCmd.Flags().StringVar(&applyEnvFile, "env", "", "Path to env file (KEY=VALUE format) for secret expansion")
	rootCmd.AddCommand(applyCmd)
}

func applyRun(cmd *cobra.Command, args []string) error {
	// Resolve file path: positional arg > -f flag
	path := applyFile
	if len(args) > 0 {
		path = args[0]
	}
	if path == "" {
		return fmt.Errorf("manifest file required: conga apply <manifest.yaml> or conga apply -f <file>")
	}

	// Load env file before anything else so $VAR expansion works
	if applyEnvFile != "" {
		if err := manifest.LoadEnvFile(applyEnvFile); err != nil {
			return err
		}
	}

	m, err := manifest.Load(path)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}
	if err := manifest.Validate(m); err != nil {
		return fmt.Errorf("validating manifest: %w", err)
	}
	if err := manifest.ExpandSecrets(m); err != nil {
		return err
	}

	// Initialize provider: --provider flag > manifest > persisted config > "local"
	cfg, _ := provider.LoadConfig(provider.DefaultConfigPath())
	if flagProvider != "" {
		cfg.Provider = flagProvider
	} else if m.Provider != "" {
		cfg.Provider = m.Provider
	}
	if flagDataDir != "" {
		cfg.DataDir = flagDataDir
	}
	if cfg.Provider == "" {
		cfg.Provider = "local"
	}
	prov, err = provider.Get(cfg.Provider, cfg)
	if err != nil {
		return fmt.Errorf("initializing provider %q: %w", cfg.Provider, err)
	}

	policyPath, err := defaultPolicyPath()
	if err != nil {
		return err
	}

	ctx, cancel := commandContext()
	defer cancel()

	result, err := manifest.Apply(ctx, prov, m, policyPath)
	if err != nil {
		if ui.OutputJSON && result != nil {
			ui.EmitJSON(result)
		}
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(result)
		return nil
	}

	fmt.Printf("\nEnvironment applied from %s\n", filepath.Base(path))
	return nil
}
