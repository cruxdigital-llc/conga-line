package localprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cruxdigital-llc/conga-line/cli/internal/channels"
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

// writeSecret writes a secret value to a file with mode 0400 using
// atomic write (temp file + rename) to prevent momentary exposure.
func writeSecret(path, value string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	// Write to a temp file in the same directory (same filesystem for atomic rename)
	tmp, err := os.CreateTemp(dir, ".secret-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	// Set restrictive permissions before writing content
	if err := tmp.Chmod(0400); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	if _, err := tmp.Write([]byte(value)); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write secret: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename (POSIX guarantees atomicity on the same filesystem)
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	success = true
	return nil
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
	s := common.SharedSecrets{
		Values: make(map[string]string),
	}

	read := func(name string) string {
		val, _ := readSecret(filepath.Join(dir, name))
		return val
	}

	// Read channel-defined secrets into the Values map
	for _, ch := range channels.All() {
		for _, def := range ch.SharedSecrets() {
			if v := read(def.Name); v != "" {
				s.Values[def.Name] = v
			}
		}
	}

	// Non-channel shared secrets
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
		return fmt.Errorf("secret %q not found for agent %s: %w", secretName, agentName, provider.ErrNotFound)
	}
	return err
}
