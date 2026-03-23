package remoteprovider

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// checkConfigIntegrity verifies openclaw.json hasn't been tampered with on the remote host.
func (p *RemoteProvider) checkConfigIntegrity(agentName string) error {
	configPath := filepath.Join(p.remoteDataSubDir(agentName), "openclaw.json")
	baselinePath := filepath.Join(p.remoteConfigDir(), agentName+".sha256")

	data, err := p.ssh.Download(configPath)
	if err != nil {
		return fmt.Errorf("config not found: %w", err)
	}

	currentHash := fmt.Sprintf("%x", sha256.Sum256(data))

	baselineData, err := p.ssh.Download(baselinePath)
	if err != nil {
		// No baseline — create one
		return p.ssh.Upload(baselinePath, []byte(currentHash), 0600)
	}

	if string(baselineData) != currentHash {
		return fmt.Errorf("CONFIG INTEGRITY VIOLATION: %s/openclaw.json has been modified (expected %s, got %s)",
			agentName, string(baselineData), currentHash)
	}

	return nil
}

// saveConfigBaseline stores the SHA256 hash of the current openclaw.json on the remote host.
func (p *RemoteProvider) saveConfigBaseline(agentName string) error {
	configPath := filepath.Join(p.remoteDataSubDir(agentName), "openclaw.json")
	data, err := p.ssh.Download(configPath)
	if err != nil {
		return err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	baselinePath := filepath.Join(p.remoteConfigDir(), agentName+".sha256")
	return p.ssh.Upload(baselinePath, []byte(hash), 0600)
}

// RunIntegrityCheck checks all agent configs on the remote host and logs results.
func (p *RemoteProvider) RunIntegrityCheck() error {
	agents, err := p.ListAgents(context.Background())
	if err != nil {
		return err
	}

	logPath := filepath.Join(p.remoteDir, "logs", "integrity.log")
	now := time.Now().Format(time.RFC3339)

	var logLines []string
	for _, a := range agents {
		if err := p.checkConfigIntegrity(a.Name); err != nil {
			logLines = append(logLines, fmt.Sprintf("%s ALERT %s: %v", now, a.Name, err))
			fmt.Fprintf(os.Stderr, "ALERT: %v\n", err)
		} else {
			logLines = append(logLines, fmt.Sprintf("%s OK %s: config integrity verified", now, a.Name))
		}
	}

	// Append to remote log
	if len(logLines) > 0 {
		content := fmt.Sprintf("%s\n", fmt.Sprintf("%s", joinLines(logLines)))
		p.ssh.Run(context.Background(), fmt.Sprintf("echo %s >> %s",
			shellQuote(content), shellQuote(logPath)))
	}

	return nil
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}
