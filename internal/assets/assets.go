// Package assets embeds the vibez icon and .desktop file and provides
// helpers to install them into the user's local share directories.
package assets

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed vibez.svg
var Icon []byte

//go:embed vibez.desktop
var DesktopEntry []byte

// InstallIcon writes the bundled SVG icon to the XDG icon theme directory
// and returns its absolute path (used as the notification icon).
// Errors are silently ignored — icon installation is best-effort.
func InstallIcon() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".local", "share", "icons", "hicolor", "scalable", "apps")
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // XDG icon dir; world-execute needed for icon lookup
		return ""
	}
	dst := filepath.Join(dir, "vibez.svg")
	_ = os.WriteFile(dst, Icon, 0o600) //nolint:gosec // SVG icon; not sensitive
	return dst
}

// InstallDesktopEntry writes the bundled .desktop file to the user's
// local applications directory so vibez appears in desktop launchers.
// Errors are silently ignored — desktop entry installation is best-effort.
// Call this explicitly (e.g. vibez --install) rather than on every launch.
func InstallDesktopEntry() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // XDG applications dir
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "vibez.desktop"), DesktopEntry, 0o600) //nolint:gosec // .desktop file; not sensitive
}
