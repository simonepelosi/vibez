//go:build windows

package browserless

import (
	"os"
	"path/filepath"

	"github.com/simone-vibes/vibez/internal/player/browserless/cdm"
)

const cdmLibName = "widevinecdm.dll"

func absEnvPath(name string) string {
	val := os.Getenv(name)
	if !cdm.IsSafeAbsPath(val) {
		return ""
	}
	cleaned := filepath.Clean(val)
	if !cdm.IsSafeAbsPath(cleaned) {
		return ""
	}
	return cleaned
}

// Standard locations where widevinecdm.dll may exist on Windows: Chromium
// browsers' component-updated copy (user data) and bundled copy (install dir),
// and Firefox's GMP profile directory.
func candidatePaths() []string {
	var res []string

	local := absEnvPath("LOCALAPPDATA")
	roaming := absEnvPath("APPDATA")
	var programFiles []string
	for _, root := range []string{absEnvPath("ProgramFiles"), absEnvPath("ProgramFiles(x86)")} {
		if root != "" {
			programFiles = append(programFiles, root)
		}
	}
	if local != "" {
		programFiles = append(programFiles, filepath.Join(local, "Programs"))
	}

	const sub = "WidevineCdm"
	plat := filepath.Join("_platform_specific", "win_x64", cdmLibName)

	var patterns []string
	if local != "" {
		userData := []string{
			filepath.Join("Google", "Chrome", "User Data"),
			filepath.Join("Microsoft", "Edge", "User Data"),
			filepath.Join("BraveSoftware", "Brave-Browser", "User Data"),
		}
		for _, rel := range userData {
			patterns = append(patterns, filepath.Join(local, rel, sub, "*", plat))
		}
	}
	installs := []string{
		filepath.Join("Google", "Chrome", "Application"),
		filepath.Join("Microsoft", "Edge", "Application"),
		filepath.Join("BraveSoftware", "Brave-Browser", "Application"),
	}
	for _, pf := range programFiles {
		for _, rel := range installs {
			patterns = append(patterns, filepath.Join(pf, rel, "*", sub, plat))
		}
	}
	if roaming != "" {
		patterns = append(patterns, filepath.Join(roaming, "Mozilla", "Firefox", "Profiles", "*", "gmp-widevinecdm", "*", cdmLibName))
	}

	for _, pattern := range patterns {
		if matches, err := filepath.Glob(pattern); err == nil {
			for _, m := range matches {
				if cdm.IsSafeAbsPath(m) {
					res = append(res, m)
				}
			}
		}
	}
	if def := DefaultCDMPath(); def != "" {
		res = append(res, def)
	}
	return res
}

// DefaultCDMPath returns the destination path for the cached standalone CDM library on Windows.
// Returns "" if no user-scoped cache or profile directory can be resolved safely.
func DefaultCDMPath() string {
	if dir, err := os.UserCacheDir(); err == nil && cdm.IsSafeAbsPath(dir) {
		if cleaned := filepath.Clean(dir); cdm.IsSafeAbsPath(cleaned) {
			return filepath.Join(cleaned, "vibez", "cdm", cdmLibName)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && cdm.IsSafeAbsPath(home) {
		if cleaned := filepath.Clean(home); cdm.IsSafeAbsPath(cleaned) {
			return filepath.Join(cleaned, "AppData", "Local", "vibez", "cdm", cdmLibName)
		}
	}
	return ""
}
