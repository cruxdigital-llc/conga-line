package common

import (
	"fmt"
	"regexp"
)

var (
	validMemberIDPattern  = regexp.MustCompile(`^U[A-Z0-9]{10}$`)
	validChannelIDPattern = regexp.MustCompile(`^C[A-Z0-9]{10}$`)
)

// ValidateMemberID validates a Slack member ID format.
func ValidateMemberID(id string) error {
	if !validMemberIDPattern.MatchString(id) {
		return fmt.Errorf("invalid Slack member ID %q: must start with 'U' followed by 10 alphanumeric characters (e.g., U0123456789)", id)
	}
	return nil
}

// ValidateChannelID validates a Slack channel ID format.
func ValidateChannelID(id string) error {
	if !validChannelIDPattern.MatchString(id) {
		return fmt.Errorf("invalid Slack channel ID %q: must start with 'C' followed by 10 alphanumeric characters (e.g., C0123456789)", id)
	}
	return nil
}

// ValidateAgentName checks that an agent name is lowercase alphanumeric + hyphens.
func ValidateAgentName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("agent name must not be empty")
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("invalid agent name %q: must be lowercase alphanumeric with hyphens (e.g., \"aaron\", \"ml-team\")", name)
		}
	}
	return nil
}
