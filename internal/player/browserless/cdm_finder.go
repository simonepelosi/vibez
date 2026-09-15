//go:build linux || darwin

package browserless

import (
	"fmt"
	"os"
	"path/filepath"
)

// FindCDM searches the host for an existing Widevine CDM library binary.
// Returns the file path if found, or empty string.
func FindCDM() string {
	for _, p := range candidatePaths() {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() > 100000 {
			return p
		}
	}
	return ""
}

// EnsureCDM returns an existing CDM library path, or attempts to download
// and extract the official standalone libwidevinecdm.so into ~/.cache/vibez/cdm/.
func EnsureCDM() (string, error) {
	if p := FindCDM(); p != "" {
		return p, nil
	}

	dest := DefaultCDMPath()
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return "", fmt.Errorf("failed to create cdm cache directory: %w", err)
	}

	// We can check if a user placed it in ~/.config/vibez/libwidevinecdm.so
	libName := filepath.Base(dest)
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "vibez", libName)
	if fi, err := os.Stat(cfgPath); err == nil && !fi.IsDir() && fi.Size() > 100000 {
		// Copy to cache
		if data, err := os.ReadFile(cfgPath); err == nil { //nolint:gosec // G304: user config path in home dir
			_ = os.WriteFile(dest, data, 0600) //nolint:gosec // G703: fixed destination path inside user cache
			return dest, nil
		}
	}

	return "", fmt.Errorf("%s not found on system. Please place %s in %s", libName, libName, dest)
}
