package cmd

import (
	"context"
	"fmt"
	"strings"

	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/spf13/cobra"
)

var secretValue string
var secretForce bool

func init() {
	secretsCmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage your OpenClaw secrets",
	}

	setCmd := &cobra.Command{
		Use:   "set [name]",
		Short: "Create or update a secret",
		Long: `Create or update a secret in AWS Secrets Manager.

The secret name is transformed into an environment variable and injected into
your OpenClaw container in SCREAMING_SNAKE_CASE format. For example:

  anthropic-api-key  →  ANTHROPIC_API_KEY
  google-client-id   →  GOOGLE_CLIENT_ID

If no name is provided, you will be prompted interactively for both the name
and value. Use --value to pass the secret value non-interactively.

After setting a secret, run 'cruxclaw refresh' to inject it into your container.`,
		Example: `  cruxclaw secrets set                          # interactive mode
  cruxclaw secrets set anthropic-api-key         # prompts for value
  cruxclaw secrets set anthropic-api-key --value sk-ant-...  # non-interactive`,
		Args: cobra.MaximumNArgs(1),
		RunE: secretsSetRun,
	}
	setCmd.Flags().StringVar(&secretValue, "value", "", "Secret value (will be prompted if omitted)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List your secrets",
		RunE:  secretsListRun,
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a secret",
		Args:  cobra.ExactArgs(1),
		RunE:  secretsDeleteRun,
	}
	deleteCmd.Flags().BoolVar(&secretForce, "force", false, "Skip confirmation")

	secretsCmd.AddCommand(setCmd, listCmd, deleteCmd)
	rootCmd.AddCommand(secretsCmd)
}

func secretsSetRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	var name string
	if len(args) > 0 {
		name = args[0]
	} else {
		fmt.Println("Secret names are injected as env vars in SCREAMING_SNAKE_CASE (e.g. anthropic-api-key → ANTHROPIC_API_KEY).")
		name, err = ui.TextPrompt("Secret name (e.g. anthropic-api-key)")
		if err != nil {
			return err
		}
		if name == "" {
			return fmt.Errorf("secret name cannot be empty")
		}
		fmt.Printf("  → will be injected as: %s\n", secretNameToEnvVar(name))
	}

	value := secretValue
	if value == "" {
		value, err = ui.SecretPrompt(fmt.Sprintf("Enter value for %s", name))
		if err != nil {
			return err
		}
	}
	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}

	secretPath := fmt.Sprintf("openclaw/agents/%s/%s", agentName, name)
	if err := awsutil.SetSecret(ctx, clients.SecretsManager, secretPath, value); err != nil {
		return err
	}

	fmt.Printf("Secret '%s' saved (env var: %s). Run `cruxclaw refresh` to pick it up.\n", name, secretNameToEnvVar(name))
	return nil
}

func secretsListRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	prefix := fmt.Sprintf("openclaw/agents/%s/", agentName)
	entries, err := awsutil.ListSecrets(ctx, clients.SecretsManager, prefix)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Println("No secrets found. Use `cruxclaw secrets set <name>` to add one.")
		return nil
	}

	headers := []string{"NAME", "ENV VAR", "LAST CHANGED"}
	var rows [][]string
	for _, e := range entries {
		rows = append(rows, []string{e.Name, secretNameToEnvVar(e.Name), e.LastChanged})
	}
	ui.PrintTable(headers, rows)
	return nil
}

func secretsDeleteRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	if !secretForce {
		if !ui.Confirm(fmt.Sprintf("Delete secret '%s' for %s?", args[0], agentName)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	secretPath := fmt.Sprintf("openclaw/agents/%s/%s", agentName, args[0])
	if err := awsutil.DeleteSecret(ctx, clients.SecretsManager, secretPath); err != nil {
		return err
	}

	fmt.Printf("Secret '%s' deleted.\n", args[0])
	return nil
}

// secretNameToEnvVar converts a secret name to the environment variable name
// injected into the container. Mirrors the bootstrap transform in user-data.sh.tftpl.
func secretNameToEnvVar(name string) string {
	return strings.NewReplacer("-", "_").Replace(strings.ToUpper(name))
}
