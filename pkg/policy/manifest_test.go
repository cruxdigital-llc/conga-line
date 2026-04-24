package policy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// --- HashEgress + canonicalization ---

func TestHashEgress_StableAcrossOrder(t *testing.T) {
	a := &EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "*.slack.com"},
		Mode:           EgressModeValidate,
	}
	b := &EgressPolicy{
		AllowedDomains: []string{"*.slack.com", "api.anthropic.com"},
		Mode:           EgressModeValidate,
	}
	if HashEgress(a) != HashEgress(b) {
		t.Errorf("hash should be stable across domain ordering; got\n  a=%s\n  b=%s",
			HashEgress(a), HashEgress(b))
	}
}

func TestHashEgress_CaseAndWhitespaceInsensitive(t *testing.T) {
	a := &EgressPolicy{AllowedDomains: []string{"API.Anthropic.com"}}
	b := &EgressPolicy{AllowedDomains: []string{" api.anthropic.com "}}
	if HashEgress(a) != HashEgress(b) {
		t.Errorf("hash should normalize case + whitespace")
	}
}

func TestHashEgress_DifferentModeDifferentHash(t *testing.T) {
	a := &EgressPolicy{AllowedDomains: []string{"x.com"}, Mode: EgressModeEnforce}
	b := &EgressPolicy{AllowedDomains: []string{"x.com"}, Mode: EgressModeValidate}
	if HashEgress(a) == HashEgress(b) {
		t.Error("mode change must produce a different hash")
	}
}

func TestHashEgress_NilPolicyStable(t *testing.T) {
	if HashEgress(nil) == "" {
		t.Error("nil policy should still produce a hash (for empty-default)")
	}
	// Nil should match an empty policy in enforce mode (the canonical default).
	if HashEgress(nil) != HashEgress(&EgressPolicy{Mode: EgressModeEnforce}) {
		t.Error("nil and empty-enforce policies must hash identically")
	}
}

func TestHashEgress_EmptyModeDefaultsToEnforce(t *testing.T) {
	a := &EgressPolicy{AllowedDomains: []string{"x.com"}} // no mode
	b := &EgressPolicy{AllowedDomains: []string{"x.com"}, Mode: EgressModeEnforce}
	if HashEgress(a) != HashEgress(b) {
		t.Error("empty mode should canonicalize to enforce")
	}
}

// --- BuildManifest ---

func TestBuildManifest_PopulatesSchemaAndHash(t *testing.T) {
	m := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com"},
		Mode:           EgressModeValidate,
	})
	if m.SchemaVersion != ManifestSchemaVersion {
		t.Errorf("schema version = %d, want %d", m.SchemaVersion, ManifestSchemaVersion)
	}
	if m.PolicyHash == "" {
		t.Error("policy hash must be set")
	}
	if m.Egress.Mode != EgressModeValidate {
		t.Errorf("mode = %s, want validate", m.Egress.Mode)
	}
}

func TestBuildManifest_CanonicalizesDomains(t *testing.T) {
	m := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"API.Anthropic.com", "*.Slack.com", "api.anthropic.com"},
	})
	// Expect lowercased, deduped, sorted.
	want := []string{"*.slack.com", "api.anthropic.com"}
	if len(m.Egress.AllowedDomains) != len(want) {
		t.Fatalf("allowed_domains = %v, want %v", m.Egress.AllowedDomains, want)
	}
	for i, w := range want {
		if m.Egress.AllowedDomains[i] != w {
			t.Errorf("allowed_domains[%d] = %q, want %q", i, m.Egress.AllowedDomains[i], w)
		}
	}
}

func TestBuildManifest_NilPolicyProducesEnforceDefault(t *testing.T) {
	m := BuildManifest(nil)
	if m.Egress.Mode != EgressModeEnforce {
		t.Errorf("nil policy should default to enforce, got %s", m.Egress.Mode)
	}
	if len(m.Egress.AllowedDomains) != 0 {
		t.Errorf("nil policy should have no domains, got %v", m.Egress.AllowedDomains)
	}
}

// --- MarshalForDeploy + ParseManifest ---

