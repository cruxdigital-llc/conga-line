package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolGetStatus() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_get_status",
			Description: "Get an agent's container status: state, uptime, memory/CPU usage, readiness phase, and any errors.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			status, err := s.prov.GetStatus(ctx, name)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return jsonResult(status)
		},
	}
}

func (s *Server) toolGetLogs() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_get_logs",
			Description: "Get the last N lines of an agent's container logs.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"lines": map[string]any{
						"type":        "integer",
						"description": "Number of log lines to return (default: 50)",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			lines := req.GetInt("lines", 50)

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			logs, err := s.prov.GetLogs(ctx, name, lines)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(logs), nil
		},
	}
}

func (s *Server) toolGetProxyLogs() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_get_proxy_logs",
			Description: "Get the last N lines of an agent's egress proxy container logs. Shows domain filtering activity: in validate mode, would-be-denied requests appear as 'egress-validate: would deny <host>' via Lua logWarn. In enforce mode, blocked requests receive a 403 from the Lua filter and appear in Envoy's access log.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"lines": map[string]any{
						"type":        "integer",
						"description": "Number of log lines to return (default: 50)",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			lines := req.GetInt("lines", 50)

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			logs, err := s.prov.GetLogs(ctx, "egress-"+name, lines)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(logs), nil
		},
	}
}

func (s *Server) toolRefreshAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_refresh_agent",
			Description: "Restart an agent's container with fresh secrets and config. Use after updating secrets or to clear cached errors.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				IdempotentHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.RefreshAgent(ctx, name); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q refreshed.", name)), nil
		},
	}
}

func (s *Server) toolRefreshAll() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_refresh_all",
			Description: "Restart all agent containers with fresh secrets and config.",
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.RefreshAll(ctx); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult("All agents refreshed."), nil
		},
	}
}

func (s *Server) toolConnectHelp() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_connect_help",
			Description: "Explains how to open the web UI for an agent. The connect operation requires a long-lived tunnel that cannot run inside an MCP tool call — use this tool to get the command the user should run in their terminal.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			msg := fmt.Sprintf(
				"The connect operation opens a long-lived tunnel to the agent's web UI.\n"+
					"This cannot be done via MCP — ask the user to run this in their terminal:\n\n"+
					"  conga connect --agent %s\n\n"+
					"This will open the OpenClaw gateway UI in their browser. "+
					"On AWS, it creates an SSM port-forwarding session. "+
					"On local, it binds to localhost directly.",
				agentName,
			)
			return mcp.NewToolResultText(msg), nil
		},
	}
}

func (s *Server) toolContainerExec() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_container_exec",
			Description: "Run a command inside an agent's container and return stdout. The command runs inside the Docker container, not on the host.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"command": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Command and arguments (e.g. [\"node\", \"-e\", \"console.log('hi')\"])",
					},
				},
				Required: []string{"agent_name", "command"},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			command, err := req.RequireStringSlice("command")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			output, err := s.prov.ContainerExec(ctx, name, command)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(output), nil
		},
	}
}
