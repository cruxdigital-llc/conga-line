package discovery

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type mockSTSClient struct {
	getCallerIdentityFn func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

func (m *mockSTSClient) GetCallerIdentity(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return m.getCallerIdentityFn(ctx, params, optFns...)
}

type mockSSMClient struct {
	getParametersByPathFn func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
}

func (m *mockSSMClient) SendCommand(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) GetCommandInvocation(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) DeleteParameter(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	if m.getParametersByPathFn != nil {
		return m.getParametersByPathFn(ctx, params, optFns...)
	}
	return &ssm.GetParametersByPathOutput{}, nil
}
func (m *mockSSMClient) StartSession(ctx context.Context, params *ssm.StartSessionInput, optFns ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestResolveIdentity_AssumedRoleWithSession(t *testing.T) {
	stsMock := &mockSTSClient{
		getCallerIdentityFn: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{
				Arn:     aws.String("arn:aws:sts::123456789012:assumed-role/OpenClawUser/user@example.com"),
				Account: aws.String("123456789012"),
			}, nil
		},
	}
	ssmMock := &mockSSMClient{
		getParametersByPathFn: func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
			return &ssm.GetParametersByPathOutput{
				Parameters: []ssmtypes.Parameter{
					{
						Name:  aws.String("/openclaw/agents/myagent"),
						Value: aws.String(`{"type":"user","slack_member_id":"U0123456789","gateway_port":18789,"iam_identity":"user@example.com"}`),
					},
				},
			}, nil
		},
	}

	identity, err := ResolveIdentity(context.Background(), stsMock, ssmMock)
	if err != nil {
		t.Fatalf("ResolveIdentity returned error: %v", err)
	}
	if identity.SessionName != "user@example.com" {
		t.Errorf("SessionName = %q, want %q", identity.SessionName, "user@example.com")
	}
	if identity.AccountID != "123456789012" {
		t.Errorf("AccountID = %q, want %q", identity.AccountID, "123456789012")
	}
	if identity.AgentName != "myagent" {
		t.Errorf("AgentName = %q, want %q", identity.AgentName, "myagent")
	}
}

func TestResolveIdentity_IAMUser(t *testing.T) {
	stsMock := &mockSTSClient{
		getCallerIdentityFn: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{
				Arn:     aws.String("arn:aws:iam::123456789012:user/admin"),
				Account: aws.String("123456789012"),
			}, nil
		},
	}
	ssmMock := &mockSSMClient{}

	identity, err := ResolveIdentity(context.Background(), stsMock, ssmMock)
	if err != nil {
		t.Fatalf("ResolveIdentity returned error: %v", err)
	}
	// IAM user ARN has only 2 slash-separated parts, so no session name
	if identity.SessionName != "" {
		t.Errorf("SessionName = %q, want empty", identity.SessionName)
	}
	if identity.AgentName != "" {
		t.Errorf("AgentName = %q, want empty", identity.AgentName)
	}
}

func TestResolveIdentity_RootAccount(t *testing.T) {
	stsMock := &mockSTSClient{
		getCallerIdentityFn: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{
				Arn:     aws.String("arn:aws:iam::123456789012:root"),
				Account: aws.String("123456789012"),
			}, nil
		},
	}
	ssmMock := &mockSSMClient{}

	identity, err := ResolveIdentity(context.Background(), stsMock, ssmMock)
	if err != nil {
		t.Fatalf("ResolveIdentity returned error: %v", err)
	}
	if identity.SessionName != "" {
		t.Errorf("SessionName = %q, want empty", identity.SessionName)
	}
}

func TestResolveIdentity_NoMatchingAgent(t *testing.T) {
	stsMock := &mockSTSClient{
		getCallerIdentityFn: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{
				Arn:     aws.String("arn:aws:sts::123456789012:assumed-role/OpenClawUser/unknown@example.com"),
				Account: aws.String("123456789012"),
			}, nil
		},
	}
	ssmMock := &mockSSMClient{
		getParametersByPathFn: func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
			return &ssm.GetParametersByPathOutput{}, nil
		},
	}

	identity, err := ResolveIdentity(context.Background(), stsMock, ssmMock)
	if err != nil {
		t.Fatalf("ResolveIdentity returned error: %v", err)
	}
	if identity.SessionName != "unknown@example.com" {
		t.Errorf("SessionName = %q, want %q", identity.SessionName, "unknown@example.com")
	}
	if identity.AgentName != "" {
		t.Errorf("AgentName = %q, want empty", identity.AgentName)
	}
}

func TestResolveIdentity_STSError(t *testing.T) {
	stsMock := &mockSTSClient{
		getCallerIdentityFn: func(ctx context.Context, params *sts.GetCallerIdentityInput, optFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
			return nil, fmt.Errorf("expired token")
		},
	}
	ssmMock := &mockSSMClient{}

	_, err := ResolveIdentity(context.Background(), stsMock, ssmMock)
	if err == nil {
		t.Fatal("expected error from ResolveIdentity when STS fails")
	}
}
