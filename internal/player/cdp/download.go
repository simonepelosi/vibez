//go:build linux

package cdp

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// maxDownloadBytes caps how much data downloadFile will write to disk (512 MiB).
var maxDownloadBytes int64 = 512 << 20

var downloadClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

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

	resp, err := downloadClient.Get(url) //nolint:noctx,gosec // URL is a compile-time constant
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
	cleanup := true
	defer func() {
		_ = f.Close()
		if cleanup {
			_ = os.Remove(dst)
		}
	}()

	total := resp.ContentLength
	if total > maxDownloadBytes {
		return fmt.Errorf("GET %s: content length %d exceeds limit %d", url, total, maxDownloadBytes)
	}
	onProgress(fmt.Sprintf("Downloading Chromium drivers… 0%% (%.0f MB)", float64(total)/1e6))

	pr := &progressReader{
		r:          io.LimitReader(resp.Body, maxDownloadBytes+1),
		total:      total,
		onProgress: onProgress,
	}
	written, err := io.Copy(f, pr)
	if err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if written > maxDownloadBytes {
		return fmt.Errorf("GET %s: response exceeded %d bytes limit", url, maxDownloadBytes)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	cleanup = false
	return nil
}
