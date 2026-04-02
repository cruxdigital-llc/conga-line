package awsprovider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmTypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	awsutil "github.com/cruxdigital-llc/conga-line/cli/pkg/aws"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/cli/pkg/discovery"

	// Register slack channel so channels.Get("slack") works
	_ "github.com/cruxdigital-llc/conga-line/cli/pkg/channels/slack"
)

// mockSecretsManager implements awsutil.SecretsManagerClient for testing.
type mockSecretsManager struct {
	awsutil.SecretsManagerClient
	secrets map[string]string
}

func (m *mockSecretsManager) GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	val, ok := m.secrets[aws.ToString(params.SecretId)]
	if !ok {
		return nil, &smtypes.ResourceNotFoundException{Message: aws.String("not found")}
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(val)}, nil
}

func (m *mockSecretsManager) PutSecretValue(ctx context.Context, params *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	m.secrets[aws.ToString(params.SecretId)] = aws.ToString(params.SecretString)
	return &secretsmanager.PutSecretValueOutput{}, nil
}

func (m *mockSecretsManager) CreateSecret(ctx context.Context, params *secretsmanager.CreateSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error) {
	m.secrets[aws.ToString(params.Name)] = aws.ToString(params.SecretString)
	return &secretsmanager.CreateSecretOutput{}, nil
}

func (m *mockSecretsManager) DeleteSecret(ctx context.Context, params *secretsmanager.DeleteSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error) {
	delete(m.secrets, aws.ToString(params.SecretId))
	return &secretsmanager.DeleteSecretOutput{}, nil
}

func (m *mockSecretsManager) ListSecrets(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	var entries []smtypes.SecretListEntry
	for name := range m.secrets {
		entries = append(entries, smtypes.SecretListEntry{Name: aws.String(name)})
	}
	return &secretsmanager.ListSecretsOutput{SecretList: entries}, nil
}

// mockSSMForChannels extends mockSSM with GetParametersByPath for ListAgents.
type mockSSMForChannels struct {
	awsutil.SSMClient
	stored map[string]string
}

func (m *mockSSMForChannels) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	val, ok := m.stored[aws.ToString(params.Name)]
	if !ok {
		return nil, &ssmTypes.ParameterNotFound{}
	}
	return &ssm.GetParameterOutput{
		Parameter: &ssmTypes.Parameter{Value: aws.String(val)},
	}, nil
}

func (m *mockSSMForChannels) PutParameter(ctx context.Context, params *ssm.PutParameterInput, optFns ...func(*ssm.Options)) (*ssm.PutParameterOutput, error) {
	m.stored[aws.ToString(params.Name)] = aws.ToString(params.Value)
	return &ssm.PutParameterOutput{}, nil
}

func (m *mockSSMForChannels) GetParametersByPath(ctx context.Context, params *ssm.GetParametersByPathInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersByPathOutput, error) {
	prefix := aws.ToString(params.Path)
	var parameters []ssmTypes.Parameter
	for name, val := range m.stored {
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			parameters = append(parameters, ssmTypes.Parameter{
				Name:  aws.String(name),
				Value: aws.String(val),
			})
		}
	}
	return &ssm.GetParametersByPathOutput{Parameters: parameters}, nil
}

func TestSaveAgentToSSM_WithChannels(t *testing.T) {
	mock := &mockSSMForChannels{stored: make(map[string]string)}
	p := &AWSProvider{clients: &awsutil.Clients{SSM: mock}}

	agent := discovery.AgentConfig{
		Name:        "testuser",
		Type:        "user",
		Channels:    []channels.ChannelBinding{{Platform: "slack", ID: "U123"}},
		GatewayPort: 18789,
		IAMIdentity: "testiam",
	}

	if err := p.saveAgentToSSM(context.Background(), agent); err != nil {
		t.Fatalf("saveAgentToSSM error: %v", err)
	}

	stored, ok := mock.stored["/conga/agents/testuser"]
	if !ok {
		t.Fatal("expected agent config in SSM")
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(stored), &parsed); err != nil {
		t.Fatalf("failed to parse stored JSON: %v", err)
	}

	if parsed["type"] != "user" {
		t.Errorf("expected type=user, got %v", parsed["type"])
	}

	chans, ok := parsed["channels"].([]any)
	if !ok || len(chans) != 1 {
		t.Fatalf("expected 1 channel, got %v", parsed["channels"])
	}

	ch := chans[0].(map[string]any)
	if ch["platform"] != "slack" || ch["id"] != "U123" {
		t.Errorf("unexpected channel: %v", ch)
	}
}

