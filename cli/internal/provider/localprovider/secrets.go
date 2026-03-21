package localprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

// sharedSecretsDir returns the path to shared secrets.
func (p *LocalProvider) sharedSecretsDir() string {
	return filepath.Join(p.dataDir, "secrets", "shared")
}

// agentSecretsDir returns the path to per-agent secrets.
func (p *LocalProvider) agentSecretsDir(agentName string) string {
	return filepath.Join(p.dataDir, "secrets", "agents", agentName)
}

// writeSecret writes a secret value to a file with mode 0400.
func writeSecret(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(value), 0400); err != nil {
		return err
	}
	return os.Chmod(path, 0400) // belt and suspenders
}

// readSecret reads a secret value from a file.
func readSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// readSharedSecrets loads all shared secrets into a SharedSecrets struct.
func (p *LocalProvider) readSharedSecrets() (common.SharedSecrets, error) {
	dir := p.sharedSecretsDir()
	var s common.SharedSecrets

	read := func(name string) string {
		val, _ := readSecret(filepath.Join(dir, name))
		return val
	}

	s.SlackBotToken = read("slack-bot-token")
	s.SlackSigningSecret = read("slack-signing-secret")
	s.SlackAppToken = read("slack-app-token")
	s.GoogleClientID = read("google-client-id")
	s.GoogleClientSecret = read("google-client-secret")

	return s, nil
}

// readAgentSecrets reads all per-agent secrets as name -> value.
func (p *LocalProvider) readAgentSecrets(agentName string) (map[string]string, error) {
	dir := p.agentSecretsDir(agentName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	secrets := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		val, err := readSecret(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		secrets[e.Name()] = val
	}
	return secrets, nil
}

// SetSecret creates or updates a secret for an agent.
func (p *LocalProvider) SetSecret(ctx context.Context, agentName, secretName, value string) error {
	path := filepath.Join(p.agentSecretsDir(agentName), secretName)
	return writeSecret(path, value)
}

// ListSecrets returns all secrets for an agent.
func (p *LocalProvider) ListSecrets(ctx context.Context, agentName string) ([]provider.SecretEntry, error) {
	dir := p.agentSecretsDir(agentName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var result []provider.SecretEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, _ := e.Info()
		lastChanged := time.Time{}
		if info != nil {
			lastChanged = info.ModTime()
		}
		result = append(result, provider.SecretEntry{
			Name:        e.Name(),
			EnvVar:      common.SecretNameToEnvVar(e.Name()),
			Path:        filepath.Join(dir, e.Name()),
			LastChanged: lastChanged,
		})
	}
	return result, nil
}

// DeleteSecret removes a secret file.
func (p *LocalProvider) DeleteSecret(ctx context.Context, agentName, secretName string) error {
	path := filepath.Join(p.agentSecretsDir(agentName), secretName)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("secret %q not found for agent %s", secretName, agentName)
	}
	return err
}
