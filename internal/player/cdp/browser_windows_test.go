//go:build windows

package cdp

import (
	"os"
	"path/filepath"
	"testing"
)

func windowsChromeFixture(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWindowsChromeOverridePrecedence(t *testing.T) {
	dir := t.TempDir()
	preferred := windowsChromeFixture(t, filepath.Join(dir, "用户 Chrome", "chrome.EXE"))
	fallback := windowsChromeFixture(t, filepath.Join(dir, "other", "chrome.exe"))
	t.Setenv("VIBEZ_CHROME_PATH", preferred)
	t.Setenv("CHROME_PATH", fallback)
	got, err := findChromePath()
	if err != nil || got != preferred {
		t.Fatalf("findChromePath() = %q, %v; want %q", got, err, preferred)
	}
	t.Setenv("VIBEZ_CHROME_PATH", "")
	got, err = findChromePath()
	if err != nil || got != fallback {
		t.Fatalf("findChromePath() = %q, %v; want %q", got, err, fallback)
	}
}

func TestWindowsChromeInvalidOverrideDoesNotFallBack(t *testing.T) {
	dir := t.TempDir()
	fallback := windowsChromeFixture(t, filepath.Join(dir, "chrome.exe"))
	nonExecutable := windowsChromeFixture(t, filepath.Join(dir, "chrome.txt"))
	for _, path := range []string{filepath.Join(dir, "missing.exe"), dir, nonExecutable} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Setenv("VIBEZ_CHROME_PATH", path)
			t.Setenv("CHROME_PATH", fallback)
			if got, err := findChromePath(); err == nil {
				t.Fatalf("invalid override %q selected %q", path, got)
			}
		})
	}
}

func TestWindowsChromeFindsPerUserInstall(t *testing.T) {
	dir := t.TempDir()
	want := windowsChromeFixture(t, filepath.Join(dir, "Google", "Chrome", "Application", "chrome.exe"))
	t.Setenv("VIBEZ_CHROME_PATH", "")
	t.Setenv("CHROME_PATH", "")
	t.Setenv("LOCALAPPDATA", dir)
	got, err := findChromePath()
	if err != nil || got != want {
		t.Fatalf("findChromePath() = %q, %v; want %q", got, err, want)
	}
}
