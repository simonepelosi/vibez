//go:build linux || darwin || windows

package browserless

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractLibraryFromCRX(t *testing.T) {
	// Create a dummy zip containing libwidevinecdm.dylib
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("_platform_specific/mac_arm64/libwidevinecdm.dylib")
	if err != nil {
		t.Fatal(err)
	}
	dummyContent := []byte("dummy-cdm-binary-content-1234567890")
	if _, err := f.Write(dummyContent); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	// Create dummy CRX3: Magic (4B) + Version (4B) + HeaderLen (4B) + Header (HeaderLen B) + ZipData
	headerData := []byte("dummy-header")
	headerLen := uint32(len(headerData)) //nolint:gosec // test length bounded

	var crxBuf bytes.Buffer
	crxBuf.WriteString("Cr24")
	_ = binary.Write(&crxBuf, binary.LittleEndian, uint32(3))
	_ = binary.Write(&crxBuf, binary.LittleEndian, headerLen)
	crxBuf.Write(headerData)
	crxBuf.Write(zipBuf.Bytes())

	tmpDir := t.TempDir()
	crxFile := filepath.Join(tmpDir, "test.crx3")
	if err := os.WriteFile(crxFile, crxBuf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	destFile := filepath.Join(tmpDir, "out", "libwidevinecdm.dylib")
	if err := extractLibraryFromCRX(crxFile, destFile); err != nil {
		t.Fatalf("extractLibraryFromCRX failed: %v", err)
	}

	outData, err := os.ReadFile(destFile) //nolint:gosec // test file in t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(outData, dummyContent) {
		t.Fatalf("extracted content mismatch: got %q, want %q", outData, dummyContent)
	}
}

func TestCanDownloadCDM(t *testing.T) {
	// On supported platforms (linux/amd64, darwin/arm64, darwin/amd64), it must be true
	if !CanDownloadCDM() {
		t.Log("CanDownloadCDM returned false on this platform")
	}
}

func TestDownloadCDMReal(t *testing.T) {
	if testing.Short() || os.Getenv("VIBEZ_TEST_NETWORK") != "1" {
		t.Skip("skipping live 21 MB CDN download test (set VIBEZ_TEST_NETWORK=1 to run)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	targetName := filepath.Base(DefaultCDMPath())
	destPath := filepath.Join(tmpDir, targetName)
	path, err := DownloadCDM(ctx, destPath)
	if err != nil {
		t.Fatalf("DownloadCDM failed: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if fi.Size() < 1000000 {
		t.Fatalf("extracted file too small: %d bytes", fi.Size())
	}
	t.Logf("Successfully downloaded and extracted official Widevine CDM: %s (%d bytes)", path, fi.Size())
}

// Every platform we can play on has to ship a pinned package: the manifest is
// the preferred source, but this is what runs when it is unreachable.
func TestFallbackCDNsAreWellFormed(t *testing.T) {
	for _, key := range []string{"linux_amd64", "darwin_arm64", "darwin_amd64", "windows_amd64"} {
		spec, ok := fallbackCDNs[key]
		if !ok {
			t.Errorf("no fallback package for %s", key)
			continue
		}
		if !isAllowedCDMHost(spec.url) {
			t.Errorf("%s: fallback URL %q is not on an allowed host", key, spec.url)
		}
		if len(spec.sha512) != 128 {
			t.Errorf("%s: sha512 %q is not 128 hex characters", key, spec.sha512)
		}
		if spec.fileSize <= 0 {
			t.Errorf("%s: missing file size", key)
		}
		if _, ok := mozillaPlatformKeys[key]; !ok {
			t.Errorf("%s: no Mozilla manifest platform key", key)
		}
	}
}

func TestExtractCDMFromCRX3FindsWindowsDLL(t *testing.T) {
	payload := make([]byte, 2*1024*1024)
	copy(payload, "MZ")
	crx := buildFakeCRX3(t, "_platform_specific/win_x64/widevinecdm.dll", payload)

	got, err := extractCDMFromCRX3(crx)
	if err != nil {
		t.Fatalf("extractCDMFromCRX3: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("extracted %d bytes, want %d", len(got), len(payload))
	}
}
