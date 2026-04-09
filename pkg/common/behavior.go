package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// BehaviorFile holds content and metadata for a single behavior file.
type BehaviorFile struct {
	Content []byte
	Source  string // "default" or "agent"
}

// BehaviorFiles maps workspace-relative filename -> file for an agent's behavior directory.
type BehaviorFiles map[string]BehaviorFile

// resolveBehaviorFiles assembles behavior files for an agent.
//
// Resolution order (all files):
//  1. agents/<agent_name>/<file> — agent-specific override (full replacement)
//  2. default/<runtime>/<type>/<file> — runtime+type-specific default
//
// USER.md.tmpl is rendered with agent template variables before deployment.
func resolveBehaviorFiles(behaviorDir string, agent provider.AgentConfig) BehaviorFiles {
	files := make(BehaviorFiles)
	agentType := string(agent.Type)

	agentDir := filepath.Join(behaviorDir, "agents", agent.Name)
	rtName := string(runtime.ResolveRuntime(agent.Runtime, ""))
	defaultDir := filepath.Join(behaviorDir, "default", rtName, agentType)

	// SOUL.md and AGENTS.md: agent-specific > runtime+type default
	for _, name := range []string{"SOUL.md", "AGENTS.md"} {
		if data, err := os.ReadFile(filepath.Join(agentDir, name)); err == nil {
			files[name] = BehaviorFile{Content: data, Source: "agent"}
			continue
		}
		if data, err := os.ReadFile(filepath.Join(defaultDir, name)); err == nil {
			files[name] = BehaviorFile{Content: data, Source: "default"}
		}
	}

	// USER.md: agent-specific > render runtime+type template
	if data, err := os.ReadFile(filepath.Join(agentDir, "USER.md")); err == nil {
		files["USER.md"] = BehaviorFile{Content: data, Source: "agent"}
	} else {
		tmplPath := filepath.Join(defaultDir, "USER.md.tmpl")
		if data, err := os.ReadFile(tmplPath); err == nil {
			content := string(data)
			content = strings.ReplaceAll(content, "{{.AgentName}}", agent.Name)
			content = strings.ReplaceAll(content, "{{AGENT_NAME}}", agent.Name) // legacy compat

			for _, binding := range agent.Channels {
				ch, ok := channels.Get(binding.Platform)
				if !ok {
					continue
				}
				for k, v := range ch.BehaviorTemplateVars(string(agent.Type), binding) {
					content = strings.ReplaceAll(content, "{{"+k+"}}", v)
				}
			}

			files["USER.md"] = BehaviorFile{Content: []byte(content), Source: "default"}
		}
	}

	return files
}

// ComposeAgentWorkspaceFiles assembles all behavior files for an agent and
// computes deletion reconciliation against the previous manifest.
//
// hashWorkspaceFile is called to hash existing workspace files for deletion
// reconciliation. Pass nil if not needed (e.g. first provision).
func ComposeAgentWorkspaceFiles(
	behaviorDir string,
	agent provider.AgentConfig,
	prevManifest *OverlayManifest,
	hashWorkspaceFile func(rel string) (string, error),
) (files BehaviorFiles, toDelete []string, next OverlayManifest, err error) {
	files = resolveBehaviorFiles(behaviorDir, agent)

	// Validate agent-specific files against protected paths
	rt := runtime.ResolveRuntime(agent.Runtime, "")
	for name := range files {
		if files[name].Source == "agent" && IsProtectedPath(name, rt) {
			return nil, nil, OverlayManifest{}, fmt.Errorf("agent behavior file %s is on the protected path list", name)
		}
	}

	if len(files) == 0 {
		return nil, nil, OverlayManifest{}, fmt.Errorf("no behavior files found in %s", behaviorDir)
	}

	toDelete = reconcileDeletions(prevManifest, files, hashWorkspaceFile)
	next = buildManifest(files)

	var agentCount int
	for _, f := range next.Files {
		if f.Source == "agent" {
			agentCount++
		}
	}
	defaultCount := len(next.Files) - agentCount
	if agentCount > 0 {
		fmt.Fprintf(os.Stderr, "behavior: %d agent-specific, %d default\n", agentCount, defaultCount)
	}

	return files, toDelete, next, nil
}

// ComposeBehaviorFiles is the legacy entry point.
// Deprecated: use ComposeAgentWorkspaceFiles instead.
func ComposeBehaviorFiles(behaviorDir string, agent provider.AgentConfig) (BehaviorFiles, error) {
	files := resolveBehaviorFiles(behaviorDir, agent)
	if len(files) == 0 {
		return nil, fmt.Errorf("no behavior files found in %s", behaviorDir)
	}
	return files, nil
}
