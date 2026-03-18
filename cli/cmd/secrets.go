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
		Use:   "set <name>",
		Short: "Create or update a secret",
		Args:  cobra.ExactArgs(1),
		RunE:  secretsSetRun,
	}
	setCmd.Flags().StringVar(&secretValue, "value", "", "Secret value (prompted if omitted)")

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

	memberID, err := resolveUserID(ctx)
	if err != nil {
		return err
	}

	value := secretValue
	if value == "" {
		value, err = ui.SecretPrompt(fmt.Sprintf("Enter value for %s", args[0]))
		if err != nil {
			return err
		}
	}

	secretPath := fmt.Sprintf("openclaw/%s/%s", memberID, args[0])
	if err := awsutil.SetSecret(ctx, clients.SecretsManager, secretPath, value); err != nil {
		return err
	}

	fmt.Printf("Secret '%s' saved. Run `cruxclaw refresh` to pick it up.\n", args[0])
	return nil
}

func secretsListRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	memberID, err := resolveUserID(ctx)
	if err != nil {
		return err
	}

	prefix := fmt.Sprintf("openclaw/%s/", memberID)
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

	memberID, err := resolveUserID(ctx)
	if err != nil {
		return err
	}

	if !secretForce {
		if !ui.Confirm(fmt.Sprintf("Delete secret '%s' for %s?", args[0], memberID)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	secretPath := fmt.Sprintf("openclaw/%s/%s", memberID, args[0])
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
