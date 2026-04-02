package common

import "testing"

func TestValidateAgentName(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"myagent", true},
		{"ml-team", true},
		{"agent1", true},
		{"a-b-c", true},
		{"Aaron", false},   // uppercase
		{"ml_team", false}, // underscore
		{"", false},        // empty
		{"with space", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateAgentName(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ValidateAgentName(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ValidateAgentName(%q) expected error, got nil", tt.input)
			}
		})
	}
}
