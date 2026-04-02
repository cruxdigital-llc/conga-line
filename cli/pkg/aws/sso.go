package aws

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AWSProfileInfo holds SSO-related fields for an AWS CLI profile.
type AWSProfileInfo struct {
	Name         string
	Region       string
	SSOStartURL  string
	SSORegion    string
	SSOAccountID string
	SSORoleName  string
	SSOSession   string
}

// GetProfileInfo returns the parsed profile info for a named profile by
// querying the AWS CLI. Returns nil if the profile doesn't exist or the
// AWS CLI is not available.
func GetProfileInfo(name string) *AWSProfileInfo {
	// Quick existence check: ask for region (every profile should have one).
	// If aws configure get fails, the profile doesn't exist.
	if _, ok := awsConfigGet("region", name); !ok {
		// Profile might exist without a region; try sso_start_url instead.
		if _, ok := awsConfigGet("sso_start_url", name); !ok {
			return nil
		}
	}

	info := &AWSProfileInfo{Name: name}
	info.Region, _ = awsConfigGet("region", name)
	info.SSOStartURL, _ = awsConfigGet("sso_start_url", name)
	info.SSORegion, _ = awsConfigGet("sso_region", name)
	info.SSOAccountID, _ = awsConfigGet("sso_account_id", name)
	info.SSORoleName, _ = awsConfigGet("sso_role_name", name)
	info.SSOSession, _ = awsConfigGet("sso_session", name)
	return info
}

// DetectSSOProfile scans configured AWS CLI profiles and returns the name
// of the first SSO profile with a valid (non-expired) cached token.
// Returns "" if no active SSO profile is found.
func DetectSSOProfile() string {
	info := DetectSSOProfileInfo()
	if info == nil {
		return ""
	}
	return info.Name
}

// DetectSSOProfileInfo scans configured AWS CLI profiles and returns the
// first SSO profile that has a valid (non-expired) cached token.
// Returns nil if no active SSO profile is found.
func DetectSSOProfileInfo() *AWSProfileInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	cachePath := filepath.Join(home, ".aws", "sso", "cache")

	profiles := listProfiles()
	for _, name := range profiles {
		startURL, hasURL := awsConfigGet("sso_start_url", name)
		session, hasSession := awsConfigGet("sso_session", name)

		if !hasURL && !hasSession {
			continue
		}

		if hasValidToken(cachePath, startURL, session) {
			info := &AWSProfileInfo{Name: name}
			info.SSOStartURL = startURL
			info.SSOSession = session
			info.Region, _ = awsConfigGet("region", name)
			info.SSORegion, _ = awsConfigGet("sso_region", name)
			info.SSOAccountID, _ = awsConfigGet("sso_account_id", name)
			info.SSORoleName, _ = awsConfigGet("sso_role_name", name)
			return info
		}
	}
	return nil
}

// listProfiles returns all configured AWS CLI profile names.
func listProfiles() []string {
	out, err := exec.Command("aws", "configure", "list-profiles").Output()
	if err != nil {
		return nil
	}
	var profiles []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			profiles = append(profiles, line)
		}
	}
	return profiles
}

// awsConfigGet runs `aws configure get <key> --profile <profile>` and returns
// the value. Returns ("", false) if the key is not set or the command fails.
func awsConfigGet(key, profile string) (string, bool) {
	out, err := exec.Command("aws", "configure", "get", key, "--profile", profile).Output()
	if err != nil {
		return "", false
	}
	val := strings.TrimSpace(string(out))
	if val == "" {
		return "", false
	}
	return val, true
}

type ssoTokenCache struct {
	ExpiresAt string `json:"expiresAt"`
}

// hasValidToken checks the SSO token cache for a non-expired token matching
// the given start URL or session name. The AWS CLI caches tokens as JSON files
// named by SHA1 hash of the key.
func hasValidToken(cachePath, startURL, sessionName string) bool {
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
