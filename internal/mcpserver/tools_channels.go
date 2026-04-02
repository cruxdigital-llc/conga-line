package mcpserver

import (
	"context"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolChannelsAdd() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_channels_add",
			Description: "Add a messaging channel integration. Currently supports Slack only. Stores shared credentials and starts the router.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"platform": map[string]any{
						"type":        "string",
						"description": "Channel platform (currently only 'slack')",
					},
					"slack_bot_token": map[string]any{
						"type":        "string",
						"description": "Slack bot token (xoxb-..., required for Slack)",
					},
					"slack_signing_secret": map[string]any{
						"type":        "string",
						"description": "Slack signing secret (required for Slack)",
					},
					"slack_app_token": map[string]any{
						"type":        "string",
						"description": "Slack app-level token (xapp-..., optional)",
					},
				},
				Required: []string{"platform"},
			},
			Annotations: mcp.ToolAnnotation{
				IdempotentHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			platform, err := req.RequireString("platform")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			// Map MCP parameter names to secret names
			secrets := map[string]string{}
			paramToSecret := map[string]string{
				"slack_bot_token":      "slack-bot-token",
				"slack_signing_secret": "slack-signing-secret",
				"slack_app_token":      "slack-app-token",
			}
			for param, secret := range paramToSecret {
				if v := req.GetString(param, ""); v != "" {
					secrets[secret] = v
				}
			}

			if err := s.prov.AddChannel(ctx, platform, secrets); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Channel %q configured and router started.", platform)), nil
		},
	}
}

func (s *Server) toolChannelsRemove() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_channels_remove",
			Description: "Remove a messaging channel integration. Stops the router, removes all agent bindings, and deletes credentials.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"platform": map[string]any{
						"type":        "string",
						"description": "Channel platform to remove (e.g., 'slack')",
					},
				},
				Required: []string{"platform"},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			platform, err := req.RequireString("platform")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.RemoveChannel(ctx, platform); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Channel %q removed.", platform)), nil
		},
	}
}

func (s *Server) toolChannelsList() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_channels_list",
			Description: "List configured channels and their status (credentials present, router running, bound agents).",
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := toolCtx(ctx)
			defer cancel()

			statuses, err := s.prov.ListChannels(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(statuses)
		},
	}
}

func (s *Server) toolChannelsBind() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_channels_bind",
			Description: "Bind an agent to a channel. The channel must be configured first via conga_channels_add.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"channel": map[string]any{
						"type":        "string",
						"description": "Channel binding (format: platform:id, e.g., slack:U0123456789)",
					},
				},
				Required: []string{"agent_name", "channel"},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			chStr, err := req.RequireString("channel")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			binding, err := channels.ParseBinding(chStr)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.BindChannel(ctx, agentName, binding); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q bound to %s:%s.", agentName, binding.Platform, binding.ID)), nil
		},
	}
}

func (s *Server) toolChannelsUnbind() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_channels_unbind",
			Description: "Remove a channel binding from an agent.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"platform": map[string]any{
						"type":        "string",
						"description": "Channel platform to unbind (e.g., 'slack')",
					},
				},
				Required: []string{"agent_name", "platform"},
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
			platform, err := req.RequireString("platform")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.UnbindChannel(ctx, agentName, platform); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Agent %q unbound from %s.", agentName, platform)), nil
		},
	}
}
