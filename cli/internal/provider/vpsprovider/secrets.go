package vpsprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

// sharedSecretsDir returns the remote path to shared secrets.
func (p *VPSProvider) sharedSecretsDir() string {
	return filepath.Join(p.remoteDir, "secrets", "shared")
}

// agentSecretsDir returns the remote path to per-agent secrets.
func (p *VPSProvider) agentSecretsDir(agentName string) string {
	return filepath.Join(p.remoteDir, "secrets", "agents", agentName)
}

// readSharedSecrets loads all shared secrets from the remote host.
func (p *VPSProvider) readSharedSecrets() (common.SharedSecrets, error) {
	dir := p.sharedSecretsDir()
	var s common.SharedSecrets

	read := func(name string) string {
		data, err := p.ssh.Download(filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return string(data)
	}

	s.SlackBotToken = read("slack-bot-token")
	s.SlackSigningSecret = read("slack-signing-secret")
	s.SlackAppToken = read("slack-app-token")
	s.GoogleClientID = read("google-client-id")
	s.GoogleClientSecret = read("google-client-secret")

	return s, nil
}

// readAgentSecrets reads all per-agent secrets from the remote host.
func (p *VPSProvider) readAgentSecrets(agentName string) (map[string]string, error) {
	dir := p.agentSecretsDir(agentName)
	output, err := p.ssh.Run(context.Background(), fmt.Sprintf("ls %s 2>/dev/null || true", shellQuote(dir)))
	if err != nil {
		return nil, nil
	}

	secrets := make(map[string]string)
	for _, name := range strings.Split(strings.TrimSpace(output), "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		data, err := p.ssh.Download(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		secrets[name] = string(data)
	}
	return secrets, nil
}

// SetSecret creates or updates a secret for an agent on the remote host.
func (p *VPSProvider) SetSecret(ctx context.Context, agentName, secretName, value string) error {
	path := filepath.Join(p.agentSecretsDir(agentName), secretName)
	// Ensure directory exists
	p.ssh.MkdirAll(filepath.Dir(path), 0700)
	return p.ssh.Upload(path, []byte(value), 0400)
}

// ListSecrets returns all secrets for an agent from the remote host.
func (p *VPSProvider) ListSecrets(ctx context.Context, agentName string) ([]provider.SecretEntry, error) {
	dir := p.agentSecretsDir(agentName)
	// Use stat to get file info
	output, err := p.ssh.Run(ctx, fmt.Sprintf("ls -la --time-style=full-iso %s 2>/dev/null || true", shellQuote(dir)))
	if err != nil || strings.TrimSpace(output) == "" {
		return nil, nil
	}

	var result []provider.SecretEntry
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 || fields[0] == "total" {
			continue
		}
		name := fields[len(fields)-1]
		if name == "." || name == ".." {
			continue
		}

		lastChanged := time.Time{}
		// fields[5] is date, fields[6] is time
		if len(fields) >= 7 {
			if t, err := time.Parse("2006-01-02 15:04:05", fields[5]+" "+fields[6][:8]); err == nil {
				lastChanged = t
			}
		}

		result = append(result, provider.SecretEntry{
			Name:        name,
			EnvVar:      common.SecretNameToEnvVar(name),
			Path:        filepath.Join(dir, name),
			LastChanged: lastChanged,
		})
	}
	return result, nil
}

// DeleteSecret removes a secret file on the remote host.
func (p *VPSProvider) DeleteSecret(ctx context.Context, agentName, secretName string) error {
	path := filepath.Join(p.agentSecretsDir(agentName), secretName)
	_, err := p.ssh.Run(ctx, fmt.Sprintf("rm %s", shellQuote(path)))
	if err != nil {
		return fmt.Errorf("secret %q not found for agent %s", secretName, agentName)
	}
	return nil
}

// writeSharedSecret writes a single shared secret to the remote host.
func (p *VPSProvider) writeSharedSecret(name, value string) error {
	if value == "" {
		return nil
	}
	path := filepath.Join(p.sharedSecretsDir(), name)
	return p.ssh.Upload(path, []byte(value), os.FileMode(0400))
}
