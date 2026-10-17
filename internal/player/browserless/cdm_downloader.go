//go:build linux || darwin

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
	"runtime"
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

type cdmPackageSpec struct {
	url      string
	sha512   string
	fileSize int64
}

// Hardcoded fallback Google CDN URLs (Widevine 4.10.3112.0) for each supported OS and architecture
// in case Mozilla's repository is unreachable or rate-limited.
var fallbackCDNs = map[string]cdmPackageSpec{
	"darwin_arm64": {
		url:      "https://edgedl.me.gvt1.com/edgedl/release2/chrome_component/ac7kldzznoqpelvhlm22cvg6iw7q_4.10.3112.0/oimompecagnajdejgnnjijobebaeigek_4.10.3112.0_mac_arm64_acwuvzzk4g3lgkvyjjsbyjwqnxoq.crx3",
		sha512:   "6f319883d2eeaabc8df1c0d682eb2b165f6bdadc22f8c5dbe5f4d5a6b22e35a4f7976eb94c4b31d2a0ab899fcb32123d15893b47138c0ddd33a395b04da05914",
		fileSize: 20423379,
	},
	"darwin_amd64": {
		url:      "https://edgedl.me.gvt1.com/edgedl/release2/chrome_component/acvkbk6uzwaavliybbd7ayuuab5a_4.10.3112.0/oimompecagnajdejgnnjijobebaeigek_4.10.3112.0_mac64_adtfufbhdjipnvjmwyq2z6yuhyaa.crx3",
		sha512:   "b16a01aed3b19ab648efc477d1c712454a39a2b5226d09a1e166e8cba4def6d6eeed5107cd80c577a4f79dec43cce6b4cd45bcc48ce026cf0f82b14b3d32ecca",
		fileSize: 22867646,
	},
	"linux_amd64": {
		url:      "https://edgedl.me.gvt1.com/edgedl/release2/chrome_component/acqexsh26ysyu3zxeotqtsp63idq_4.10.3112.0/oimompecagnajdejgnnjijobebaeigek_4.10.3112.0_linux64_ad7kyx4q7wx7xknykmkbonbbmnwq.crx3",
		sha512:   "4bc44f7e2010ff1dec995ab496702467ec02f45db2f4d977822be89e40043ee0dd75f3d777539ecf243617029bbe5b41a1171d293b2fa615ffeacf5cd7112f29",
		fileSize: 21970001,
	},
}

var mozillaPlatformKeys = map[string]string{
	"darwin_arm64": "Darwin_aarch64-gcc3",
	"darwin_amd64": "Darwin_x86_64-gcc3-u-i386-x86_64",
	"linux_amd64":  "Linux_x86_64-gcc3",
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

// resolveDownloadSpec resolves the official Google CDN download URL and SHA-512 hash for the current platform.
func resolveDownloadSpec(ctx context.Context) (cdmPackageSpec, error) {
	platformKey := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	fallbackSpec, supported := fallbackCDNs[platformKey]
	if !supported {
		return cdmPackageSpec{}, fmt.Errorf("widevine cdm download not supported for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	mozKey, hasMozKey := mozillaPlatformKeys[platformKey]
	if !hasMozKey {
		return fallbackSpec, nil
	}

	client := &http.Client{Timeout: 8 * time.Second}
	for _, mURL := range widevineManifestURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, mURL, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			continue
		}

		var manifest struct {
			HashFunction string `json:"hashFunction"`
			Vendors      struct {
				GMPWidevine struct {
					Platforms map[string]struct {
						FileURL   string `json:"fileUrl"`
						HashValue string `json:"hashValue"`
						FileSize  int64  `json:"filesize"`
					} `json:"platforms"`
				} `json:"gmp-widevinecdm"`
			} `json:"vendors"`
		}
		if err := json.Unmarshal(body, &manifest); err == nil {
			if p, ok := manifest.Vendors.GMPWidevine.Platforms[mozKey]; ok && p.FileURL != "" && isAllowedCDMHost(p.FileURL) {
				spec := cdmPackageSpec{
					url:      p.FileURL,
					fileSize: p.FileSize,
				}
				if strings.EqualFold(manifest.HashFunction, "sha512") {
					spec.sha512 = p.HashValue
				}
				return spec, nil
			}
		}
	}

	return fallbackSpec, nil
}

// DownloadCDM downloads the official Google Widevine CRX3 package, verifies its
// SHA-512 digest, and extracts only the standalone shared library into destPath.
func DownloadCDM(ctx context.Context, destPath string, progress ...func(string)) (string, error) {
	var report func(string)
	if len(progress) > 0 && progress[0] != nil {
		report = progress[0]
	} else {
		report = func(string) {}
	}

	report("Resolving latest Widevine CDM manifest...")
	spec, err := resolveDownloadSpec(ctx)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
		return "", fmt.Errorf("creating cdm directory: %w", err)
	}

	if spec.fileSize > 0 {
		report(fmt.Sprintf("Downloading Widevine CDM (~%.0f MB)...", float64(spec.fileSize)/(1024*1024)))
	} else {
		report("Downloading Widevine CDM (~21 MB)...")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.url, nil)
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

	crxBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxCRXBytes))
	if err != nil {
		return "", fmt.Errorf("reading cdm package: %w", err)
	}

	if spec.sha512 != "" {
		sum := sha512.Sum512(crxBytes)
		gotHash := hex.EncodeToString(sum[:])
		if !strings.EqualFold(gotHash, spec.sha512) {
			return "", fmt.Errorf("widevine CRX3 SHA-512 mismatch: got %s, want %s", gotHash, spec.sha512)
		}
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

	if _, err := tmpCRX.Write(crxBytes); err != nil {
		return "", fmt.Errorf("writing cdm package: %w", err)
	}
	_ = tmpCRX.Close()

	report(fmt.Sprintf("Extracting %s...", filepath.Base(destPath)))
	if err := extractLibraryFromCRX(tmpCRXPath, destPath); err != nil {
		return "", fmt.Errorf("extracting cdm library: %w", err)
	}

	return destPath, nil
}

// extractCDMFromCRX3 strips the CRX3 header and extracts the Widevine CDM shared
// library from the embedded ZIP archive in memory.
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
		base := filepath.Base(f.Name)
		if base == "libwidevinecdm.so" || base == "libwidevinecdm.dylib" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(io.LimitReader(rc, maxCDMBytes))
			_ = rc.Close()
			if readErr != nil {
				return nil, readErr
			}
			if len(data) < 1024*1024 {
				return nil, fmt.Errorf("extracted %s is unexpectedly small (%d bytes)", base, len(data))
			}
			return data, nil
		}
	}
	return nil, errors.New("widevine cdm library not found inside CRX3 archive")
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
			limited := io.LimitReader(rc, maxCDMBytes)
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
