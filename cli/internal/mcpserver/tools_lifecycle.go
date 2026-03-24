package mcpserver

import (
	"context"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/cli/internal/common"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolProvisionAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_provision_agent",
			Description: "Create a new agent. Type must be 'user' (DM-only) or 'team' (channel-based). Slack IDs are optional — agents can run in gateway-only mode without Slack.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name (lowercase alphanumeric + hyphens)",
					},
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"user", "team"},
						"description": "Agent type: 'user' for DM-only, 'team' for channel-based",
					},
					"slack_member_id": map[string]any{
						"type":        "string",
						"description": "Slack member ID (e.g. U0123456789) for user agents",
					},
					"slack_channel": map[string]any{
						"type":        "string",
						"description": "Slack channel ID (e.g. C0123456789) for team agents",
					},
					"gateway_port": map[string]any{
						"type":        "integer",
						"description": "Gateway port (auto-assigned if omitted)",
					},
				},
				Required: []string{"agent_name", "type"},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			agentType, err := req.RequireString("type")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if agentType != "user" && agentType != "team" {
				return mcp.NewToolResultError(fmt.Sprintf("invalid agent type %q: must be \"user\" or \"team\"", agentType)), nil
			}

			gatewayPort := req.GetInt("gateway_port", 0)

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			// Auto-assign gateway port if not specified, same as CLI.
			if gatewayPort == 0 {
				agents, err := s.prov.ListAgents(ctx)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to auto-assign port: %v", err)), nil
				}
				gatewayPort = common.NextAvailablePort(agents)
			}

			cfg := provider.AgentConfig{
				Name:          agentName,
				Type:          provider.AgentType(agentType),
				SlackMemberID: req.GetString("slack_member_id", ""),
				SlackChannel:  req.GetString("slack_channel", ""),
				GatewayPort:   gatewayPort,
			}

			if err := s.prov.ProvisionAgent(ctx, cfg); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q provisioned successfully.", agentName)), nil
		},
	}
}

func (s *Server) toolRemoveAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_remove_agent",
			Description: "Remove an agent. Stops the container, removes network and config. This is destructive and cannot be undone.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name to remove",
					},
					"delete_secrets": map[string]any{
						"type":        "boolean",
						"description": "Also delete the agent's secrets (default: false)",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			deleteSecrets := req.GetBool("delete_secrets", false)

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.RemoveAgent(ctx, agentName, deleteSecrets); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q removed.", agentName)), nil
		},
	}
}

func (s *Server) toolPauseAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_pause_agent",
			Description: "Pause an agent. Stops the container and removes it from routing. Config, secrets, and data are preserved.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name to pause",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				IdempotentHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.PauseAgent(ctx, agentName); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q paused.", agentName)), nil
		},
	}
}

func (s *Server) toolUnpauseAgent() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_unpause_agent",
			Description: "Unpause a previously paused agent. Restarts the container and restores routing.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name to unpause",
					},
				},
				Required: []string{"agent_name"},
			},
			Annotations: mcp.ToolAnnotation{
				IdempotentHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.UnpauseAgent(ctx, agentName); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q unpaused.", agentName)), nil
		},
	}
}

func boolPtr(b bool) *bool { return &b }
