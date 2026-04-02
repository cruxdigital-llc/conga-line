package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/pkg/common"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/ui"
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
		Long: `Create or update a secret for your agent.

The secret name is transformed into an environment variable and injected into
your OpenClaw container in SCREAMING_SNAKE_CASE format. For example:

  anthropic-api-key  →  ANTHROPIC_API_KEY
  google-client-id   →  GOOGLE_CLIENT_ID

After setting a secret, run 'conga refresh' to inject it into your container.`,
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
	ctx, cancel := commandContext()
	defer cancel()

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	var name string
	if len(args) > 0 {
		name = args[0]
	} else if ui.JSONInputActive {
		name, err = ui.MustGetString("name")
		if err != nil {
			return err
		}
	} else {
		fmt.Println("Secret names are injected as env vars in SCREAMING_SNAKE_CASE (e.g. anthropic-api-key → ANTHROPIC_API_KEY).")
		name, err = ui.TextPrompt("Secret name (e.g. anthropic-api-key)")
		if err != nil {
			return err
		}
	}
	if name == "" {
		return fmt.Errorf("secret name cannot be empty")
	}
	if !ui.OutputJSON {
		fmt.Printf("  -> will be injected as: %s\n", common.SecretNameToEnvVar(name))
	}

	value := secretValue
	if value == "" {
		if ui.JSONInputActive {
			value, err = ui.MustGetString("value")
			if err != nil {
				return err
			}
		} else {
			value, err = ui.SecretPrompt(fmt.Sprintf("Enter value for %s", name))
			if err != nil {
				return err
			}
		}
	}
	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}

	if err := prov.SetSecret(ctx, agentName, name, value); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Secret string `json:"secret"`
			EnvVar string `json:"env_var"`
			Status string `json:"status"`
		}{
			Secret: name,
			EnvVar: common.SecretNameToEnvVar(name),
			Status: "saved",
		})
		return nil
	}

	fmt.Printf("Secret '%s' saved (value: %s, env var: %s). Run `conga refresh` to pick it up.\n", name, common.MaskSecret(value), common.SecretNameToEnvVar(name))
	return nil
}

func secretsListRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	entries, err := prov.ListSecrets(ctx, agentName)
	if err != nil {
		return err
	}

	if ui.OutputJSON {
		type entry struct {
			Name        string `json:"name"`
			EnvVar      string `json:"env_var"`
			LastChanged string `json:"last_changed,omitempty"`
		}
		result := make([]entry, 0, len(entries))
		for _, e := range entries {
			lc := ""
			if !e.LastChanged.IsZero() {
				lc = e.LastChanged.Format("2006-01-02T15:04:05Z")
			}
			result = append(result, entry{Name: e.Name, EnvVar: e.EnvVar, LastChanged: lc})
		}
		ui.EmitJSON(result)
		return nil
	}

	if len(entries) == 0 {
		fmt.Println("No secrets found. Use `conga secrets set <name>` to add one.")
		return nil
	}

	headers := []string{"NAME", "ENV VAR", "LAST CHANGED"}
	var rows [][]string
	for _, e := range entries {
		lastChanged := ""
		if !e.LastChanged.IsZero() {
			lastChanged = e.LastChanged.Format("2006-01-02 15:04:05")
		}
		rows = append(rows, []string{e.Name, e.EnvVar, lastChanged})
	}
	ui.PrintTable(headers, rows)
	return nil
}

func secretsDeleteRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	if !secretForce && !ui.JSONInputActive {
		if !ui.Confirm(fmt.Sprintf("Delete secret '%s' for %s?", args[0], agentName)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := prov.DeleteSecret(ctx, agentName, args[0]); err != nil {
		return err
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Secret string `json:"secret"`
			Status string `json:"status"`
		}{Secret: args[0], Status: "deleted"})
		return nil
	}

	fmt.Printf("Secret '%s' deleted.\n", args[0])
	return nil
}
