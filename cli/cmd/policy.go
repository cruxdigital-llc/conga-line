package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/cruxdigital-llc/conga-line/cli/internal/policy"
	provpkg "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/spf13/cobra"
)

var policyFilePath string

func init() {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage deployment policy",
	}

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the policy file and show enforcement report",
		Long: `Validate the conga-policy.yaml file and display which rules each
provider can enforce. The report shows enforcement levels:

  enforced       — Provider fully enforces this rule
  partial        — Provider partially enforces (best-effort)
  validate-only  — Provider validates but does not enforce
  not-applicable — Rule does not apply to this provider`,
		RunE: policyValidateRun,
	}
	validateCmd.Flags().StringVar(&policyFilePath, "file", "", "Path to policy file (default: auto-detect from provider)")

	policyCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(policyCmd)
}

func defaultPolicyPath() string {
	cfg, _ := provpkg.LoadConfig(provpkg.DefaultConfigPath())
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = provpkg.DefaultDataDir()
	}
	return filepath.Join(dataDir, "conga-policy.yaml")
}

func policyValidateRun(cmd *cobra.Command, args []string) error {
	path := policyFilePath
	if path == "" {
		path = defaultPolicyPath()
	}

	pf, err := policy.Load(path)
	if err != nil {
		return fmt.Errorf("failed to load policy: %w", err)
	}

	if pf == nil {
		if ui.OutputJSON {
			ui.EmitJSON(struct {
				Status  string `json:"status"`
				Message string `json:"message"`
				Path    string `json:"path"`
			}{
				Status:  "no_policy",
				Message: "No policy file found",
				Path:    path,
			})
			return nil
		}
		fmt.Printf("No policy file found at %s\n", path)
		fmt.Println("Create one from conga-policy.yaml.example to define your deployment policy.")
		return nil
	}

	if err := pf.Validate(); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	providerName := prov.Name()

	agentName := flagAgent
	effectivePolicy := pf
	if agentName != "" {
		effectivePolicy = pf.MergeForAgent(agentName)
	}

	reports := effectivePolicy.EnforcementReport(providerName)

	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Status   string              `json:"status"`
			Path     string              `json:"path"`
			Provider string              `json:"provider"`
			Agent    string              `json:"agent,omitempty"`
			Rules    []policy.RuleReport `json:"rules"`
		}{
			Status:   "valid",
			Path:     path,
			Provider: providerName,
			Agent:    agentName,
			Rules:    reports,
		})
		return nil
	}

	fmt.Printf("Policy: %s\n", path)
	fmt.Printf("Provider: %s\n", providerName)
	if agentName != "" {
		fmt.Printf("Agent: %s (merged)\n", agentName)
	}
	fmt.Println()

	if len(reports) == 0 {
		fmt.Println("No policy rules defined.")
		return nil
	}

	headers := []string{"SECTION", "RULE", "LEVEL", "DETAIL"}
	var rows [][]string
	for _, r := range reports {
		rows = append(rows, []string{r.Section, r.Rule, string(r.Level), r.Detail})
	}
	ui.PrintTable(headers, rows)

	return nil
}
