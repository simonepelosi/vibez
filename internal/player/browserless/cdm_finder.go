//go:build linux

package browserless

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxManifestBytes = 1 << 20  // 1 MiB
	maxCRXBytes      = 64 << 20 // 64 MiB
	maxCDMBytes      = 64 << 20 // 64 MiB
)

var widevineManifestURLs = []string{
	"https://raw.githubusercontent.com/mozilla-firefox/firefox/refs/heads/main/toolkit/content/gmp-sources/widevinecdm.json",
	"https://hg.mozilla.org/mozilla-central/raw-file/tip/toolkit/content/gmp-sources/widevinecdm.json",
}

// Standard locations where libwidevinecdm.so may exist on Linux.
// System browser locations come before the standalone cache so a stale copy in
// ~/.cache/vibez/cdm never shadows a browser-updated CDM.
func candidatePaths() []string {
	home, _ := os.UserHomeDir()
	var paths []string

	// Component-updated Widevine inside user browser profiles (e.g. ~/.config/google-chrome/WidevineCdm/*/_platform_specific/linux_x64/libwidevinecdm.so)
	for _, pattern := range []string{
		filepath.Join(home, ".config", "google-chrome", "WidevineCdm", "*", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
		filepath.Join(home, ".config", "chromium", "WidevineCdm", "*", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
		"/usr/lib/firefox/gmp-widevinecdm/*/libwidevinecdm.so",
	} {
		if matches, err := filepath.Glob(pattern); err == nil {
			paths = append(paths, matches...)
		}
	}

	paths = append(paths,
		"/opt/google/chrome/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/opt/google/chrome-unstable/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/usr/lib/chromium/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		"/usr/lib/chromium-browser/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		filepath.Join(home, ".cache", "vibez", "chrome", "opt", "google", "chrome", "WidevineCdm", "_platform_specific", "linux_x64", "libwidevinecdm.so"),
		"/usr/lib/firefox/gmp-widevinecdm/system-installed/libwidevinecdm.so",
		"/var/lib/flatpak/app/org.chromium.Chromium/current/active/files/WidevineCdm/_platform_specific/linux_x64/libwidevinecdm.so",
		filepath.Join(home, ".cache", "vibez", "cdm", "libwidevinecdm.so"),
	)
	return paths
}

// FindCDM searches the host for an existing libwidevinecdm.so binary and
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

// Destination path for the cached standalone CDM library.
func DefaultCDMPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "vibez", "cdm", "libwidevinecdm.so")
}

// EnsureCDM returns an existing CDM library path from the host or user cache,
// or downloads and extracts only the standalone Widevine CDM library (~21 MB)
// from Google's official component CDN if no local copy is present.
func EnsureCDM(progress ...func(string)) (string, error) {
	if p := FindCDM(); p != "" {
		return p, nil
	}

	var report func(string)
	if len(progress) > 0 && progress[0] != nil {
		report = progress[0]
	} else {
		report = func(string) {}
	}

	dest := DefaultCDMPath()
	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return "", fmt.Errorf("failed to create cdm cache directory: %w", err)
	}

	// Check if a user placed it in ~/.config/vibez/libwidevinecdm.so
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "vibez", "libwidevinecdm.so")
	if fi, err := os.Stat(cfgPath); err == nil && !fi.IsDir() && fi.Size() > 100000 {
		if data, err := os.ReadFile(cfgPath); err == nil { //nolint:gosec // G304: user config path in home dir
			if err := os.WriteFile(dest, data, 0600); err == nil { //nolint:gosec // G306: destination path inside user cache
				return dest, nil
			}
		}
	}

	if err := DownloadCDM(dest, report); err != nil {
		return "", fmt.Errorf("downloading standalone Widevine CDM: %w (or place libwidevinecdm.so in %s)", err, dest)
	}
	return dest, nil
}

type widevineGMPManifest struct {
	HashFunction string `json:"hashFunction"`
	Name         string `json:"name"`
	Vendors      map[string]struct {
		Platforms map[string]struct {
			FileURL    string   `json:"fileUrl"`
			MirrorURLs []string `json:"mirrorUrls"`
			FileSize   int64    `json:"filesize"`
			HashValue  string   `json:"hashValue"`
		} `json:"platforms"`
	} `json:"vendors"`
}

func isAllowedCDMHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	switch u.Hostname() {
	case "edgedl.me.gvt1.com", "www.google.com", "dl.google.com":
		return true
	default:
		return false
	}
}

