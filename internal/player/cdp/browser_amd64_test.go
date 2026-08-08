//go:build linux && amd64

package cdp

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	playwright "github.com/mxschmitt/playwright-go"
)

// These tests cover the amd64 layout, where vibez downloads Google Chrome into
// its private cache and hard-links a vibez-helper alias. The arm64 backend uses
// a discovered system browser instead (see browser_arm64_test.go).

func TestChromePath_IsAbsolute(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	got := ChromePath()
	if !filepath.IsAbs(got) {
		t.Errorf("ChromePath() = %q, want absolute path", got)
	}
	if filepath.Base(got) != "chrome" {
		t.Errorf("ChromePath() base = %q, want %q", filepath.Base(got), "chrome")
	}
}

func TestHelperPath_IsAbsolute(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	got := HelperPath()
	if !filepath.IsAbs(got) {
		t.Errorf("HelperPath() = %q, want absolute path", got)
	}
	if filepath.Base(got) != "vibez-helper" {
		t.Errorf("HelperPath() base = %q, want %q", filepath.Base(got), "vibez-helper")
	}
}

func TestLinkHelper_CreatesHardLink(t *testing.T) {
	// Set up a fake chrome directory structure in a temp cache dir.
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	// Create the directories and a fake chrome binary.
	chromeBin := ChromePath()
	if err := os.MkdirAll(filepath.Dir(chromeBin), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(chromeBin, []byte("fake chrome"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write chrome: %v", err)
	}

	// Call linkHelper — should create vibez-helper.
	linkHelper()

	if _, err := os.Stat(HelperPath()); err != nil {
		t.Errorf("vibez-helper not created by linkHelper(): %v", err)
	}
}

func TestLinkHelper_IdempotentWhenHelperExists(t *testing.T) {
	// Set up a fake chrome + helper already present.
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	chromeBin := ChromePath()
	if err := os.MkdirAll(filepath.Dir(chromeBin), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(chromeBin, []byte("fake chrome"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write chrome: %v", err)
	}
	if err := os.WriteFile(HelperPath(), []byte("fake helper"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write helper: %v", err)
	}

	// Should not panic and should be a no-op.
	linkHelper()
}

func TestEnsureBrowser_AlreadyInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	chromeBin := ChromePath()
	if err := os.MkdirAll(filepath.Dir(chromeBin), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(chromeBin, []byte("fake chrome"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write chrome: %v", err)
	}

	// Mock the playwright driver as also already installed and up-to-date.
	driver, err := playwright.NewDriver(&playwright.RunOptions{
		DriverDirectory: driverDir(),
	})
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}
	pkgDir := filepath.Join(driverDir(), "package")
	if err := os.MkdirAll(pkgDir, 0o750); err != nil {
		t.Fatalf("mkdir driver package: %v", err)
	}
	pkgJSON := fmt.Sprintf(`{"version": %q}`, driver.Version)
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(pkgJSON), 0o600); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write driver package.json: %v", err)
	}

	var progress []string
	err = EnsureBrowser(func(s string) { progress = append(progress, s) })
	if err != nil {
		t.Errorf("EnsureBrowser when already installed should return nil, got: %v", err)
	}
	// No download should have been triggered.
	if len(progress) > 0 {
		t.Errorf("EnsureBrowser when installed should not call onProgress, got: %v", progress)
	}
}
