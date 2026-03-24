package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolWhoAmI() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_whoami",
			Description: "Return the current caller's identity (username, AWS account, mapped agent name).",
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := toolCtx(ctx)
			defer cancel()

			id, err := s.prov.WhoAmI(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(id)
		},
	}
}

func (s *Server) toolListAgents() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_list_agents",
			Description: "List all configured agents with their type, Slack mapping, gateway port, and paused state.",
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := toolCtx(ctx)
			defer cancel()

			agents, err := s.prov.ListAgents(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(agents)
		},
	}
}

func (s *Server) toolGetAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_get_agent",
			Description: "Get a single agent's configuration by name.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
				},
				Required: []string{"name"},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name, err := req.RequireString("name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			agent, err := s.prov.GetAgent(ctx, name)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(agent)
		},
	}
}
