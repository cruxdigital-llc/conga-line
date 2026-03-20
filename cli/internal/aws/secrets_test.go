package aws

import (
	"context"
	"fmt"
	"testing"
	"time"

	awslib "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

type mockSecretsClient struct {
	listSecretsFn    func(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error)
	createSecretFn   func(ctx context.Context, params *secretsmanager.CreateSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error)
	putSecretValueFn func(ctx context.Context, params *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
	getSecretValueFn func(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	deleteSecretFn   func(ctx context.Context, params *secretsmanager.DeleteSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error)
}

func (m *mockSecretsClient) ListSecrets(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	return m.listSecretsFn(ctx, params, optFns...)
}
func (m *mockSecretsClient) CreateSecret(ctx context.Context, params *secretsmanager.CreateSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error) {
	return m.createSecretFn(ctx, params, optFns...)
}
func (m *mockSecretsClient) PutSecretValue(ctx context.Context, params *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	return m.putSecretValueFn(ctx, params, optFns...)
}
func (m *mockSecretsClient) GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	return m.getSecretValueFn(ctx, params, optFns...)
}
func (m *mockSecretsClient) DeleteSecret(ctx context.Context, params *secretsmanager.DeleteSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error) {
	return m.deleteSecretFn(ctx, params, optFns...)
}

func TestSetSecret_UpdateExisting(t *testing.T) {
	mock := &mockSecretsClient{
		putSecretValueFn: func(ctx context.Context, params *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
			return &secretsmanager.PutSecretValueOutput{}, nil
		},
	}

	err := SetSecret(context.Background(), mock, "test/secret", "value123")
	if err != nil {
		t.Fatalf("SetSecret returned error: %v", err)
	}
}

func TestSetSecret_CreateNew(t *testing.T) {
	createCalled := false
	mock := &mockSecretsClient{
		putSecretValueFn: func(ctx context.Context, params *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
			return nil, &smtypes.ResourceNotFoundException{Message: awslib.String("not found")}
		},
		createSecretFn: func(ctx context.Context, params *secretsmanager.CreateSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error) {
			createCalled = true
			if awslib.ToString(params.Name) != "test/secret" {
				t.Errorf("expected name 'test/secret', got %q", awslib.ToString(params.Name))
			}
			return &secretsmanager.CreateSecretOutput{}, nil
		},
	}

	err := SetSecret(context.Background(), mock, "test/secret", "value123")
	if err != nil {
		t.Fatalf("SetSecret returned error: %v", err)
	}
	if !createCalled {
		t.Error("expected CreateSecret to be called")
	}
}

func TestSetSecret_BothFail(t *testing.T) {
	mock := &mockSecretsClient{
		putSecretValueFn: func(ctx context.Context, params *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
			return nil, &smtypes.ResourceNotFoundException{Message: awslib.String("not found")}
		},
		createSecretFn: func(ctx context.Context, params *secretsmanager.CreateSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error) {
			return nil, fmt.Errorf("create failed")
		},
	}

	err := SetSecret(context.Background(), mock, "test/secret", "value123")
	if err == nil {
		t.Fatal("expected error when both PutSecretValue and CreateSecret fail")
	}
}

func TestListSecrets_SinglePage(t *testing.T) {
	now := time.Now()
	mock := &mockSecretsClient{
		listSecretsFn: func(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
			return &secretsmanager.ListSecretsOutput{
				SecretList: []smtypes.SecretListEntry{
					{Name: awslib.String("prefix/api-key"), LastChangedDate: &now},
					{Name: awslib.String("prefix/token"), LastChangedDate: &now},
				},
			}, nil
		},
	}

	entries, err := ListSecrets(context.Background(), mock, "prefix/")
	if err != nil {
		t.Fatalf("ListSecrets returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "api-key" {
		t.Errorf("expected name 'api-key', got %q", entries[0].Name)
	}
	if entries[1].Name != "token" {
		t.Errorf("expected name 'token', got %q", entries[1].Name)
	}
}

func TestListSecrets_MultiPage(t *testing.T) {
	callCount := 0
	mock := &mockSecretsClient{
		listSecretsFn: func(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
			callCount++
			if callCount == 1 {
				return &secretsmanager.ListSecretsOutput{
					SecretList: []smtypes.SecretListEntry{
						{Name: awslib.String("prefix/first")},
					},
					NextToken: awslib.String("page2"),
				}, nil
			}
			return &secretsmanager.ListSecretsOutput{
				SecretList: []smtypes.SecretListEntry{
					{Name: awslib.String("prefix/second")},
				},
			}, nil
		},
	}

	entries, err := ListSecrets(context.Background(), mock, "prefix/")
	if err != nil {
		t.Fatalf("ListSecrets returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestListSecrets_Empty(t *testing.T) {
	mock := &mockSecretsClient{
		listSecretsFn: func(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
			return &secretsmanager.ListSecretsOutput{}, nil
		},
	}

	entries, err := ListSecrets(context.Background(), mock, "prefix/")
	if err != nil {
		t.Fatalf("ListSecrets returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestGetSecretValue_Success(t *testing.T) {
	mock := &mockSecretsClient{
		getSecretValueFn: func(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
			return &secretsmanager.GetSecretValueOutput{
				SecretString: awslib.String("my-secret-value"),
			}, nil
		},
	}

	val, err := GetSecretValue(context.Background(), mock, "test/secret")
	if err != nil {
		t.Fatalf("GetSecretValue returned error: %v", err)
	}
	if val != "my-secret-value" {
		t.Errorf("expected %q, got %q", "my-secret-value", val)
	}
}

func TestGetSecretValue_NotFound(t *testing.T) {
	mock := &mockSecretsClient{
		getSecretValueFn: func(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
			return nil, &smtypes.ResourceNotFoundException{Message: awslib.String("not found")}
		},
	}

	val, err := GetSecretValue(context.Background(), mock, "test/secret")
	if err != nil {
		t.Fatalf("GetSecretValue returned error for not-found: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for not-found, got %q", val)
	}
}

func TestGetSecretValue_OtherError(t *testing.T) {
	mock := &mockSecretsClient{
		getSecretValueFn: func(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	_, err := GetSecretValue(context.Background(), mock, "test/secret")
	if err == nil {
		t.Fatal("expected error from GetSecretValue")
	}
}

func TestDeleteSecret_Success(t *testing.T) {
	var gotForceDelete bool
	mock := &mockSecretsClient{
		deleteSecretFn: func(ctx context.Context, params *secretsmanager.DeleteSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error) {
			gotForceDelete = awslib.ToBool(params.ForceDeleteWithoutRecovery)
			return &secretsmanager.DeleteSecretOutput{}, nil
		},
	}

	err := DeleteSecret(context.Background(), mock, "test/secret")
	if err != nil {
		t.Fatalf("DeleteSecret returned error: %v", err)
	}
	if !gotForceDelete {
		t.Error("expected ForceDeleteWithoutRecovery to be true")
	}
}

func TestDeleteSecret_WrapsError(t *testing.T) {
	mock := &mockSecretsClient{
		deleteSecretFn: func(ctx context.Context, params *secretsmanager.DeleteSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error) {
			return nil, fmt.Errorf("access denied")
		},
	}

	err := DeleteSecret(context.Background(), mock, "test/secret")
	if err == nil {
		t.Fatal("expected error from DeleteSecret")
	}
	expected := "failed to delete secret test/secret: access denied"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}
