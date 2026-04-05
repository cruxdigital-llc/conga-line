package hermes

import (
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

func (r *Runtime) HealthEndpoint() string { return "/health" }

func (r *Runtime) DetectReady(logOutput string, hasSlack bool) runtime.ReadyPhase {
	phase := runtime.ReadyPhase{Phase: "starting", Message: "Container starting"}

	// Hermes logs to files (logs/gateway.log), not stdout.
	// Docker logs (logOutput) may be empty. Check both the docker logs
	// and known Hermes log markers.
	if strings.Contains(logOutput, "API server listening on") ||
		strings.Contains(logOutput, "api_server connected") {
		phase = runtime.ReadyPhase{Phase: "gateway_up", Message: "API server up"}
	}
	if strings.Contains(logOutput, "Gateway running with") {
		phase = runtime.ReadyPhase{Phase: "ready", Message: "Ready", IsReady: true}
	}

	// If docker logs are empty but the container is running, assume it's
	// starting (Hermes writes to log files, not stdout).
	// The caller (GetStatus) can also check the /health endpoint directly.

	lower := strings.ToLower(logOutput)
	if strings.Contains(lower, "error") || strings.Contains(lower, "traceback") {
		phase.HasError = true
		phase.Message += " (errors in logs — check `conga logs`)"
	}

	return phase
}
