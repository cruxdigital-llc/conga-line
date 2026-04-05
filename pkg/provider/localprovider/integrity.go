package localprovider

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// configFileForAgent returns the config file path for the given agent,
// using the runtime's config file name if available, falling back to
// checking both openclaw.json and config.yaml on disk.
func (p *LocalProvider) configFileForAgent(agentName string) string {
	dataDir := p.dataSubDir(agentName)

	// Try to resolve via the runtime
	if cfg, err := p.GetAgent(context.Background(), agentName); err == nil {
		if rt, err := p.runtimeForAgent(*cfg); err == nil {
			return filepath.Join(dataDir, rt.ConfigFileName())
		}
	}

	// Fallback: check which config file exists on disk
	for _, name := range []string{"config.yaml", "openclaw.json"} {
		path := filepath.Join(dataDir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Last resort
	return filepath.Join(dataDir, "openclaw.json")
}

// checkConfigIntegrity verifies the agent's config file hasn't been tampered with.
// Returns nil if hash matches or no baseline exists. Returns error on mismatch.
func (p *LocalProvider) checkConfigIntegrity(agentName string) error {
	configPath := p.configFileForAgent(agentName)
	baselinePath := filepath.Join(p.configDir(), agentName+".sha256")

	// Read current config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("config not found: %w", err)
	}

	currentHash := fmt.Sprintf("%x", sha256.Sum256(data))

	// Read baseline
	baselineData, err := os.ReadFile(baselinePath)
	if err != nil {
		// No baseline — create one
		return os.WriteFile(baselinePath, []byte(currentHash), 0600)
	}

	if string(baselineData) != currentHash {
		return fmt.Errorf("CONFIG INTEGRITY VIOLATION: %s config has been modified (expected %s, got %s)",
			agentName, string(baselineData), currentHash)
	}

	return nil
}

// saveConfigBaseline stores the SHA256 hash of the current agent config file.
func (p *LocalProvider) saveConfigBaseline(agentName string) error {
	configPath := p.configFileForAgent(agentName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	baselinePath := filepath.Join(p.configDir(), agentName+".sha256")
	return os.WriteFile(baselinePath, []byte(hash), 0600)
}

// RunIntegrityCheck checks all agent configs and logs results.
func (p *LocalProvider) RunIntegrityCheck() error {
	agents, err := p.ListAgents(context.Background())
	if err != nil {
		return err
	}

	logPath := filepath.Join(p.logsDir(), "integrity.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	now := time.Now().Format(time.RFC3339)
	for _, a := range agents {
		if err := p.checkConfigIntegrity(a.Name); err != nil {
			fmt.Fprintf(f, "%s ALERT %s: %v\n", now, a.Name, err)
			fmt.Fprintf(os.Stderr, "ALERT: %v\n", err)
		} else {
			fmt.Fprintf(f, "%s OK %s: config integrity verified\n", now, a.Name)
		}
	}

	return nil
}
