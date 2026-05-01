package updater

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	repo          = "simonepelosi/vibez"
	apiURL        = "https://api.github.com/repos/" + repo + "/releases/latest"
	checkInterval = 24 * time.Hour
	apiTimeout    = 5 * time.Second
	dlTimeout     = 2 * time.Minute
	// maxBinarySize caps extraction to guard against decompression bombs.
	maxBinarySize = 256 << 20 // 256 MB
)

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckAndUpdate checks GitHub for a newer release. If one is found it
// downloads, verifies the SHA-256 checksum, and installs it in-place.
// It returns the path of the updated binary when a restart is needed, or ""
// when already up to date, on error, or when noUpdate is true.
//
// The caller is responsible for re-execing after cleaning up (e.g. after the
// TUI exits). All errors are handled internally — the function never blocks
// startup fatally.
func CheckAndUpdate(current string, noUpdate bool, log func(string)) string {
	if noUpdate {
		return ""
	}
	if !shouldCheck() {
		return ""
	}

	log("Checking for updates…")

	rel, err := fetchLatestRelease()
	if err != nil {
		return ""
	}

	markChecked()

	if !isNewer(rel.TagName, current) {
		return ""
	}

	assetName := fmt.Sprintf("vibez_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	var downloadURL, checksumURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case assetName:
			downloadURL = a.BrowserDownloadURL
		case "checksums.txt":
			checksumURL = a.BrowserDownloadURL
		}
	}
	if downloadURL == "" {
		return "" // no asset for this platform
	}

	// Only attempt self-update for writable, self-managed installs.
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	if !isWritable(exe) {
		return ""
	}

	log(fmt.Sprintf("Downloading update %s…", rel.TagName))

	tmpDir, err := os.MkdirTemp("", "vibez-update-*")
	if err != nil {
		return ""
	}

	tarPath := filepath.Join(tmpDir, assetName)
	if err := downloadFile(downloadURL, tarPath); err != nil {
		_ = os.RemoveAll(tmpDir)
		return ""
	}

	if checksumURL != "" {
		if err := verifyChecksum(tarPath, assetName, checksumURL); err != nil {
			log("Update aborted: checksum verification failed")
			_ = os.RemoveAll(tmpDir)
			return ""
		}
	}

	log("Installing update…")

	newBin := filepath.Join(tmpDir, "vibez")
	if err := extractBinary(tarPath, "vibez", newBin); err != nil {
		_ = os.RemoveAll(tmpDir)
		return ""
	}
	if err := os.Chmod(newBin, 0o755); err != nil { //nolint:gosec // executables require 0755
		_ = os.RemoveAll(tmpDir)
		return ""
	}

	// Atomic replace: write to exe.new, rename over exe.
	tmpBin := exe + ".new"
	data, err := os.ReadFile(newBin) //nolint:gosec // path comes from our own tmpDir
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return ""
	}
	if err := os.WriteFile(tmpBin, data, 0o755); err != nil { //nolint:gosec // executable permissions required
		_ = os.RemoveAll(tmpDir)
		return ""
	}
	if err := os.Rename(tmpBin, exe); err != nil {
		_ = os.Remove(tmpBin)
		_ = os.RemoveAll(tmpDir)
		return ""
	}

	log(fmt.Sprintf("Updated to %s — restarting…", rel.TagName))
	_ = os.RemoveAll(tmpDir)
	return exe
}

func fetchLatestRelease() (*ghRelease, error) {
	client := &http.Client{Timeout: apiTimeout}
	resp, err := client.Get(apiURL) //nolint:gosec // URL is a hardcoded constant
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API: unexpected status %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func downloadFile(url, dst string) error {
	client := &http.Client{Timeout: dlTimeout}
	resp, err := client.Get(url) //nolint:gosec // URL comes from the GitHub releases API response
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	f, err := os.Create(dst) //nolint:gosec // dst is a path inside our own tmpDir
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return err
}

func verifyChecksum(tarPath, assetName, checksumURL string) error {
	client := &http.Client{Timeout: apiTimeout}
	resp, err := client.Get(checksumURL) //nolint:gosec // URL comes from the GitHub releases API response
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// checksums.txt format: "<sha256>  <filename>" per line.
	var expected string
	for line := range strings.SplitSeq(string(body), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == assetName {
			expected = parts[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum for %s not found in checksums.txt", assetName)
	}

	f, err := os.Open(tarPath) //nolint:gosec // tarPath is a path inside our own tmpDir
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if actual := hex.EncodeToString(h.Sum(nil)); actual != expected {
		return fmt.Errorf("checksum mismatch: got %s, want %s", actual, expected)
	}
	return nil
}

func extractBinary(tarPath, binaryName, dst string) error {
	f, err := os.Open(tarPath) //nolint:gosec // tarPath is a path inside our own tmpDir
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) == binaryName && hdr.Typeflag == tar.TypeReg {
			out, err := os.Create(dst) //nolint:gosec // dst is a path inside our own tmpDir
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, io.LimitReader(tr, maxBinarySize))
			if closeErr := out.Close(); closeErr != nil && copyErr == nil {
				copyErr = closeErr
			}
			return copyErr
		}
	}
	return fmt.Errorf("binary %q not found in archive", binaryName)
}

func isWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // intentional write-check
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// isNewer reports whether latestTag represents a higher version than currentTag.
// Both may carry a leading "v". Uses integer comparison per component so that
// e.g. 0.0.10 > 0.0.9 is handled correctly.
func isNewer(latestTag, currentTag string) bool {
	l := parseVersion(strings.TrimPrefix(latestTag, "v"))
	c := parseVersion(strings.TrimPrefix(currentTag, "v"))
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	parts := strings.SplitN(v, ".", 3)
	var r [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		r[i], _ = strconv.Atoi(p) //nolint:gosec // i is always < 3: SplitN cap + guard above
	}
	return r
}

func cacheDir() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "vibez")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "vibez")
}

func shouldCheck() bool {
	stamp := filepath.Join(cacheDir(), "last_update_check")
	info, err := os.Stat(stamp)
	if err != nil {
		return true // file missing → check
	}
	return time.Since(info.ModTime()) > checkInterval
}

func markChecked() {
	dir := cacheDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return
	}
	stamp := filepath.Join(dir, "last_update_check")
	f, err := os.OpenFile(stamp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // stamp path is derived from our own cacheDir()
	if err != nil {
		return
	}
	_ = f.Close()
}
