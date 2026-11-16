//go:build linux

package browserless

import (
	"os"
	"path/filepath"
)

// Standard locations where libwidevinecdm.so may exist on Linux.
func candidatePaths() []string {
	home, homeErr := os.UserHomeDir()
	hasHome := homeErr == nil && home != "" && filepath.IsAbs(home)
	var res []string

	patterns := []string{
		"/usr/lib/firefox/gmp-widevinecdm/*/libwidevinecdm.so",
	}
	if hasHome {
		patterns = append(patterns,
			filepath.Join(home, ".config", "google-chrome", "WidevineCdm", "*", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
			filepath.Join(home, ".config", "chromium", "WidevineCdm", "*", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
			filepath.Join(home, ".mozilla", "firefox", "*", "gmp-widevinecdm", "*", "libwidevinecdm.so"),
		)
	}
	for _, pattern := range patterns {
		if matches, err := filepath.Glob(pattern); err == nil {
			for _, m := range matches {
				if filepath.IsAbs(m) {
					res = append(res, m)
				}
			}
		}
	}

	res = append(res,
		"/opt/google/chrome/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/opt/google/chrome-unstable/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/usr/lib/chromium/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/usr/lib/chromium-browser/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/usr/lib/firefox/gmp-widevinecdm/system-installed/libwidevinecdm.so",
		"/var/lib/flatpak/app/org.chromium.Chromium/current/active/files/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
	)
	if hasHome {
		res = append(res,
			filepath.Join(home, ".cache", "vibez", "chrome", "opt", "google", "chrome", "WidevineCdm", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
			filepath.Join(home, ".cache", "vibez", "cdm", "libwidevinecdm.so"),
		)
	}
	return res
}

// DefaultCDMPath returns the destination path for the cached standalone CDM library on Linux.
// Returns "" if the user home directory cannot be resolved safely.
func DefaultCDMPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, ".cache", "vibez", "cdm", "libwidevinecdm.so")
}
