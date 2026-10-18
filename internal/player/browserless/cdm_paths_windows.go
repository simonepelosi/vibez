//go:build windows

package browserless

import (
	"os"
	"path/filepath"
)

const cdmLibName = "widevinecdm.dll"

// Standard locations where widevinecdm.dll may exist on Windows: Chromium
// browsers' component-updated copy (user data) and bundled copy (install dir),
// and Firefox's GMP profile directory.
func candidatePaths() []string {
	var res []string

	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	programFiles := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), filepath.Join(local, "Programs")}

	const sub = "WidevineCdm"
	plat := filepath.Join("_platform_specific", "win_x64", cdmLibName)

	var patterns []string
	userData := []string{
		filepath.Join("Google", "Chrome", "User Data"),
		filepath.Join("Microsoft", "Edge", "User Data"),
		filepath.Join("BraveSoftware", "Brave-Browser", "User Data"),
	}
	for _, rel := range userData {
		if local != "" {
			patterns = append(patterns, filepath.Join(local, rel, sub, "*", plat))
		}
	}
	installs := []string{
		filepath.Join("Google", "Chrome", "Application"),
		filepath.Join("Microsoft", "Edge", "Application"),
		filepath.Join("BraveSoftware", "Brave-Browser", "Application"),
	}
	for _, pf := range programFiles {
		if pf == "" {
			continue
		}
		for _, rel := range installs {
			patterns = append(patterns, filepath.Join(pf, rel, "*", sub, plat))
		}
	}
	if roaming != "" {
		patterns = append(patterns, filepath.Join(roaming, "Mozilla", "Firefox", "Profiles", "*", "gmp-widevinecdm", "*", cdmLibName))
	}

	for _, pattern := range patterns {
		if matches, err := filepath.Glob(pattern); err == nil {
			res = append(res, matches...)
		}
	}
	return append(res, DefaultCDMPath())
}

// DefaultCDMPath returns the destination path for the cached standalone CDM library on Windows.
func DefaultCDMPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "vibez", "cdm", cdmLibName)
}
