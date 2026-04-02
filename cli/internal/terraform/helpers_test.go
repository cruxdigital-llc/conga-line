package terraform

import (
	"fmt"
	"testing"

	"github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

func TestSplitImportID(t *testing.T) {
	tests := []struct {
		id       string
		n        int
		expected []string
	}{
		{"agent/secret", 2, []string{"agent", "secret"}},
		{"agent", 2, nil},
		{"agent/", 2, nil},
		{"/secret", 2, nil},
		{"", 2, nil},
		{"a/b/c", 2, []string{"a", "b/c"}},
		{"a/b/c", 3, []string{"a", "b", "c"}},
		{"single", 1, []string{"single"}},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_n%d", tt.id, tt.n), func(t *testing.T) {
			result := splitImportID(tt.id, tt.n)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Fatalf("expected %v, got nil", tt.expected)
			}
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d parts, got %d", len(tt.expected), len(result))
			}
			for i, v := range tt.expected {
				if result[i] != v {
					t.Errorf("part %d: expected %q, got %q", i, v, result[i])
				}
			}
		})
	}
}

func TestIsNotFoundErr(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"wrapped ErrNotFound", fmt.Errorf("agent %q not found: %w", "test", provider.ErrNotFound), true},
		{"bare ErrNotFound", provider.ErrNotFound, true},
		{"double-wrapped ErrNotFound", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", provider.ErrNotFound)), true},
		{"connection timeout", fmt.Errorf("connection timeout"), false},
		{"access denied", fmt.Errorf("access denied"), false},
		{"not found without sentinel", fmt.Errorf("agent not found"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNotFoundErr(tt.err)
			if result != tt.expected {
				t.Errorf("isNotFoundErr(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}
