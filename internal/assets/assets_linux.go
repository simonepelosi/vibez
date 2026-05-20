//go:build linux

package assets

import (
	"os"
	"os/exec"
	"path/filepath"
)

// InstallIcon writes the bundled SVG icon to the XDG icon theme directory,
// regenerates the GTK icon cache so the DE finds it immediately, and
// returns the absolute path to the installed file (used for notifications).
// All operations are best-effort; errors are silently ignored.
func InstallIcon() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".local", "share", "icons", "hicolor", "scalable", "apps")
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // XDG icon dir
		return ""
	}
	dst := filepath.Join(dir, "vibez.svg")
	if err := os.WriteFile(dst, Icon, 0o644); err != nil { //nolint:gosec // public icon file
		return ""
	}
	hicolor := filepath.Join(home, ".local", "share", "icons", "hicolor")
	_ = exec.Command("gtk-update-icon-cache", "--force", "--ignore-theme-index", hicolor).Run() //nolint:gosec
	return dst
}

// InstallDesktopEntry writes the bundled .desktop file (NoDisplay=true, so
// it stays invisible to app launchers) and refreshes the GIO application
// database so MPRIS consumers can resolve the DesktopEntry -> icon immediately.
// All operations are best-effort; errors are silently ignored.
func InstallDesktopEntry() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // XDG applications dir
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "vibez.desktop"), DesktopEntry, 0o644); err != nil { //nolint:gosec // public .desktop file
		return
	}
	_ = exec.Command("update-desktop-database", dir).Run() //nolint:gosec
}
