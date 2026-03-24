package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Package-level JSON mode state. Not goroutine-safe; the CLI is single-threaded.
// If parallel command execution is ever added, this must be wrapped in a struct
// passed through context.
var (
	// JSONInputActive is true when --json was provided with data.
	JSONInputActive bool

	// OutputJSON is true when output should be JSON (--output json or implied by --json).
	OutputJSON bool

	// jsonData holds the parsed input object.
	jsonData map[string]any
)

// SetJSONMode parses JSON input and activates JSON mode.
func SetJSONMode(input string) error {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	var raw []byte
	if strings.HasPrefix(input, "@") {
		var err error
		raw, err = os.ReadFile(input[1:])
		if err != nil {
			return fmt.Errorf("reading JSON file %s: %w", input[1:], err)
		}
	} else {
		raw = []byte(input)
	}

	jsonData = make(map[string]any)
	if err := json.Unmarshal(raw, &jsonData); err != nil {
		return fmt.Errorf("invalid JSON input: %w", err)
	}

	JSONInputActive = true
	OutputJSON = true
	return nil
}

// ResetJSONMode clears JSON mode state. Used in tests.
func ResetJSONMode() {
	JSONInputActive = false
	OutputJSON = false
	jsonData = nil
}

// JSONData returns the raw parsed JSON input map.
func JSONData() map[string]any {
	return jsonData
}

// GetString returns a string value from JSON input.
func GetString(key string) (string, bool) {
	if jsonData == nil {
		return "", false
	}
	v, ok := jsonData[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

// GetInt returns an int value from JSON input.
func GetInt(key string) (int, bool) {
	if jsonData == nil {
		return 0, false
	}
	v, ok := jsonData[key]
	if !ok {
		return 0, false
	}
	// JSON numbers decode as float64
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// GetBool returns a bool value from JSON input.
func GetBool(key string) (bool, bool) {
	if jsonData == nil {
		return false, false
	}
	v, ok := jsonData[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	if !ok {
		return false, false
	}
	return b, true
}

// MustGetString returns a string from JSON input or an error if missing.
func MustGetString(key string) (string, error) {
	s, ok := GetString(key)
	if !ok {
		return "", fmt.Errorf("missing required JSON field: %q", key)
	}
	return s, nil
}

