package hermes

import (
	"os"
	"path/filepath"
)

func (r *Runtime) CreateDirectories(dataDir string) error {
	for _, sub := range []string{"workspace", "memory", "skills", "logs"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0755); err != nil {
			return err
		}
	}
	return nil
}
