// Package common contains shared logic used by multiple provider implementations.
package common

import "strings"

// SecretNameToEnvVar converts a kebab-case secret name to SCREAMING_SNAKE_CASE.
// Example: "anthropic-api-key" -> "ANTHROPIC_API_KEY"
func SecretNameToEnvVar(name string) string {
	return strings.NewReplacer("-", "_").Replace(strings.ToUpper(name))
}

// MaskSecret returns a masked version of a secret value, showing only the
// first 4 and last 3 characters (e.g. "sk-a...5yU"). Short values are
// fully masked.
func MaskSecret(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "..." + s[len(s)-3:]
}
