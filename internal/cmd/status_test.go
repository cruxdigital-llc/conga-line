package cmd

import (
	"testing"
	"time"
)

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
