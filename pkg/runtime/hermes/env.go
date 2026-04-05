package hermes

import (
	"fmt"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// secretNameToEnvVar converts a kebab-case secret name to SCREAMING_SNAKE_CASE.
func secretNameToEnvVar(name string) string {
	return strings.NewReplacer("-", "_").Replace(strings.ToUpper(name))
}

func (r *Runtime) GenerateEnvFile(params runtime.EnvParams) []byte {
	var buf []byte

	appendEnv := func(key, val string) {
		if val != "" {
			buf = append(buf, []byte(fmt.Sprintf("%s=%s\n", key, val))...)
		}
	}

	// Channel-provided env vars (SLACK_BOT_TOKEN, SLACK_SIGNING_SECRET, etc.)
	seen := map[string]bool{}
	for _, binding := range params.Agent.Channels {
		ch, ok := channels.Get(binding.Platform)
		if !ok {
			continue
		}
		for k, v := range ch.AgentEnvVars(params.Secrets.Values) {
			if !seen[k] {
				appendEnv(k, v)
				seen[k] = true
			}
		}
	}

	// Enable the API server platform and bind to all interfaces (required for Docker).
	appendEnv("API_SERVER_ENABLED", "true")
	appendEnv("API_SERVER_HOST", "0.0.0.0")


	// Hermes gateway needs explicit user access configuration.
	// Default to open access for the API server (secured by gateway token).
	appendEnv("GATEWAY_ALLOW_ALL_USERS", "true")

	// Per-agent secrets (ANTHROPIC_API_KEY, etc.)
	for name, value := range params.PerAgent {
		appendEnv(secretNameToEnvVar(name), value)
	}

	return buf
}
