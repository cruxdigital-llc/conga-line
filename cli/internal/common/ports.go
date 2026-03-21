package common

import "github.com/cruxdigital-llc/conga-line/cli/internal/provider"

// BaseGatewayPort is the starting port for agent gateway assignment.
const BaseGatewayPort = 18789

// NextAvailablePort returns the next unused gateway port based on existing agents.
func NextAvailablePort(agents []provider.AgentConfig) int {
	maxPort := BaseGatewayPort - 1
	for _, a := range agents {
		if a.GatewayPort > maxPort {
			maxPort = a.GatewayPort
		}
	}
	return maxPort + 1
}
