package ui

import (
	"io"
	"strings"
	"testing"
)

func TestConfirmWith(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{"y", "y\n", true},
		{"yes", "yes\n", true},
		{"Y", "Y\n", true},
		{"YES", "YES\n", true},
		{"n", "n\n", false},
		{"no", "no\n", false},
		{"empty", "\n", false},
		{"random", "maybe\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConfirmWith(strings.NewReader(tt.input), io.Discard, "test?")
			if result != tt.expect {
				t.Errorf("ConfirmWith(input=%q) = %v, want %v", tt.input, result, tt.expect)
			}
		})
	}
}

func TestConfirmWith_EOF(t *testing.T) {
	result := ConfirmWith(strings.NewReader(""), io.Discard, "test?")
	if result != false {
		t.Error("ConfirmWith on EOF should return false")
	}
}

func TestTextPromptWith(t *testing.T) {
	result, err := TextPromptWith(strings.NewReader("hello\n"), io.Discard, "name")
	if err != nil {
		t.Fatalf("TextPromptWith returned error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTextPromptWith_Whitespace(t *testing.T) {
	result, err := TextPromptWith(strings.NewReader("  hello  \n"), io.Discard, "name")
	if err != nil {
		t.Fatalf("TextPromptWith returned error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTextPromptWith_EOF(t *testing.T) {
	_, err := TextPromptWith(strings.NewReader(""), io.Discard, "name")
	if err == nil {
		t.Error("expected error on EOF")
	}
}

func TestTextPromptWithDefaultFrom_EmptyReturnsDefault(t *testing.T) {
	result, err := TextPromptWithDefaultFrom(strings.NewReader("\n"), io.Discard, "label", "mydefault")
	if err != nil {
		t.Fatalf("returned error: %v", err)
	}
	if result != "mydefault" {
		t.Errorf("expected 'mydefault', got %q", result)
	}
}

func TestTextPromptWithDefaultFrom_ValueOverridesDefault(t *testing.T) {
	result, err := TextPromptWithDefaultFrom(strings.NewReader("override\n"), io.Discard, "label", "mydefault")
	if err != nil {
		t.Fatalf("returned error: %v", err)
	}
	if result != "override" {
		t.Errorf("expected 'override', got %q", result)
	}
}

func TestTextPromptWithDefaultFrom_WhitespaceReturnsDefault(t *testing.T) {
	result, err := TextPromptWithDefaultFrom(strings.NewReader("   \n"), io.Discard, "label", "mydefault")
	if err != nil {
		t.Fatalf("returned error: %v", err)
	}
	if result != "mydefault" {
		t.Errorf("expected 'mydefault' for whitespace input, got %q", result)
	}
}
