package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/policy"
	provpkg "github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
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

	proxyLogsCmd := &cobra.Command{
		Use:   "proxy-logs",
		Short: "Tail the egress proxy logs for an agent",
		Long: `Show the egress proxy container logs for an agent.

In validate mode, would-be-denied requests appear in the proxy application
log as "egress-validate: would deny <host>". In enforce mode, blocked
requests receive a 403 from the Lua filter (visible to the requesting
client but not logged by the proxy).`,
		RunE: policyProxyLogsRun,
	}
	proxyLogsCmd.Flags().IntVarP(&logLines, "lines", "n", 50, "Number of log lines")

	policyCmd.AddCommand(validateCmd)
	policyCmd.AddCommand(proxyLogsCmd)
	rootCmd.AddCommand(policyCmd)
}

func defaultPolicyPath() (string, error) {
	dataDir := flagDataDir
	if dataDir == "" {
		cfg, err := provpkg.LoadConfig(provpkg.ConfigPathForDataDir(flagDataDir))
		if err != nil {
			return "", fmt.Errorf("failed to load config: %w", err)
		}
		dataDir = cfg.DataDir
	}
	if dataDir == "" {
		dataDir = provpkg.DefaultDataDir()
	}
	return filepath.Join(dataDir, "conga-policy.yaml"), nil
}

func policyValidateRun(cmd *cobra.Command, args []string) error {
	path := policyFilePath
	if path == "" {
		var err error
		path, err = defaultPolicyPath()
		if err != nil {
			return err
		}
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

	if prov == nil {
		return fmt.Errorf("no provider configured; use --provider or run conga admin setup")
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

func policyProxyLogsRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	output, err := prov.GetLogs(ctx, "egress-"+agentName, logLines)
	if err != nil {
		return err
	}

	if ui.OutputJSON {
		lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = []string{}
		}
		ui.EmitJSON(struct {
			Agent string   `json:"agent"`
			Proxy string   `json:"proxy"`
			Lines []string `json:"lines"`
		}{
			Agent: agentName,
			Proxy: "conga-egress-" + agentName,
			Lines: lines,
		})
		return nil
	}

	fmt.Print(output)
	return nil
}
