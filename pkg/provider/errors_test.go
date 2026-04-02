package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrNotFound_Wrapping(t *testing.T) {
	// Simulate how providers wrap ErrNotFound
	wrapped := fmt.Errorf("agent %q not found: %w", "myagent", ErrNotFound)

	if !errors.Is(wrapped, ErrNotFound) {
		t.Error("errors.Is should match ErrNotFound through wrapping")
	}

	// Double-wrapped (e.g., caller wraps again)
	doubleWrapped := fmt.Errorf("operation failed: %w", wrapped)
	if !errors.Is(doubleWrapped, ErrNotFound) {
		t.Error("errors.Is should match ErrNotFound through double wrapping")
	}
}

func TestErrBindingExists_Wrapping(t *testing.T) {
	// Simulate how providers wrap ErrBindingExists
	wrapped := fmt.Errorf("agent %q already has a %s binding: %w",
		"myagent", "slack", ErrBindingExists)

	if !errors.Is(wrapped, ErrBindingExists) {
		t.Error("errors.Is should match ErrBindingExists through wrapping")
	}
}

func TestSentinelErrors_NotConfused(t *testing.T) {
	wrappedNotFound := fmt.Errorf("agent not found: %w", ErrNotFound)
	wrappedBinding := fmt.Errorf("binding exists: %w", ErrBindingExists)

	if errors.Is(wrappedNotFound, ErrBindingExists) {
		t.Error("ErrNotFound should not match ErrBindingExists")
	}
	if errors.Is(wrappedBinding, ErrNotFound) {
		t.Error("ErrBindingExists should not match ErrNotFound")
	}
}
