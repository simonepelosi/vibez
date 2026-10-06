//go:build darwin

package cdp

import (
	"os"
	"path/filepath"
)

func baseDir() string {
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		return filepath.Join(d, "vibez")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Caches", "vibez")
}

func chromeCandidates() []string {
	home, _ := os.UserHomeDir()
	return []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		filepath.Join(home, "Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome"),
	}
}
