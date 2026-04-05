package hermes

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

	// Set WEBHOOK_SECRET for Hermes webhook adapter HMAC verification.
	// Uses the Slack signing secret if available (shared with the Conga router
	// which signs forwarded events). Falls back to the Telegram webhook secret
	// if no Slack is configured. Without this, the webhook adapter cannot
	// verify that events came from the router.
	if v := params.Secrets.Values["slack-signing-secret"]; v != "" {
		appendEnv("WEBHOOK_SECRET", v)
	} else if v := params.Secrets.Values["telegram-webhook-secret"]; v != "" {
		appendEnv("WEBHOOK_SECRET", v)
	}

	// Allow all users by default — access is controlled by the gateway token
	// (API_SERVER_KEY) set in config.yaml, not user allowlists.
	appendEnv("GATEWAY_ALLOW_ALL_USERS", "true")

	// Per-agent secrets (ANTHROPIC_API_KEY, etc.)
	for name, value := range params.PerAgent {
		appendEnv(runtime.SecretNameToEnvVar(name), value)
	}

	return buf
}
