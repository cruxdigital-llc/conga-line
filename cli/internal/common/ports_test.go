package common

import (
	"testing"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

func TestNextAvailablePort(t *testing.T) {
	tests := []struct {
		name   string
		agents []provider.AgentConfig
		want   int
	}{
		{"no agents", nil, BaseGatewayPort},
		{"one agent", []provider.AgentConfig{{GatewayPort: 18789}}, 18790},
		{"two agents", []provider.AgentConfig{{GatewayPort: 18789}, {GatewayPort: 18791}}, 18792},
		{"non-sequential", []provider.AgentConfig{{GatewayPort: 18800}}, 18801},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextAvailablePort(tt.agents)
			if got != tt.want {
				t.Errorf("NextAvailablePort() = %d, want %d", got, tt.want)
			}
		})
	}
}
