//go:build darwin || windows

package assets

import (
	"os"
	"path/filepath"
)

// InstallIcon writes a user-cache copy of the icon for TUI metadata.
// These platforms do not use Linux's MPRIS desktop-entry lookup.
func InstallIcon() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(cache, "vibez")
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec
		return ""
	}
	dst := filepath.Join(dir, "vibez.svg")
	if err := os.WriteFile(dst, Icon, 0o644); err != nil { //nolint:gosec
		return ""
	}
	return dst
}

func InstallDesktopEntry() {}
