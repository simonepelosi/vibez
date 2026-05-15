//go:build darwin

package assets

import (
	"os"
	"path/filepath"
)

// InstallIcon writes a user-cache copy of the icon for TUI metadata. macOS has
// no MPRIS desktop-entry lookup, so no app database refresh is needed.
func InstallIcon() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, "Library", "Caches", "vibez")
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
