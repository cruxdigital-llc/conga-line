package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// BehaviorFiles maps filename -> content for an agent's behavior directory.
type BehaviorFiles map[string][]byte

// ComposeBehaviorFiles assembles behavior files for an agent.
// Priority: overrides/{agent_name}/ > base/ > {agent_type}/
//
// For SOUL.md and AGENTS.md: override > base, concatenate with type-specific if exists.
// For USER.md: override > render type template with agent name and Slack ID.
//
// behaviorDir is the root of the behavior/ tree.
func ComposeBehaviorFiles(behaviorDir string, agent provider.AgentConfig) (BehaviorFiles, error) {
	files := make(BehaviorFiles)
	agentType := string(agent.Type)

	// SOUL.md and AGENTS.md: same composition logic
	for _, name := range []string{"SOUL.md", "AGENTS.md"} {
		// Check override first
		overridePath := filepath.Join(behaviorDir, "overrides", agent.Name, name)
		if data, err := os.ReadFile(overridePath); err == nil {
			files[name] = data
			continue
		}

		// Base + type-specific concatenation
		var content []byte
		basePath := filepath.Join(behaviorDir, "base", name)
		if data, err := os.ReadFile(basePath); err == nil {
			content = data
		}

		typePath := filepath.Join(behaviorDir, agentType, name)
		if data, err := os.ReadFile(typePath); err == nil {
			if len(content) > 0 {
				content = append(content, '\n')
			}
			content = append(content, data...)
		}

		if len(content) > 0 {
			files[name] = content
		}
	}

	// USER.md: override > template rendering
	overridePath := filepath.Join(behaviorDir, "overrides", agent.Name, "USER.md")
	if data, err := os.ReadFile(overridePath); err == nil {
		files["USER.md"] = data
	} else {
		tmplPath := filepath.Join(behaviorDir, agentType, "USER.md.tmpl")
		if data, err := os.ReadFile(tmplPath); err == nil {
			content := string(data)
			content = strings.ReplaceAll(content, "{{AGENT_NAME}}", agent.Name)

			// Gather template vars from all channel bindings
			for _, binding := range agent.Channels {
				ch, ok := channels.Get(binding.Platform)
				if !ok {
					continue
				}
				for k, v := range ch.BehaviorTemplateVars(string(agent.Type), binding) {
					content = strings.ReplaceAll(content, "{{"+k+"}}", v)
				}
			}

			files["USER.md"] = []byte(content)
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no behavior files found in %s", behaviorDir)
	}

	return files, nil
}
