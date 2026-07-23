package localprovider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/common"
)

// TestAgentSecretsRoundTripMCPOAuth verifies the local store handles the
// namespaced mcp-oauth/<file> secret name in both directions: SetSecret creates
// the subdir + file, and readAgentSecrets returns it under the "mcp-oauth/<file>"
// key alongside ordinary top-level secrets.
func TestAgentSecretsRoundTripMCPOAuth(t *testing.T) {
	p := &LocalProvider{dataDir: t.TempDir()}
	ctx := context.Background()
	blob := `{"tokens":{"refresh_token":"r1"}}`

	if err := p.SetSecret(ctx, "team-a", "anthropic-api-key", "sk-x"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetSecret(ctx, "team-a", "mcp-oauth/linear-4cca6302a658efcc.json", blob); err != nil {
		t.Fatalf("SetSecret with namespaced name: %v", err)
	}

	got, err := p.readAgentSecrets("team-a")
	if err != nil {
		t.Fatal(err)
	}
	if got["anthropic-api-key"] != "sk-x" {
		t.Errorf("top-level secret missing/wrong: %q", got["anthropic-api-key"])
	}
	if got["mcp-oauth/linear-4cca6302a658efcc.json"] != blob {
		t.Errorf("namespaced oauth secret not read back: %q", got["mcp-oauth/linear-4cca6302a658efcc.json"])
	}
}

// TestRestoreMCPOAuthLocalPrimitives exercises common.RestoreMCPOAuth with the
// exact os-based exists/write closures the local provider uses in RefreshAgent:
// a cold slot gets a 0600 file, and a warm refresh (file already present) leaves
// the on-disk copy untouched.
func TestRestoreMCPOAuthLocalPrimitives(t *testing.T) {
	dataDir := t.TempDir()
	targetDir := filepath.Join(dataDir, "mcp-oauth")
	blob := `{"tokens":{"refresh_token":"r1"}}`
	secrets := map[string]string{
		"anthropic-api-key":                      "sk-x",
		"mcp-oauth/linear-4cca6302a658efcc.json": blob,
	}
	exists := func(f string) bool { _, err := os.Stat(filepath.Join(targetDir, f)); return err == nil }
	write := func(f string, d []byte) error {
		if err := os.MkdirAll(targetDir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(targetDir, f), d, 0o600)
	}

	// Cold: writes the one blob at 0600.
	n, err := common.RestoreMCPOAuth(secrets, exists, write)
	if err != nil || n != 1 {
		t.Fatalf("cold restore: got (n=%d, err=%v), want (1, nil)", n, err)
	}
	blobPath := filepath.Join(targetDir, "linear-4cca6302a658efcc.json")
	data, err := os.ReadFile(blobPath)
	if err != nil || string(data) != blob {
		t.Fatalf("restored blob content wrong: %q err=%v", string(data), err)
	}
	if info, _ := os.Stat(blobPath); info != nil && info.Mode().Perm() != 0o600 {
		t.Errorf("restored blob mode = %o, want 600", info.Mode().Perm())
	}

	// Warm: the file exists now → nothing rewritten, and content stays as-is
	// even though the secret value differs (on-disk is authoritative).
	secrets["mcp-oauth/linear-4cca6302a658efcc.json"] = `{"tokens":{"refresh_token":"STALE"}}`
	n, err = common.RestoreMCPOAuth(secrets, exists, write)
	if err != nil || n != 0 {
		t.Fatalf("warm restore: got (n=%d, err=%v), want (0, nil)", n, err)
	}
	data, _ = os.ReadFile(blobPath)
	if string(data) != blob {
		t.Errorf("warm restore overwrote the authoritative on-disk blob: %q", string(data))
	}
}
