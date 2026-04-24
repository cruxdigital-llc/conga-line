package cmd

import (
	"context"
	"errors"
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

	driftCmd := &cobra.Command{
		Use:   "drift",
		Short: "Detect drift between the policy file and the running egress proxy",
		Long: `Compare the desired egress policy (from the local policy file) against
the policy manifest deployed to each agent's host, and report any drift.

Drift means the running proxy is enforcing a different allowlist or mode
than the policy file says it should. The most common cause is forgetting
to run 'conga policy deploy' after editing the policy — since mode and
domains are baked into the Envoy filter at deploy time, the proxy won't
pick up changes until the next deploy.

Use --agent to check a single agent; omit it to scan all non-paused agents.
Use --output json for structured output.`,
		RunE: policyDriftRun,
	}

	policyCmd.AddCommand(validateCmd)
	policyCmd.AddCommand(proxyLogsCmd)
	policyCmd.AddCommand(driftCmd)
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

// AgentDriftReport is the per-agent drift result returned by conga policy drift.
type AgentDriftReport struct {
	Agent   string              `json:"agent"`
	InSync  bool                `json:"in_sync"`
	Summary string              `json:"summary"`
	Drift   []policy.DriftEntry `json:"drift,omitempty"`
	Error   string              `json:"error,omitempty"` // populated when the agent couldn't be inspected
}

func policyDriftRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	if prov == nil {
		return fmt.Errorf("no provider configured; use --provider or run conga admin setup")
	}

	// Load the desired policy once so per-agent merging is cheap.
	path, err := defaultPolicyPath()
	if err != nil {
		return err
	}
	pf, err := policy.Load(path)
	if err != nil {
		return fmt.Errorf("failed to load policy: %w", err)
	}
	if pf == nil {
		return fmt.Errorf("no policy file at %s — nothing to compare against", path)
	}
	if err := pf.Validate(); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	// Build the list of agents to check.
	var targets []string
	if flagAgent != "" {
		targets = []string{flagAgent}
	} else {
		agents, err := prov.ListAgents(ctx)
		if err != nil {
			return fmt.Errorf("listing agents: %w", err)
		}
		for _, a := range agents {
			if !a.Paused {
				targets = append(targets, a.Name)
			}
		}
	}
	if len(targets) == 0 {
		if ui.OutputJSON {
			ui.EmitJSON([]AgentDriftReport{})
			return nil
		}
		fmt.Println("No active agents to check.")
		return nil
	}

	reports := make([]AgentDriftReport, 0, len(targets))
	for _, name := range targets {
		reports = append(reports, driftReportForAgent(ctx, pf, name))
	}

	if ui.OutputJSON {
		ui.EmitJSON(reports)
		return nil
	}
	return renderDriftTable(reports)
}

// driftReportForAgent computes the drift report for a single agent, wrapping
// lookups in a structured report so the caller can render a full table even
// when individual agents fail (e.g. manifest not deployed yet).
func driftReportForAgent(ctx context.Context, pf *policy.PolicyFile, agentName string) AgentDriftReport {
	report := AgentDriftReport{Agent: agentName}

	merged := pf.MergeForAgent(agentName)
	desired := policy.BuildManifest(merged.Egress)

	raw, err := prov.ReadProxyManifest(ctx, agentName)
	if err != nil {
		if errors.Is(err, provpkg.ErrNotFound) {
			// No manifest on host — definitely drifted (or never deployed).
			report.InSync = false
			report.Drift = policy.DiffManifests(desired, nil)
			report.Summary = "not deployed"
			return report
		}
		report.InSync = false
		report.Summary = "error"
		report.Error = err.Error()
		return report
	}

	actual, parseErr := policy.ParseManifest(raw)
	if parseErr != nil {
		report.InSync = false
		report.Summary = "malformed manifest"
		report.Error = parseErr.Error()
		return report
	}

	report.Drift = policy.DiffManifests(desired, actual)
	report.InSync = len(report.Drift) == 0
	report.Summary = policy.Summary(report.Drift)
	return report
}

// renderDriftTable writes a human-readable table of drift reports to stdout.
// One row per report; drifted agents get a second indented block listing
// per-field drift entries for quick scanning.
func renderDriftTable(reports []AgentDriftReport) error {
	headers := []string{"AGENT", "STATUS", "SUMMARY"}
	var rows [][]string
	for _, r := range reports {
		status := "in-sync"
		switch {
		case r.Error != "":
			status = "ERROR"
		case !r.InSync:
			status = "DRIFTED"
		}
		rows = append(rows, []string{r.Agent, status, r.Summary})
	}
	ui.PrintTable(headers, rows)

	// Detail blocks for drifted agents, rendered below the table.
	for _, r := range reports {
		if r.InSync && r.Error == "" {
			continue
		}
		fmt.Println()
		fmt.Printf("Agent %s:\n", r.Agent)
		if r.Error != "" {
			fmt.Printf("  error: %s\n", r.Error)
			continue
		}
		for _, e := range r.Drift {
			switch e.Kind {
			case policy.DriftMismatch:
				fmt.Printf("  %s: desired=%q, actual=%q\n", e.Field, e.Desired, e.Actual)
			case policy.DriftMissingOnHost:
				fmt.Printf("  %s: missing on host: %s\n", e.Field, e.Desired)
			case policy.DriftExtraOnHost:
				fmt.Printf("  %s: extra on host: %s\n", e.Field, e.Actual)
			}
		}
	}

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
