package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/config"
)

// writeCfg writes a JSON config to a temp file and returns the path.
func writeCfg(t *testing.T, v any) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(f).Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func TestConfigPath_Override(t *testing.T) {
	want := "/custom/path/config.json"
	got, err := config.ConfigPath(want)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestConfigPath_Default(t *testing.T) {
	got, err := config.ConfigPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "vibez", "config.json")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoad_CreatesDefaultWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-dir", "config.json")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.StoreFront != "" {
		t.Errorf("StoreFront = %q, want %q (auto-detected from Apple)", cfg.StoreFront, "")
	}
	if cfg.AuthPort != 7777 {
		t.Errorf("AuthPort = %d, want %d", cfg.AuthPort, 7777)
	}
	if cfg.Provider != "apple" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "apple")
	}
	if cfg.Theme != "default" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "default")
	}

	// File must have been written.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file not created: %v", err)
	}
}

func TestLoad_ReadsExistingConfig(t *testing.T) {
	path := writeCfg(t, map[string]any{
		"apple_developer_token": "dev-tok",
		"apple_user_token":      "usr-tok",
		"storefront":            "it",
		"auth_port":             9999,
		"provider":              "spotify",
		"theme":                 "dark",
	})

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.AppleDeveloperToken != "dev-tok" {
		t.Errorf("AppleDeveloperToken = %q", cfg.AppleDeveloperToken)
	}
	if cfg.AppleUserToken != "usr-tok" {
		t.Errorf("AppleUserToken = %q", cfg.AppleUserToken)
	}
	if cfg.StoreFront != "it" {
		t.Errorf("StoreFront = %q", cfg.StoreFront)
	}
	if cfg.AuthPort != 9999 {
		t.Errorf("AuthPort = %d", cfg.AuthPort)
	}
}

func TestLoad_DefaultsPreservedForMissingKeys(t *testing.T) {
	// Only override one field; defaults should fill the rest.
	path := writeCfg(t, map[string]any{
		"storefront": "gb",
	})

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.StoreFront != "gb" {
		t.Errorf("StoreFront = %q, want %q", cfg.StoreFront, "gb")
	}
	if cfg.AuthPort != 7777 {
		t.Errorf("AuthPort = %d, want default 7777", cfg.AuthPort)
	}
	if cfg.Provider != "apple" {
		t.Errorf("Provider = %q, want default apple", cfg.Provider)
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := &config.Config{
		AppleDeveloperToken: "devtoken",
		AppleUserToken:      "usertoken",
		AppleKeyID:          "KEYID123",
		AppleTeamID:         "TEAMID456",
		StoreFront:          "de",
		AuthPort:            8888,
		Provider:            "apple",
		Theme:               "dark",
	}

	if err := original.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}

	if loaded.AppleDeveloperToken != original.AppleDeveloperToken {
		t.Errorf("AppleDeveloperToken mismatch")
	}
	if loaded.AppleUserToken != original.AppleUserToken {
		t.Errorf("AppleUserToken mismatch")
	}
	if loaded.StoreFront != original.StoreFront {
		t.Errorf("StoreFront mismatch")
	}
	if loaded.AuthPort != original.AuthPort {
		t.Errorf("AuthPort mismatch")
	}
}

func TestSave_CreatesDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "nested", "dir", "config.json")
	cfg := &config.Config{StoreFront: "fr", AuthPort: 7777, Provider: "apple", Theme: "default"}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestSave_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := &config.Config{StoreFront: "us", AuthPort: 7777, Provider: "apple", Theme: "default"}

	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestLoad_FileIsDirectory(t *testing.T) {
	// Using a directory path as the config file should cause a read error.
	dirPath := t.TempDir()
	_, err := config.Load(dirPath)
	if err == nil {
		t.Fatal("expected error when config path is a directory, got nil")
	}
}

func TestConfigPath_UsesHomeEnv(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	got, err := config.ConfigPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(got, tmpHome) {
		t.Errorf("ConfigPath should use HOME env, got %q (HOME=%q)", got, tmpHome)
	}
}

func TestSave_WriteError(t *testing.T) {
	// Make the directory unwritable so WriteFile fails.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg := &config.Config{StoreFront: "us", AuthPort: 7777, Provider: "apple", Theme: "default"}

	// First save succeeds.
	if err := cfg.Save(path); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Make dir read-only so the file can't be overwritten.
	if err := os.Chmod(dir, 0o400); err != nil {
		t.Skip("cannot change dir permissions:", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: restoring temp dir permissions for cleanup
			t.Logf("cleanup chmod: %v", err)
		}
	})

	err := cfg.Save(path)
	if err == nil {
		t.Error("expected error when directory is read-only, got nil")
	}
}

// --- normalize called via Load ---

func TestLoad_NormalizesOnLoad(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "config.json")

// Write a config with 0 AuthPort and empty Provider — normalize should fix both.
raw := `{"auth_port": 0, "provider": "", "storefront": "us"}`
if err := os.WriteFile(path, []byte(raw), 0o600); err != nil { //nolint:gosec // test fixture
t.Fatal(err)
}

cfg, err := config.Load(path)
if err != nil {
t.Fatalf("Load: %v", err)
}
if cfg.AuthPort != 7777 {
t.Errorf("AuthPort = %d, want 7777 (normalize should have set it)", cfg.AuthPort)
}
if cfg.Provider != "apple" {
t.Errorf("Provider = %q, want 'apple' (normalize should have set it)", cfg.Provider)
}
}

// --- Save with override path ---

func TestSave_WithOverridePath(t *testing.T) {
dir := t.TempDir()
path := filepath.Join(dir, "override-config.json")
cfg := &config.Config{AuthPort: 1234, Provider: "apple", StoreFront: "gb"}
if err := cfg.Save(path); err != nil {
t.Fatalf("Save: %v", err)
}
// Verify file exists and is valid JSON.
data, err := os.ReadFile(path) //nolint:gosec // test fixture
if err != nil {
t.Fatalf("ReadFile: %v", err)
}
var loaded config.Config
if err := json.Unmarshal(data, &loaded); err != nil {
t.Fatalf("Unmarshal: %v", err)
}
if loaded.AuthPort != 1234 {
t.Errorf("AuthPort = %d, want 1234", loaded.AuthPort)
}
}
