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
	bootstrapFile    string
	bootstrapEnvFile string
)

func init() {
	bootstrapCmd := &cobra.Command{
		Use:   "bootstrap [manifest.yaml]",
		Short: "Bootstrap an environment from a manifest",
		Long: `Read a YAML manifest and provision an environment:
setup, agents, secrets, channels, bindings, and policy.

This is an additive, one-time setup command. Each step is
idempotent — re-running skips completed work. Resources not
in the manifest are left untouched (no removals).

Secrets use $VAR references expanded from the --env file.
Policy is seeded from the manifest only if no conga-policy.yaml
exists — an existing policy file always takes precedence.

Example:
  conga bootstrap demo.yaml --env demo.env`,
		Args: cobra.MaximumNArgs(1),
		RunE: bootstrapRun,
	}
	bootstrapCmd.Flags().StringVarP(&bootstrapFile, "file", "f", "", "Path to manifest file")
	bootstrapCmd.Flags().StringVar(&bootstrapEnvFile, "env", "", "Path to env file (KEY=VALUE format) for secret expansion")
	rootCmd.AddCommand(bootstrapCmd)
}

func bootstrapRun(cmd *cobra.Command, args []string) error {
	// Resolve file path: positional arg > -f flag
	path := bootstrapFile
	if len(args) > 0 {
		path = args[0]
	}
	if path == "" {
		return fmt.Errorf("manifest file required: conga bootstrap <manifest.yaml> or conga bootstrap -f <file>")
	}

	// Load env file before anything else so $VAR expansion works
	if bootstrapEnvFile != "" {
		if err := manifest.LoadEnvFile(bootstrapEnvFile); err != nil {
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
	cfg, err := provider.LoadConfig(provider.DefaultConfigPath())
	if err != nil {
		return fmt.Errorf("loading provider config: %w", err)
	}
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

	result, err := manifest.Bootstrap(ctx, prov, m, policyPath)
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

	fmt.Printf("\nEnvironment bootstrapped from %s\n", filepath.Base(path))
	return nil
}