// DownloadCDM fetches the latest Linux x86_64 Widevine CDM CRX3 package from
// Google's official component CDN, verifies its SHA-512 digest, and extracts
// only libwidevinecdm.so to dest.
func DownloadCDM(dest string, progress func(string)) error {
	if progress == nil {
		progress = func(string) {}
	}
	progress("Resolving latest Widevine CDM manifest...")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 60 * time.Second}

	var manifest widevineGMPManifest
	var manifestErr error
	for _, mURL := range widevineManifestURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, mURL, nil)
		if err != nil {
			manifestErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			manifestErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes))
		_ = resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			manifestErr = fmt.Errorf("manifest HTTP %d", resp.StatusCode)
			continue
		}
		if err := json.Unmarshal(body, &manifest); err != nil {
			manifestErr = err
			continue
		}
		manifestErr = nil
		break
	}
	if manifestErr != nil {
		return fmt.Errorf("fetching widevine manifest: %w", manifestErr)
	}

	plat, ok := manifest.Vendors["gmp-widevinecdm"].Platforms["Linux_x86_64-gcc3"]
	if !ok || plat.FileURL == "" || plat.HashValue == "" {
		return errors.New("Linux_x86_64-gcc3 entry missing from Widevine manifest")
	}
	if !strings.EqualFold(manifest.HashFunction, "sha512") {
		return fmt.Errorf("unsupported manifest hash function %q", manifest.HashFunction)
	}

	candidates := append([]string{plat.FileURL}, plat.MirrorURLs...)
	var crxBytes []byte
	var dlErr error
	for _, rawURL := range candidates {
		if !isAllowedCDMHost(rawURL) {
			continue
		}
		progress(fmt.Sprintf("Downloading %s (~%.0f MB)...", manifest.Name, float64(plat.FileSize)/(1024*1024)))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			dlErr = err
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			dlErr = err
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxCRXBytes))
		_ = resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			dlErr = fmt.Errorf("download HTTP %d", resp.StatusCode)
			continue
		}
		crxBytes = data
		dlErr = nil
		break
	}
	if len(crxBytes) == 0 {
		if dlErr != nil {
			return dlErr
		}
		return errors.New("no valid Widevine download URL succeeded")
	}

	sum := sha512.Sum512(crxBytes)
	gotHash := hex.EncodeToString(sum[:])
	if !strings.EqualFold(gotHash, plat.HashValue) {
		return fmt.Errorf("widevine CRX3 SHA-512 mismatch: got %s, want %s", gotHash, plat.HashValue)
	}

	progress("Extracting libwidevinecdm.so...")
	soBytes, err := extractCDMFromCRX3(crxBytes)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0750); err != nil {
		return err
	}
	tmpPath := dest + ".tmp"
	if err := os.WriteFile(tmpPath, soBytes, 0600); err != nil { //nolint:gosec // G306: cached CDM library inside user cache dir
		return err
	}
	return os.Rename(tmpPath, dest)
}

// extractCDMFromCRX3 strips the CRX3 header and extracts libwidevinecdm.so
// from the embedded ZIP archive.
func extractCDMFromCRX3(crx []byte) ([]byte, error) {
	if len(crx) < 12 || string(crx[:4]) != "Cr24" {
		return nil, errors.New("invalid CRX3 magic header")
	}
	version := binary.LittleEndian.Uint32(crx[4:8])
	if version != 3 {
		return nil, fmt.Errorf("unsupported CRX version %d (want 3)", version)
	}
	hdrLen := binary.LittleEndian.Uint32(crx[8:12])
	zipStart := int64(12) + int64(hdrLen)
	if zipStart >= int64(len(crx)) {
		return nil, errors.New("truncated CRX3 header")
	}

	zipPayload := crx[zipStart:]
	zr, err := zip.NewReader(bytes.NewReader(zipPayload), int64(len(zipPayload)))
	if err != nil {
		return nil, fmt.Errorf("opening CRX3 zip payload: %w", err)
	}

	for _, f := range zr.File {
		if f.Name == "_platform_specific/linux_x64/libwidevinecdm.so" || strings.HasSuffix(f.Name, "/libwidevinecdm.so") || f.Name == "libwidevinecdm.so" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(io.LimitReader(rc, maxCDMBytes))
			_ = rc.Close()
			if readErr != nil {
				return nil, readErr
			}
			if len(data) <= 100000 {
				return nil, fmt.Errorf("extracted libwidevinecdm.so is unexpectedly small (%d bytes)", len(data))
			}
			return data, nil
		}
	}
	return nil, errors.New("libwidevinecdm.so not found inside CRX3 archive")
}
