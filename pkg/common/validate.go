package common

import "fmt"

// ValidateAgentName checks that an agent name is lowercase alphanumeric + hyphens.
func ValidateAgentName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("agent name must not be empty")
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("invalid agent name %q: must be lowercase alphanumeric with hyphens (e.g., \"myagent\", \"ml-team\")", name)
		}
	}
	return nil
}
