package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

func SecretPrompt(label string) (string, error) {
	fmt.Printf("%s: ", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// ConfirmWith reads a yes/no answer from the given reader and writes the prompt to the given writer.
func ConfirmWith(r io.Reader, w io.Writer, prompt string) bool {
	fmt.Fprintf(w, "%s [y/N]: ", prompt)
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}

// Confirm prompts the user on stdout/stdin for a yes/no answer.
func Confirm(prompt string) bool {
	return ConfirmWith(os.Stdin, os.Stdout, prompt)
}

// TextPromptWith reads a line of text from the given reader and writes the prompt to the given writer.
func TextPromptWith(r io.Reader, w io.Writer, label string) (string, error) {
	fmt.Fprintf(w, "%s: ", label)
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return "", fmt.Errorf("no input received")
}

// TextPrompt prompts the user on stdout/stdin for a line of text.
func TextPrompt(label string) (string, error) {
	return TextPromptWith(os.Stdin, os.Stdout, label)
}

// TextPromptWithDefaultFrom reads a line of text with a default value from the given reader/writer.
func TextPromptWithDefaultFrom(r io.Reader, w io.Writer, label, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Fprintf(w, "%s [%s]: ", label, defaultVal)
	} else {
		fmt.Fprintf(w, "%s: ", label)
	}
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		val := strings.TrimSpace(scanner.Text())
		if val == "" {
			return defaultVal, nil
		}
		return val, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}
	return "", fmt.Errorf("no input received (stdin closed)")
}

// TextPromptWithDefault prompts the user on stdout/stdin for a line of text with a default value.
func TextPromptWithDefault(label, defaultVal string) (string, error) {
	return TextPromptWithDefaultFrom(os.Stdin, os.Stdout, label, defaultVal)
}
