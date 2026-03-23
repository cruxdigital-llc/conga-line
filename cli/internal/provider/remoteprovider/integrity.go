package remoteprovider

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	posixpath "path"
	"strings"
	"time"
)

// checkConfigIntegrity verifies openclaw.json hasn't been tampered with on the remote host.
func (p *RemoteProvider) checkConfigIntegrity(agentName string) error {
	configPath := posixpath.Join(p.remoteDataSubDir(agentName), "openclaw.json")
	baselinePath := posixpath.Join(p.remoteConfigDir(), agentName+".sha256")

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
	configPath := posixpath.Join(p.remoteDataSubDir(agentName), "openclaw.json")
	data, err := p.ssh.Download(configPath)
	if err != nil {
		return err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	baselinePath := posixpath.Join(p.remoteConfigDir(), agentName+".sha256")
	return p.ssh.Upload(baselinePath, []byte(hash), 0600)
}

// RunIntegrityCheck checks all agent configs on the remote host and logs results.
func (p *RemoteProvider) RunIntegrityCheck() error {
	agents, err := p.ListAgents(context.Background())
	if err != nil {
		return err
	}

	logPath := posixpath.Join(p.remoteDir, "logs", "integrity.log")
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

	// Append to remote log via stdin pipe (avoids shell interpretation of log content)
	if len(logLines) > 0 {
		content := strings.Join(logLines, "\n") + "\n"
		session, err := p.ssh.client.NewSession()
		if err == nil {
			session.Stdin = strings.NewReader(content)
			session.Run(fmt.Sprintf("cat >> %s", shellQuote(logPath)))
			session.Close()
		}
	}

	return nil
}
