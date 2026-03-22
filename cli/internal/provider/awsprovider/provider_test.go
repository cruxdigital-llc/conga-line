package awsprovider

import "testing"

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

func TestBuildAgentStatus_NotFound(t *testing.T) {
	kv := map[string]string{"CONTAINER_STATUS": "not found"}
	status := buildAgentStatus("test", kv)
	if status.Container.State != "not found" {
		t.Errorf("expected 'not found', got %q", status.Container.State)
	}
}

func TestBuildAgentStatus_Ready(t *testing.T) {
	kv := map[string]string{
		"SERVICE_STATE":       "active",
		"CONTAINER_STATUS":    "running",
		"CONTAINER_STARTED":   "2026-03-21T10:00:00Z",
		"BOOT_GATEWAY":        "1",
		"BOOT_SLACK_START":    "1",
		"BOOT_SLACK_HTTP":     "1",
		"BOOT_SLACK_CHANNELS": "1",
		"BOOT_ERROR":          "0",
		"CONTAINER_STATS":     "1.5%|256MiB / 2GiB|12",
	}
	status := buildAgentStatus("test", kv)

	if status.ReadyPhase != "ready" {
		t.Errorf("expected 'ready', got %q", status.ReadyPhase)
	}
	if status.Container.CPUPercent != "1.5%" {
		t.Errorf("expected CPU '1.5%%', got %q", status.Container.CPUPercent)
	}
	if status.Container.MemoryUsage != "256MiB / 2GiB" {
		t.Errorf("expected mem '256MiB / 2GiB', got %q", status.Container.MemoryUsage)
	}
}
