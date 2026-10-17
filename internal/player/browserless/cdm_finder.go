//go:build linux

package browserless

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Standard locations where libwidevinecdm.so may exist on Linux.
// System browser locations come before the standalone cache so a stale copy in
// ~/.cache/vibez/cdm never shadows a browser-updated CDM.
func candidatePaths() []string {
	home, _ := os.UserHomeDir()
	var paths []string

	// Component-updated Widevine inside user browser profiles (e.g. ~/.config/google-chrome/WidevineCdm/*/_platform_specific/linux_x64/libwidevinecdm.so)
	for _, pattern := range []string{
		filepath.Join(home, ".config", "google-chrome", "WidevineCdm", "*", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
		filepath.Join(home, ".config", "chromium", "WidevineCdm", "*", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
		"/usr/lib/firefox/gmp-widevinecdm/*/libwidevinecdm.so",
	} {
		if matches, err := filepath.Glob(pattern); err == nil {
			paths = append(paths, matches...)
		}
	}

	paths = append(paths,
		"/opt/google/chrome/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/opt/google/chrome-unstable/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/usr/lib/chromium/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/usr/lib/chromium-browser/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		filepath.Join(home, ".cache", "vibez", "chrome", "opt", "google", "chrome", "WidevineCdm", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
		"/usr/lib/firefox/gmp-widevinecdm/system-installed/libwidevinecdm.so",
		"/var/lib/flatpak/app/org.chromium.Chromium/current/active/files/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		filepath.Join(home, ".cache", "vibez", "cdm", "libwidevinecdm.so"),
	)
	return paths
}

// FindCDM searches the host for an existing libwidevinecdm.so binary and
// returns the newest valid candidate so revoked older copies are skipped.
func FindCDM() string {
	if envPath := os.Getenv("VIBEZ_CDM_PATH"); envPath != "" {
		if fi, err := os.Stat(envPath); err == nil && !fi.IsDir() && fi.Size() > 100000 { //nolint:gosec // G703: explicit user override via VIBEZ_CDM_PATH
			return envPath
		}
	}

	var bestPath string
	var bestMod time.Time
	for _, p := range candidatePaths() {
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() || fi.Size() <= 100000 {
			continue
		}
		if bestPath == "" || fi.ModTime().After(bestMod) {
			bestPath = p
			bestMod = fi.ModTime()
		}
	}
	return bestPath
}

// Destination path for the cached standalone CDM library.
func DefaultCDMPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "vibez", "cdm", "libwidevinecdm.so")
}

// EnsureCDM returns an existing CDM library path from the host or user cache.
func EnsureCDM() (string, error) {
	if p := FindCDM(); p != "" {
		return p, nil
	}

	dest := DefaultCDMPath()
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return "", fmt.Errorf("failed to create cdm cache directory: %w", err)
	}

	// We can check if a user placed it in ~/.config/vibez/libwidevinecdm.so
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "vibez", "libwidevinecdm.so")
	if fi, err := os.Stat(cfgPath); err == nil && !fi.IsDir() && fi.Size() > 100000 {
		// Copy to cache
		if data, err := os.ReadFile(cfgPath); err == nil { //nolint:gosec // G304: user config path in home dir
			if err := os.WriteFile(dest, data, 0600); err == nil { //nolint:gosec // G306: destination path inside user cache
				return dest, nil
			}
		}
	}

	return "", fmt.Errorf("libwidevinecdm.so not found on system. Please place libwidevinecdm.so in %s", dest)
}
