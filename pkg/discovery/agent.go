package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	awsutil "github.com/cruxdigital-llc/conga-line/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// parseAgentConfig parses an agent config from its SSM parameter name and JSON value.
// The agent name is derived from the last segment of the parameter path.
func parseAgentConfig(paramName, jsonValue string) (*provider.AgentConfig, error) {
	var cfg provider.AgentConfig
	if err := json.Unmarshal([]byte(jsonValue), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse agent config for %q: %w", paramName, err)
	}
	parts := strings.Split(paramName, "/")
	cfg.Name = parts[len(parts)-1]
	return &cfg, nil
}

func ResolveAgent(ctx context.Context, ssmClient awsutil.SSMClient, name string) (*provider.AgentConfig, error) {
	paramName := fmt.Sprintf("/conga/agents/%s", name)
	value, err := awsutil.GetParameter(ctx, ssmClient, paramName)
	if err != nil {
		return nil, fmt.Errorf("agent %q not found: %w", name, provider.ErrNotFound)
	}

	cfg, err := parseAgentConfig(paramName, value)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func ResolveAgentByIAM(ctx context.Context, ssmClient awsutil.SSMClient, iamIdentity string) (*provider.AgentConfig, error) {
	entries, err := awsutil.GetParametersByPath(ctx, ssmClient, "/conga/agents/")
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}

	for _, e := range entries {
		cfg, err := parseAgentConfig(e.Name, e.Value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping agent parameter %s: %v\n", e.Name, err)
			continue
		}
		if cfg.IAMIdentity != "" && cfg.IAMIdentity == iamIdentity {
			return cfg, nil
		}
	}

	return nil, fmt.Errorf("no agent found with iam_identity %q", iamIdentity)
}

func ListAgents(ctx context.Context, ssmClient awsutil.SSMClient) ([]provider.AgentConfig, error) {
	entries, err := awsutil.GetParametersByPath(ctx, ssmClient, "/conga/agents/")
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}

	var agents []provider.AgentConfig
	for _, e := range entries {
		cfg, err := parseAgentConfig(e.Name, e.Value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping agent parameter %s: %v\n", e.Name, err)
			continue
		}
		agents = append(agents, *cfg)
	}
	return agents, nil
}
