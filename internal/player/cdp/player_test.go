//go:build linux

package cdp

import (
	"slices"
	"strings"
	"testing"
)

func TestLaunchArgs_WSL(t *testing.T) {
	args := launchArgs("/tmp/widevine", false, true)

	if !slices.Contains(args, "--audio-buffer-size=4096") {
		t.Fatal("wsl=true: missing --audio-buffer-size=4096")
	}
	disableFeaturesContains(t, args, "AudioServiceOutOfProcess", true)
	if !slices.Contains(args, "--widevine-path=/tmp/widevine") {
		t.Fatal("missing --widevine-path")
	}
	if slices.Contains(args, "--single-process") {
		t.Fatal("--single-process must not be present")
	}
	if slices.Contains(args, "--ignore-certificate-errors") {
		t.Fatal("--ignore-certificate-errors must not be present")
	}
}

func TestLaunchArgs_NonWSL(t *testing.T) {
	args := launchArgs("/tmp/widevine", false, false)

	if slices.Contains(args, "--audio-buffer-size=4096") {
		t.Fatal("wsl=false: --audio-buffer-size=4096 should not be present")
	}
	disableFeaturesContains(t, args, "AudioServiceOutOfProcess", false)
	if !slices.Contains(args, "--widevine-path=/tmp/widevine") {
		t.Fatal("missing --widevine-path")
	}
}

// disableFeaturesContains checks whether a feature name appears inside the
// --disable-features=... argument in args.
func disableFeaturesContains(t *testing.T, args []string, feature string, want bool) {
	t.Helper()
	for _, a := range args {
		if strings.HasPrefix(a, "--disable-features=") {
			got := strings.Contains(a, feature)
			if got != want {
				if want {
					t.Fatalf("--disable-features missing %q", feature)
				} else {
					t.Fatalf("--disable-features should not contain %q", feature)
				}
			}
			return
		}
	}
	t.Fatal("--disable-features flag not found")
}
