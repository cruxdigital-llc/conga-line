package discovery

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
)

type UserConfig struct {
	MemberID    string
	AgentName   string `json:"agent_name"`
	GatewayPort int    `json:"gateway_port"`
}

type TeamConfig struct {
	TeamName     string
	SlackChannel string `json:"slack_channel"`
	GatewayPort  int    `json:"gateway_port"`
}

func ResolveUser(ctx context.Context, ssmClient *ssm.Client, memberID string) (*UserConfig, error) {
	paramName := fmt.Sprintf("/openclaw/users/%s", memberID)
	value, err := awsutil.GetParameter(ctx, ssmClient, paramName)
	if err != nil {
		return nil, fmt.Errorf("user %s not found. Ask admin to run `cruxclaw admin add-user`", memberID)
	}

	var cfg UserConfig
	if err := json.Unmarshal([]byte(value), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse user config for %s: %w", memberID, err)
	}
	cfg.MemberID = memberID
	return &cfg, nil
}

func ResolveTeam(ctx context.Context, ssmClient *ssm.Client, teamName string) (*TeamConfig, error) {
	paramName := fmt.Sprintf("/openclaw/teams/%s", teamName)
	value, err := awsutil.GetParameter(ctx, ssmClient, paramName)
	if err != nil {
		return nil, fmt.Errorf("team %s not found. Ask admin to run `cruxclaw admin add-team`", teamName)
	}

	var cfg TeamConfig
	if err := json.Unmarshal([]byte(value), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse team config for %s: %w", teamName, err)
	}
	cfg.TeamName = teamName
	return &cfg, nil
}
