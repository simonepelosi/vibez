package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	AppleDeveloperToken string `json:"apple_developer_token"`
	AppleUserToken      string `json:"apple_user_token"`
	AppleKeyID          string `json:"apple_key_id"`
	AppleTeamID         string `json:"apple_team_id"`
	StoreFront          string `json:"storefront"`
	AuthPort            int    `json:"auth_port"`
	Provider            string `json:"provider"`
	Theme               string `json:"theme"`
	// Last.fm scrobbling. LastfmAPIKey and LastfmAPISecret are typically
	// embedded in the binary at build time via ldflags; set them manually here
	// only when building from source without the embedded keys.
	LastfmAPIKey     string `json:"lastfm_api_key,omitempty"`
	LastfmAPISecret  string `json:"lastfm_api_secret,omitempty"`
	LastfmSessionKey string `json:"lastfm_session_key,omitempty"`
}

func defaults() *Config {
	return &Config{
		AuthPort: 7777,
		Provider: "apple",
		Theme:    "default",
	}
}

func ConfigPath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "vibez", "config.json"), nil
}

func Load(override string) (*Config, error) {
	path, err := ConfigPath(override)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // path comes from user config, not external input
	if os.IsNotExist(err) {
		cfg := defaults()
		if saveErr := cfg.save(path); saveErr != nil {
			return nil, fmt.Errorf("creating default config: %w", saveErr)
		}
		fmt.Printf("Created default config at %s\n", path)
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := defaults()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	cfg.normalize()
	return cfg, nil
}

// normalize replaces zero values with defaults so that an existing config file
// with missing or empty fields still behaves correctly.
func (c *Config) normalize() {
	if c.AuthPort == 0 {
		c.AuthPort = 7777
	}
	if c.Provider == "" {
		c.Provider = "apple"
	}
}

func (c *Config) Save(override string) error {
	path, err := ConfigPath(override)
	if err != nil {
		return err
	}
	return c.save(path)
}

func (c *Config) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}
