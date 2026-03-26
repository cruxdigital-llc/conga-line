package mcpserver

import "github.com/mark3labs/mcp-go/server"

func (s *Server) registerTools() {
	// Connect() is not directly exposed — it requires a long-lived tunnel. Instead,
	// conga_connect_help tells the AI agent how to instruct the user to run it.
	// ResolveAgentByIdentity() is omitted — MCP callers specify agent names explicitly;
	// WhoAmI() already returns the mapped agent name.

	s.tools = []server.ServerTool{
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
		s.toolConnectHelp(),

		// Secrets
		s.toolSetSecret(),
		s.toolListSecrets(),
		s.toolDeleteSecret(),

		// Environment Management
		s.toolSetup(),
		s.toolCycleHost(),
		s.toolTeardown(),

		// Policy
		s.toolPolicyGet(),
		s.toolPolicyValidate(),
		s.toolPolicyGetAgent(),
		s.toolPolicySetEgress(),
		s.toolPolicySetRouting(),
		s.toolPolicySetPosture(),
		s.toolPolicyDeploy(),
	}
	s.mcp.AddTools(s.tools...)
}
