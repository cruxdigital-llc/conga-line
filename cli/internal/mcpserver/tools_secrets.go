package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolSetSecret() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_set_secret",
			Description: "Create or update a secret for an agent. The value is stored encrypted (AWS Secrets Manager or file mode 0400). The agent container must be refreshed for the new secret to take effect.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"secret_name": map[string]any{
						"type":        "string",
						"description": "Secret name (e.g. 'anthropic-api-key')",
					},
					"value": map[string]any{
						"type":        "string",
						"description": "Secret value (plaintext — will be stored encrypted)",
					},
				},
				Required: []string{"agent_name", "secret_name", "value"},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			secretName, err := req.RequireString("secret_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			value, err := req.RequireString("value")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.SetSecret(ctx, agentName, secretName, value); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Secret %q set for agent %q. Refresh the agent to apply.", secretName, agentName)), nil
		},
	}
}

func (s *Server) toolListSecrets() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_list_secrets",
			Description: "List all secrets for an agent. Returns names and metadata, not values.",
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

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			secrets, err := s.prov.ListSecrets(ctx, agentName)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			// SecretEntry lacks JSON tags, build response manually.
			entries := make([]map[string]any, len(secrets))
			for i, s := range secrets {
				entries[i] = map[string]any{
					"name":         s.Name,
					"env_var":      s.EnvVar,
					"path":         s.Path,
					"last_changed": s.LastChanged.Format("2006-01-02T15:04:05Z"),
				}
			}
			return jsonResult(entries)
		},
	}
}

func (s *Server) toolDeleteSecret() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_delete_secret",
			Description: "Delete a secret for an agent. The agent container must be refreshed for the change to take effect.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"secret_name": map[string]any{
						"type":        "string",
						"description": "Secret name to delete",
					},
				},
				Required: []string{"agent_name", "secret_name"},
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
			secretName, err := req.RequireString("secret_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.DeleteSecret(ctx, agentName, secretName); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult(fmt.Sprintf("Secret %q deleted for agent %q. Refresh the agent to apply.", secretName, agentName)), nil
		},
	}
}
