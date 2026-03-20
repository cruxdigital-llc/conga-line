package aws

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// mockSSMClient is a minimal mock for testing RunCommand.
type mockSSMClient struct {
	sendCommandFn         func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error)
	getCommandInvFn       func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error)
	getParameterFn        func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
	putParameterFn        func(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error)
	deleteParameterFn     func(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error)
	getParametersByPathFn func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error)
	startSessionFn        func(ctx context.Context, params *ssm.StartSessionInput, optFns ...func(*ssm.Options)) (*ssm.StartSessionOutput, error)
}

func (m *mockSSMClient) SendCommand(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
	return m.sendCommandFn(ctx, params, optFns...)
}
func (m *mockSSMClient) GetCommandInvocation(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
	return m.getCommandInvFn(ctx, params, optFns...)
}
func (m *mockSSMClient) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	if m.getParameterFn != nil {
		return m.getParameterFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	if m.putParameterFn != nil {
		return m.putParameterFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) DeleteParameter(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
	if m.deleteParameterFn != nil {
		return m.deleteParameterFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	if m.getParametersByPathFn != nil {
		return m.getParametersByPathFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSSMClient) StartSession(ctx context.Context, params *ssm.StartSessionInput, optFns ...func(*ssm.Options)) (*ssm.StartSessionOutput, error) {
	if m.startSessionFn != nil {
		return m.startSessionFn(ctx, params, optFns...)
	}
	return nil, fmt.Errorf("not implemented")
}

func TestRunCommand_Success(t *testing.T) {
	callCount := 0
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			callCount++
			if callCount < 2 {
				return &ssm.GetCommandInvocationOutput{
					Status: ssmtypes.CommandInvocationStatusInProgress,
				}, nil
			}
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("hello world"),
				StandardErrorContent:  aws.String(""),
			}, nil
		},
	}

	result, err := RunCommand(context.Background(), mock, "i-12345", "echo hello", 30*time.Second)
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if result.Status != "Success" {
		t.Errorf("expected Success, got %s", result.Status)
	}
	if result.Stdout != "hello world" {
		t.Errorf("expected 'hello world', got %q", result.Stdout)
	}
}

func TestRunCommand_Failed(t *testing.T) {
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusFailed,
				StandardOutputContent: aws.String(""),
				StandardErrorContent:  aws.String("command not found"),
			}, nil
		},
	}

	result, err := RunCommand(context.Background(), mock, "i-12345", "bad-cmd", 30*time.Second)
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if result.Status != "Failed" {
		t.Errorf("expected Failed, got %s", result.Status)
	}
	if result.Stderr != "command not found" {
		t.Errorf("expected 'command not found', got %q", result.Stderr)
	}
}

func TestRunCommand_Timeout(t *testing.T) {
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			// Always return InProgress
			return &ssm.GetCommandInvocationOutput{
				Status: ssmtypes.CommandInvocationStatusInProgress,
			}, nil
		},
	}

	// Use a very short timeout
	_, err := RunCommand(context.Background(), mock, "i-12345", "sleep 999", 4*time.Second)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if err.Error() != "command timed out after 4s" {
		t.Errorf("expected timeout error message, got: %v", err)
	}
}

func TestRunCommand_ConsecutiveErrors_Recovery(t *testing.T) {
	errorCount := 0
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			errorCount++
			if errorCount <= 5 {
				return nil, fmt.Errorf("transient error")
			}
			return &ssm.GetCommandInvocationOutput{
				Status:                ssmtypes.CommandInvocationStatusSuccess,
				StandardOutputContent: aws.String("recovered"),
				StandardErrorContent:  aws.String(""),
			}, nil
		},
	}

	result, err := RunCommand(context.Background(), mock, "i-12345", "echo test", 60*time.Second)
	if err != nil {
		t.Fatalf("RunCommand returned error: %v", err)
	}
	if result.Stdout != "recovered" {
		t.Errorf("expected 'recovered', got %q", result.Stdout)
	}
}

func TestRunCommand_ConsecutiveErrors_Exceeded(t *testing.T) {
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return &ssm.SendCommandOutput{
				Command: &ssmtypes.Command{CommandId: aws.String("cmd-123")},
			}, nil
		},
		getCommandInvFn: func(ctx context.Context, params *ssm.GetCommandInvocationInput, optFns ...func(*ssm.Options)) (*ssm.GetCommandInvocationOutput, error) {
			return nil, fmt.Errorf("persistent error")
		},
	}

	_, err := RunCommand(context.Background(), mock, "i-12345", "echo test", 60*time.Second)
	if err == nil {
		t.Fatal("expected error after consecutive failures, got nil")
	}
}

