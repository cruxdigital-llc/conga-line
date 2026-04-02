package mcpserver

import (
	"context"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolSetup() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_setup",
			Description: "Run initial environment setup. Configures Docker image, Slack tokens, and shared secrets. All fields are optional — omitted fields use existing values or are skipped. Secret values are transmitted as plaintext parameters.",
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"image": map[string]any{
						"type":        "string",
						"description": "Docker image for OpenClaw containers",
					},
					"slack_bot_token": map[string]any{
						"type":        "string",
						"description": "Slack bot token (xoxb-...)",
					},
					"slack_signing_secret": map[string]any{
						"type":        "string",
						"description": "Slack signing secret",
					},
					"slack_app_token": map[string]any{
						"type":        "string",
						"description": "Slack app-level token (xapp-...) for Socket Mode",
					},
					"google_client_id": map[string]any{
						"type":        "string",
						"description": "Google OAuth client ID (optional)",
					},
					"google_client_secret": map[string]any{
						"type":        "string",
						"description": "Google OAuth client secret (optional)",
					},
					"ssh_host": map[string]any{
						"type":        "string",
						"description": "SSH hostname (remote provider only)",
					},
					"ssh_port": map[string]any{
						"type":        "integer",
						"description": "SSH port (remote provider only)",
					},
					"ssh_user": map[string]any{
						"type":        "string",
						"description": "SSH user (remote provider only)",
					},
					"ssh_key_path": map[string]any{
						"type":        "string",
						"description": "Path to SSH private key (remote provider only)",
					},
					"repo_path": map[string]any{
						"type":        "string",
						"description": "Path to conga-line repo (for behavior files)",
					},
					"install_docker": map[string]any{
						"type":        "boolean",
						"description": "Install Docker if missing (remote provider only)",
					},
				},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			secrets := map[string]string{}
			// MCP params use snake_case; secret names use kebab-case
			for _, pair := range [][2]string{
				{"slack_bot_token", "slack-bot-token"},
				{"slack_signing_secret", "slack-signing-secret"},
				{"slack_app_token", "slack-app-token"},
				{"google_client_id", "google-client-id"},
				{"google_client_secret", "google-client-secret"},
			} {
				if v := req.GetString(pair[0], ""); v != "" {
					secrets[pair[1]] = v
				}
			}
			cfg := &provider.SetupConfig{
				Image:         req.GetString("image", ""),
				Secrets:       secrets,
				SSHHost:       req.GetString("ssh_host", ""),
				SSHPort:       req.GetInt("ssh_port", 0),
				SSHUser:       req.GetString("ssh_user", ""),
				SSHKeyPath:    req.GetString("ssh_key_path", ""),
				RepoPath:      req.GetString("repo_path", ""),
				InstallDocker: req.GetBool("install_docker", false),
			}

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.Setup(ctx, cfg); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult("Setup completed."), nil
		},
	}
}

func (s *Server) toolCycleHost() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_cycle_host",
			Description: "Restart the entire deployment environment. On AWS this restarts the EC2 instance; on local/remote this restarts all containers. All agents will be briefly unavailable.",
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.CycleHost(ctx); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult("Host cycled. Agents are restarting."), nil
		},
	}
}

func (s *Server) toolTeardown() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name:        "conga_teardown",
			Description: "Remove the entire deployment environment. Deletes all containers, networks, and config. This is irreversible for local and remote providers. On AWS, use terraform destroy instead.",
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: map[string]any{},
			},
			Annotations: mcp.ToolAnnotation{
				DestructiveHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if err := s.prov.Teardown(ctx); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return okResult("Teardown complete."), nil
		},
	}
}
