package openclaw

import (
	"os"
	"path/filepath"
)

func (r *Runtime) CreateDirectories(dataDir string) error {
	for _, sub := range []string{
		"data/workspace", "memory", "logs", "agents",
		"canvas", "cron", "devices", "identity", "media",
	} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0755); err != nil {
			return err
		}
	}
	// Create empty MEMORY.md so OpenClaw doesn't error on first read
	memoryPath := filepath.Join(dataDir, "data", "workspace", "MEMORY.md")
	if _, err := os.Stat(memoryPath); os.IsNotExist(err) {
		return os.WriteFile(memoryPath, []byte("# Memory\n"), 0644)
	}
	return nil
}
