package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
	"github.com/spf13/cobra"
)

var behaviorAsName string

func init() {
	agentBehaviorCmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage per-agent behavior files",
		Long: `Manage per-agent behavior files that override the defaults.

Agent-specific behavior files (SOUL.md, AGENTS.md, USER.md) replace the
shared defaults when present. They are deployed on provision and refresh
but never clobber agent-mutable state like MEMORY.md.

Changes take effect on the next 'conga refresh'.`,
	}

	listCmd := &cobra.Command{
		Use:   "list <agent>",
		Short: "List behavior files for an agent",
		Args:  cobra.ExactArgs(1),
		RunE:  agentBehaviorListRun,
	}

	addCmd := &cobra.Command{
		Use:   "add <agent> <file>",
		Short: "Add a behavior file to an agent",
		Long: `Copy a markdown file into the agent's behavior directory.
The file will be deployed to the agent's workspace on the next refresh,
replacing the default version of that file.`,
		Args: cobra.ExactArgs(2),
		RunE: agentBehaviorAddRun,
	}
	addCmd.Flags().StringVar(&behaviorAsName, "as", "", "Rename the file on copy (e.g. --as SOUL.md)")

	rmCmd := &cobra.Command{
		Use:   "rm <agent> <name>",
		Short: "Remove a behavior file from an agent",
		Long: `Remove an agent-specific behavior file. On the next refresh, the agent
will fall back to the shared default for that file.`,
		Args: cobra.ExactArgs(2),
		RunE: agentBehaviorRmRun,
	}

	showCmd := &cobra.Command{
		Use:   "show <agent> <name>",
		Short: "Display an agent behavior file",
		Args:  cobra.ExactArgs(2),
		RunE:  agentBehaviorShowRun,
	}

	diffCmd := &cobra.Command{
		Use:   "diff <agent>",
		Short: "Compare agent behavior source to deployed workspace",
		Args:  cobra.ExactArgs(1),
		RunE:  agentBehaviorDiffRun,
	}

	agentBehaviorCmd.AddCommand(listCmd, addCmd, rmCmd, showCmd, diffCmd)
	rootCmd.AddCommand(agentBehaviorCmd)
}

// agentBehaviorDir returns the per-agent behavior source directory.
// Reads repo_path from local-config.json and points at behavior/agents/.
// Falls back to the deployed copy at ~/.conga/behavior/agents/.
func agentBehaviorDir() string {
	dataDir := provider.DefaultDataDir()
	if flagDataDir != "" {
		dataDir = flagDataDir
	}

	if repoPath := readLocalConfigValue(dataDir, "repo_path"); repoPath != "" {
		return filepath.Join(repoPath, "behavior", "agents")
	}

	return filepath.Join(dataDir, "behavior", "agents")
}

// readLocalConfigValue reads a key from local-config.json.
func readLocalConfigValue(dataDir, key string) string {
	data, err := os.ReadFile(filepath.Join(dataDir, "local-config.json"))
	if err != nil {
		return ""
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m[key]
}

// syncBehaviorToDeployed copies the repo's behavior/ tree to the deployed
// location (~/.conga/behavior/) so that the next refresh picks up changes.
func syncBehaviorToDeployed(dataDir string) {
	repoPath := readLocalConfigValue(dataDir, "repo_path")
	if repoPath == "" {
		return
	}
	src := filepath.Join(repoPath, "behavior", "agents")
	dst := filepath.Join(dataDir, "behavior", "agents")
	if _, err := os.Stat(src); err != nil {
		return
	}
	filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			os.MkdirAll(target, 0755)
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		os.MkdirAll(filepath.Dir(target), 0755)
		os.WriteFile(target, content, 0644)
		return nil
	})
}

// validateBehaviorFileName rejects names containing path separators or
// traversal components. Prevents path traversal via rm, show, or add --as.
func validateBehaviorFileName(name string) error {
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid behavior file name %q: must not contain path separators or '..'", name)
	}
	return nil
}

func agentBehaviorListRun(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	dir := filepath.Join(agentBehaviorDir(), agentName)

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		fmt.Printf("No agent-specific behavior files for %s (using defaults).\n", agentName)
		return nil
	}
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		fmt.Printf("No agent-specific behavior files for %s (using defaults).\n", agentName)
		return nil
	}

	fmt.Printf("behavior/agents/%s/\n", agentName)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, _ := e.Info()
		size := "?"
		if info != nil {
			size = formatSize(info.Size())
		}
		fmt.Printf("  %s  (%s)\n", e.Name(), size)
	}
	return nil
}

func agentBehaviorAddRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]
	srcPath := args[1]

	if _, err := prov.GetAgent(ctx, agentName); err != nil {
		return fmt.Errorf("agent %q not found: %w", agentName, err)
	}

	if _, err := os.Stat(srcPath); err != nil {
		return fmt.Errorf("source file %q: %w", srcPath, err)
	}

	targetName := filepath.Base(srcPath)
	if behaviorAsName != "" {
		targetName = behaviorAsName
	}

	if err := validateBehaviorFileName(targetName); err != nil {
		return err
	}
	if !strings.HasSuffix(strings.ToLower(targetName), ".md") {
		return fmt.Errorf("behavior files must be .md (got %q)", targetName)
	}

	rt := runtime.ResolveRuntime("", "")
	if common.IsProtectedPath(targetName, rt) {
		return fmt.Errorf("%q is a protected path and cannot be used as a behavior file", targetName)
	}

	content, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	if int64(len(content)) > common.MaxBehaviorFileSize {
		return fmt.Errorf("file exceeds size limit (%d bytes > %d)", len(content), common.MaxBehaviorFileSize)
	}

	dir := filepath.Join(agentBehaviorDir(), agentName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	targetPath := filepath.Join(dir, targetName)
	if err := os.WriteFile(targetPath, content, 0644); err != nil {
		return err
	}

	dataDir := provider.DefaultDataDir()
	if flagDataDir != "" {
		dataDir = flagDataDir
	}
	syncBehaviorToDeployed(dataDir)

	fmt.Printf("Copied %s -> behavior/agents/%s/%s\n", filepath.Base(srcPath), agentName, targetName)
	fmt.Printf("Run 'conga refresh --agent %s' to deploy.\n", agentName)
	return nil
}

func agentBehaviorRmRun(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	name := args[1]

	if err := validateBehaviorFileName(name); err != nil {
		return err
	}

	path := filepath.Join(agentBehaviorDir(), agentName, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("behavior file %s/%s not found", agentName, name)
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	// Also remove from deployed location
	dataDir := provider.DefaultDataDir()
	if flagDataDir != "" {
		dataDir = flagDataDir
	}
	deployedPath := filepath.Join(dataDir, "behavior", "agents", agentName, name)
	os.Remove(deployedPath)

	fmt.Printf("Removed behavior/agents/%s/%s\n", agentName, name)
	fmt.Printf("Run 'conga refresh --agent %s' to apply (will fall back to default).\n", agentName)
	return nil
}

func agentBehaviorShowRun(cmd *cobra.Command, args []string) error {
	agentName := args[0]
	name := args[1]

	if err := validateBehaviorFileName(name); err != nil {
		return err
	}

	path := filepath.Join(agentBehaviorDir(), agentName, name)
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("behavior file %s/%s: %w", agentName, name, err)
	}

	fmt.Print(string(content))
	return nil
}

func agentBehaviorDiffRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName := args[0]

	if _, err := prov.GetAgent(ctx, agentName); err != nil {
		return fmt.Errorf("agent %q not found: %w", agentName, err)
	}

	// List agent-specific source files
	srcDir := filepath.Join(agentBehaviorDir(), agentName)
	srcFiles := map[string][]byte{}
	if entries, err := os.ReadDir(srcDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(srcDir, e.Name()))
			srcFiles[e.Name()] = data
		}
	}

	// Read workspace manifest via container exec
	manifestJSON, err := prov.ContainerExec(ctx, agentName, []string{"cat", "/home/node/.openclaw/data/workspace/.conga-overlay-manifest.json"})
	var manifest *common.OverlayManifest
	if err == nil && manifestJSON != "" {
		manifest = common.ParseOverlayManifest([]byte(manifestJSON))
	}

	if len(srcFiles) == 0 && manifest == nil {
		fmt.Printf("No agent-specific behavior files for %s (using defaults).\n", agentName)
		return nil
	}

	// Build workspace hash map from manifest (agent-sourced files only)
	wsHashes := map[string]string{}
	if manifest != nil {
		for _, entry := range manifest.Files {
			if entry.Source == "agent" || entry.Source == "overlay" {
				wsHashes[entry.Path] = entry.SHA256
			}
		}
	}

	for name, srcContent := range srcFiles {
		srcHash := common.HashFileContent(srcContent)
		if wsHash, ok := wsHashes[name]; ok {
			if srcHash == wsHash {
				fmt.Printf("  %-30s in-sync\n", name)
			} else {
				fmt.Printf("  %-30s DIFFERS (refresh to update)\n", name)
			}
			delete(wsHashes, name)
		} else {
			fmt.Printf("  %-30s NEW (not yet deployed)\n", name)
		}
	}
	for name := range wsHashes {
		fmt.Printf("  %-30s REMOVED FROM SOURCE (will revert to default on refresh)\n", name)
	}

	return nil
}

func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
}
