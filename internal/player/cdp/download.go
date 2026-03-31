//go:build linux

package cdp

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// downloadFile downloads url to dst, writing progress to w.
func downloadFile(w io.Writer, dst, url string) error {
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
	if total > 0 {
		_, _ = fmt.Fprintf(w, "Downloading Chrome (%.0f MB)...\n", float64(total)/1e6)
	} else {
		_, _ = fmt.Fprintln(w, "Downloading Chrome...")
	}

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}
