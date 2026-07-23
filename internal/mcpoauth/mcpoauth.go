// Package mcpoauth holds the pure parsing/orchestration helpers shared by the
// `conga mcp login` and `conga doctor` surfaces (CLI + MCP server). It contains
// no provider or transport code — callers supply the container output — so the
// logic is unit-testable in isolation.
package mcpoauth

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// OAuthRequiredRe matches OpenClaw's bundle-mcp startup failure for an OAuth
// server, capturing the server name, e.g.:
//
//	[bundle-mcp] failed to start server "linear" (https://…): Error: MCP server "linear" requires OAuth authorization. Run openclaw mcp login linear.
var OAuthRequiredRe = regexp.MustCompile(`failed to start server "([^"]+)"[^\n]*requires OAuth authorization`)

// ParseAuthorizeURL extracts the first http(s) URL from `openclaw mcp login`
// output. Returns "" if none is present.
func ParseAuthorizeURL(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			return line
		}
	}
	return ""
}

// NormalizeCode is forgiving about what an operator pastes: a bare code, a
// "code=..." fragment, or the whole redirect query string (with a trailing
// "&state=..."). It also percent-decodes, since browsers show the code
// URL-encoded in the address bar.
func NormalizeCode(code string) string {
	code = strings.TrimSpace(code)
	if i := strings.Index(code, "code="); i >= 0 {
		code = code[i+len("code="):]
	}
	if i := strings.IndexByte(code, '&'); i >= 0 {
		code = code[:i]
	}
	if dec, err := url.QueryUnescape(code); err == nil {
		code = dec
	}
	return strings.TrimSpace(code)
}

// DetectOAuthServer parses `openclaw mcp list --json` (a name-keyed object whose
// values carry an "auth" field) and returns the sole OAuth server. It errors
// with an actionable message when there are zero or multiple candidates, or when
// the output can't be parsed. agentName is used only for the error text.
func DetectOAuthServer(listJSON, agentName string) (string, error) {
	var servers map[string]struct {
		Auth string `json:"auth"`
	}
	if err := json.Unmarshal([]byte(listJSON), &servers); err != nil {
		return "", fmt.Errorf("parsing MCP server list for %s — specify the server explicitly: %w", agentName, err)
	}
	var oauth []string
	for name, s := range servers {
		if strings.EqualFold(s.Auth, "oauth") {
			oauth = append(oauth, name)
		}
	}
	sort.Strings(oauth)
	switch len(oauth) {
	case 1:
		return oauth[0], nil
	case 0:
		return "", fmt.Errorf("no OAuth MCP servers configured for %s; nothing to log in to", agentName)
	default:
		return "", fmt.Errorf("%s has multiple OAuth MCP servers (%s); specify one: conga mcp login <server> --agent %s",
			agentName, strings.Join(oauth, ", "), agentName)
	}
}

// OAuthNeed is one MCP server that logged an OAuth-authorization failure, with
// the timestamp of its most recent occurrence in the scanned window (best-effort;
// empty if the log line had no recognizable leading timestamp).
type OAuthNeed struct {
	Server   string `json:"server"`
	LastSeen string `json:"last_seen,omitempty"`
}

// leadingTimestampRe matches a log line's leading ISO-8601-ish timestamp token.
var leadingTimestampRe = regexp.MustCompile(`^\s*(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}\S*)`)

// ScanOAuthNeeds returns the sorted, de-duplicated set of MCP servers that logged
// a "requires OAuth authorization" failure within the given logs, each with the
// timestamp of its latest occurrence.
//
// Important: because OpenClaw logs this on every failing turn, a stale error can
// linger in the log window after a successful re-auth. LastSeen lets a caller
// judge staleness — a credential re-authed more recently than LastSeen is already
// fixed and the error will age out of the window. This is why the scan is not a
// positive health assertion.
func ScanOAuthNeeds(logs string) []OAuthNeed {
	latest := map[string]string{} // server -> last-seen timestamp (last occurrence wins)
	for _, line := range strings.Split(logs, "\n") {
		m := OAuthRequiredRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ts := ""
		if tm := leadingTimestampRe.FindStringSubmatch(line); tm != nil {
			ts = tm[1]
		}
		latest[m[1]] = ts // iterate in log order → last write is the most recent
	}
	if len(latest) == 0 {
		return nil
	}
	servers := make([]string, 0, len(latest))
	for s := range latest {
		servers = append(servers, s)
	}
	sort.Strings(servers)
	out := make([]OAuthNeed, 0, len(servers))
	for _, s := range servers {
		out = append(out, OAuthNeed{Server: s, LastSeen: latest[s]})
	}
	return out
}

// AgentFinding is one agent's OAuth health, shaped identically for every surface
// (CLI --json and the MCP tool) so machine consumers get one contract.
type AgentFinding struct {
	Agent   string      `json:"agent"`
	Servers []OAuthNeed `json:"servers_needing_oauth,omitempty"`
	Fixes   []string    `json:"fixes,omitempty"` // one `conga mcp login …` per server
	Error   string      `json:"error,omitempty"` // set when the agent's logs couldn't be read
}

// BuildFinding assembles an agent's finding from its scanned logs, including the
// remediation command for each server needing OAuth.
func BuildFinding(agent, logs string) AgentFinding {
	needs := ScanOAuthNeeds(logs)
	f := AgentFinding{Agent: agent, Servers: needs}
	for _, n := range needs {
		f.Fixes = append(f.Fixes, fmt.Sprintf("conga mcp login %s --agent %s", n.Server, agent))
	}
	return f
}
