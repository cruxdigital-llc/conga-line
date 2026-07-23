package common

import (
	"context"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"

	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/hermes"
	_ "github.com/cruxdigital-llc/conga-line/pkg/runtime/openclaw"
)

// fakeProvider implements only the Provider methods CaptureMCPOAuth uses; the
// embedded interface panics on anything else (nothing else should be called).
type fakeProvider struct {
	provider.Provider
	files   map[string]string // container path -> file content
	listing string            // `ls -1` output for the state dir
	setCall map[string]string // secretName -> value (records SetSecret)
	execLog []string
}

func (f *fakeProvider) ContainerExec(_ context.Context, _ string, cmd []string) (string, error) {
	f.execLog = append(f.execLog, strings.Join(cmd, " "))
	if len(cmd) >= 1 && cmd[0] == "cat" {
		return f.files[cmd[len(cmd)-1]], nil
	}
	// the `sh -c "ls -1 ..."` listing call
	return f.listing, nil
}

func (f *fakeProvider) SetSecret(_ context.Context, _ string, name, value string) error {
	if f.setCall == nil {
		f.setCall = map[string]string{}
	}
	f.setCall[name] = value
	return nil
}

func mustRuntime(t *testing.T, name runtime.RuntimeName) runtime.Runtime {
	t.Helper()
	rt, err := runtime.Get(name)
	if err != nil {
		t.Fatalf("Get(%q): %v", name, err)
	}
	return rt
}

func TestCaptureMCPOAuth(t *testing.T) {
	oc := mustRuntime(t, runtime.RuntimeOpenClaw)
	base := oc.ContainerDataPath() + "/mcp-oauth/"

	t.Run("captures each json blob under the mcp-oauth prefix", func(t *testing.T) {
		linear := `{"tokens":{"refresh_token":"r1"}}`
		github := `{"tokens":{"refresh_token":"r2"}}`
		fp := &fakeProvider{
			listing: "linear-4cca6302a658efcc.json\ngithub-336ff6f3750dcf7c.json\n",
			files: map[string]string{
				base + "linear-4cca6302a658efcc.json": linear,
				base + "github-336ff6f3750dcf7c.json": github,
			},
		}
		n, err := CaptureMCPOAuth(context.Background(), fp, oc, "team-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 2 {
			t.Fatalf("captured = %d, want 2", n)
		}
		if got := fp.setCall["mcp-oauth/linear-4cca6302a658efcc.json"]; got != linear {
			t.Errorf("linear secret = %q, want %q", got, linear)
		}
		if got := fp.setCall["mcp-oauth/github-336ff6f3750dcf7c.json"]; got != github {
			t.Errorf("github secret = %q, want %q", got, github)
		}
	})

	t.Run("empty state dir is a no-op", func(t *testing.T) {
		fp := &fakeProvider{listing: "\n"}
		n, err := CaptureMCPOAuth(context.Background(), fp, oc, "team-a")
		if err != nil || n != 0 {
			t.Fatalf("got (n=%d, err=%v), want (0, nil)", n, err)
		}
		if len(fp.setCall) != 0 {
			t.Errorf("no secrets should be set, got %v", fp.setCall)
		}
	})

	t.Run("ignores non-json entries", func(t *testing.T) {
		fp := &fakeProvider{
			listing: "linear-abc.json\nREADME.txt\n.keep\n",
			files:   map[string]string{base + "linear-abc.json": "{}"},
		}
		n, err := CaptureMCPOAuth(context.Background(), fp, oc, "team-a")
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 || len(fp.setCall) != 1 {
			t.Errorf("captured=%d secrets=%d, want 1/1", n, len(fp.setCall))
		}
	})

	t.Run("runtime without OAuth state dir is a no-op (no exec)", func(t *testing.T) {
		hermes := mustRuntime(t, runtime.RuntimeHermes)
		fp := &fakeProvider{}
		n, err := CaptureMCPOAuth(context.Background(), fp, hermes, "team-a")
		if err != nil || n != 0 {
			t.Fatalf("got (n=%d, err=%v), want (0, nil)", n, err)
		}
		if len(fp.execLog) != 0 {
			t.Errorf("expected no ContainerExec for a runtime without OAuth state, got %v", fp.execLog)
		}
	})
}

func TestRestoreMCPOAuth(t *testing.T) {
	secrets := map[string]string{
		"anthropic-api-key":                      "sk-ignored",
		"mcp-oauth/linear-4cca6302a658efcc.json": `{"tokens":{"refresh_token":"r1"}}`,
		"mcp-oauth/github-336ff6f3750dcf7c.json": `{"tokens":{"refresh_token":"r2"}}`,
	}

	t.Run("cold slot: writes only absent blobs, skips non-oauth", func(t *testing.T) {
		// linear already on disk (authoritative), github absent.
		onDisk := map[string]bool{"linear-4cca6302a658efcc.json": true}
		written := map[string]string{}
		n, err := RestoreMCPOAuth(secrets,
			func(f string) bool { return onDisk[f] },
			func(f string, d []byte) error { written[f] = string(d); return nil },
		)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("restored = %d, want 1 (only the absent github blob)", n)
		}
		if _, ok := written["linear-4cca6302a658efcc.json"]; ok {
			t.Error("must NOT overwrite the present (authoritative) linear blob")
		}
		if written["github-336ff6f3750dcf7c.json"] != secrets["mcp-oauth/github-336ff6f3750dcf7c.json"] {
			t.Error("github blob restored with wrong content")
		}
		if _, ok := written["anthropic-api-key"]; ok {
			t.Error("non-oauth secret must not be written as a blob")
		}
	})

	t.Run("all present -> nothing restored (warm refresh leaves data untouched)", func(t *testing.T) {
		written := map[string]string{}
		n, err := RestoreMCPOAuth(secrets,
			func(string) bool { return true }, // everything already on disk
			func(f string, d []byte) error { written[f] = string(d); return nil },
		)
		if err != nil || n != 0 {
			t.Fatalf("got (n=%d, err=%v), want (0, nil)", n, err)
		}
		if len(written) != 0 {
			t.Errorf("warm refresh must not write anything, wrote %v", written)
		}
	})

	t.Run("no oauth secrets -> no-op", func(t *testing.T) {
		n, err := RestoreMCPOAuth(map[string]string{"anthropic-api-key": "x"},
			func(string) bool { return false },
			func(string, []byte) error { return nil })
		if err != nil || n != 0 {
			t.Fatalf("got (n=%d, err=%v), want (0, nil)", n, err)
		}
	})
}

func TestMCPOAuthSecretToFile(t *testing.T) {
	if got := MCPOAuthSecretToFile("mcp-oauth/linear-abc.json"); got != "linear-abc.json" {
		t.Errorf("got %q, want linear-abc.json", got)
	}
	if got := MCPOAuthSecretToFile("anthropic-api-key"); got != "" {
		t.Errorf("non-oauth secret should map to empty, got %q", got)
	}
}
