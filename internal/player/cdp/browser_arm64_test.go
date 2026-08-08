//go:build linux && arm64

package cdp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The arm64 backend discovers a system Chromium/Chrome and a system-registered
// Widevine CDM instead of downloading Google Chrome (which Google does not
// publish for Linux/arm64). Tests drive discovery through the
// VIBEZ_CHROME_PATH override and an overridable widevineSystemDirs list so they
// never depend on what is actually installed on the host.

// fakeBrowser writes an executable stub and points VIBEZ_CHROME_PATH at it,
// isolating HOME and the fixed CDM search list so discovery is hermetic.
func fakeBrowser(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "chromium")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write fake browser: %v", err)
	}
	t.Setenv("VIBEZ_CHROME_PATH", bin)
	t.Setenv("HOME", dir)

	saved := widevineSystemDirs
	widevineSystemDirs = nil // only browser-adjacent discovery is exercised
	t.Cleanup(func() { widevineSystemDirs = saved })
	return bin
}

// installAdjacentCDM drops a fake arm64 Widevine CDM next to the given browser
// and returns the WidevineCdm directory.
func installAdjacentCDM(t *testing.T, browser string) string {
	t.Helper()
	cdmDir := filepath.Join(filepath.Dir(browser), "WidevineCdm")
	soDir := filepath.Join(cdmDir, "_platform_specific", "linux_arm64")
	if err := os.MkdirAll(soDir, 0o750); err != nil {
		t.Fatalf("mkdir cdm: %v", err)
	}
	if err := os.WriteFile(filepath.Join(soDir, "libwidevinecdm.so"), []byte("fake cdm"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write cdm: %v", err)
	}
	return cdmDir
}

func TestFindSystemBrowser_EnvOverride(t *testing.T) {
	bin := fakeBrowser(t)
	got, err := findSystemBrowser()
	if err != nil {
		t.Fatalf("findSystemBrowser: %v", err)
	}
	if got != bin {
		t.Errorf("findSystemBrowser() = %q, want %q", got, bin)
	}
}

func TestFindSystemBrowser_EnvOverrideInvalid(t *testing.T) {
	t.Setenv("VIBEZ_CHROME_PATH", filepath.Join(t.TempDir(), "does-not-exist"))
	if _, err := findSystemBrowser(); err == nil {
		t.Error("expected error for non-existent VIBEZ_CHROME_PATH, got nil")
	}
}

func TestChromePath_UsesSystemBrowser(t *testing.T) {
	bin := fakeBrowser(t)
	if got := ChromePath(); got != bin {
		t.Errorf("ChromePath() = %q, want %q", got, bin)
	}
}

func TestHelperPath_EqualsChromePath(t *testing.T) {
	fakeBrowser(t)
	if HelperPath() != ChromePath() {
		t.Errorf("HelperPath() = %q, want == ChromePath() = %q", HelperPath(), ChromePath())
	}
}

func TestLinkHelper_NoOpOnARM64(t *testing.T) {
	bin := fakeBrowser(t)
	linkHelper() // must not panic and must not create a sibling helper
	if _, err := os.Stat(filepath.Join(filepath.Dir(bin), "vibez-helper")); err == nil {
		t.Error("linkHelper() created a helper link on arm64; expected no-op")
	}
}

func TestWidevineCDMDir_DiscoversAdjacentCDM(t *testing.T) {
	bin := fakeBrowser(t)
	if got := widevineCDMDir(); got != "" {
		t.Fatalf("widevineCDMDir() = %q before install, want empty", got)
	}
	want := installAdjacentCDM(t, bin)
	if got := widevineCDMDir(); got != want {
		t.Errorf("widevineCDMDir() = %q, want %q", got, want)
	}
}

func TestAvailable_RequiresBrowserAndCDM(t *testing.T) {
	bin := fakeBrowser(t)
	if Available() {
		t.Error("Available() = true with no Widevine CDM; want false")
	}
	installAdjacentCDM(t, bin)
	if !Available() {
		t.Error("Available() = false with browser + CDM present; want true")
	}
}

func TestChromeLaunchArgs_ARM64WidevinePath(t *testing.T) {
	bin := fakeBrowser(t)
	cdmDir := installAdjacentCDM(t, bin)
	args := chromeLaunchArgs(true, false)
	if !slices.Contains(args, "--widevine-path="+cdmDir) {
		t.Errorf("chromeLaunchArgs missing --widevine-path=%s; got %v", cdmDir, args)
	}
	if !slices.Contains(args, "--headless=new") {
		t.Error("chromeLaunchArgs should contain --headless=new when headless=true")
	}
}
