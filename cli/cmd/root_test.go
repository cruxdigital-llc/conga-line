package cmd

import (
	"testing"
)

func TestValidateAgentName(t *testing.T) {
	valid := []string{"myagent", "ml-team", "a", "a-b-c-1", "agent123"}
	for _, name := range valid {
		if err := validateAgentName(name); err != nil {
			t.Errorf("validateAgentName(%q) returned error: %v", name, err)
		}
	}

	invalid := []string{"Aaron", "ml_team", "a b", "", "a!b", "UPPER", "has.dot"}
	for _, name := range invalid {
		if err := validateAgentName(name); err == nil {
			t.Errorf("validateAgentName(%q) should have returned error", name)
		}
	}
}

func TestValidateMemberID(t *testing.T) {
	valid := []string{"U0123456789", "UABCDEFGHIJ", "UEXAMPLE0101", "UXXXXXXXXXX"}
	for _, id := range valid {
		if err := validateMemberID(id); err != nil {
			t.Errorf("validateMemberID(%q) returned error: %v", id, err)
		}
	}

	invalid := []string{
		"u0123456789",  // lowercase
		"U012345678",   // too short (10 chars total)
		"U01234567890", // too long (12 chars total)
		"C0123456789",  // wrong prefix
		"",             // empty
		"0123456789A",  // no U prefix
	}
	for _, id := range invalid {
		if err := validateMemberID(id); err == nil {
			t.Errorf("validateMemberID(%q) should have returned error", id)
		}
	}
}

func TestValidateChannelID(t *testing.T) {
	valid := []string{"C0123456789", "CABCDEFGHIJ"}
	for _, id := range valid {
		if err := validateChannelID(id); err != nil {
			t.Errorf("validateChannelID(%q) returned error: %v", id, err)
		}
	}

	invalid := []string{
		"c0123456789",  // lowercase
		"C012345678",   // too short
		"C01234567890", // too long
		"U0123456789",  // wrong prefix
		"",             // empty
	}
	for _, id := range invalid {
		if err := validateChannelID(id); err == nil {
			t.Errorf("validateChannelID(%q) should have returned error", id)
		}
	}
}
