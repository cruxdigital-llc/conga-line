package mcpserver

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolSetSecret() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_set_secret",
			Description: "Create or update a secret for an agent. The value is stored encrypted (AWS Secrets Manager or file mode 0400). The agent container must be refreshed for the new secret to take effect. IMPORTANT: To avoid exposing the secret in tool call logs, write the value to a temporary file and pass the file path via 'value_file' instead of using 'value' directly.",
			Annotations: mcp.ToolAnnotation{
				IdempotentHint: boolPtr(true),
			},
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
					"value_file": map[string]any{
						"type":        "string",
						"description": "Path to a temporary file containing the secret value. The file will be read and deleted. Preferred over 'value' to avoid exposing secrets in tool call logs.",
					},
					"value": map[string]any{
						"type":        "string",
						"description": "Secret value (plaintext). Prefer 'value_file' instead to avoid exposing the secret in tool call logs.",
					},
				},
				Required: []string{"agent_name", "secret_name"},
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

			// Resolve the secret value: prefer value_file over value.
			var value string
			if valueFile, _ := req.RequireString("value_file"); valueFile != "" {
				data, err := os.ReadFile(valueFile)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("failed to read value_file %q: %v", valueFile, err)), nil
				}
				value = strings.TrimRight(string(data), "\n")
				os.Remove(valueFile) // clean up temp file
			} else if v, _ := req.RequireString("value"); v != "" {
				value = v
			} else {
				return mcp.NewToolResultError("either 'value_file' or 'value' must be provided"), nil
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

			return jsonResult(secrets)
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
