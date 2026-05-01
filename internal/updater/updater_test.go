package updater

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// ── isNewer ───────────────────────────────────────────────────────────────────

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.0.10", "v0.0.9", true},  // integer, not lexicographic
		{"v0.0.9", "v0.0.9", false},  // same version
		{"v0.0.8", "v0.0.9", false},  // older
		{"v1.0.0", "v0.9.9", true},   // major bump
		{"v0.1.0", "v0.0.9", true},   // minor bump
		{"0.0.10", "0.0.9", true},    // no leading v
		{"v0.0.9", "v0.0.10", false}, // current is newer
	}
	for _, tc := range cases {
		got := isNewer(tc.latest, tc.current)
		if got != tc.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

// ── verifyChecksum ────────────────────────────────────────────────────────────

func TestVerifyChecksum_Valid(t *testing.T) {
	data := []byte("fake tarball content")
	sum := sha256.Sum256(data)
	hashHex := hex.EncodeToString(sum[:])
	assetName := "vibez_linux_amd64.tar.gz"

	checksumBody := hashHex + "  " + assetName + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(checksumBody))
	}))
	defer srv.Close()

	tarPath := filepath.Join(t.TempDir(), assetName)
	if err := os.WriteFile(tarPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := verifyChecksum(tarPath, assetName, srv.URL); err != nil {
		t.Errorf("verifyChecksum unexpectedly failed: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	data := []byte("tampered content")
	assetName := "vibez_linux_amd64.tar.gz"

	checksumBody := "0000000000000000000000000000000000000000000000000000000000000000  " + assetName + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(checksumBody))
	}))
	defer srv.Close()

	tarPath := filepath.Join(t.TempDir(), assetName)
	if err := os.WriteFile(tarPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := verifyChecksum(tarPath, assetName, srv.URL); err == nil {
		t.Error("verifyChecksum should fail on hash mismatch")
	}
}

// ── extractBinary ─────────────────────────────────────────────────────────────

func TestExtractBinary_Found(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho vibez")
	tarPath := filepath.Join(t.TempDir(), "vibez_linux_amd64.tar.gz")

	f, err := os.Create(tarPath) //nolint:gosec // tarPath is a path inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "vibez",
		Typeflag: tar.TypeReg,
		Size:     int64(len(binaryContent)),
		Mode:     0o755,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binaryContent); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "vibez")
	if err := extractBinary(tarPath, "vibez", dst); err != nil {
		t.Fatalf("extractBinary failed: %v", err)
	}
	got, err := os.ReadFile(dst) //nolint:gosec // dst is a path inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binaryContent) {
		t.Errorf("extracted content = %q, want %q", got, binaryContent)
	}
}

func TestExtractBinary_NotFound(t *testing.T) {
	tarPath := filepath.Join(t.TempDir(), "empty.tar.gz")

	f, err := os.Create(tarPath) //nolint:gosec // tarPath is a path inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "vibez")
	if err := extractBinary(tarPath, "vibez", dst); err == nil {
		t.Error("extractBinary should error when binary not in archive")
	}
}

// ── shouldCheck / markChecked ─────────────────────────────────────────────────

func TestShouldCheck_NoStamp(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if !shouldCheck() {
		t.Error("shouldCheck should return true when stamp does not exist")
	}
}

func TestShouldCheck_RecentStamp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "vibez"), 0o750); err != nil {
		t.Fatal(err)
	}
	stamp := filepath.Join(dir, "vibez", "last_update_check")
	if err := os.WriteFile(stamp, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if shouldCheck() {
		t.Error("shouldCheck should return false when stamp was just written")
	}
}
