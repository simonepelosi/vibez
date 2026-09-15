//go:build linux || darwin

package browserless

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
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

// EnsureCDM returns an existing CDM library path, or automatically downloads
// and extracts the official Widevine CDM component into the user cache directory.
func EnsureCDM() (string, error) {
	if p := FindCDM(); p != "" {
		return p, nil
	}

	dest := DefaultCDMPath()
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return "", fmt.Errorf("failed to create cdm cache directory: %w", err)
	}

	// We can check if a user placed it in ~/.config/vibez/libwidevinecdm.<ext>
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

	// Automatically download official Google Widevine package (mirroring Firefox's mechanism)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	downloaded, err := DownloadCDM(ctx, dest)
	if err != nil {
		return "", fmt.Errorf("%s not found and auto-download failed: %w (place %s manually in %s)", libName, err, libName, dest)
	}
	return downloaded, nil
}
