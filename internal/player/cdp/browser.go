//go:build linux

// Package cdp provides an Apple Music player backed by a Playwright-managed
// Chrome browser. On first run, Playwright downloads Chrome (~150 MB) into
// ~/.cache/vibez/browsers/ — NOT a system package install; it is vibez's own
// private browser, completely isolated from any system Chrome/Chromium.
// Subsequent launches reuse the cached binary (instant, no network).
package cdp

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	playwright "github.com/playwright-community/playwright-go"
)

func baseDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "vibez")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "vibez")
}

func driverDir() string   { return filepath.Join(baseDir(), "driver") }
func browsersDir() string { return filepath.Join(baseDir(), "browsers") }

// setBrowserEnv makes Playwright use only vibez's private cache, completely
// ignoring any system Chrome installation.
func setBrowserEnv() {
	// PLAYWRIGHT_BROWSERS_PATH tells the playwright CLI where to install and
	// find browsers. Pointing it to our cache means system Chrome is never
	// used — even if it is installed.
	_ = os.Setenv("PLAYWRIGHT_BROWSERS_PATH", browsersDir())
	// Skip apt-get / polkit host-library validation. Required libs (glib,
	// nss, etc.) are present on any Ubuntu desktop already.
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
}

// EnsureBrowser downloads Chrome into vibez's private cache if not already
// present. Never touches system package managers. w receives progress output;
// pass os.Stderr to show progress on first run, io.Discard to silence it.
func EnsureBrowser(w io.Writer) error {
	setBrowserEnv()
	if err := playwright.Install(&playwright.RunOptions{
		DriverDirectory: driverDir(),
		Browsers:        []string{"chrome"},
		Stdout:          w,
		Stderr:          w,
	}); err != nil {
		return fmt.Errorf("browser setup: %w", err)
	}
	return nil
}

// runPlaywright starts the Playwright driver using vibez's private cache.
func runPlaywright() (*playwright.Playwright, error) {
	setBrowserEnv()
	pw, err := playwright.Run(&playwright.RunOptions{
		DriverDirectory:     driverDir(),
		SkipInstallBrowsers: true, // EnsureBrowser handles the download
	})
	if err != nil {
		return nil, fmt.Errorf("playwright driver: %w", err)
	}
	return pw, nil
}

// findCachedChrome returns the path to the Google Chrome binary that was
// downloaded into vibez's private browsers cache by EnsureBrowser.
// Using ExecutablePath instead of Channel avoids touching system Chrome.
func findCachedChrome() (string, error) {
	// Playwright stores Chrome as: $PLAYWRIGHT_BROWSERS_PATH/chrome-REVISION/chrome-linux/chrome
	pattern := filepath.Join(browsersDir(), "chrome-*", "chrome-linux", "chrome")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("chrome not found in vibez cache (%s); run vibez once to download it", browsersDir())
	}
	return matches[len(matches)-1], nil // use latest if multiple revisions exist
}