func TestMarshalForDeploy_StampsTime(t *testing.T) {
	m := BuildManifest(&EgressPolicy{AllowedDomains: []string{"x.com"}, Mode: EgressModeValidate})
	data, err := m.MarshalForDeploy()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if m.DeployedAt.IsZero() {
		t.Error("DeployedAt should be set after MarshalForDeploy")
	}
	// Idempotent: a second call shouldn't overwrite a set time.
	first := m.DeployedAt
	time.Sleep(1 * time.Millisecond)
	_, _ = m.MarshalForDeploy()
	if !m.DeployedAt.Equal(first) {
		t.Error("MarshalForDeploy should not overwrite an existing DeployedAt")
	}
	if !json.Valid(data) {
		t.Error("marshaled manifest must be valid JSON")
	}
}

func TestParseManifest_RoundTrip(t *testing.T) {
	orig := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "*.slack.com"},
		Mode:           EgressModeValidate,
	})
	data, err := orig.MarshalForDeploy()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.PolicyHash != orig.PolicyHash {
		t.Errorf("hash mismatch after round-trip: %s vs %s", got.PolicyHash, orig.PolicyHash)
	}
}

func TestParseManifest_EmptyRejected(t *testing.T) {
	if _, err := ParseManifest(nil); err == nil {
		t.Error("empty input should error")
	}
	if _, err := ParseManifest([]byte("")); err == nil {
		t.Error("empty input should error")
	}
}

func TestParseManifest_MalformedRejected(t *testing.T) {
	if _, err := ParseManifest([]byte("not-json")); err == nil {
		t.Error("malformed input should error")
	}
}

// --- DiffManifests ---

func sameHash(ep *EgressPolicy) *PolicyManifest {
	return BuildManifest(ep)
}

func TestDiffManifests_NoDrift(t *testing.T) {
	desired := sameHash(&EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "*.slack.com"},
		Mode:           EgressModeValidate,
	})
	actual := sameHash(&EgressPolicy{
		AllowedDomains: []string{"*.slack.com", "api.anthropic.com"}, // reordered
		Mode:           EgressModeValidate,
	})
	if diff := DiffManifests(desired, actual); len(diff) != 0 {
		t.Errorf("want no drift, got %+v", diff)
	}
}

func TestDiffManifests_NilActualMeansNotDeployed(t *testing.T) {
	desired := BuildManifest(&EgressPolicy{AllowedDomains: []string{"x.com"}})
	diff := DiffManifests(desired, nil)
	if len(diff) != 1 {
		t.Fatalf("want 1 entry for missing manifest, got %+v", diff)
	}
	if diff[0].Field != "manifest" || diff[0].Kind != DriftMissingOnHost {
		t.Errorf("unexpected entry: %+v", diff[0])
	}
}

func TestDiffManifests_ModeDrift(t *testing.T) {
	desired := BuildManifest(&EgressPolicy{AllowedDomains: []string{"x.com"}, Mode: EgressModeValidate})
	actual := BuildManifest(&EgressPolicy{AllowedDomains: []string{"x.com"}, Mode: EgressModeEnforce})
	diff := DiffManifests(desired, actual)
	if len(diff) != 1 {
		t.Fatalf("want exactly 1 entry (mode), got %+v", diff)
	}
	e := diff[0]
	if e.Field != "mode" || e.Kind != DriftMismatch {
		t.Errorf("wrong entry: %+v", e)
	}
	if e.Desired != "validate" || e.Actual != "enforce" {
		t.Errorf("desired/actual values: desired=%s actual=%s", e.Desired, e.Actual)
	}
}

func TestDiffManifests_MissingDomainOnHost(t *testing.T) {
	desired := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "oauth2.googleapis.com", "www.googleapis.com"},
		Mode:           EgressModeValidate,
	})
	actual := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com"},
		Mode:           EgressModeValidate,
	})
	diff := DiffManifests(desired, actual)
	if len(diff) != 2 {
		t.Fatalf("want 2 missing entries, got %+v", diff)
	}
	for _, e := range diff {
		if e.Field != "allowed_domains" || e.Kind != DriftMissingOnHost {
			t.Errorf("unexpected entry: %+v", e)
		}
	}
}