func TestSaveAgentToSSM_WithoutChannels(t *testing.T) {
	mock := &mockSSMForChannels{stored: make(map[string]string)}
	p := &AWSProvider{clients: &awsutil.Clients{SSM: mock}}

	agent := discovery.AgentConfig{
		Name:        "solo",
		Type:        "user",
		GatewayPort: 18789,
	}

	if err := p.saveAgentToSSM(context.Background(), agent); err != nil {
		t.Fatalf("saveAgentToSSM error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(mock.stored["/conga/agents/solo"]), &parsed); err != nil {
		t.Fatalf("failed to parse stored JSON: %v", err)
	}

	// Channels should be null/empty, not missing (SSM agent format expects the field)
	if parsed["type"] != "user" {
		t.Errorf("expected type=user, got %v", parsed["type"])
	}
}

func TestReadSharedSecrets_ReadsFromSecretsManager(t *testing.T) {
	mock := &mockSecretsManager{secrets: map[string]string{
		"conga/shared/slack-bot-token":      "xoxb-test",
		"conga/shared/slack-signing-secret": "test-signing",
		"conga/shared/slack-app-token":      "xapp-test",
		"conga/shared/google-client-id":     "google-id",
		"conga/shared/google-client-secret": "google-secret",
	}}
	p := &AWSProvider{clients: &awsutil.Clients{SecretsManager: mock}}

	shared, err := p.readSharedSecrets(context.Background())
	if err != nil {
		t.Fatalf("readSharedSecrets error: %v", err)
	}

	if shared.Values["slack-bot-token"] != "xoxb-test" {
		t.Errorf("expected slack-bot-token=xoxb-test, got %s", shared.Values["slack-bot-token"])
	}
	if shared.Values["slack-signing-secret"] != "test-signing" {
		t.Errorf("expected slack-signing-secret=test-signing, got %s", shared.Values["slack-signing-secret"])
	}
	if shared.GoogleClientID != "google-id" {
		t.Errorf("expected google-client-id=google-id, got %s", shared.GoogleClientID)
	}
}

func TestReadSharedSecrets_IgnoresReplaceMe(t *testing.T) {
	mock := &mockSecretsManager{secrets: map[string]string{
		"conga/shared/slack-bot-token": "REPLACE_ME",
	}}
	p := &AWSProvider{clients: &awsutil.Clients{SecretsManager: mock}}

	shared, err := p.readSharedSecrets(context.Background())
	if err != nil {
		t.Fatalf("readSharedSecrets error: %v", err)
	}

	if _, ok := shared.Values["slack-bot-token"]; ok {
		t.Error("expected REPLACE_ME values to be excluded")
	}
}

func TestReadSharedSecrets_IgnoresMissing(t *testing.T) {
	mock := &mockSecretsManager{secrets: map[string]string{}}
	p := &AWSProvider{clients: &awsutil.Clients{SecretsManager: mock}}

	shared, err := p.readSharedSecrets(context.Background())
	if err != nil {
		t.Fatalf("readSharedSecrets error: %v", err)
	}

	if len(shared.Values) != 0 {
		t.Errorf("expected empty values, got %d", len(shared.Values))
	}
}

func TestReadAgentSecrets_ReadsFromSecretsManager(t *testing.T) {
	mock := &mockSecretsManager{secrets: map[string]string{
		"conga/agents/aaron/anthropic-api-key": "sk-test",
		"conga/agents/aaron/trello-api-key":    "trello-key",
	}}
	p := &AWSProvider{clients: &awsutil.Clients{SecretsManager: mock}}

	secrets, err := p.readAgentSecrets(context.Background(), "aaron")
	if err != nil {
		t.Fatalf("readAgentSecrets error: %v", err)
	}

	if secrets["anthropic-api-key"] != "sk-test" {
		t.Errorf("expected anthropic-api-key=sk-test, got %s", secrets["anthropic-api-key"])
	}
	if secrets["trello-api-key"] != "trello-key" {
		t.Errorf("expected trello-api-key=trello-key, got %s", secrets["trello-api-key"])
	}
}
