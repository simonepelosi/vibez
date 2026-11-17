//go:build windows

package cdm

import (
	"fmt"
	"os"
)

func validatePlatformOwnershipAndPerms(path string, fileInfo os.FileInfo) error {
	f, err := os.Open(path) //nolint:gosec // G304: path validated via IsSafeAbsPath
	if err != nil {
		return fmt.Errorf("opening Widevine CDM path %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(fileInfo, actual) {
		return fmt.Errorf("widevine CDM file identity changed for %q", path)
	}
	return nil
}

// OpenRegularNoFollow validates path and opens the regular CDM binary safely.
func OpenRegularNoFollow(path string) (*os.File, error) {
	info, err := ValidateLibraryPath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path) //nolint:gosec // G304: path validated via ValidateLibraryPath
	if err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		_ = f.Close()
		return nil, fmt.Errorf("widevine CDM file identity changed for %q", path)
	}
	return f, nil
}
