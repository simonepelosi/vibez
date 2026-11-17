//go:build linux || darwin || windows

package cdm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// IsSafeAbsPath reports whether p is a clean, non-empty, NUL-free,
// control-free local absolute path (on Windows requiring a drive-letter root
// and rejecting UNC, device namespace, and extended-length prefixes).
func IsSafeAbsPath(p string) bool {
	if p == "" {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f {
			return false
		}
	}
	if !isSafePlatformAbsPath(p, runtime.GOOS) {
		return false
	}
	return filepath.IsAbs(filepath.Clean(p))
}

func isSafePlatformAbsPath(p, targetOS string) bool {
	if targetOS == "windows" {
		if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `//`) || strings.HasPrefix(p, `\??\`) {
			return false
		}
		if len(p) < 3 {
			return false
		}
		drive := p[0]
		isLetter := (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')
		return isLetter && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
	}
	return strings.HasPrefix(p, "/")
}

// ValidateLibraryPath verifies that libraryPath is an absolute non-symlink
// regular file in a safe directory (non-world-writable, trusted ownership on
// Unix) and returns its Lstat FileInfo.
func ValidateLibraryPath(libraryPath string) (os.FileInfo, error) {
	if !IsSafeAbsPath(libraryPath) {
		return nil, errors.New("invalid Widevine CDM path")
	}
	info, err := os.Lstat(libraryPath) //nolint:gosec // G703: libraryPath validated via IsSafeAbsPath
	if err != nil {
		return nil, fmt.Errorf("invalid Widevine CDM path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("widevine CDM path %q is not a regular non-symlink file", libraryPath)
	}
	if err := validatePlatformOwnershipAndPerms(libraryPath, info); err != nil {
		return nil, err
	}
	return info, nil
}