func TestDiffManifests_ExtraDomainOnHost(t *testing.T) {
	desired := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com"},
		Mode:           EgressModeEnforce,
	})
	actual := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"api.anthropic.com", "legacy.example.com"},
		Mode:           EgressModeEnforce,
	})
	diff := DiffManifests(desired, actual)
	if len(diff) != 1 {
		t.Fatalf("want 1 extra entry, got %+v", diff)
	}
	e := diff[0]
	if e.Field != "allowed_domains" || e.Kind != DriftExtraOnHost || e.Actual != "legacy.example.com" {
		t.Errorf("wrong entry: %+v", e)
	}
}

func TestDiffManifests_MixedMissingAndExtra(t *testing.T) {
	desired := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"a.com", "b.com"},
		Mode:           EgressModeValidate,
	})
	actual := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"b.com", "c.com"},
		Mode:           EgressModeValidate,
	})
	diff := DiffManifests(desired, actual)
	if len(diff) != 2 {
		t.Fatalf("want 2 entries (missing + extra), got %+v", diff)
	}
	var missing, extra int
	for _, e := range diff {
		switch e.Kind {
		case DriftMissingOnHost:
			missing++
		case DriftExtraOnHost:
			extra++
		}
	}
	if missing != 1 || extra != 1 {
		t.Errorf("want 1 missing + 1 extra, got missing=%d extra=%d", missing, extra)
	}
}

func TestDiffManifests_HashMismatchOnlyWhenFieldsAgree(t *testing.T) {
	// Construct two manifests where the fields match but the hash doesn't
	// (simulating a manifest from an older schema, say).
	desired := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"a.com"},
		Mode:           EgressModeValidate,
	})
	actual := &PolicyManifest{
		SchemaVersion: ManifestSchemaVersion,
		PolicyHash:    "deadbeef",
		Egress: EgressPolicy{
			AllowedDomains: []string{"a.com"},
			Mode:           EgressModeValidate,
		},
	}
	diff := DiffManifests(desired, actual)
	if len(diff) != 1 {
		t.Fatalf("want 1 entry (hash), got %+v", diff)
	}
	if diff[0].Field != "hash" || diff[0].Kind != DriftMismatch {
		t.Errorf("wrong entry: %+v", diff[0])
	}
}

func TestDiffManifests_FieldDriftSuppressesHashDrift(t *testing.T) {
	// When there's field-level drift, we don't want to ALSO emit a hash
	// drift entry — it would be redundant noise.
	desired := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"a.com"},
		Mode:           EgressModeValidate,
	})
	actual := BuildManifest(&EgressPolicy{
		AllowedDomains: []string{"a.com"},
		Mode:           EgressModeEnforce, // drift
	})
	diff := DiffManifests(desired, actual)
	for _, e := range diff {
		if e.Field == "hash" {
			t.Errorf("hash entry should be suppressed when field drift exists; got %+v", diff)
		}
	}
}

func TestDiffManifests_NilDesiredReturnsNothing(t *testing.T) {
	diff := DiffManifests(nil, BuildManifest(&EgressPolicy{}))
	if len(diff) != 0 {
		t.Errorf("nil desired should return no entries, got %+v", diff)
	}
}

// --- Summary ---

func TestSummary_Empty(t *testing.T) {
	if s := Summary(nil); s != "in sync" {
		t.Errorf("empty summary = %q, want 'in sync'", s)
	}
}

func TestSummary_WithDrift(t *testing.T) {
	entries := []DriftEntry{
		{Field: "mode", Kind: DriftMismatch},
		{Field: "allowed_domains", Kind: DriftMissingOnHost},
		{Field: "allowed_domains", Kind: DriftMissingOnHost},
	}
	s := Summary(entries)
	if !strings.Contains(s, "mismatch=1") {
		t.Errorf("summary missing mismatch count: %q", s)
	}
	if !strings.Contains(s, "missing-on-host=2") {
		t.Errorf("summary missing missing count: %q", s)
	}
}
