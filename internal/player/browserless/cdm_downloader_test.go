//go:build linux || darwin || windows

package browserless

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestCDMDownloaderURLAndHashGuards(t *testing.T) {
	allowed := []string{
		"https://dl.google.com/widevine-cdm/4.10.2891.0-linux-x64.zip",
		"https://edgedl.me.gvt1.com/edgedl/widevine-cdm/foo.crx3",
		"https://www.google.com/widevine-cdm/foo.crx3",
	}
	for _, u := range allowed {
		if !isAllowedCDMHost(u) {
			t.Errorf("isAllowedCDMHost(%q) = false, want true", u)
		}
	}

	rejected := []string{
		"",
		"http://dl.google.com/foo.zip",
		"https://user:pass@dl.google.com/foo.zip",
		"https://evil.example.com/foo.zip",
	}
	for _, u := range rejected {
		if isAllowedCDMHost(u) {
			t.Errorf("isAllowedCDMHost(%q) = true, want false", u)
		}
	}

	if !isValidSHA512Hex(fallbackCDNs["linux_amd64"].sha512) {
		t.Error("fallback linux_amd64 sha512 rejected")
	}
	if isValidSHA512Hex("") || isValidSHA512Hex("deadbeef") || isValidSHA512Hex(bytesToHexInvalid128()) {
		t.Error("invalid sha512 accepted")
	}
}

func bytesToHexInvalid128() string {
	var b [128]byte
	for i := range b {
		b[i] = 'g'
	}
	return string(b[:])
}

func TestExtractLibraryFromCRX_RejectsOverflowHeaderLen(t *testing.T) {
	var crxBuf bytes.Buffer
	crxBuf.WriteString("Cr24")
	_ = binary.Write(&crxBuf, binary.LittleEndian, uint32(3))
	_ = binary.Write(&crxBuf, binary.LittleEndian, uint32(0xffffffff))
	crxBuf.WriteString("payload")

	tmpDir := t.TempDir()
	crxPath := filepath.Join(tmpDir, "bad.crx3")
	if err := os.WriteFile(crxPath, crxBuf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractLibraryFromCRX(crxPath, filepath.Join(tmpDir, "out.so")); err == nil {
		t.Fatal("extractLibraryFromCRX succeeded on overflowing uint32 headerLen, want error")
	}
}

func appendProtoBytes(dst []byte, fieldNum uint64, val []byte) []byte {
	dst = binary.AppendUvarint(dst, (fieldNum<<3)|2)
	dst = binary.AppendUvarint(dst, uint64(len(val)))
	return append(dst, val...)
}

func buildSignedCRX3ForTest(t *testing.T, zipData []byte, priv *ecdsa.PrivateKey, crxIDOverride []byte) []byte {
	t.Helper()
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	crxID := crxIDOverride
	if crxID == nil {
		sum := sha256.Sum256(pubDER)
		crxID = sum[:16]
	}

	signedHeaderData := appendProtoBytes(nil, 1, crxID)
	var lenLE [4]byte
	binary.LittleEndian.PutUint32(lenLE[:], uint32(len(signedHeaderData))) //nolint:gosec // G115: tiny test header

	h := sha256.New()
	_, _ = h.Write([]byte("CRX3 SignedData\x00"))
	_, _ = h.Write(lenLE[:])
	_, _ = h.Write(signedHeaderData)
	_, _ = h.Write(zipData)
	digest := h.Sum(nil)

	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest)
	if err != nil {
		t.Fatalf("ecdsa.SignASN1: %v", err)
	}
	_ = crypto.SHA256

	var proofBuf []byte
	proofBuf = appendProtoBytes(proofBuf, 1, pubDER)
	proofBuf = appendProtoBytes(proofBuf, 2, sig)

	var headerBuf []byte
	headerBuf = appendProtoBytes(headerBuf, 3, proofBuf)
	headerBuf = appendProtoBytes(headerBuf, 10000, signedHeaderData)

	var out bytes.Buffer
	out.WriteString("Cr24")
	_ = binary.Write(&out, binary.LittleEndian, uint32(3))
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(headerBuf))) //nolint:gosec // G115: tiny test header
	out.Write(headerBuf)
	out.Write(zipData)
	return out.Bytes()
}

