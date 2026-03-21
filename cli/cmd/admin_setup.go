package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/spf13/cobra"
)

func adminSetupRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	// Read the setup manifest from SSM
	manifestJSON, err := awsutil.GetParameter(ctx, clients.SSM, "/openclaw/config/setup-manifest")
	if err != nil {
		return fmt.Errorf("setup manifest not found in SSM. Run `terraform apply` first to create infrastructure")
	}

	var manifest struct {
		Config   map[string]string `json:"config"`
		Defaults map[string]string `json:"defaults"`
		Secrets  map[string]string `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return fmt.Errorf("failed to parse setup manifest: %w", err)
	}

	fmt.Println("Reading setup manifest...")
	changed := 0

	// Process config values (stored in SSM) — sorted for deterministic prompt order
	configKeys := make([]string, 0, len(manifest.Config))
	for key := range manifest.Config {
		configKeys = append(configKeys, key)
	}
	sort.Strings(configKeys)
	for _, key := range configKeys {
		description := manifest.Config[key]
		paramName := fmt.Sprintf("/openclaw/config/%s", key)
		current, err := awsutil.GetParameter(ctx, clients.SSM, paramName)
		if err != nil {
			// GetParameter wraps "not found" errors; any error means we couldn't read it.
			// Warn the user but treat as unset so they can provide a value.
			fmt.Fprintf(os.Stderr, "  Warning: could not read %s: %v\n", paramName, err)
			current = ""
		}

		status := "set"
		if current == "" {
			status = "not set"
		}
		fmt.Printf("\n[config] %s — %s (%s)\n", key, description, status)

		if current != "" {
			if !ui.Confirm("  Update this value?") {
				continue
			}
		}

		defaultVal := manifest.Defaults[key]
		value, err := ui.TextPromptWithDefault(fmt.Sprintf("  Enter value for %s", key), defaultVal)
		if err != nil {
			return err
		}
		if value == "" {
			fmt.Println("  Skipped (empty value)")
			continue
		}

		if err := awsutil.PutParameter(ctx, clients.SSM, paramName, value); err != nil {
			return fmt.Errorf("failed to set config %s: %w", key, err)
		}
		fmt.Printf("  Saved to SSM: %s\n", paramName)
		changed++
	}

	// Process secrets (stored in Secrets Manager) — sorted for deterministic prompt order
	secretPaths := make([]string, 0, len(manifest.Secrets))
	for path := range manifest.Secrets {
		secretPaths = append(secretPaths, path)
	}
	sort.Strings(secretPaths)
	for _, path := range secretPaths {
		description := manifest.Secrets[path]
		current, err := awsutil.GetSecretValue(ctx, clients.SecretsManager, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not read secret %s: %v\n", path, err)
			current = ""
		}

		status := "set"
		if current == "" || current == "REPLACE_ME" {
			status = "not set"
		}
		fmt.Printf("\n[secret] %s — %s (%s)\n", path, description, status)

		if current != "" && current != "REPLACE_ME" {
			if !ui.Confirm("  Update this value?") {
				continue
			}
		}

		value, err := ui.SecretPrompt(fmt.Sprintf("  Enter value for %s", path))
		if err != nil {
			return err
		}
		if value == "" {
			fmt.Println("  Skipped (empty value)")
			continue
		}

		if err := awsutil.SetSecret(ctx, clients.SecretsManager, path, value); err != nil {
			return fmt.Errorf("failed to set secret %s: %w", path, err)
		}
		fmt.Printf("  Saved to Secrets Manager\n")
		changed++
	}

	if changed > 0 {
		fmt.Printf("\n%d value(s) updated. Run `cruxclaw admin cycle-host` to apply.\n", changed)
	} else {
		fmt.Println("\nAll values already configured. No changes needed.")
	}
	return nil
}
