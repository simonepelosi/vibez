//go:build linux

package browserless

import (
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

// DefaultCDMPath returns the destination path for the cached standalone CDM library on Linux.
func DefaultCDMPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "vibez", "cdm", "libwidevinecdm.so")
}
