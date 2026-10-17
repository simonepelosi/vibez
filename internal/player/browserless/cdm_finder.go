//go:build linux || darwin

package browserless

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FindCDM searches the host for an existing Widevine CDM library binary and
// returns the newest valid candidate so revoked older copies are skipped.
func FindCDM() string {
	if envPath := os.Getenv("VIBEZ_CDM_PATH"); envPath != "" {
		if fi, err := os.Stat(envPath); err == nil && !fi.IsDir() && fi.Size() > 100000 { //nolint:gosec // G703: explicit user override via VIBEZ_CDM_PATH
			return envPath
		}
	}

	var bestPath string
	var bestMod time.Time
	for _, p := range candidatePaths() {
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() || fi.Size() <= 100000 {
			continue
		}
		if bestPath == "" || fi.ModTime().After(bestMod) {
			bestPath = p
			bestMod = fi.ModTime()
		}
	}
	return bestPath
}

// EnsureCDM returns an existing CDM library path, or automatically downloads
// and extracts the official standalone Widevine CDM component into the user cache directory.
func EnsureCDM(progress ...func(string)) (string, error) {
	if p := FindCDM(); p != "" {
		return p, nil
	}

	dest := DefaultCDMPath()
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return "", fmt.Errorf("failed to create cdm cache directory: %w", err)
	}

	libName := filepath.Base(dest)
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "vibez", libName)
	if fi, err := os.Stat(cfgPath); err == nil && !fi.IsDir() && fi.Size() > 100000 {
		if data, err := os.ReadFile(cfgPath); err == nil { //nolint:gosec // G304: user config path in home dir
			if err := os.WriteFile(dest, data, 0600); err == nil { //nolint:gosec // G306: destination path inside user cache
				return dest, nil
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	downloaded, err := DownloadCDM(ctx, dest, progress...)
	if err != nil {
		return "", fmt.Errorf("%s not found and auto-download failed: %w (place %s manually in %s)", libName, err, libName, dest)
	}
	return downloaded, nil
}
