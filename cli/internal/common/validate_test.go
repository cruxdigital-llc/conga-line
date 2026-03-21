package common

import "testing"

func TestValidateMemberID(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"U0123456789", true},
		{"UABCDEFGHIJ", true},
		{"U012345678", false},   // too short
		{"U01234567890", false}, // too long
		{"C0123456789", false},  // wrong prefix
		{"u0123456789", false},  // lowercase
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateMemberID(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ValidateMemberID(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ValidateMemberID(%q) expected error, got nil", tt.input)
			}
		})
	}
}

func TestValidateChannelID(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"C0123456789", true},
		{"CABCDEFGHIJ", true},
		{"C012345678", false},
		{"U0123456789", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			err := ValidateChannelID(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ValidateChannelID(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ValidateChannelID(%q) expected error, got nil", tt.input)
			}
		})
	}
}

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
