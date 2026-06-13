package managedhost

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	_ "github.com/cruxdigital-llc/conga-line/pkg/channels/slack"
	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

// fakeTransport is an in-memory Transport for engine unit tests: it records every
// PutFile/RunCommand/ReadFile and can inject failures. Asserting against it lets
// us test artifact generation + the seam without a real host (the testability win
// the engine exists for).
type fakeTransport struct {
	files map[string][]byte
	modes map[string]os.FileMode
	cmds  []string
	reads map[string][]byte

	failPut, failRun, failRead error
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		files: map[string][]byte{},
		modes: map[string]os.FileMode{},
		reads: map[string][]byte{},
	}
}

func (f *fakeTransport) PutFile(_ context.Context, path string, content []byte, mode os.FileMode) error {
	if f.failPut != nil {
		return f.failPut
	}
	f.files[path] = content
	f.modes[path] = mode
	return nil
}

func (f *fakeTransport) RunCommand(_ context.Context, cmd string) (string, error) {
	if f.failRun != nil {
		return "", f.failRun
	}
	f.cmds = append(f.cmds, cmd)
	return "", nil
}

func (f *fakeTransport) ReadFile(_ context.Context, path string) ([]byte, error) {
	if f.failRead != nil {
		return nil, f.failRead
	}
	return f.reads[path], nil
}

// compile-time check the fake satisfies the seam.
var _ Transport = (*fakeTransport)(nil)

func testAgents() []provider.AgentConfig {
	return []provider.AgentConfig{
		{Name: "myagent", Type: provider.AgentTypeUser, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "U0123456789"}}, GatewayPort: 18789},
		{Name: "leadership", Type: provider.AgentTypeTeam, Channels: []channels.ChannelBinding{{Platform: "slack", ID: "C9876543210"}}, GatewayPort: 18790},
	}
}

func TestWriteRoutingJSON_Loopback(t *testing.T) {
	ft := newFakeTransport()
	const path = "/opt/conga/config/routing.json"

	if err := WriteRoutingJSON(context.Background(), ft, path, testAgents(), common.LoopbackWebhookResolver("")); err != nil {
		t.Fatalf("WriteRoutingJSON() error: %v", err)
	}

	got, ok := ft.files[path]
	if !ok {
		t.Fatalf("routing.json was not written to %s; files=%v", path, keys(ft.files))
	}
	if mode := ft.modes[path]; mode != 0o644 {
		t.Errorf("routing.json mode = %v, want 0644", mode)
	}

	out := string(got)
	// Loopback topology: routes must target 127.0.0.1:<hostPort>, NOT the bridge form.
	if !strings.Contains(out, "http://127.0.0.1:18789/slack/events") {
		t.Errorf("routing.json missing loopback member route; got:\n%s", out)
	}
	if strings.Contains(out, "conga-myagent:") || strings.Contains(out, "conga-leadership:") {
		t.Errorf("routing.json contains forbidden bridge-form URL; got:\n%s", out)
	}
}

func TestWriteRoutingJSON_PutFileErrorPropagates(t *testing.T) {
	ft := newFakeTransport()
	ft.failPut = errors.New("transport down")

	err := WriteRoutingJSON(context.Background(), ft, "/x/routing.json", testAgents(), common.LoopbackWebhookResolver(""))
	if err == nil {
		t.Fatal("expected error when PutFile fails, got nil")
	}
	if !strings.Contains(err.Error(), "transport down") {
		t.Errorf("error should wrap the transport failure, got: %v", err)
	}
}

func TestExecFuncFor_RunsViaTransport(t *testing.T) {
	ft := newFakeTransport()
	exec := ExecFuncFor(context.Background(), ft)

	if err := exec("iptables -C DOCKER-USER -s 10.0.0.2 -j DROP"); err != nil {
		t.Fatalf("ExecFuncFor exec error: %v", err)
	}
	if len(ft.cmds) != 1 || !strings.Contains(ft.cmds[0], "DOCKER-USER") {
		t.Errorf("command not run through transport; cmds=%v", ft.cmds)
	}

	ft.failRun = errors.New("ssm timeout")
	if err := exec("anything"); err == nil {
		t.Error("expected RunCommand error to propagate through ExecFuncFor")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
