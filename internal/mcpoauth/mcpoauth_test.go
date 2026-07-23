package mcpoauth

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseAuthorizeURL(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "typical login output",
			out: "Open this URL to authorize \"linear\":\n" +
				"https://mcp.linear.app/authorize?response_type=code&client_id=abc&state=xyz\n" +
				"After approval, run openclaw mcp login linear --code <code>.",
			want: "https://mcp.linear.app/authorize?response_type=code&client_id=abc&state=xyz",
		},
		{name: "http url", out: "go here:\n  http://localhost:8989/x", want: "http://localhost:8989/x"},
		{name: "no url", out: "MCP server already authorized.", want: ""},
		{name: "empty", out: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseAuthorizeURL(tt.out); got != tt.want {
				t.Errorf("ParseAuthorizeURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeCode(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "bare code", in: "abc123", want: "abc123"},
		{name: "trims whitespace", in: "  abc123\n", want: "abc123"},
		{
			// Fabricated example of Linear's colon-delimited code format (uuid:seg:seg),
			// percent-encoded as a browser address bar shows it. Not a real code.
			name: "percent-encoded colons (Linear format)",
			in:   "11111111-2222%3AAAAAsegOne%3ABBBB-seg2",
			want: "11111111-2222:AAAAsegOne:BBBB-seg2",
		},
		{
			name: "whole redirect query string with trailing state",
			in:   "11111111%3AAAAA%3ABBBB&state=33333333-4444",
			want: "11111111:AAAA:BBBB",
		},
		{
			name: "code= prefix from pasted fragment",
			in:   "code=abc%3Adef&state=zzz",
			want: "abc:def",
		},
		{
			name: "full URL pasted",
			in:   "http://127.0.0.1:8989/oauth/callback?code=abc%3Adef&state=zzz",
			want: "abc:def",
		},
		{name: "empty", in: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeCode(tt.in); got != tt.want {
				t.Errorf("NormalizeCode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDetectOAuthServer(t *testing.T) {
	tests := []struct {
		name      string
		listJSON  string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "single oauth server",
			listJSON: `{"linear":{"url":"https://mcp.linear.app/mcp","transport":"streamable-http","auth":"oauth"}}`,
			want:     "linear",
		},
		{
			name: "oauth plus static-header server picks the oauth one",
			listJSON: `{"linear":{"url":"https://mcp.linear.app/mcp","auth":"oauth"},` +
				`"github":{"url":"https://api.githubcopilot.com/mcp/","transport":"streamable-http"}}`,
			want: "linear",
		},
		{
			name:      "no oauth servers",
			listJSON:  `{"github":{"url":"https://api.githubcopilot.com/mcp/"}}`,
			wantErr:   true,
			errSubstr: "no OAuth MCP servers",
		},
		{
			name:      "multiple oauth servers is ambiguous",
			listJSON:  `{"linear":{"auth":"oauth"},"asana":{"auth":"oauth"}}`,
			wantErr:   true,
			errSubstr: "multiple OAuth MCP servers (asana, linear)",
		},
		{
			name:      "unparseable output",
			listJSON:  "not json at all",
			wantErr:   true,
			errSubstr: "specify the server explicitly",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectOAuthServer(tt.listJSON, "myagent")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result %q)", got)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("DetectOAuthServer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScanOAuthNeeds(t *testing.T) {
	// %[1]s is the timestamp, %[2]s the server name (used three times).
	const line = `%[1]s [bundle-mcp] failed to start server "%[2]s" (https://x): Error: MCP server "%[2]s" requires OAuth authorization. Run openclaw mcp login %[2]s.`

	tests := []struct {
		name string
		logs string
		want []OAuthNeed
	}{
		{
			name: "single server; latest occurrence's timestamp wins",
			logs: mkline(line, "2026-07-17T16:06:01.860+00:00", "linear") + "\nunrelated log\n" +
				mkline(line, "2026-07-22T14:38:54.062+00:00", "linear"),
			want: []OAuthNeed{{Server: "linear", LastSeen: "2026-07-22T14:38:54.062+00:00"}},
		},
		{
			name: "multiple distinct servers sorted by name",
			logs: mkline(line, "2026-07-22T10:00:00Z", "linear") + "\n" +
				mkline(line, "2026-07-22T11:00:00Z", "github"),
			want: []OAuthNeed{
				{Server: "github", LastSeen: "2026-07-22T11:00:00Z"},
				{Server: "linear", LastSeen: "2026-07-22T10:00:00Z"},
			},
		},
		{
			name: "match without a leading timestamp still reported (empty LastSeen)",
			logs: `[bundle-mcp] failed to start server "linear" (https://x): Error: MCP server "linear" requires OAuth authorization.`,
			want: []OAuthNeed{{Server: "linear", LastSeen: ""}},
		},
		{
			name: "no oauth errors",
			logs: "gateway ready\nslack http mode listening\n",
			want: nil,
		},
		{
			name: "does not match unrelated bundle-mcp failures",
			logs: `2026-07-22T10:00:00Z [bundle-mcp] failed to start server "linear" (https://x): Error: connection refused`,
			want: nil,
		},
		{name: "empty logs", logs: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScanOAuthNeeds(tt.logs)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ScanOAuthNeeds() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBuildFinding(t *testing.T) {
	const line = `%[1]s [bundle-mcp] failed to start server "%[2]s" (https://x): Error: MCP server "%[2]s" requires OAuth authorization. Run openclaw mcp login %[2]s.`

	t.Run("populates servers and matching fix commands", func(t *testing.T) {
		logs := mkline(line, "2026-07-22T10:00:00Z", "linear") + "\n" + mkline(line, "2026-07-22T11:00:00Z", "asana")
		got := BuildFinding("myagent", logs)
		if got.Agent != "myagent" {
			t.Errorf("Agent = %q, want myagent", got.Agent)
		}
		wantFixes := []string{
			"conga mcp login asana --agent myagent",
			"conga mcp login linear --agent myagent",
		}
		if !reflect.DeepEqual(got.Fixes, wantFixes) {
			t.Errorf("Fixes = %v, want %v", got.Fixes, wantFixes)
		}
		if len(got.Servers) != 2 {
			t.Errorf("Servers len = %d, want 2", len(got.Servers))
		}
	})

	t.Run("clean agent has no servers and no fixes", func(t *testing.T) {
		got := BuildFinding("myagent", "gateway ready\n")
		if got.Servers != nil || got.Fixes != nil {
			t.Errorf("expected empty servers/fixes, got servers=%v fixes=%v", got.Servers, got.Fixes)
		}
	})
}

// mkline formats the failure-line template for a timestamp + server name.
func mkline(tmpl, ts, server string) string {
	return fmt.Sprintf(tmpl, ts, server)
}
