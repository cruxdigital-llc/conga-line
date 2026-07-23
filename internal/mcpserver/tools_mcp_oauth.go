package mcpserver

import (
	"context"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/internal/mcpoauth"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func (s *Server) toolMCPLogin() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name: "conga_mcp_login",
			Description: "Authenticate a remote-MCP OAuth server (e.g. Linear) for an agent. " +
				"Two-leg flow: call WITHOUT 'code' to start — returns an authorize_url the operator " +
				"must open in a browser (as the agent's identity); the browser then redirects to a " +
				"localhost URL that won't load, from which the operator copies the 'code' query " +
				"parameter. Call AGAIN WITH that 'code' to complete. If 'server' is omitted and the " +
				"agent has exactly one OAuth MCP server, it is used.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name",
					},
					"server": map[string]any{
						"type":        "string",
						"description": "MCP server name (optional; auto-detected if the agent has one OAuth server)",
					},
					"code": map[string]any{
						"type":        "string",
						"description": "Authorization code from the browser redirect. Omit on the first call; provide to complete.",
					},
				},
				Required: []string{"agent_name"},
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			agentName, err := req.RequireString("agent_name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			server := req.GetString("server", "")
			code := req.GetString("code", "")

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			if server == "" {
				out, err := s.prov.ContainerExec(ctx, agentName, []string{"openclaw", "mcp", "list", "--json"})
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("listing MCP servers for %s: %v", agentName, err)), nil
				}
				server, err = mcpoauth.DetectOAuthServer(out, agentName)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
			}

			// Leg 2: complete the exchange.
			if code != "" {
				if _, err := s.prov.ContainerExec(ctx, agentName,
					[]string{"openclaw", "mcp", "login", server, "--code", mcpoauth.NormalizeCode(code)}); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf(
						"completing OAuth login for %q on %s (code is single-use — call again without 'code' for a fresh URL if it expired): %v",
						server, agentName, err)), nil
				}
				return jsonResult(struct {
					Agent  string `json:"agent"`
					Server string `json:"server"`
					Status string `json:"status"`
				}{agentName, server, "authenticated"})
			}

			// Leg 1: start authorization.
			out, err := s.prov.ContainerExec(ctx, agentName, []string{"openclaw", "mcp", "login", server})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("starting OAuth login for %q on %s: %v", server, agentName, err)), nil
			}
			authURL := mcpoauth.ParseAuthorizeURL(out)
			// No URL — the server was already authenticated (idempotent login).
			if authURL == "" {
				return jsonResult(struct {
					Agent  string `json:"agent"`
					Server string `json:"server"`
					Status string `json:"status"`
				}{agentName, server, "authenticated"})
			}
			return jsonResult(struct {
				Agent        string `json:"agent"`
				Server       string `json:"server"`
				AuthorizeURL string `json:"authorize_url"`
				Status       string `json:"status"`
				Next         string `json:"next"`
			}{
				Agent:        agentName,
				Server:       server,
				AuthorizeURL: authURL,
				Status:       "authorization_pending",
				Next:         "Open authorize_url in a browser, then call conga_mcp_login again with the 'code' from the redirect.",
			})
		},
	}
}

func (s *Server) toolDoctor() server.ServerTool {
	return server.ServerTool{
		Tool: mcp.Tool{
			Name: "conga_doctor",
			Description: "Scan agents' container logs for remote-MCP servers that need OAuth " +
				"re-authentication (token expired or credential lost) and report the fix command " +
				"for each. Scans the last N log lines per agent; a server clean in that window is " +
				"not a positive health assertion. Omit 'agent_name' to scan the whole fleet.",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]any{
					"agent_name": map[string]any{
						"type":        "string",
						"description": "Agent name (optional; omit to scan all agents)",
					},
					"lines": map[string]any{
						"type":        "integer",
						"description": "Log lines to scan per agent (default: 200)",
					},
				},
			},
			Annotations: mcp.ToolAnnotation{
				ReadOnlyHint: boolPtr(true),
			},
		},
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			lines := req.GetInt("lines", 200)

			ctx, cancel := toolCtx(ctx)
			defer cancel()

			var agents []provider.AgentConfig
			if name := req.GetString("agent_name", ""); name != "" {
				ag, err := s.prov.GetAgent(ctx, name)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				agents = []provider.AgentConfig{*ag}
			} else {
				var err error
				agents, err = s.prov.ListAgents(ctx)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
			}

			findings := make([]mcpoauth.AgentFinding, 0, len(agents))
			unhealthy := 0
			for _, ag := range agents {
				logs, err := s.prov.GetLogs(ctx, ag.Name, lines)
				if err != nil {
					findings = append(findings, mcpoauth.AgentFinding{Agent: ag.Name, Error: err.Error()})
					continue
				}
				f := mcpoauth.BuildFinding(ag.Name, logs)
				if len(f.Servers) > 0 {
					unhealthy++
				}
				findings = append(findings, f)
			}

			return jsonResult(struct {
				Healthy   bool                    `json:"healthy"`
				Unhealthy int                     `json:"agents_needing_oauth"`
				Findings  []mcpoauth.AgentFinding `json:"findings"`
			}{Healthy: unhealthy == 0, Unhealthy: unhealthy, Findings: findings})
		},
	}
}
