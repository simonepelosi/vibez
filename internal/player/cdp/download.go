//go:build linux

package cdp

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// progressReader wraps an io.Reader and calls onProgress with a formatted
// "Downloading Chrome… XX%" message each time the percentage advances.
type progressReader struct {
	r          io.Reader
	total      int64
	read       int64
	onProgress func(string)
	lastPct    int
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.r.Read(p)
	pr.read += int64(n)
	if pr.total > 0 {
		pct := int(float64(pr.read) / float64(pr.total) * 100)
		if pct > pr.lastPct {
			pr.lastPct = pct
			pr.onProgress(fmt.Sprintf("Downloading Chromium drivers… %d%%", pct))
		}
	}
	return
}

// downloadFile downloads url to dst, reporting progress via onProgress.
func downloadFile(onProgress func(string), dst, url string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	resp, err := http.Get(url) //nolint:noctx,gosec // URL is a compile-time constant
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %s", url, resp.Status)
	}

	f, err := os.Create(dst) //nolint:gosec // dst is constructed from cache dir
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer f.Close() //nolint:errcheck

	total := resp.ContentLength
	onProgress(fmt.Sprintf("Downloading Chromium drivers… 0%% (%.0f MB)", float64(total)/1e6))

	pr := &progressReader{r: resp.Body, total: total, onProgress: onProgress}
	if _, err := io.Copy(f, pr); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}
