package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ManifestSchemaVersion identifies the manifest format. Bump when we add a
// field that existing readers can't safely ignore.
const ManifestSchemaVersion = 1

// EgressManifestFileName returns the manifest filename for an agent (no
// directory). Use alongside the provider's config directory to get a full
// path: e.g. /opt/conga/config/egress-contracts.manifest.json.
func EgressManifestFileName(agentName string) string {
	return fmt.Sprintf("egress-%s.manifest.json", agentName)
}

// EgressYAMLFileName returns the Envoy YAML filename for an agent (no
// directory). Mirrors the convention used by provider deploy paths.
func EgressYAMLFileName(agentName string) string {
	return fmt.Sprintf("egress-%s.yaml", agentName)
}

// PolicyManifest captures the egress policy that was actually deployed to an
// agent's host. It's written next to the Envoy YAML by deploy-egress and
// read back by drift detection to tell the operator whether the running
// proxy matches the desired policy.
type PolicyManifest struct {
	SchemaVersion    int          `json:"schema_version"`
	PolicyHash       string       `json:"policy_hash"` // sha256 of the canonical egress JSON (hex)
	Egress           EgressPolicy `json:"egress"`      // effective merged policy
	DeployedAt       time.Time    `json:"deployed_at"`
	DeployedBy       string       `json:"deployed_by,omitempty"`
	CongalineVersion string       `json:"congaline_version,omitempty"`
}

// BuildManifest snapshots an effective EgressPolicy into a manifest suitable
// for writing to the host. Domains and blocked_domains are canonicalized
// (lowercased + sorted) so the hash is stable across input ordering.
//
// The returned manifest's DeployedAt is zero — callers should set it at the
// deploy site (or use MarshalForDeploy which sets it to time.Now()).
func BuildManifest(ep *EgressPolicy) *PolicyManifest {
	return &PolicyManifest{
		SchemaVersion: ManifestSchemaVersion,
		PolicyHash:    HashEgress(ep),
		Egress:        canonicalEgress(ep),
	}
}

// MarshalForDeploy returns the manifest as pretty-printed JSON bytes suitable
// for embedding in the deploy-egress.sh.tmpl ManifestJSON field. DeployedAt
// is stamped here if zero.
func (m *PolicyManifest) MarshalForDeploy() ([]byte, error) {
	if m.DeployedAt.IsZero() {
		m.DeployedAt = time.Now().UTC()
	}
	return json.MarshalIndent(m, "", "  ")
}

// ParseManifest decodes manifest JSON bytes. Tolerates manifests with a
// higher SchemaVersion by still returning them — callers decide whether to
// accept unknown fields or warn. Rejects malformed JSON.
func ParseManifest(data []byte) (*PolicyManifest, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty manifest")
	}
	var m PolicyManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// HashEgress returns a stable SHA256 hex digest of the effective (lowercased,
