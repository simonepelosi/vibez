//go:build darwin

package cdp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFindChromePathUsesVibezOverride(t *testing.T) {
	tmp := t.TempDir()
	chrome := filepath.Join(tmp, "chrome")
	if err := os.WriteFile(chrome, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write chrome fixture: %v", err)
	}
	t.Setenv("VIBEZ_CHROME_PATH", chrome)
	t.Setenv("CHROME_PATH", "")

	got, err := findChromePath()
	if err != nil {
		t.Fatalf("findChromePath: %v", err)
	}
	if got != chrome {
		t.Fatalf("findChromePath = %q, want %q", got, chrome)
	}
}

func TestFindChromePathUsesChromePathOverride(t *testing.T) {
	tmp := t.TempDir()
	chrome := filepath.Join(tmp, "chrome")
	if err := os.WriteFile(chrome, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write chrome fixture: %v", err)
	}
	t.Setenv("VIBEZ_CHROME_PATH", "")
	t.Setenv("CHROME_PATH", chrome)

	got, err := findChromePath()
	if err != nil {
		t.Fatalf("findChromePath: %v", err)
	}
	if got != chrome {
		t.Fatalf("findChromePath = %q, want %q", got, chrome)
	}
}

func TestFindChromePathRejectsMissingOverride(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("VIBEZ_CHROME_PATH", missing)
	t.Setenv("CHROME_PATH", "")

	got, err := findChromePath()
	if err == nil {
		t.Fatalf("findChromePath accepted missing override: %q", got)
	}
	if !strings.Contains(err.Error(), "VIBEZ_CHROME_PATH") || !strings.Contains(err.Error(), missing) {
		t.Fatalf("findChromePath error = %q, want override path error", err)
	}
}

func TestChromeLaunchArgsDoNotUseLinuxOnlyOrUnsafeFlags(t *testing.T) {
	args := chromeLaunchArgs(false, false)
	for _, forbidden := range []string{"--no-sandbox", "--disable-setuid-sandbox", "--no-zygote", "--single-process", "--ignore-certificate-errors", "--disable-component-update"} {
		if slices.Contains(args, forbidden) {
			t.Fatalf("chromeLaunchArgs contains forbidden flag %q", forbidden)
		}
	}
}

func TestChromeLaunchArgs_DoesNotHaveHeadlessNew(t *testing.T) {
	args := chromeLaunchArgs(true, false)
	if slices.Contains(args, "--headless=new") {
		t.Error("chromeLaunchArgs should not contain --headless=new on macOS (DRM requires headed mode)")
	}
	if !slices.Contains(args, "--window-position=-2000,-2000") {
		t.Error("chromeLaunchArgs should contain --window-position=-2000,-2000 when headless=true on macOS")
	}

	args = chromeLaunchArgs(false, false)
	if slices.Contains(args, "--headless=new") {
		t.Error("chromeLaunchArgs should not contain --headless=new when headless=false on macOS")
	}
	if slices.Contains(args, "--window-position=-2000,-2000") {
		t.Error("chromeLaunchArgs should not contain --window-position=-2000,-2000 when headless=false on macOS")
	}
}

func TestCDPLaunchProbeInvalidToken(t *testing.T) {
	if os.Getenv("VIBEZ_CDP_LAUNCH_PROBE") != "1" {
		t.Skip("set VIBEZ_CDP_LAUNCH_PROBE=1 to launch Chrome without Apple credentials")
	}
	if err := EnsureBrowser(func(msg string) { t.Log(msg) }); err != nil {
		t.Fatalf("EnsureBrowser: %v", err)
	}

	p, err := New("invalid.developer.token", "invalid.user.token", "", false, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		p.Run()
	}()
	defer func() {
		p.Terminate()
		<-runDone
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = p.WaitReady(ctx)
	if err == nil {
		t.Fatal("WaitReady unexpectedly succeeded with invalid MusicKit token")
	}
	if msg := err.Error(); !strings.Contains(msg, "musickit") && !strings.Contains(msg, "context deadline") {
		t.Fatalf("WaitReady error = %q, want MusicKit or timeout error", msg)
	}
}
