//go:build linux

// Package cdp provides an Apple Music player backed by a Playwright-managed
// Chrome browser. On first run, Playwright downloads Chrome (~150 MB) into
// ~/.cache/ms-playwright/ — this is NOT a system package install; it is
// vibez's own private browser and has no effect on apt/snap/flatpak.
// Subsequent launches reuse the cached binary (instant, no network).
package cdp

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	playwright "github.com/playwright-community/playwright-go"
)

// cacheDir returns the directory where Playwright stores the Chrome binary.
// Using XDG_CACHE_HOME keeps it out of ~/.local/share and makes it clearly
// a cache that can be deleted without losing user data.
func cacheDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "vibez", "playwright")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "vibez", "playwright")
}

// EnsureBrowser downloads Chrome into vibez's cache if not already present.
// It sets PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS=1 so that Playwright
// never calls apt-get or polkit — it only downloads the browser binary.
// w receives progress lines; pass os.Stderr or io.Discard to silence it.
func EnsureBrowser(w io.Writer) error {
	// This env var tells Playwright to skip running apt-get / polkit to
	// install system library dependencies. Chrome on Ubuntu 22.04+ works
	// fine without this step because the required libs (glib, nss, etc.)
	// are already present on any desktop system.
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")

	if err := playwright.Install(&playwright.RunOptions{
		DriverDirectory: cacheDir(),
		Browsers:        []string{"chrome"},
		Stdout:          w,
		Stderr:          w,
	}); err != nil {
		return fmt.Errorf("browser setup: %w", err)
	}
	return nil
}

// runPlaywright starts the Playwright driver using vibez's private cache dir.
func runPlaywright() (*playwright.Playwright, error) {
	_ = os.Setenv("PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS", "1")
	pw, err := playwright.Run(&playwright.RunOptions{
		DriverDirectory:     cacheDir(),
		SkipInstallBrowsers: true, // already handled by EnsureBrowser
	})
	if err != nil {
		return nil, fmt.Errorf("playwright driver: %w", err)
	}
	return pw, nil
}
