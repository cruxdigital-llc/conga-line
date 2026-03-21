package cmd

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
	"time"

	awsutil "github.com/cruxdigital-llc/conga-line/cli/internal/aws"
	"github.com/cruxdigital-llc/conga-line/cli/internal/discovery"
	"github.com/cruxdigital-llc/conga-line/cli/internal/ui"
	"github.com/cruxdigital-llc/conga-line/cli/scripts"
	"github.com/spf13/cobra"
)

func adminRemoveAgentRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()
	if err := ensureClients(ctx); err != nil {
		return err
	}

	agentName := args[0]

	// Look up the agent to confirm it exists and get its type
	agent, err := discovery.ResolveAgent(ctx, clients.SSM, agentName)
	if err != nil {
		return err
	}

	if !adminForce {
		if !ui.Confirm(fmt.Sprintf("Remove agent %s (type: %s)? This will stop the container and delete config.", agentName, agent.Type)) {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	instanceID, err := findInstance(ctx)
	if err != nil {
		return err
	}

	// Run remove script on instance
	tmpl, err := template.New("removeagent").Parse(scripts.RemoveAgentScript)
	if err != nil {
		return fmt.Errorf("failed to parse remove-agent template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, struct{ ContainerID string }{ContainerID: agentName})
	if err != nil {
		return fmt.Errorf("failed to render remove-agent script: %w", err)
	}

	spin := ui.NewSpinner(fmt.Sprintf("Removing agent %s...", agentName))
	result, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, buf.String(), 60*time.Second)
	spin.Stop()
	if err != nil {
		return err
	}

	var cleanupErrs []string

	if result.Status != "Success" {
		cleanupErrs = append(cleanupErrs, fmt.Sprintf("instance cleanup: %s", result.Stderr))
	}

	// Delete SSM parameter
	if err := awsutil.DeleteParameter(ctx, clients.SSM, fmt.Sprintf("/conga/agents/%s", agentName)); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Sprintf("SSM parameter: %v", err))
	}

	// Delete secrets if requested
	if adminDeleteSecrets {
		secretPrefix := fmt.Sprintf("conga/agents/%s/", agentName)
		secrets, err := awsutil.ListSecrets(ctx, clients.SecretsManager, secretPrefix)
		if err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Sprintf("list secrets: %v", err))
		} else {
			for _, s := range secrets {
				if err := awsutil.DeleteSecret(ctx, clients.SecretsManager, fmt.Sprintf("conga/agents/%s/%s", agentName, s.Name)); err != nil {
					cleanupErrs = append(cleanupErrs, fmt.Sprintf("delete secret %s: %v", s.Name, err))
				}
			}
		}
	}

	// Update CloudWatch dashboard to reflect removal
	if _, err := awsutil.RunCommand(ctx, clients.SSM, instanceID, "/opt/conga/bin/update-dashboard.sh", 30*time.Second); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Sprintf("dashboard update: %v", err))
	}

	if len(cleanupErrs) > 0 {
		fmt.Fprintf(os.Stderr, "Agent %s removed, but %d cleanup operation(s) failed:\n", agentName, len(cleanupErrs))
		for _, e := range cleanupErrs {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		return fmt.Errorf("agent removed but %d cleanup step(s) failed", len(cleanupErrs))
	}

	fmt.Printf("Agent %s removed.\n", agentName)
	return nil
}
