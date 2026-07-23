package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/cruxdigital-llc/conga-line/internal/mcpoauth"
	"github.com/cruxdigital-llc/conga-line/pkg/ui"
	"github.com/spf13/cobra"
)

var mcpLoginCode string

func init() {
	mcpLoginCmd := &cobra.Command{
		Use:   "login [server]",
		Short: "Authenticate a remote-MCP OAuth server for an agent",
		Long: `Complete the OAuth authorization for a remote-MCP server (e.g. Linear) inside
an agent's container.

The flow has two legs. Run without --code to start authorization: it prints a URL
you open in a browser as the agent's identity. After you approve, the browser
redirects to a localhost URL that will not load — copy the "code" query parameter
from the address bar and either paste it at the prompt or re-run with --code.

If [server] is omitted and the agent has exactly one OAuth MCP server, it is used.`,
		Args: cobra.MaximumNArgs(1),
		RunE: mcpLoginRun,
	}
	mcpLoginCmd.Flags().StringVar(&mcpLoginCode, "code", "", "Authorization code from the browser redirect (completes the login)")
	mcpCmd.AddCommand(mcpLoginCmd)
}

func mcpLoginRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := commandContext()
	defer cancel()

	agentName, err := resolveAgentName(ctx)
	if err != nil {
		return err
	}

	// Resolve the server: positional arg, JSON input, or auto-detect the sole OAuth server.
	server := ""
	if len(args) > 0 {
		server = args[0]
	} else if ui.JSONInputActive {
		server, _ = ui.GetString("server")
	}
	if server == "" {
		out, err := prov.ContainerExec(ctx, agentName, []string{"openclaw", "mcp", "list", "--json"})
		if err != nil {
			return fmt.Errorf("listing MCP servers for %s: %w", agentName, err)
		}
		server, err = mcpoauth.DetectOAuthServer(out, agentName)
		if err != nil {
			return err
		}
	}

	// The authorization code may arrive via --code or JSON input.
	code := mcpLoginCode
	if code == "" && ui.JSONInputActive {
		code, _ = ui.GetString("code")
	}

	// Leg 2: we already have a code — complete the exchange and finish.
	if code != "" {
		return mcpLoginComplete(ctx, agentName, server, code)
	}

	// Leg 1: start authorization and surface the URL.
	out, err := prov.ContainerExec(ctx, agentName, []string{"openclaw", "mcp", "login", server})
	if err != nil {
		return fmt.Errorf("starting OAuth login for %q on %s: %w", server, agentName, err)
	}
	authURL := mcpoauth.ParseAuthorizeURL(out)

	// No URL means OpenClaw completed without a browser leg — the server was
	// already authenticated (the login is idempotent). Report success rather
	// than claim a second leg is pending.
	if authURL == "" {
		if ui.OutputJSON {
			ui.EmitJSON(struct {
				Agent   string `json:"agent"`
				Server  string `json:"server"`
				Status  string `json:"status"`
				Message string `json:"message,omitempty"`
			}{Agent: agentName, Server: server, Status: "authenticated", Message: strings.TrimSpace(out)})
			return nil
		}
		if msg := strings.TrimSpace(out); msg != "" {
			fmt.Println(msg)
		}
		fmt.Printf("✓ %q on %s is already authenticated; nothing to do.\n", server, agentName)
		return nil
	}

	if ui.OutputJSON {
		ui.EmitJSON(struct {
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
			Next:         fmt.Sprintf("conga mcp login %s --agent %s --code <code>", server, agentName),
		})
		return nil
	}

	fmt.Printf("Open this URL in a browser to authorize %q (sign in as the agent's identity):\n\n  %s\n\n", server, authURL)
	fmt.Println("After approving, your browser redirects to a localhost URL that will not load —")
	fmt.Println("copy the \"code\" query parameter from the address bar.")

	// Interactive: prompt for the code and complete inline.
	entered, err := ui.TextPrompt("Paste the authorization code (or the whole redirect URL)")
	if err != nil {
		return err
	}
	if mcpoauth.NormalizeCode(entered) == "" {
		fmt.Printf("No code entered. Re-run to finish:\n  conga mcp login %s --agent %s --code <code>\n", server, agentName)
		return nil
	}
	return mcpLoginComplete(ctx, agentName, server, entered)
}

// mcpLoginComplete runs the token-exchange leg with the given (raw) code.
func mcpLoginComplete(ctx context.Context, agentName, server, rawCode string) error {
	code := mcpoauth.NormalizeCode(rawCode)
	if code == "" {
		return fmt.Errorf("authorization code is empty")
	}
	out, err := prov.ContainerExec(ctx, agentName, []string{"openclaw", "mcp", "login", server, "--code", code})
	if err != nil {
		return fmt.Errorf("completing OAuth login for %q on %s (the code is single-use — re-run without --code for a fresh URL if it expired): %w", server, agentName, err)
	}
	if ui.OutputJSON {
		ui.EmitJSON(struct {
			Agent  string `json:"agent"`
			Server string `json:"server"`
			Status string `json:"status"`
		}{Agent: agentName, Server: server, Status: "authenticated"})
		return nil
	}
	if msg := strings.TrimSpace(out); msg != "" {
		fmt.Println(msg)
	}
	fmt.Printf("✓ OAuth login complete for %q on %s. It connects on the agent's next turn.\n", server, agentName)
	return nil
}
