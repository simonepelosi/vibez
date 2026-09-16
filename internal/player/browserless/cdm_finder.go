//go:build linux

package browserless

import (
	"fmt"
	"os"
	"path/filepath"
)

// Standard locations where libwidevinecdm.so may exist on Linux.
func candidatePaths() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".cache", "vibez", "cdm", "libwidevinecdm.so"),
		filepath.Join(home, ".cache", "vibez", "chrome", "opt", "google", "chrome", "WidevineCdm", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
		"/usr/lib/chromium/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/usr/lib/chromium-browser/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/opt/google/chrome/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/opt/google/chrome-unstable/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/usr/lib/firefox/gmp-widevinecdm/system-installed/libwidevinecdm.so",
		"/var/lib/flatpak/app/org.chromium.Chromium/current/active/files/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
	}
}

// FindCDM searches the host for an existing libwidevinecdm.so binary.
// Returns the file path if found, or empty string.
func FindCDM() string {
	for _, p := range candidatePaths() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() > 100000 {
			return p
		}
	}
	return ""
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
