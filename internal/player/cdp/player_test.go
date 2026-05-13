//go:build linux

package cdp

import (
	"slices"
	"testing"
)

func TestLaunchArgs_WSL2AudioFlags(t *testing.T) {
	args := launchArgs("/tmp/widevine")

	if !slices.Contains(args, "--audio-buffer-size=4096") {
		t.Fatalf("launch args missing audio buffer flag")
	}
	if !slices.Contains(args, "--disable-features=AudioServiceOutOfProcess") {
		t.Fatalf("launch args missing AudioServiceOutOfProcess flag")
	}
	if slices.Contains(args, "--single-process") {
		t.Fatalf("launch args should not include --single-process")
	}
	if !slices.Contains(args, "--widevine-path=/tmp/widevine") {
		t.Fatalf("launch args missing widevine path")
	}
}
