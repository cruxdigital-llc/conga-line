package openclaw

import (
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

func (r *Runtime) GenerateEnvFile(params runtime.EnvParams) []byte {
	var buf []byte

	appendEnv := func(key, val string) {
		if val != "" {
			buf = append(buf, []byte(fmt.Sprintf("%s=%s\n", key, val))...)
		}
	}

	// Channel-provided env vars (deduplicated)
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

	// Non-channel shared secrets
	appendEnv("GOOGLE_CLIENT_ID", params.Secrets.GoogleClientID)
	appendEnv("GOOGLE_CLIENT_SECRET", params.Secrets.GoogleClientSecret)
	// Base NODE_OPTIONS for heap size. When egress proxy is active, providers
	// override this via Docker -e flag to add --require proxy-bootstrap.js.
	appendEnv("NODE_OPTIONS", "--max-old-space-size=1536")

	for name, value := range params.PerAgent {
		appendEnv(runtime.SecretNameToEnvVar(name), value)
	}

	return buf
}
