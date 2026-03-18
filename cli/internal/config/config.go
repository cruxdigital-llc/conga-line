package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Region        string `toml:"region"`
	SSOStartURL   string `toml:"sso_start_url"`
	SSOAccountID  string `toml:"sso_account_id"`
	SSORoleName   string `toml:"sso_role_name"`
	InstanceTag   string `toml:"instance_tag"`
	OpenClawImage string `toml:"openclaw_image"`
}

func Defaults() *Config {
	return &Config{
		InstanceTag: "openclaw-host",
	}
}

func (c *Config) RequiredFieldsMissing() []string {
	var missing []string
	if c.Region == "" {
		missing = append(missing, "region")
	}
	if c.SSOStartURL == "" {
		missing = append(missing, "sso_start_url")
	}
	if c.SSOAccountID == "" {
		missing = append(missing, "sso_account_id")
	}
	if c.OpenClawImage == "" {
		missing = append(missing, "openclaw_image")
	}
	return missing
}

func (c *Config) Save() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dir := filepath.Join(home, ".cruxclaw")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	p := filepath.Join(dir, "config.toml")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	return toml.NewEncoder(f).Encode(c)
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
	if v := os.Getenv("CRUXCLAW_OPENCLAW_IMAGE"); v != "" {
		cfg.OpenClawImage = v
	}

	return cfg
}
