package common

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// CaptureMCPOAuth reads the agent's on-disk remote-MCP OAuth credential blobs
// from inside its running container and stores each in the per-agent secrets
// store under the mcp-oauth/ prefix, so the credential survives container/host
// lifecycle events (the Phase 2 durability path). It returns the number of blobs
// captured.
//
// No-op (0, nil) when the runtime has no OAuth state dir (e.g. Hermes) or the
// agent has none on disk. Requires the container to be running — call it after
// `conga mcp login` succeeds, or after RefreshAgent brings the container up.
// Blob values are never logged.
func CaptureMCPOAuth(ctx context.Context, prov provider.Provider, rt runtime.Runtime, agentName string) (int, error) {
	dir := rt.OAuthStateDir()
	if dir == "" {
		return 0, nil
	}
	stateDir := path.Join(rt.ContainerDataPath(), dir)

	// List blob filenames (bare names, one per line). Tolerates a missing/empty dir.
	listing, err := prov.ContainerExec(ctx, agentName, []string{
		"sh", "-c", fmt.Sprintf("ls -1 %q 2>/dev/null || true", stateDir),
	})
	if err != nil {
		return 0, fmt.Errorf("listing MCP OAuth blobs for %s: %w", agentName, err)
	}

	files := mcpOAuthBlobFiles(listing)
	captured := 0
	for _, f := range files {
		// argv form (no shell) — filename is a single argument, no injection risk.
		content, err := prov.ContainerExec(ctx, agentName, []string{"cat", path.Join(stateDir, f)})
		if err != nil {
			return captured, fmt.Errorf("reading MCP OAuth blob %q for %s: %w", f, agentName, err)
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		if err := prov.SetSecret(ctx, agentName, runtime.MCPOAuthSecretPrefix+f, content); err != nil {
			return captured, fmt.Errorf("storing MCP OAuth blob %q for %s: %w", f, agentName, err)
		}
		captured++
	}
	return captured, nil
}

// mcpOAuthBlobFiles extracts the `*.json` blob filenames from an `ls -1` listing,
// sorted for determinism.
func mcpOAuthBlobFiles(listing string) []string {
	var out []string
	for _, ln := range strings.Split(listing, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" && strings.HasSuffix(ln, ".json") {
			out = append(out, ln)
		}
	}
	sort.Strings(out)
	return out
}

// MCPOAuthSecretToFile returns the on-disk blob filename for a mcp-oauth/ secret
// name (the inverse of the mcp-oauth/ + filename convention), or "" if the secret
// is not an MCP OAuth blob. Used by providers implementing restore.
func MCPOAuthSecretToFile(secretName string) string {
	if !runtime.IsMCPOAuthSecret(secretName) {
		return ""
	}
	return strings.TrimPrefix(secretName, runtime.MCPOAuthSecretPrefix)
}
