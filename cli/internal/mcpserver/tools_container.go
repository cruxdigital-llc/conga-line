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

			// Marshal with human-friendly field names since AgentStatus lacks JSON tags.
			result := map[string]any{
				"agent_name":    status.AgentName,
				"service_state": status.ServiceState,
				"ready_phase":   status.ReadyPhase,
				"errors":        status.Errors,
				"container": map[string]any{
					"state":         status.Container.State,
					"uptime":        status.Container.Uptime.String(),
					"started_at":    status.Container.StartedAt,
					"restart_count": status.Container.RestartCount,
					"memory_usage":  status.Container.MemoryUsage,
					"cpu_percent":   status.Container.CPUPercent,
					"pids":          status.Container.PIDs,
				},
			}
			return jsonResult(result)
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
