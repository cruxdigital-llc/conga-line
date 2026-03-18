package discovery

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	awsutil "github.com/cruxdigital-llc/openclaw-template/cli/internal/aws"
)

type ResolvedIdentity struct {
	ARN         string
	AccountID   string
	SessionName string
	MemberID    string
}

func ResolveIdentity(ctx context.Context, stsClient *sts.Client, ssmClient *ssm.Client) (*ResolvedIdentity, error) {
	out, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to get caller identity (is your session valid?): %w", err)
	}

	arn := aws.ToString(out.Arn)
	accountID := aws.ToString(out.Account)

	// Extract session name from ARN
	// Format: arn:aws:sts::123456789012:assumed-role/RoleName/session-name
	sessionName := ""
	parts := strings.Split(arn, "/")
	if len(parts) >= 3 {
		sessionName = parts[len(parts)-1]
	}

	identity := &ResolvedIdentity{
		ARN:         arn,
		AccountID:   accountID,
		SessionName: sessionName,
	}

	// Try to resolve member ID from SSM parameter
	if sessionName != "" {
		paramName := fmt.Sprintf("/openclaw/users/by-iam/%s", sessionName)
		memberID, err := awsutil.GetParameter(ctx, ssmClient, paramName)
		if err == nil {
			identity.MemberID = strings.TrimSpace(memberID)
		}
	}

	return identity, nil
}
