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
