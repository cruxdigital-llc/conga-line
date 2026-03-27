package mcpserver_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/cli/internal/mcpserver"
	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcptest"
)

func newChannelTestClient(t *testing.T) *client.Client {
	t.Helper()
	mock := &mockProvider{name: "local"}
	srv := mcpserver.NewServer(mock, "test")
	testSrv, err := mcptest.NewServer(t, srv.Tools()...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testSrv.Close() })
	return testSrv.Client()
}

func TestToolChannelsAdd(t *testing.T) {
	c := newChannelTestClient(t)
	result := callTool(t, c, "conga_channels_add", map[string]any{
		"platform":             "slack",
		"slack_bot_token":      "xoxb-test",
		"slack_signing_secret": "test-secret",
		"slack_app_token":      "xapp-test",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}
	if text := textContent(t, result); !strings.Contains(text, "configured") {
		t.Errorf("expected 'configured' in result, got: %s", text)
	}
}

func TestToolChannelsRemove(t *testing.T) {
	c := newChannelTestClient(t)
	result := callTool(t, c, "conga_channels_remove", map[string]any{
		"platform": "slack",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}
	if text := textContent(t, result); !strings.Contains(text, "removed") {
		t.Errorf("expected 'removed' in result, got: %s", text)
	}
}

func TestToolChannelsList(t *testing.T) {
	c := newChannelTestClient(t)
	result := callTool(t, c, "conga_channels_list", nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}
	text := textContent(t, result)
	var statuses []provider.ChannelStatus
	if err := json.Unmarshal([]byte(text), &statuses); err != nil {
		if text != "null" {
			t.Errorf("expected valid JSON, got: %s", text)
		}
	}
}

func TestToolChannelsBind(t *testing.T) {
	c := newChannelTestClient(t)
	result := callTool(t, c, "conga_channels_bind", map[string]any{
		"agent_name": "aaron",
		"channel":    "slack:U0123456789",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}
	if text := textContent(t, result); !strings.Contains(text, "bound") {
		t.Errorf("expected 'bound' in result, got: %s", text)
	}
}

func TestToolChannelsBindInvalidFormat(t *testing.T) {
	c := newChannelTestClient(t)
	result := callTool(t, c, "conga_channels_bind", map[string]any{
		"agent_name": "aaron",
		"channel":    "invalid-format",
	})
	if !result.IsError {
		t.Fatalf("expected error for invalid channel format, got: %s", textContent(t, result))
	}
}

func TestToolChannelsUnbind(t *testing.T) {
	c := newChannelTestClient(t)
	result := callTool(t, c, "conga_channels_unbind", map[string]any{
		"agent_name": "aaron",
		"platform":   "slack",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", textContent(t, result))
	}
	if text := textContent(t, result); !strings.Contains(text, "unbound") {
		t.Errorf("expected 'unbound' in result, got: %s", text)
	}
}
