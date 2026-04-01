//go:build linux

// Package cdp provides an Apple Music player backed by a private Chrome
// installation managed entirely by vibez. On first run, vibez downloads the
// Google Chrome .deb (~130 MB) from Google's public CDN, extracts it with
// dpkg-deb (no root required), and caches the result in ~/.cache/vibez/chrome/.
// Subsequent launches use the cached binary instantly — no system packages,
// no apt-get, no sudo, invisible to the rest of the OS.
// Widevine CDM is bundled inside Chrome and is available automatically.
package cdp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	playwright "github.com/playwright-community/playwright-go"
)

const chromeDebURL = "https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb"

func baseDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "vibez")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "vibez")
}

// chromeInstallDir is where the Chrome .deb is extracted.
func chromeInstallDir() string { return filepath.Join(baseDir(), "chrome") }

// driverDir is where the Playwright Node.js driver lives.
func driverDir() string { return filepath.Join(baseDir(), "driver") }

// ChromePath returns the path to the cached Chrome binary.
func ChromePath() string {
	return filepath.Join(chromeInstallDir(), "opt", "google", "chrome", "chrome")
}

// EnsureBrowser downloads and extracts Google Chrome into vibez's private
// cache directory if not already present. Never calls apt-get or sudo.
// onProgress is called with human-readable status strings (e.g. "Downloading
// Chrome… 42%", "Extracting Chrome…"). Pass func(string){} to silence.
func EnsureBrowser(onProgress func(string)) error {
	if _, err := os.Stat(ChromePath()); err == nil {
		return nil // already installed
	}

	// Also ensure the playwright driver is available (no browser install via playwright).
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	onProgress("Fetching dependencies…")
	if err := playwright.Install(&playwright.RunOptions{
		DriverDirectory:     driverDir(),
		SkipInstallBrowsers: true,
	}); err != nil {
		return fmt.Errorf("playwright driver: %w", err)
	}

	debPath := filepath.Join(baseDir(), "chrome.deb")
	if err := downloadFile(onProgress, debPath, chromeDebURL); err != nil {
		return fmt.Errorf("download Chrome: %w", err)
	}
	defer os.Remove(debPath) //nolint:errcheck // best-effort cleanup of temp file

	onProgress("Extracting Chrome…")
	if err := os.MkdirAll(chromeInstallDir(), 0o750); err != nil {
		return fmt.Errorf("create chrome dir: %w", err)
	}
	// dpkg-deb --extract extracts the .deb payload without root.
	cmd := exec.Command("dpkg-deb", "--extract", debPath, chromeInstallDir()) //nolint:gosec // paths are constructed internally
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("extract chrome: %w", err)
	}

	if _, err := os.Stat(ChromePath()); err != nil {
		return fmt.Errorf("chrome binary not found after extraction: %w", err)
	}
	onProgress("Chrome ready.")
	return nil
}

// runPlaywright starts the Playwright driver backed by our cached Chrome.
func runPlaywright() (*playwright.Playwright, error) {
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	pw, err := playwright.Run(&playwright.RunOptions{
		DriverDirectory:     driverDir(),
		SkipInstallBrowsers: true,
	})
	if err != nil {
		return nil, fmt.Errorf("playwright driver: %w", err)
	}
	return pw, nil
}
