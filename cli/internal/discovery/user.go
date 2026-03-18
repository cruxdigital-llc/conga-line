package discovery

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
)

type UserConfig struct {
	MemberID     string
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
