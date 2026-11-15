package local

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// verifyRegularAudioPath validates a local track path before passing it to an
// OS audio decoder or GStreamer URI handler. It rejects empty paths, embedded
// NUL bytes, relative paths, Windows device namespaces, and any target that
// does not resolve to a regular file.
func verifyRegularAudioPath(rawPath string) (string, error) {
	if rawPath == "" || strings.ContainsRune(rawPath, 0) {
		return "", fmt.Errorf("local player: invalid audio path")
	}
	if !filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("local player: track path must be absolute")
	}
	if runtime.GOOS == "windows" && strings.HasPrefix(rawPath, `\\.\`) {
		return "", fmt.Errorf("local player: device paths are not allowed")
	}
	cleaned := filepath.Clean(rawPath)
	info, err := os.Stat(cleaned)
	if err != nil {
		return "", fmt.Errorf("local player: stat audio file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("local player: track path is not a regular file")
	}
	return cleaned, nil
}
