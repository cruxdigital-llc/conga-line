package cmd

import (
	"fmt"

	"github.com/cruxdigital-llc/openclaw-template/cli/internal/config"
	"github.com/cruxdigital-llc/openclaw-template/cli/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Configure CruxClaw for first use",
	Long:  "Interactively set up ~/.cruxclaw/config.toml with your deployment details.",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	existing := config.Load()
	missing := make(map[string]bool)
	for _, f := range existing.RequiredFieldsMissing() {
		missing[f] = true
	}
	promptAll := cmd != nil && cmd.Name() == "init"

	fmt.Println("CruxClaw Setup")
	fmt.Println("==============")
	fmt.Println()

	region := existing.Region
	if promptAll || missing["region"] {
		var err error
		region, err = ui.TextPromptWithDefault("AWS region", defaultOrVal(existing.Region, "us-east-2"))
		if err != nil {
			return err
		}
	}

	ssoURL := existing.SSOStartURL
	if promptAll || missing["sso_start_url"] {
		var err error
		ssoURL, err = ui.TextPromptWithDefault("AWS SSO start URL (e.g., https://your-org.awsapps.com/start/)", existing.SSOStartURL)
		if err != nil {
			return err
		}
	}

	accountID := existing.SSOAccountID
	if promptAll || missing["sso_account_id"] {
		var err error
		accountID, err = ui.TextPromptWithDefault("AWS account ID", existing.SSOAccountID)
		if err != nil {
			return err
		}
	}

	roleName := existing.SSORoleName
	if promptAll || missing["sso_role_name"] {
		var err error
		roleName, err = ui.TextPromptWithDefault("SSO role/permission set name", defaultOrVal(existing.SSORoleName, "OpenClawUser"))
		if err != nil {
			return err
		}
	}

	image := existing.OpenClawImage
	if promptAll || missing["openclaw_image"] {
		var err error
		image, err = ui.TextPromptWithDefault("OpenClaw Docker image (must include PR #49514 fix)", existing.OpenClawImage)
		if err != nil {
			return err
		}
	}

	newCfg := &config.Config{
		Region:        region,
		SSOStartURL:   ssoURL,
		SSOAccountID:  accountID,
		SSORoleName:   roleName,
		InstanceTag:   defaultOrVal(existing.InstanceTag, "openclaw-host"),
		OpenClawImage: image,
	}

	if err := newCfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println("Config saved to ~/.cruxclaw/config.toml")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. cruxclaw auth login")
	fmt.Println("  2. aws sso login")
	fmt.Println("  3. cruxclaw auth status")
	return nil
}

func defaultOrVal(existing, fallback string) string {
	if existing != "" {
		return existing
	}
	return fallback
}
