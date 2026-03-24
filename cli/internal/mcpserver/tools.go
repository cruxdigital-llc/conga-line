package mcpserver

func (s *Server) registerTools() {
	s.mcp.AddTools(
		// Identity & Discovery
		s.toolWhoAmI(),
		s.toolListAgents(),
		s.toolGetAgent(),

		// Agent Lifecycle
		s.toolProvisionAgent(),
		s.toolRemoveAgent(),
		s.toolPauseAgent(),
		s.toolUnpauseAgent(),

		// Container Operations
		s.toolGetStatus(),
		s.toolGetLogs(),
		s.toolRefreshAgent(),
		s.toolRefreshAll(),
		s.toolContainerExec(),

		// Secrets
		s.toolSetSecret(),
		s.toolListSecrets(),
		s.toolDeleteSecret(),

		// Environment Management
		s.toolSetup(),
		s.toolCycleHost(),
		s.toolTeardown(),
	)
}
