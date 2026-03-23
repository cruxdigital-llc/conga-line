package remoteprovider

import "testing"

func TestDetectReadyPhase(t *testing.T) {
	tests := []struct {
		name     string
		logs     string
		expected string
	}{
		{"empty logs", "", "starting"},
		{"gateway up", "[gateway] listening on port 18789", "gateway up, waiting for plugins"},
		{"slack loading", "[gateway] listening\n[slack] starting provider", "slack plugin loading"},
		{"slack http ready", "[gateway] listening\n[slack] http mode listening", "slack endpoint ready, resolving channels"},
		{"fully ready", "[gateway] listening\n[slack] channels resolved", "ready"},
		{"ready with errors", "[gateway] listening\n[slack] channels resolved\nError: something failed", "ready (errors in logs — check `conga logs`)"},
		{"starting with fatal", "fatal: cannot start", "starting (errors in logs — check `conga logs`)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectReadyPhase(tt.logs)
			if got != tt.expected {
				t.Errorf("detectReadyPhase() = %q, want %q", got, tt.expected)
			}
		})
	}
}
