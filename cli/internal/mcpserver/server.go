// Package mcpserver exposes the Conga Line Provider interface as an MCP server.
package mcpserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const toolTimeout = 5 * time.Minute

// Server wraps a Provider as an MCP tool server.
type Server struct {
	prov  provider.Provider
	mcp   *server.MCPServer
	tools []server.ServerTool
}

// NewServer creates an MCP server backed by the given provider.
func NewServer(prov provider.Provider, version string) *Server {
	s := &Server{prov: prov}
	s.mcp = server.NewMCPServer(
		"conga-line",
		version,
		server.WithToolCapabilities(true),
		server.WithInstructions("Conga Line agent management. Use these tools to provision, configure, monitor, and manage OpenClaw AI agents."),
	)
	s.registerTools()
	return s
}

// Tools returns the registered MCP tools. Useful for testing.
func (s *Server) Tools() []server.ServerTool {
	return s.tools
}

// Serve blocks on stdio transport until the client disconnects.
func (s *Server) Serve() error {
	return server.ServeStdio(s.mcp)
}

// toolCtx layers a timeout on the request context.
func toolCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, toolTimeout)
}

// jsonResult marshals v as JSON and returns it as a text content result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultText(string(data)), nil
}

// okResult returns a simple success message.
func okResult(msg string) *mcp.CallToolResult {
	return mcp.NewToolResultText(msg)
}