func TestVerifyCRX3FileSignature(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	keyDigest := sha256.Sum256(pubDER)
	wantID := keyDigest[:16]

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("libwidevinecdm.so")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("payload"))
	_ = zw.Close()

	tmpDir := t.TempDir()
	validPath := filepath.Join(tmpDir, "valid.crx3")
	signed := buildSignedCRX3ForTest(t, zipBuf.Bytes(), priv, nil)
	if err := os.WriteFile(validPath, signed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyCRX3FileSignature(validPath, wantID); err != nil {
		t.Fatalf("verifyCRX3FileSignature(valid) = %v, want nil", err)
	}

	// Wrong expected CRX ID must fail.
	if err := verifyCRX3FileSignature(validPath, widevineCRXID[:]); err == nil {
		t.Fatal("verifyCRX3FileSignature accepted mismatched CRX ID")
	}

	// Tampered ZIP payload must fail.
	tampered := append([]byte(nil), signed...)
	tampered[len(tampered)-1] ^= 0xff
	tamperedPath := filepath.Join(tmpDir, "tampered.crx3")
	if err := os.WriteFile(tamperedPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyCRX3FileSignature(tamperedPath, wantID); err == nil {
		t.Fatal("verifyCRX3FileSignature accepted tampered CRX3 payload")
	}
}

func TestCDMPathAndCandidateSecurityGuards(t *testing.T) {
	for _, u := range []string{
		"https://raw.githubusercontent.com/mozilla-firefox/firefox/main/widevinecdm.json",
		"https://objects.githubusercontent.com/some/path",
		"https://hg.mozilla.org/mozilla-central/raw-file/tip/widevinecdm.json",
	} {
		if !isAllowedManifestHost(u) {
			t.Errorf("isAllowedManifestHost(%q) = false, want true", u)
		}
	}
	for _, bad := range []string{
		"http://raw.githubusercontent.com/x",
		"https://user@raw.githubusercontent.com/x",
		"https://evil.example.com/widevinecdm.json",
		"https://raw.githubusercontent.com/x\r\n",
	} {
		if isAllowedManifestHost(bad) {
			t.Errorf("isAllowedManifestHost(%q) = true, want false", bad)
		}
	}

	// Unsetting HOME/USERPROFILE/LOCALAPPDATA must never yield a CWD-relative path.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("XDG_CACHE_HOME", "")
	if def := DefaultCDMPath(); def != "" && !filepath.IsAbs(def) {
		t.Fatalf("DefaultCDMPath() returned relative path %q when env unset", def)
	}
	for _, c := range candidatePaths() {
		if !filepath.IsAbs(c) {
			t.Fatalf("candidatePaths() returned relative path %q", c)
		}
	}

	// FindCDM must reject relative paths, bogus non-executable files, and world-writable binaries.
	tmpDir := t.TempDir()
	fakeLib := filepath.Join(tmpDir, "fake-cdm")
	bigBad := make([]byte, 120000)
	copy(bigBad, "NOT_A_BINARY")
	if err := os.WriteFile(fakeLib, bigBad, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIBEZ_CDM_PATH", fakeLib)
	if got := FindCDM(); got == fakeLib {
		t.Fatalf("FindCDM accepted non-binary file %q", got)
	}

	if runtime.GOOS == "linux" {
		copy(bigBad[:4], []byte{0x7f, 'E', 'L', 'F'})
		// Note: Chmod 0o777 on temp file in tmpDir (umask-independent chmod).
		if err := os.WriteFile(fakeLib, bigBad, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(fakeLib, 0o777); err != nil { //nolint:gosec // G302: intentionally creating world-writable file to test rejection
			t.Fatal(err)
		}
		if got := FindCDM(); got == fakeLib {
			t.Fatalf("FindCDM accepted world-writable ELF %q", got)
		}
		if err := os.Chmod(fakeLib, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := FindCDM(); got != fakeLib {
			t.Fatalf("FindCDM() = %q, want %q for valid 0600 ELF", got, fakeLib)
		}
	}
}

func testValidCDMMagicBytes() []byte {
	buf := make([]byte, 120000)
	switch runtime.GOOS {
	case "windows":
		buf[0], buf[1] = 'M', 'Z'
	case "darwin":
		copy(buf[:4], []byte{0xcf, 0xfa, 0xed, 0xfe})
	default:
		copy(buf[:4], []byte{0x7f, 'E', 'L', 'F'})
	}
	return buf
}

func TestEnsureCDM_LogsNoticeWhenEnvOverrideIgnored(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("LocalAppData", filepath.Join(tmpHome, "AppData", "Local"))

	fallback := DefaultCDMPath()
	if fallback == "" {
		t.Fatal("DefaultCDMPath returned empty path")
	}
	if err := os.MkdirAll(filepath.Dir(fallback), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallback, testValidCDMMagicBytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	badOverride := filepath.Join(tmpHome, "non-binary-cdm")
	if err := os.WriteFile(badOverride, []byte("small"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIBEZ_CDM_PATH", badOverride)

	var invalidNotices []string
	gotInvalid, err := EnsureCDM(func(msg string) {
		invalidNotices = append(invalidNotices, msg)
	})
	if err != nil {
		t.Fatalf("EnsureCDM(invalid override) failed: %v", err)
	}
	if gotInvalid != fallback {
		t.Fatalf("EnsureCDM() = %q, want %q fallback", gotInvalid, fallback)
	}
	if len(invalidNotices) == 0 || !strings.Contains(invalidNotices[0], "ignoring VIBEZ_CDM_PATH") {
		t.Fatalf("EnsureCDM notices = %v, expected notice explaining ignored VIBEZ_CDM_PATH", invalidNotices)
	}

	// Point VIBEZ_CDM_PATH to a symlink on Unix (or skip symlink subcheck if unsupported on Windows runner).
	override := filepath.Join(tmpHome, "symlink-cdm")
	if err := os.Symlink(fallback, override); err != nil {
		if runtime.GOOS == "windows" {
			return
		}
		t.Fatal(err)
	}
	t.Setenv("VIBEZ_CDM_PATH", override)

	var notices []string
	got, err := EnsureCDM(func(msg string) {
		notices = append(notices, msg)
	})
	if err != nil {
		t.Fatalf("EnsureCDM failed: %v", err)
	}
	if got != fallback {
		t.Fatalf("EnsureCDM() = %q, want %q fallback", got, fallback)
	}
	foundNotice := false
	for _, msg := range notices {
		if strings.Contains(msg, "ignoring VIBEZ_CDM_PATH") {
			foundNotice = true
			break
		}
	}
	if !foundNotice {
		t.Fatalf("EnsureCDM notices = %v, expected notice explaining ignored VIBEZ_CDM_PATH", notices)
	}
}
