//go:build linux

// Package cdp provides an Apple Music player that uses the Chrome DevTools
// Protocol to drive a system Chrome/Chromium instance with built-in Widevine,
// enabling full-track DRM playback without building custom CDM plugins.
package cdp

import (
	"errors"
	"os"
	"os/exec"
)

// chromeCandidates is the ordered list of executable paths to probe for a
// Chrome/Chromium binary with built-in Widevine support.
var chromeCandidates = []string{
	"/usr/bin/google-chrome",
	"/usr/bin/google-chrome-stable",
	"/usr/bin/chromium",
	"/usr/bin/chromium-browser",
	"/opt/google/chrome/chrome",
	"/snap/bin/chromium",
	"/snap/bin/google-chrome",
}

// findChrome returns the path to the first Chrome/Chromium binary found on
// this system, or an error if none is available.
func findChrome() (string, error) {
	for _, p := range chromeCandidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Also honour whatever is in PATH as a last resort.
	if p, err := exec.LookPath("chromium"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("google-chrome"); err == nil {
		return p, nil
	}
	return "", errors.New("no Chrome or Chromium installation found; " +
		"install google-chrome or chromium to enable full-track playback")
}
