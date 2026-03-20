package cmd

import (
	"testing"
	"time"
)

func TestParseKeyValues(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect map[string]string
	}{
		{"basic", "KEY=value\nFOO=bar", map[string]string{"KEY": "value", "FOO": "bar"}},
		{"empty value", "KEY=", map[string]string{"KEY": ""}},
		{"equals in value", "KEY=a=b", map[string]string{"KEY": "a=b"}},
		{"empty input", "", map[string]string{}},
		{"trailing newline", "KEY=val\n", map[string]string{"KEY": "val"}},
		{"no equals", "NOEQ", map[string]string{}},
		{"mixed", "KEY=val\nBAD\nFOO=bar", map[string]string{"KEY": "val", "FOO": "bar"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseKeyValues(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("parseKeyValues(%q) returned %d entries, want %d", tt.input, len(got), len(tt.expect))
				return
			}
			for k, want := range tt.expect {
				if got[k] != want {
					t.Errorf("parseKeyValues(%q)[%q] = %q, want %q", tt.input, k, got[k], want)
				}
			}
		})
	}
}

func TestSplitStats(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{"three parts", "0.50% | 128MiB / 2GiB | 12", []string{"0.50%", "128MiB / 2GiB", "12"}},
		{"one part", "only", []string{"only"}},
		{"two parts", "a|b", []string{"a", "b"}},
		{"empty", "", []string{""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitStats(tt.input)
			if len(got) != len(tt.expect) {
				t.Errorf("splitStats(%q) returned %d parts, want %d", tt.input, len(got), len(tt.expect))
				return
			}
			for i, want := range tt.expect {
				if got[i] != want {
					t.Errorf("splitStats(%q)[%d] = %q, want %q", tt.input, i, got[i], want)
				}
			}
		})
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name     string
		offset   time.Duration
		expected string
	}{
		{"seconds", 30 * time.Second, "30s"},
		{"minutes", 5 * time.Minute, "5m"},
		{"hours and minutes", 3*time.Hour + 42*time.Minute, "3h 42m"},
		{"days and hours", 2*24*time.Hour + 5*time.Hour, "2d 5h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := time.Now().Add(-tt.offset).Format(time.RFC3339Nano)
			result := formatUptime(started)
			if result != tt.expected {
				t.Errorf("formatUptime(%q) = %q, want %q (offset=%v)", started, result, tt.expected, tt.offset)
			}
		})
	}
}

func TestFormatUptime_InvalidInput(t *testing.T) {
	result := formatUptime("not-a-timestamp")
	if result != "" {
		t.Errorf("formatUptime(invalid) = %q, want empty string", result)
	}
}

func TestFormatUptime_RFC3339(t *testing.T) {
	started := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	result := formatUptime(started)
	expected := "1h 0m"
	if result != expected {
		t.Errorf("formatUptime(RFC3339) = %q, want %q", result, expected)
	}
}
