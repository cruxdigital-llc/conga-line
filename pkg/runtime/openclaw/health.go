package openclaw

import (
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

func (r *Runtime) HealthEndpoint() string { return "" }

func (r *Runtime) DetectReady(logOutput string, hasSlack bool) runtime.ReadyPhase {
	phase := runtime.ReadyPhase{Phase: "starting", Message: "Container starting"}

	if strings.Contains(logOutput, "[gateway] listening") {
		phase = runtime.ReadyPhase{Phase: "gateway_up", Message: "Gateway up, waiting for plugins"}
	}

	if hasSlack {
		if strings.Contains(logOutput, "[slack]") && strings.Contains(logOutput, "starting provider") {
			phase = runtime.ReadyPhase{Phase: "loading", Message: "Slack plugin loading"}
		}
		if strings.Contains(logOutput, "[slack] http mode listening") {
			phase = runtime.ReadyPhase{Phase: "loading", Message: "Slack endpoint ready, resolving channels"}
		}
		if strings.Contains(logOutput, "[slack] channels resolved") {
			phase = runtime.ReadyPhase{Phase: "ready", Message: "Ready", IsReady: true}
		}
	} else {
		// Gateway-only mode — gateway listening = ready
		if strings.Contains(logOutput, "[gateway] listening") {
			phase = runtime.ReadyPhase{Phase: "ready", Message: "Ready (gateway only)", IsReady: true}
		}
	}

	lower := strings.ToLower(logOutput)
	if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") {
		phase.HasError = true
		phase.Message += " (errors in logs — check `conga logs`)"
	}

	return phase
}
