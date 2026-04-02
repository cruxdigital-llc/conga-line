package ui

import (
	"encoding/json"
	"fmt"
	"os"
)

// EmitJSON writes a JSON-serializable value to stdout with a trailing newline.
// Uses compact encoding for machine consumption (the primary use case).
func EmitJSON(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		EmitError(fmt.Errorf("failed to marshal JSON output: %w", err))
		return
	}
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
}

// EmitError writes {"error": "message"} to stdout.
func EmitError(err error) {
	data, _ := json.Marshal(map[string]string{"error": err.Error()})
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
}

// Info writes human-readable text to stderr in JSON mode, stdout in text mode.
func Info(format string, args ...any) {
	if OutputJSON {
		fmt.Fprintf(os.Stderr, format, args...)
	} else {
		fmt.Printf(format, args...)
	}
}

// Infoln writes a human-readable line. Stderr in JSON mode, stdout in text mode.
func Infoln(msg string) {
	if OutputJSON {
		fmt.Fprintln(os.Stderr, msg)
	} else {
		fmt.Println(msg)
	}
}
