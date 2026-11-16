package local

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVerifyRegularAudioPath(t *testing.T) {
	dir := t.TempDir()
	regFile := filepath.Join(dir, "song.flac")
	if err := os.WriteFile(regFile, []byte("fLaC"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := verifyRegularAudioPath(regFile)
	if err != nil {
		t.Fatalf("verifyRegularAudioPath(regular file) unexpected error: %v", err)
	}
	if got != filepath.Clean(regFile) {
		t.Fatalf("got %q, want %q", got, filepath.Clean(regFile))
	}

	cases := []struct {
		name string
		path string
	}{
		{"empty", ""},
		{"nul byte", regFile + "\x00.wav"},
		{"relative", "song.flac"},
		{"directory", dir},
		{"missing", filepath.Join(dir, "missing.flac")},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, struct {
			name string
			path string
		}{"device namespace", `\\.\NUL`})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := verifyRegularAudioPath(tc.path); err == nil {
				t.Fatalf("verifyRegularAudioPath(%q) unexpectedly succeeded", tc.path)
			}
		})
	}

	if runtime.GOOS != "windows" {
		t.Run("symlink to directory", func(t *testing.T) {
			link := filepath.Join(dir, "dirlink")
			if err := os.Symlink(dir, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if _, err := verifyRegularAudioPath(link); err == nil {
				t.Fatal("verifyRegularAudioPath(symlink to dir) unexpectedly succeeded")
			}
		})
	}
}
