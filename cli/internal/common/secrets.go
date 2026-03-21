// Package common contains shared logic used by multiple provider implementations.
package common

import "strings"

// SecretNameToEnvVar converts a kebab-case secret name to SCREAMING_SNAKE_CASE.
// Example: "anthropic-api-key" -> "ANTHROPIC_API_KEY"
func SecretNameToEnvVar(name string) string {
	return strings.NewReplacer("-", "_").Replace(strings.ToUpper(name))
}
