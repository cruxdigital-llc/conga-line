package localprovider

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// checkConfigIntegrity verifies openclaw.json hasn't been tampered with.
// Returns nil if hash matches or no baseline exists. Returns error on mismatch.
func (p *LocalProvider) checkConfigIntegrity(agentName string) error {
	configPath := filepath.Join(p.dataSubDir(agentName), "openclaw.json")
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
		return fmt.Errorf("CONFIG INTEGRITY VIOLATION: %s/openclaw.json has been modified (expected %s, got %s)",
			agentName, string(baselineData), currentHash)
	}

	return nil
}

// saveConfigBaseline stores the SHA256 hash of the current openclaw.json.
func (p *LocalProvider) saveConfigBaseline(agentName string) error {
	configPath := filepath.Join(p.dataSubDir(agentName), "openclaw.json")
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
	agents, err := p.ListAgents(nil)
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