func TestGetParameter_Success(t *testing.T) {
	mock := &mockSSMClient{
		getParameterFn: func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			return &ssm.GetParameterOutput{
				Parameter: &ssmtypes.Parameter{Value: aws.String("test-value")},
			}, nil
		},
	}

	val, err := GetParameter(context.Background(), mock, "/test/param")
	if err != nil {
		t.Fatalf("GetParameter returned error: %v", err)
	}
	if val != "test-value" {
		t.Errorf("expected %q, got %q", "test-value", val)
	}
}

func TestGetParameter_NotFound(t *testing.T) {
	mock := &mockSSMClient{
		getParameterFn: func(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
			return nil, fmt.Errorf("ParameterNotFound")
		},
	}

	_, err := GetParameter(context.Background(), mock, "/test/missing")
	if err == nil {
		t.Fatal("expected error from GetParameter")
	}
	if !strings.Contains(err.Error(), "parameter") {
		t.Errorf("expected wrapped error with parameter context, got: %v", err)
	}
}

func TestPutParameter_WrapsError(t *testing.T) {
	mock := &mockSSMClient{
		putParameterFn: func(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	err := PutParameter(context.Background(), mock, "/test/param", "value")
	if err == nil {
		t.Fatal("expected error from PutParameter")
	}
	expected := "failed to put parameter /test/param: access denied"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestDeleteParameter_WrapsError(t *testing.T) {
	mock := &mockSSMClient{
		deleteParameterFn: func(ctx context.Context, params *ssm.DeleteParameterInput, optFns ...func(*ssm.Options)) (*ssm.DeleteParameterOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	err := DeleteParameter(context.Background(), mock, "/test/param")
	if err == nil {
		t.Fatal("expected error from DeleteParameter")
	}
	expected := "failed to delete parameter /test/param: access denied"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestGetParametersByPath_Pagination(t *testing.T) {
	callCount := 0
	mock := &mockSSMClient{
		getParametersByPathFn: func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
			callCount++
			if callCount == 1 {
				return &ssm.GetParametersByPathOutput{
					Parameters: []ssmtypes.Parameter{
						{Name: aws.String("/openclaw/agents/myagent"), Value: aws.String(`{"type":"user"}`)},
					},
					NextToken: aws.String("page2"),
				}, nil
			}
			return &ssm.GetParametersByPathOutput{
				Parameters: []ssmtypes.Parameter{
					{Name: aws.String("/openclaw/agents/leadership"), Value: aws.String(`{"type":"team"}`)},
				},
			}, nil
		},
	}

	entries, err := GetParametersByPath(context.Background(), mock, "/openclaw/agents/")
	if err != nil {
		t.Fatalf("GetParametersByPath returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestGetParametersByPath_FiltersByIAM(t *testing.T) {
	mock := &mockSSMClient{
		getParametersByPathFn: func(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
			return &ssm.GetParametersByPathOutput{
				Parameters: []ssmtypes.Parameter{
					{Name: aws.String("/openclaw/agents/myagent"), Value: aws.String(`{"type":"user"}`)},
					{Name: aws.String("/openclaw/agents/by-iam/user@example.com"), Value: aws.String("myagent")},
				},
			}, nil
		},
	}

	entries, err := GetParametersByPath(context.Background(), mock, "/openclaw/agents/")
	if err != nil {
		t.Fatalf("GetParametersByPath returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (by-iam filtered), got %d", len(entries))
	}
	if entries[0].Name != "/openclaw/agents/myagent" {
		t.Errorf("expected agent entry, got %q", entries[0].Name)
	}
}

func TestRunCommand_SendCommandFailure(t *testing.T) {
	mock := &mockSSMClient{
		sendCommandFn: func(ctx context.Context, params *ssm.SendCommandInput, optFns ...func(*ssm.Options)) (*ssm.SendCommandOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	_, err := RunCommand(context.Background(), mock, "i-12345", "echo test", 30*time.Second)
	if err == nil {
		t.Fatal("expected error from SendCommand, got nil")
	}
}
