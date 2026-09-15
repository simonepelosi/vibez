//go:build linux || darwin

package browserless

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const mozillaManifestURL = "https://raw.githubusercontent.com/mozilla/gecko-dev/master/toolkit/content/gmp-sources/widevinecdm.json"

// Hardcoded fallback Google CDN URLs for each supported OS and architecture
// in case Mozilla's repository is unreachable or rate-limited.
var fallbackCDNs = map[string]string{
	"darwin_arm64": "https://edgedl.me.gvt1.com/edgedl/release2/chrome_component/adpwdrehowm2a6w7spq52lx3eyla_4.10.2891.0/oimompecagnajdejgnnjijobebaeigek_4.10.2891.0_mac_arm64_adebp6igda2i2udepjmfqykgfjja.crx3",
	"darwin_amd64": "https://edgedl.me.gvt1.com/edgedl/release2/chrome_component/acyemmluq5srlg2kz2pthbczio6a_4.10.2891.0/oimompecagnajdejgnnjijobebaeigek_4.10.2891.0_mac64_nguapth3dlbha4l6aixmapcasq.crx3",
	"linux_amd64":  "https://edgedl.me.gvt1.com/edgedl/release2/chrome_component/aclxnidwwkj5di3vduduj2gqpgpq_4.10.2891.0/oimompecagnajdejgnnjijobebaeigek_4.10.2891.0_linux_b4hin3q5s66ws2322cyyfp35lu.crx3",
}

var mozillaPlatformKeys = map[string]string{
	"darwin_arm64": "Darwin_aarch64-gcc3",
	"darwin_amd64": "Darwin_x86_64-gcc3-u-i386-x86_64",
	"linux_amd64":  "Linux_x86_64-gcc3",
}

// CanDownloadCDM reports whether the current OS and architecture have a known Widevine package.
func CanDownloadCDM() bool {
	key := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	_, ok := fallbackCDNs[key]
	return ok
}

// Available reports whether the browserless engine can be used on this system.
func Available() bool {
	return FindCDM() != "" || CanDownloadCDM()
}

// resolveDownloadURL resolves the official Google CDN download URL for the current platform.
func resolveDownloadURL(ctx context.Context) (string, error) {
	platformKey := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	fallbackURL, supported := fallbackCDNs[platformKey]
	if !supported {
		return "", fmt.Errorf("widevine cdm download not supported for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	mozKey, hasMozKey := mozillaPlatformKeys[platformKey]
	if !hasMozKey {
		return fallbackURL, nil
	}

	// Attempt to query Mozilla manifest for the latest Google component URL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mozillaManifestURL, nil)
	if err != nil {
		return fallbackURL, nil
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return fallbackURL, nil
	}
	defer func() { _ = resp.Body.Close() }()

	var manifest struct {
		Vendors struct {
			GMPWidevine struct {
				Platforms map[string]struct {
					FileURL string `json:"fileUrl"`
				} `json:"platforms"`
			} `json:"gmp-widevinecdm"`
		} `json:"vendors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&manifest); err == nil {
		if p, ok := manifest.Vendors.GMPWidevine.Platforms[mozKey]; ok && p.FileURL != "" {
			return p.FileURL, nil
		}
	}

	return fallbackURL, nil
}

// DownloadCDM downloads the official Google Widevine CRX3 package and extracts
// the shared library into destPath.
func DownloadCDM(ctx context.Context, destPath string) (string, error) {
	dlURL, err := resolveDownloadURL(ctx)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
		return "", fmt.Errorf("creating cdm directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading widevine cdm: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading widevine cdm failed (HTTP %d)", resp.StatusCode)
	}

	tmpCRX, err := os.CreateTemp(filepath.Dir(destPath), "widevine-*.crx3")
	if err != nil {
		return "", fmt.Errorf("creating temp file for cdm download: %w", err)
	}
	tmpCRXPath := tmpCRX.Name()
	defer func() {
		_ = tmpCRX.Close()
		_ = os.Remove(tmpCRXPath)
	}()

	if _, err := io.Copy(tmpCRX, resp.Body); err != nil {
		return "", fmt.Errorf("writing cdm package: %w", err)
	}
	_ = tmpCRX.Close()

	if err := extractLibraryFromCRX(tmpCRXPath, destPath); err != nil {
		return "", fmt.Errorf("extracting cdm library: %w", err)
	}

	return destPath, nil
}

// extractLibraryFromCRX reads a Chromium CRX3 package, locates the ZIP archive
// after the header, and extracts the target library.
func extractLibraryFromCRX(crxPath, destPath string) error {
	f, err := os.Open(crxPath) //nolint:gosec // crxPath is created in user cache directory
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return err
	}
	fileSize := stat.Size()
	if fileSize < 16 {
		return errors.New("crx file too small")
	}

	headerPrefix := make([]byte, 12)
	if _, err := io.ReadFull(f, headerPrefix); err != nil {
		return err
	}

	if string(headerPrefix[0:4]) != "Cr24" {
		return errors.New("invalid crx format: missing Cr24 magic")
	}

	version := binary.LittleEndian.Uint32(headerPrefix[4:8])
	if version != 3 {
		return fmt.Errorf("unsupported crx version: %d", version)
	}

	headerLen := binary.LittleEndian.Uint32(headerPrefix[8:12])
	zipOffset := int64(12 + headerLen)
	if zipOffset >= fileSize {
		return errors.New("invalid crx header length")
	}

	zipSize := fileSize - zipOffset
	zipReader, err := zip.NewReader(io.NewSectionReader(f, zipOffset, zipSize), zipSize)
	if err != nil {
		return fmt.Errorf("reading crx zip content: %w", err)
	}

	targetName := filepath.Base(destPath)
	for _, zf := range zipReader.File {
		if filepath.Base(zf.Name) == targetName {
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			defer func() { _ = rc.Close() }()

			if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
				return err
			}

			tmpDest := destPath + ".tmp"
			out, err := os.OpenFile(tmpDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755) //nolint:gosec
			if err != nil {
				return err
			}

			// Bounded to 64 MiB to guard against decompression bombs
			limited := io.LimitReader(rc, 64<<20)
			if _, err := io.Copy(out, limited); err != nil { //nolint:gosec
				_ = out.Close()
				_ = os.Remove(tmpDest)
				return err
			}
			_ = out.Close()

			return os.Rename(tmpDest, destPath)
		}
	}

	return fmt.Errorf("%s not found in downloaded package", targetName)
}
