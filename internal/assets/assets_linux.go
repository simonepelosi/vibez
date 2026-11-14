//go:build linux

package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// validXDGDir checks that an XDG base directory entry is non-empty, absolute,
// and free of ASCII control characters or a leading dash.
func validXDGDir(d string) bool {
	if d == "" || strings.HasPrefix(d, "-") || !filepath.IsAbs(d) {
		return false
	}
	for i := 0; i < len(d); i++ {
		if d[i] < 0x20 || d[i] == 0x7f {
			return false
		}
	}
	return true
}

// dataHome is $XDG_DATA_HOME, or ~/.local/share when unset, per the XDG base
// directory spec. The fallback icon and desktop entry are written under it.
func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); validXDGDir(d) {
		return filepath.Clean(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share")
}

// dataDirs lists every directory a desktop searches for applications and
// icons: dataHome first, then $XDG_DATA_DIRS (default
// /usr/local/share:/usr/share). A distro package, a Flatpak export, and the
// install script each put their files in one of these.
func dataDirs() []string {
	var dirs []string
	if h := dataHome(); h != "" {
		dirs = append(dirs, h)
	}
	sys := os.Getenv("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	for d := range strings.SplitSeq(sys, ":") {
		if validXDGDir(d) {
			dirs = append(dirs, filepath.Clean(d))
		}
	}
	return dirs
}

// installedIcon returns the path of an AppID icon already present in the
// hicolor theme of any data dir, or "" when there is none.
func installedIcon() string {
	prefix := AppID + "."
	for _, d := range dataDirs() {
		hicolor := filepath.Join(d, "icons", "hicolor")
		sizes, err := os.ReadDir(hicolor)
		if err != nil {
			continue
		}
		for _, sz := range sizes {
			if !sz.IsDir() {
				continue
			}
			appsDir := filepath.Join(hicolor, sz.Name(), "apps")
			entries, err := os.ReadDir(appsDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), prefix) {
					return filepath.Join(appsDir, e.Name())
				}
			}
		}
	}
	return ""
}

// installedDesktopEntry reports whether an AppID.desktop already exists in the
// applications dir of any data dir.
func installedDesktopEntry() bool {
	for _, d := range dataDirs() {
		if _, err := os.Stat(filepath.Join(d, "applications", AppID+".desktop")); err == nil {
			return true
		}
	}
	return false
}

// removeLegacy deletes a file that versions before the AppID rename wrote
// under ~/.local/share on every launch. Nothing resolves those names now.
func removeLegacy(rel ...string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(append([]string{home, ".local", "share"}, rel...)...))
}

// InstallIcon makes sure an AppID icon is available to the desktop and returns
// its absolute path (used for notifications), or "" if none could be found or
// written. An icon a package, a Flatpak, or the install script already put in
// a data dir wins. The embedded SVG is the fallback for installs that brought
// no icon of their own, such as `make install` or a plain `go build`. All
// operations are best-effort; errors are silently ignored.
func InstallIcon() string {
	removeLegacy("icons", "hicolor", "scalable", "apps", "vibez.svg")
	if p := installedIcon(); p != "" {
		return p
	}
	base := dataHome()
	if base == "" {
		return ""
	}
	dir := filepath.Join(base, "icons", "hicolor", "scalable", "apps")
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // XDG icon dir
		return ""
	}
	dst := filepath.Join(dir, AppID+".svg")
	if err := writeAtomicFile(dst, Icon, 0o644); err != nil {
		return ""
	}
	hicolor := filepath.Join(base, "icons", "hicolor")
	_ = exec.Command("gtk-update-icon-cache", "--force", "--ignore-theme-index", "--", hicolor).Run() //nolint:gosec
	return dst
}

// InstallDesktopEntry makes sure an AppID.desktop exists somewhere the desktop
// looks, so MPRIS consumers can resolve the DesktopEntry property to an icon.
// One a package, a Flatpak, or the install script already provides wins,
// whatever it contains. The embedded fallback (NoDisplay=true, so invisible to
// app launchers) is written under dataHome only when nothing else is there,
// and once written is itself found on the next launch and left alone. All
// operations are best-effort; errors are silently ignored.
func InstallDesktopEntry() {
	removeLegacy("applications", "vibez.desktop")
	if installedDesktopEntry() {
		return
	}
	base := dataHome()
	if base == "" {
		return
	}
	dir := filepath.Join(base, "applications")
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // XDG applications dir
		return
	}
	if err := writeAtomicFile(filepath.Join(dir, AppID+".desktop"), DesktopEntry, 0o644); err != nil {
		return
	}
	_ = exec.Command("update-desktop-database", "--", dir).Run() //nolint:gosec
}
