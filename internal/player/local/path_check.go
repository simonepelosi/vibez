package local

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func isSafeAudioPathForOS(rawPath, targetOS string) bool {
	if rawPath == "" || strings.ContainsRune(rawPath, 0) {
		return false
	}
	if targetOS == "windows" {
		if hasUnsafeWindowsAudioPrefix(rawPath) {
			return false
		}
		if len(rawPath) < 3 || !isASCIIAlpha(rawPath[0]) || rawPath[1] != ':' || (rawPath[2] != '\\' && rawPath[2] != '/') {
			return false
		}
		cleaned := filepath.Clean(rawPath)
		return !hasUnsafeWindowsAudioPrefix(cleaned)
	}
	return filepath.IsAbs(rawPath)
}

func hasUnsafeWindowsAudioPrefix(p string) bool {
	return strings.HasPrefix(p, `\\`) ||
		strings.HasPrefix(p, `//`) ||
		strings.HasPrefix(p, `\??\`) ||
		strings.HasPrefix(p, `/?/`)
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// VerifyRegularAudioPath validates a local track path before passing it to an
// OS audio decoder or GStreamer URI handler. It rejects empty paths, embedded
// NUL bytes, relative paths, Windows UNC/device namespaces, symlinks, and any
// target that does not open as the same regular file without following links.
func VerifyRegularAudioPath(rawPath string) (string, error) {
	if rawPath == "" || strings.ContainsRune(rawPath, 0) {
		return "", fmt.Errorf("local player: invalid audio path")
	}
	if runtime.GOOS == "windows" && !isSafeAudioPathForOS(rawPath, "windows") {
		return "", fmt.Errorf("local player: unsafe or non-drive audio path")
	}
	if !filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("local player: track path must be absolute")
	}
	cleaned := filepath.Clean(rawPath)
	if !isSafeAudioPathForOS(cleaned, runtime.GOOS) {
		return "", fmt.Errorf("local player: invalid normalized audio path")
	}
	info, err := os.Lstat(cleaned) //nolint:gosec // G703: cleaned validated via isSafeAudioPathForOS
	if err != nil {
		return "", fmt.Errorf("local player: stat audio file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("local player: track path must be a non-symlink regular file")
	}
	if err := verifyOpenRegularAudio(cleaned, info); err != nil {
		return "", err
	}
	return cleaned, nil
}
