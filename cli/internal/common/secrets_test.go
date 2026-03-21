package common

import "testing"

func TestSecretNameToEnvVar(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"anthropic-api-key", "ANTHROPIC_API_KEY"},
		{"google-client-id", "GOOGLE_CLIENT_ID"},
		{"simple", "SIMPLE"},
		{"multi--dash", "MULTI__DASH"},
		{"already-UPPER", "ALREADY_UPPER"},
		{"a-", "A_"},
		{"a", "A"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SecretNameToEnvVar(tt.input)
			if got != tt.expect {
				t.Errorf("SecretNameToEnvVar(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}
