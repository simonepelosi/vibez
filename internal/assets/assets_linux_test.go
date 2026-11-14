//go:build linux

package assets

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateXDG points every directory the installers read or write at a fresh
// temp tree and empties PATH so the cache-refresh commands are never found. It
// returns the data home the fallback files land in and one system data dir.
func isolateXDG(t *testing.T) (dataHome, sysDir string) {
	t.Helper()
	root := t.TempDir()
	dataHome = filepath.Join(root, "xdg-data-home")
	sysDir = filepath.Join(root, "usr", "share")
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_DATA_DIRS", sysDir)
	t.Setenv("PATH", "")
	return dataHome, sysDir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { //nolint:gosec // test fixture path
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDesktopEntry_IconMatchesAppID(t *testing.T) {
	if !strings.Contains(string(DesktopEntry), "\nIcon="+AppID+"\n") {
		t.Errorf("embedded desktop entry does not set Icon=%s:\n%s", AppID, DesktopEntry)
	}
}

func TestInstallDesktopEntry_WritesFallbackUnderAppID(t *testing.T) {
	dataHome, _ := isolateXDG(t)
	InstallDesktopEntry()
	got, err := os.ReadFile(filepath.Join(dataHome, "applications", AppID+".desktop")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("fallback desktop entry not written: %v", err)
	}
	if !bytes.Equal(got, DesktopEntry) {
		t.Error("fallback desktop entry differs from the embedded one")
	}
}

func TestInstallDesktopEntry_SkipsWhenSystemEntryExists(t *testing.T) {
	dataHome, sysDir := isolateXDG(t)
	writeFile(t, filepath.Join(sysDir, "applications", AppID+".desktop"), "[Desktop Entry]\nName=packaged\n")
	InstallDesktopEntry()
	if _, err := os.Stat(filepath.Join(dataHome, "applications")); !os.IsNotExist(err) {
		t.Errorf("wrote into %s although a system entry exists (stat err = %v)", dataHome, err)
	}
}

func TestInstallDesktopEntry_LeavesUserEntryAlone(t *testing.T) {
	dataHome, _ := isolateXDG(t)
	path := filepath.Join(dataHome, "applications", AppID+".desktop")
	const theirs = "[Desktop Entry]\nName=from the install script\n"
	writeFile(t, path, theirs)
	InstallDesktopEntry()
	got, err := os.ReadFile(path) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != theirs {
		t.Errorf("existing user entry was overwritten:\n%s", got)
	}
}

func TestInstallDesktopEntry_RemovesLegacyEntry(t *testing.T) {
	isolateXDG(t)
	legacy := filepath.Join(os.Getenv("HOME"), ".local", "share", "applications", "vibez.desktop")
	writeFile(t, legacy, "[Desktop Entry]\n")
	InstallDesktopEntry()
	if _, err := os.Stat(legacy); !os.IsNotExist(err) { //nolint:gosec // test fixture path
		t.Errorf("legacy vibez.desktop still present (stat err = %v)", err)
	}
}

func TestInstallIcon_WritesFallbackAndReturnsPath(t *testing.T) {
	dataHome, _ := isolateXDG(t)
	want := filepath.Join(dataHome, "icons", "hicolor", "scalable", "apps", AppID+".svg")
	if got := InstallIcon(); got != want {
		t.Errorf("InstallIcon() = %q, want %q", got, want)
	}
	got, err := os.ReadFile(want) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("fallback icon not written: %v", err)
	}
	if !bytes.Equal(got, Icon) {
		t.Error("fallback icon differs from the embedded one")
	}
}

func TestInstallIcon_PrefersInstalledIcon(t *testing.T) {
	dataHome, sysDir := isolateXDG(t)
	installed := filepath.Join(sysDir, "icons", "hicolor", "512x512", "apps", AppID+".png")
	writeFile(t, installed, "png")
	if got := InstallIcon(); got != installed {
		t.Errorf("InstallIcon() = %q, want installed %q", got, installed)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "icons")); !os.IsNotExist(err) {
		t.Errorf("wrote into %s although a system icon exists (stat err = %v)", dataHome, err)
	}
}

func TestInstallIcon_RemovesLegacyIcon(t *testing.T) {
	isolateXDG(t)
	legacy := filepath.Join(os.Getenv("HOME"), ".local", "share", "icons", "hicolor", "scalable", "apps", "vibez.svg")
	writeFile(t, legacy, "<svg/>")
	InstallIcon()
	if _, err := os.Stat(legacy); !os.IsNotExist(err) { //nolint:gosec // test fixture path
		t.Errorf("legacy vibez.svg still present (stat err = %v)", err)
	}
}

func TestValidXDGDir(t *testing.T) {
	for _, bad := range []string{"", "relative/dir", "-dash/dir", "/bad\x00dir", "/bad\ndir"} {
		if validXDGDir(bad) {
			t.Errorf("validXDGDir(%q) = true, want false", bad)
		}
	}
	if !validXDGDir("/tmp/xdg-data") {
		t.Error("validXDGDir(/tmp/xdg-data) = false, want true")
	}
}

func TestInstallIcon_HandlesGlobMetacharsInDataDirs(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "xdg-home")
	sysDir := filepath.Join(root, "share[1]")
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_DATA_DIRS", sysDir)
	t.Setenv("PATH", "")

	installed := filepath.Join(sysDir, "icons", "hicolor", "256x256", "apps", AppID+".png")
	writeFile(t, installed, "png")
	if got := InstallIcon(); got != installed {
		t.Errorf("InstallIcon() = %q, want %q", got, installed)
	}
}

func TestWriteAtomicFile_DoesNotFollowSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "victim.txt")
	writeFile(t, outside, "untouched")

	dest := filepath.Join(dir, "sub", "target.desktop")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(outside, dest); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := writeAtomicFile(dest, []byte("new-content"), 0o600); err != nil {
		t.Fatalf("writeAtomicFile: %v", err)
	}
	gotOutside, err := os.ReadFile(outside) //nolint:gosec // test fixture path
	if err != nil || string(gotOutside) != "untouched" {
		t.Errorf("symlink target was modified: %q (err=%v)", gotOutside, err)
	}
	info, err := os.Lstat(dest)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("dest should have replaced symlink cleanly with regular file, mode=%v err=%v", info.Mode(), err)
	}
}