// sorted, blocked-filtered) allowed domains plus the mode. This is the hash
// used in PolicyManifest.PolicyHash — two manifests with matching hashes
// represent the same enforcement.
func HashEgress(ep *EgressPolicy) string {
	c := canonicalEgress(ep)
	// Sort domain lists in the canonical view, then emit a stable JSON
	// encoding (encoding/json sorts map keys, and our slices are pre-sorted).
	payload, _ := json.Marshal(struct {
		Mode           EgressMode `json:"mode"`
		AllowedDomains []string   `json:"allowed_domains"`
		BlockedDomains []string   `json:"blocked_domains"`
	}{
		Mode:           c.Mode,
		AllowedDomains: c.AllowedDomains,
		BlockedDomains: c.BlockedDomains,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// canonicalEgress returns a copy of ep with domains lowercased, sorted, and
// duplicates removed. An absent/empty mode defaults to enforce.
func canonicalEgress(ep *EgressPolicy) EgressPolicy {
	if ep == nil {
		return EgressPolicy{Mode: EgressModeEnforce}
	}
	mode := ep.Mode
	if mode == "" {
		mode = EgressModeEnforce
	}
	return EgressPolicy{
		AllowedDomains: canonDomains(ep.AllowedDomains),
		BlockedDomains: canonDomains(ep.BlockedDomains),
		Mode:           mode,
	}
}

// canonDomains lowercases, trims, de-duplicates, and sorts a domain slice.
// Returns nil for an empty input so the JSON encoding stays stable.
func canonDomains(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, d := range in {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if _, dup := seen[d]; dup {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// DriftKind classifies how a desired manifest differs from what's deployed.
type DriftKind string

const (
	DriftMismatch      DriftKind = "mismatch"        // same field, different value
	DriftMissingOnHost DriftKind = "missing-on-host" // desired has it, host doesn't
	DriftExtraOnHost   DriftKind = "extra-on-host"   // host has it, desired doesn't
)

// DriftEntry describes one axis of drift between desired and deployed state.
type DriftEntry struct {
	Field   string    `json:"field"` // "mode" | "allowed_domains" | "blocked_domains" | "hash"
	Desired string    `json:"desired,omitempty"`
	Actual  string    `json:"actual,omitempty"`
	Kind    DriftKind `json:"kind"`
}

// DiffManifests compares a desired manifest against the actual manifest
// read from the host. Returns an empty slice when in sync. When actual is
// nil (no manifest on host), returns a single entry reporting "not deployed."
//
// Semantics:
//   - Mode mismatch → DriftMismatch on field "mode".
//   - Missing domains (in desired, not in actual) → DriftMissingOnHost per domain.
//   - Extra domains (in actual, not in desired) → DriftExtraOnHost per domain.
//   - Hash mismatch with no other drift reported → still emitted as DriftMismatch
//     on field "hash". This handles subtle drift the field-level checks miss
//     (e.g. different canonicalization, whitespace in the YAML that the proxy
//     sees differently).
func DiffManifests(desired, actual *PolicyManifest) []DriftEntry {
	if desired == nil {
		// Nothing desired — we can't meaningfully compare.
		return nil
	}
	if actual == nil {
		return []DriftEntry{{
			Field:   "manifest",
			Desired: "deployed",
			Actual:  "missing",
			Kind:    DriftMissingOnHost,
		}}
	}

	var entries []DriftEntry

	// Mode.
	dm := desired.Egress.Mode
	if dm == "" {
		dm = EgressModeEnforce
	}
	am := actual.Egress.Mode
	if am == "" {
		am = EgressModeEnforce
	}
	if dm != am {
		entries = append(entries, DriftEntry{
			Field:   "mode",
			Desired: string(dm),
			Actual:  string(am),
			Kind:    DriftMismatch,
		})
	}

	// Allowed / blocked domains — per-field set diff.
	entries = append(entries, diffDomainSet("allowed_domains",
		desired.Egress.AllowedDomains, actual.Egress.AllowedDomains)...)
	entries = append(entries, diffDomainSet("blocked_domains",
		desired.Egress.BlockedDomains, actual.Egress.BlockedDomains)...)

	// Hash mismatch as a catch-all: if the fields above show no drift but the
	// hashes still differ, surface it so the operator can redeploy. This
	// happens when canonicalization changed between versions, for example.
	if len(entries) == 0 && desired.PolicyHash != "" && actual.PolicyHash != "" &&
		desired.PolicyHash != actual.PolicyHash {
		entries = append(entries, DriftEntry{
			Field:   "hash",
			Desired: desired.PolicyHash,
			Actual:  actual.PolicyHash,
			Kind:    DriftMismatch,
		})
	}

	return entries
}

// diffDomainSet returns per-domain drift entries for a single field. Input
// slices are canonicalized so ordering doesn't matter.
func diffDomainSet(field string, desired, actual []string) []DriftEntry {
	dSet := make(map[string]struct{}, len(desired))
	for _, d := range canonDomains(desired) {
		dSet[d] = struct{}{}
	}
	aSet := make(map[string]struct{}, len(actual))
	for _, a := range canonDomains(actual) {
		aSet[a] = struct{}{}
	}

	var missing []string
	for d := range dSet {
		if _, ok := aSet[d]; !ok {
			missing = append(missing, d)
		}
	}
	sort.Strings(missing)

	var extra []string
	for a := range aSet {
		if _, ok := dSet[a]; !ok {
			extra = append(extra, a)
		}
	}
	sort.Strings(extra)

	var entries []DriftEntry
	for _, d := range missing {
		entries = append(entries, DriftEntry{
			Field:   field,
			Desired: d,
			Kind:    DriftMissingOnHost,
		})
	}
	for _, a := range extra {
		entries = append(entries, DriftEntry{
			Field:  field,
			Actual: a,
			Kind:   DriftExtraOnHost,
		})
	}
	return entries
}

// Summary returns a one-line human-readable summary of a drift list.
// Returns "in sync" for an empty slice.
func Summary(entries []DriftEntry) string {
	if len(entries) == 0 {
		return "in sync"
	}
	counts := map[DriftKind]int{}
	fields := map[string]struct{}{}
	for _, e := range entries {
		counts[e.Kind]++
		fields[e.Field] = struct{}{}
	}
	parts := []string{}
	for _, k := range []DriftKind{DriftMismatch, DriftMissingOnHost, DriftExtraOnHost} {
		if counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
		}
	}
	return strings.Join(parts, ", ")
}
