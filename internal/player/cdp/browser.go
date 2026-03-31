//go:build linux

// Package cdp provides an Apple Music player backed by a Playwright-managed
// Chrome browser. Playwright downloads and manages Chrome automatically on
// first run (~300 MB, cached in ~/.cache/ms-playwright). Chrome ships with
// the Widevine CDM, enabling full-track DRM playback without any manual
// browser installation.
package cdp

import (
	"io"

	playwright "github.com/playwright-community/playwright-go"
)

// EnsureBrowser downloads the Chrome browser if not already cached.
// Subsequent calls are instant. w receives progress output; pass os.Stderr
// or io.Discard to silence it. Call this once before New() so the caller
// can show a status message during the (one-time) download.
func EnsureBrowser(w io.Writer) error {
	return playwright.Install(&playwright.RunOptions{
		Browsers: []string{"chrome"},
		Stdout:   w,
		Stderr:   w,
	})
}
