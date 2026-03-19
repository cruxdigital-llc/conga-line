package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
)

type AgentConfig struct {
	Name          string
	Type          string `json:"type"`
	SlackMemberID string `json:"slack_member_id,omitempty"`
	SlackChannel  string `json:"slack_channel,omitempty"`
	GatewayPort   int    `json:"gateway_port"`
	IAMIdentity   string `json:"iam_identity,omitempty"`
}

func ResolveAgent(ctx context.Context, ssmClient *ssm.Client, name string) (*AgentConfig, error) {
	paramName := fmt.Sprintf("/openclaw/agents/%s", name)
	value, err := awsutil.GetParameter(ctx, ssmClient, paramName)
	if err != nil {
		return nil, fmt.Errorf("agent %q not found. Use `cruxclaw admin add-user` or `add-team` to provision", name)
	}

	var cfg AgentConfig
	if err := json.Unmarshal([]byte(value), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse agent config for %q: %w", name, err)
	}
	cfg.Name = name
	return &cfg, nil
}

func ResolveAgentByIAM(ctx context.Context, ssmClient *ssm.Client, iamIdentity string) (*AgentConfig, error) {
	entries, err := awsutil.GetParametersByPath(ctx, ssmClient, "/openclaw/agents/")
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}

	for _, e := range entries {
		var cfg AgentConfig
		if json.Unmarshal([]byte(e.Value), &cfg) != nil {
			continue
		}
		if cfg.IAMIdentity != "" && cfg.IAMIdentity == iamIdentity {
			parts := strings.Split(e.Name, "/")
			cfg.Name = parts[len(parts)-1]
			return &cfg, nil
		}
	}

	return nil, fmt.Errorf("no agent found with iam_identity %q", iamIdentity)
}

func ListAgents(ctx context.Context, ssmClient *ssm.Client) ([]AgentConfig, error) {
	entries, err := awsutil.GetParametersByPath(ctx, ssmClient, "/openclaw/agents/")
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}

	var agents []AgentConfig
	for _, e := range entries {
		var cfg AgentConfig
		if json.Unmarshal([]byte(e.Value), &cfg) != nil {
			continue
		}
		parts := strings.Split(e.Name, "/")
		cfg.Name = parts[len(parts)-1]
		agents = append(agents, cfg)
	}
	return agents, nil
}
