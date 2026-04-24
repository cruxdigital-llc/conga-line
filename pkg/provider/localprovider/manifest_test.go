package localprovider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/policy"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// testProvider constructs a LocalProvider rooted at a temp data dir. Only
// the paths used by the manifest round-trip are exercised — no Docker, no
// secrets setup.
func testProvider(t *testing.T) *LocalProvider {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".conga")
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	return &LocalProvider{dataDir: dir}
}

func TestReadProxyManifest_NotFound(t *testing.T) {
	p := testProvider(t)
	_, err := p.ReadProxyManifest(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestReadProxyManifest_RoundTrip(t *testing.T) {
	p := testProvider(t)
	agentName := "contracts"

	// Build the manifest the same way the deploy path would, then write it
	// to the expected on-disk location manually. This is a unit-level proxy
	// for "agent has been deployed" without actually running containers.
	ep := &policy.EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "*.slack.com"},
		Mode:           policy.EgressModeValidate,
	}
	manifestBytes, err := policy.BuildManifest(ep).MarshalForDeploy()
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(p.configDir(), policy.EgressManifestFileName(agentName))
	if err := os.WriteFile(path, manifestBytes, 0444); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Read it back via the provider method and parse.
	got, err := p.ReadProxyManifest(context.Background(), agentName)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	parsed, err := policy.ParseManifest(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Egress.Mode != policy.EgressModeValidate {
		t.Errorf("mode = %s, want validate", parsed.Egress.Mode)
	}
	// Manifest canonicalizes domains to lowercase/sorted, so the order is
	// deterministic even though the input had "*.slack.com" second.
	want := []string{"*.slack.com", "api.anthropic.com"}
	if len(parsed.Egress.AllowedDomains) != len(want) {
		t.Fatalf("domains = %v, want %v", parsed.Egress.AllowedDomains, want)
	}
	for i, w := range want {
		if parsed.Egress.AllowedDomains[i] != w {
			t.Errorf("domains[%d] = %s, want %s", i, parsed.Egress.AllowedDomains[i], w)
		}
	}
}

// TestDriftDetection_EndToEnd simulates the drift-detection path without
// Docker. It writes a manifest reflecting an "old" policy, then computes
// drift against a "new" desired policy via DiffManifests.
func TestDriftDetection_EndToEnd(t *testing.T) {
	p := testProvider(t)
	agentName := "contracts"

	// Step 1: deploy-time snapshot — old policy written to host.
	oldPolicy := &policy.EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "*.slack.com"},
		Mode:           policy.EgressModeValidate,
	}
	oldBytes, _ := policy.BuildManifest(oldPolicy).MarshalForDeploy()
	path := filepath.Join(p.configDir(), policy.EgressManifestFileName(agentName))
	if err := os.WriteFile(path, oldBytes, 0444); err != nil {
		t.Fatalf("seed host manifest: %v", err)
	}

	// Step 2: operator edits the policy locally — adds a domain, flips mode.
	newPolicy := &policy.EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "*.slack.com", "oauth2.googleapis.com"},
		Mode:           policy.EgressModeEnforce,
	}
	desired := policy.BuildManifest(newPolicy)

	// Step 3: drift detection reads the host manifest and diffs.
	raw, err := p.ReadProxyManifest(context.Background(), agentName)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	actual, err := policy.ParseManifest(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	drift := policy.DiffManifests(desired, actual)
	if len(drift) == 0 {
		t.Fatal("expected drift (mode + missing domain), got none")
	}

	var modeFound, missingFound bool
	for _, e := range drift {
		if e.Field == "mode" && e.Kind == policy.DriftMismatch {
			modeFound = true
		}
		if e.Field == "allowed_domains" && e.Kind == policy.DriftMissingOnHost &&
			e.Desired == "oauth2.googleapis.com" {
			missingFound = true
		}
	}
	if !modeFound {
		t.Errorf("expected mode drift entry, got %+v", drift)
	}
	if !missingFound {
		t.Errorf("expected missing-on-host entry for oauth2.googleapis.com, got %+v", drift)
	}

	// Step 4: operator runs deploy — re-write host manifest to match desired.
	// The real provider removes-then-writes because the manifest file is
	// installed read-only; mirror that here.
	newBytes, _ := desired.MarshalForDeploy()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove old manifest: %v", err)
	}
	if err := os.WriteFile(path, newBytes, 0444); err != nil {
		t.Fatalf("re-deploy manifest: %v", err)
	}

	// Step 5: drift detection returns in-sync.
	raw2, _ := p.ReadProxyManifest(context.Background(), agentName)
	actual2, _ := policy.ParseManifest(raw2)
	drift2 := policy.DiffManifests(desired, actual2)
	if len(drift2) != 0 {
		t.Errorf("after re-deploy, expected no drift, got %+v", drift2)
	}
}
