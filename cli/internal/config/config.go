package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Region       string `toml:"region"`
	SSOStartURL  string `toml:"sso_start_url"`
	SSOAccountID string `toml:"sso_account_id"`
	SSORoleName  string `toml:"sso_role_name"`
	InstanceTag  string `toml:"instance_tag"`
}

func Defaults() *Config {
	return &Config{
		Region:       "us-east-2",
		SSOStartURL:  "https://example-sso.awsapps.com/start/",
		SSOAccountID: "123456789012",
		SSORoleName:  "OpenClawUser",
		InstanceTag:  "openclaw-host",
	}
}

func Load() *Config {
	cfg := Defaults()

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg
	}

	configPath := filepath.Join(home, ".cruxclaw", "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		toml.DecodeFile(configPath, cfg)
	}

	if v := os.Getenv("CRUXCLAW_REGION"); v != "" {
		cfg.Region = v
	}
	if v := os.Getenv("CRUXCLAW_SSO_START_URL"); v != "" {
		cfg.SSOStartURL = v
	}
	if v := os.Getenv("CRUXCLAW_SSO_ACCOUNT_ID"); v != "" {
		cfg.SSOAccountID = v
	}
	if v := os.Getenv("CRUXCLAW_SSO_ROLE_NAME"); v != "" {
		cfg.SSORoleName = v
	}
	if v := os.Getenv("CRUXCLAW_INSTANCE_TAG"); v != "" {
		cfg.InstanceTag = v
	}

	return cfg
}
