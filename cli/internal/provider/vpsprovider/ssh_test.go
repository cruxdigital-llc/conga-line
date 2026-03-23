package vpsprovider

import "testing"

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "'hello'"},
		{"hello world", "'hello world'"},
		{"it's", "'it'\\''s'"},
		{"", "''"},
		{"a'b'c", "'a'\\''b'\\''c'"},
		{"/opt/conga/agents", "'/opt/conga/agents'"},
		{"$(whoami)", "'$(whoami)'"},
		{"; rm -rf /", "'; rm -rf /'"},
		{"hello\nworld", "'hello\nworld'"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := shellQuote(tt.input)
			if got != tt.expected {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestShelljoin(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{"simple args", []string{"run", "-d", "--name", "conga-test"}, "run -d --name conga-test"},
		{"with spaces", []string{"exec", "conga-test", "echo", "hello world"}, "exec conga-test echo 'hello world'"},
		{"with special chars", []string{"run", "--env", "FOO=bar baz"}, "run --env 'FOO=bar baz'"},
		{"empty string", []string{"echo", ""}, "echo ''"},
		{"with quotes", []string{"exec", "conga-test", "node", "-e", "console.log('hi')"}, "exec conga-test node -e 'console.log('\\''hi'\\'')'"},
		{"paths and flags", []string{"run", "-v", "/opt/conga/data:/home/node:rw"}, "run -v /opt/conga/data:/home/node:rw"},
		{"port binding", []string{"-p", "127.0.0.1:18789:18789"}, "-p 127.0.0.1:18789:18789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shelljoin(tt.args...)
			if got != tt.expected {
				t.Errorf("shelljoin(%v) = %q, want %q", tt.args, got, tt.expected)
			}
		})
	}
}

func TestIsSafeArg(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"hello", true},
		{"conga-test", true},
		{"127.0.0.1:18789:18789", true},
		{"/opt/conga/data:/home/node:rw", true},
		{"--name", true},
		{"-d", true},
		{"NODE_OPTIONS=--max-old-space-size=1536", true},
		{"", false},
		{"hello world", false},
		{"$(whoami)", false},
		{"; rm -rf /", false},
		{"it's", false},
		{"foo\nbar", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isSafeArg(tt.input)
			if got != tt.expected {
				t.Errorf("isSafeArg(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
