package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/audioquality"
)

var saveMu sync.Mutex

type Config struct {
	// path remembers where this config was loaded from. It is deliberately not
	// serialized; it only ensures that Save("") persists back to the selected
	// --config file instead of silently falling back to the default profile.
	path string

	AppleDeveloperToken string `json:"apple_developer_token"`
	AppleUserToken      string `json:"apple_user_token"`
	AppleKeyID          string `json:"apple_key_id"`
	AppleTeamID         string `json:"apple_team_id"`
	StoreFront          string `json:"storefront"`
	AuthPort            int    `json:"auth_port"`
	Provider            string `json:"provider"`
	Theme               string `json:"theme"`
	AudioQuality        string `json:"audio_quality,omitempty"`
	// Volume is the last user-set playback volume (0.0–1.0). nil means
	// "not yet saved" and the player default (1.0) is used on startup.
	Volume *float64 `json:"volume,omitempty"`
	// Last.fm scrobbling. LastfmAPIKey and LastfmAPISecret are typically
	// embedded in the binary at build time via ldflags; set them manually here
	// only when building from source without the embedded keys.
	LastfmAPIKey     string `json:"lastfm_api_key,omitempty"`
	LastfmAPISecret  string `json:"lastfm_api_secret,omitempty"`
	LastfmSessionKey string `json:"lastfm_session_key,omitempty"`
	// EQBands stores the last saved 10-band equalizer settings. nil means flat.
	EQBands []EQBand `json:"eq_bands,omitempty"`
	// WSL enables audio tuning workarounds for WSL2 environments where Hyper-V
	// scheduler jitter causes audio underruns with default Chrome buffer sizes.
	WSL bool `json:"wsl,omitempty"`
	// AlbumArt starts the now-playing panel in the album-art view (the :art
	// command toggles it at runtime and persists the choice here).
	AlbumArt bool `json:"album_art,omitempty"`
	// MusicDir is the local directory to scan for audio files
	// Supports MP3, FLAC, M4A. Scanned recursively.
	MusicDir string `json:"music_dir,omitempty"`
	// HybridPlayback substitutes matching lossless files from MusicDir for
	// tracks in Apple Music queues. Off by default.
	HybridPlayback bool `json:"hybrid_playback,omitempty"`
	// DiscordRPC enables Discord Rich Presence (defaults to true).
	DiscordRPC *bool `json:"discord_rpc,omitempty"`
	// DiscordClientID overrides the default Discord application client ID.
	DiscordClientID string `json:"discord_client_id,omitempty"`
	// VibeBackend selects the Vibe Mode planner backend ("auto", "ollama",
	// "lmstudio", "llamacpp", "openai", or "keyword"). Defaults to "auto"
	// (prefers local on-device HTTP runtimes and falls back to KeywordAgent).
	VibeBackend string `json:"vibe_backend,omitempty"`
	// VibePrivacy controls whether "auto" stays strictly on-device
	// ("local-only", default) or may use external/cloud backends ("auto").
	VibePrivacy string `json:"vibe_privacy,omitempty"`
	// VibeModel optionally pins the model name used by the Vibe planner.
	VibeModel string `json:"vibe_model,omitempty"`
	// VibeEndpoint optionally overrides the local HTTP runtime base URL
	// (e.g. "http://127.0.0.1:11434" or "http://127.0.0.1:8080/v1").
	VibeEndpoint string `json:"vibe_endpoint,omitempty"`
	// VibeTimeout optionally overrides how long Vibe Mode waits for the AI
	// planner to return a plan (e.g. "30s", "1m", or "45" for seconds).
	// Unset or invalid values fall back to the planner default (30s).
	VibeTimeout string `json:"vibe_timeout,omitempty"`
}

// VibePlanTimeout parses VibeTimeout as a Go duration or a positive number of
// seconds, returning 0 when unset or invalid so callers fall back to default.
func (c *Config) VibePlanTimeout() time.Duration {
	if c == nil {
		return 0
	}
	raw := strings.TrimSpace(c.VibeTimeout)
	if raw == "" {
		return 0
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

type EQBand struct {
	Frequency float64 `json:"frequency"`
	Q         float64 `json:"q"`
	Gain      float64 `json:"gain"`
}

// VolumeOrDefault returns the saved volume, or 1.0 if none has been stored yet.
func (c *Config) VolumeOrDefault() float64 {
	if c.Volume != nil {
		return *c.Volume
	}
	return 1.0
}

// DiscordRPCEnabled reports whether Discord Rich Presence is enabled (default true).
func (c *Config) DiscordRPCEnabled() bool {
	if c.DiscordRPC != nil {
		return *c.DiscordRPC
	}
	return true
}

// SetVolume updates the in-memory volume field. Call Save to persist it.
func (c *Config) SetVolume(v float64) {
	c.Volume = &v
}

func defaults() *Config {
	return &Config{
		AuthPort:     7777,
		Provider:     "apple",
		Theme:        "default",
		AudioQuality: "high",
	}
}

func (c *Config) AudioBitrateKbps() (int, error) {
	return audioquality.Parse(c.AudioQuality)
}

func (c *Config) SetAudioBitrate(kbps int) error {
	value, err := audioquality.ConfigValue(kbps)
	if err != nil {
		return err
	}
	c.AudioQuality = value
	return nil
}

func ConfigPath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if runtime.GOOS == "windows" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("could not determine config directory: %w", err)
		}
		return filepath.Join(dir, "vibez", "config.json"), nil
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
		cfg.path = path
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
	cfg.path = path
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
	if c.AudioQuality == "" {
		c.AudioQuality = "high"
	}
}

func (c *Config) Save(override string) error {
	saveMu.Lock()
	defer saveMu.Unlock()

	path := override
	if path == "" {
		path = c.path
	}
	if path == "" {
		var err error
		path, err = ConfigPath("")
		if err != nil {
			return err
		}
	}
	if err := c.save(path); err != nil {
		return err
	}
	c.path = path
	return nil
}

func (c *Config) save(path string) error {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp config: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if runtime.GOOS != "windows" {
		if err := tmp.Chmod(0o600); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chmod temp config: %w", err)
		}
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	cleanup = false
	return nil
}
