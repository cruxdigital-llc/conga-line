package aws

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DetectSSOProfile scans ~/.aws/config for profiles with SSO configuration
// and returns the first one that has a valid (non-expired) cached SSO token.
// Returns "" if no active SSO profile is found.
func DetectSSOProfile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	configPath := filepath.Join(home, ".aws", "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	profiles := parseSSOProfiles(string(data))
	cachePath := filepath.Join(home, ".aws", "sso", "cache")

	for _, p := range profiles {
		if hasValidToken(cachePath, p.ssoStartURL, p.ssoSession) {
			return p.name
		}
	}
	return ""
}

type ssoProfile struct {
	name        string
	ssoStartURL string
	ssoSession  string
}

// parseSSOProfiles extracts profile names and their SSO start URLs from AWS config INI content.
func parseSSOProfiles(content string) []ssoProfile {
	var profiles []ssoProfile
	var current *ssoProfile
	sessions := make(map[string]string) // session name -> start URL

	lines := strings.Split(content, "\n")

	// First pass: collect sso-session definitions
	var currentSessionName string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[sso-session ") && strings.HasSuffix(line, "]") {
			currentSessionName = strings.TrimSuffix(strings.TrimPrefix(line, "[sso-session "), "]")
		} else if strings.HasPrefix(line, "[") {
			currentSessionName = ""
		} else if currentSessionName != "" {
			if key, val, ok := parseINILine(line); ok && key == "sso_start_url" {
				sessions[currentSessionName] = val
			}
		}
	}

	// Second pass: collect profiles with SSO config
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if current != nil && (current.ssoStartURL != "" || current.ssoSession != "") {
				// Resolve session reference to start URL
				if current.ssoStartURL == "" && current.ssoSession != "" {
					current.ssoStartURL = sessions[current.ssoSession]
				}
				profiles = append(profiles, *current)
			}
			header := line[1 : len(line)-1]
			current = nil
			if header == "default" {
				current = &ssoProfile{name: "default"}
			} else if strings.HasPrefix(header, "profile ") {
				current = &ssoProfile{name: strings.TrimPrefix(header, "profile ")}
			}
			continue
		}
		if current == nil {
			continue
		}
		if key, val, ok := parseINILine(line); ok {
			switch key {
			case "sso_start_url":
				current.ssoStartURL = val
			case "sso_session":
				current.ssoSession = val
			}
		}
	}
	// Don't forget the last profile
	if current != nil && (current.ssoStartURL != "" || current.ssoSession != "") {
		if current.ssoStartURL == "" && current.ssoSession != "" {
			current.ssoStartURL = sessions[current.ssoSession]
		}
		profiles = append(profiles, *current)
	}

	return profiles
}

func parseINILine(line string) (key, val string, ok bool) {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
		return "", "", false
	}
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), true
}

type ssoTokenCache struct {
	ExpiresAt string `json:"expiresAt"`
}

// hasValidToken checks the SSO token cache for a non-expired token matching the given start URL or session name.
func hasValidToken(cachePath, startURL, sessionName string) bool {
	// The SDK caches tokens by SHA1 hash of the start URL or session name
	candidates := []string{}
	if startURL != "" {
		candidates = append(candidates, startURL)
	}
	if sessionName != "" {
		candidates = append(candidates, sessionName)
	}

	for _, key := range candidates {
		h := sha1.Sum([]byte(key))
		filename := hex.EncodeToString(h[:]) + ".json"
		data, err := os.ReadFile(filepath.Join(cachePath, filename))
		if err != nil {
			continue
		}
		var token ssoTokenCache
		if err := json.Unmarshal(data, &token); err != nil {
			continue
		}
		expiry, err := time.Parse(time.RFC3339, token.ExpiresAt)
		if err != nil {
			// Try alternate format used by some AWS CLI versions
			expiry, err = time.Parse("2006-01-02T15:04:05Z", token.ExpiresAt)
			if err != nil {
				continue
			}
		}
		if time.Now().Before(expiry) {
			return true
		}
	}
	return false
}
