//go:build linux

package cdp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildAr writes a minimal ar(1) archive from an ordered list of (name, data)
// pairs. The ar format is: global magic + repeated 60-byte headers + data.
// We write members in the order given so tests are deterministic.
func buildAr(members []arMember) []byte {
	var buf bytes.Buffer
	buf.WriteString("!<arch>\n")
	for _, m := range members {
		// Each header is exactly 60 bytes:
		//   name(16) mtime(12) uid(6) gid(6) mode(8) size(10) magic(2)
		// The two-byte magic is a backtick followed by a newline.
		hdr := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8o%-10d`\n",
			m.name, 0, 0, 0, 0o644, len(m.data))
		buf.WriteString(hdr)
		buf.Write(m.data)
		if len(m.data)%2 != 0 {
			buf.WriteByte('\n') // ar pads entries to even offsets
		}
	}
	return buf.Bytes()
}

type arMember struct {
	name string
	data []byte
}

// buildDataTarGz creates a gzip-compressed tar archive containing the given
// files (path → content). Paths should use the "./prefix/..." convention that
// tar uses when extracting relative to a destination directory.
func buildDataTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %q: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar write %q: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// makeDebFixture builds a minimal .deb file and returns its path.
// It contains debian-binary + control.tar.gz (empty) + data.tar.gz with files.
func makeDebFixture(t *testing.T, files map[string]string) string {
	t.Helper()

	// Empty control.tar.gz (extractDeb must skip it).
	var ctrlBuf bytes.Buffer
	gw := gzip.NewWriter(&ctrlBuf)
	_ = tar.NewWriter(gw).Close()
	_ = gw.Close()

	deb := buildAr([]arMember{
		{name: "debian-binary", data: []byte("2.0\n")},
		{name: "control.tar.gz", data: ctrlBuf.Bytes()},
		{name: "data.tar.gz", data: buildDataTarGz(t, files)},
	})

	path := filepath.Join(t.TempDir(), "test.deb")
	if err := os.WriteFile(path, deb, 0o600); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write deb: %v", err)
	}
	return path
}

// --- tests ---

func TestExtractDeb_BasicFile(t *testing.T) {
	debPath := makeDebFixture(t, map[string]string{
		"./opt/chrome/chrome": "fake chrome binary",
	})
	dest := t.TempDir()

	if err := extractDeb(debPath, dest); err != nil {
		t.Fatalf("extractDeb: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "opt/chrome/chrome")) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(got) != "fake chrome binary" {
		t.Errorf("content = %q, want %q", got, "fake chrome binary")
	}
}

func TestExtractDeb_SkipsControlTar(t *testing.T) {
	// control.tar.gz comes before data.tar.gz in the archive;
	// extractDeb must skip it and land on data.tar.gz.
	debPath := makeDebFixture(t, map[string]string{
		"./sentinel": "data",
	})
	dest := t.TempDir()

	if err := extractDeb(debPath, dest); err != nil {
		t.Fatalf("extractDeb: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "sentinel")); err != nil {
		t.Errorf("sentinel file not extracted: %v", err)
	}
}

func TestExtractDeb_MultipleFiles(t *testing.T) {
	files := map[string]string{
		"./a": "aaa",
		"./b": "bbb",
		"./c": "ccc",
	}
	debPath := makeDebFixture(t, files)
	dest := t.TempDir()

	if err := extractDeb(debPath, dest); err != nil {
		t.Fatalf("extractDeb: %v", err)
	}
	for name, want := range files {
		// Strip leading "./" for the path join.
		got, err := os.ReadFile(filepath.Join(dest, name[2:])) //nolint:gosec // test path
		if err != nil {
			t.Errorf("file %q not extracted: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("file %q: got %q, want %q", name, got, want)
		}
	}
}

func TestExtractDeb_InvalidMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.deb")
	if err := os.WriteFile(path, []byte("NOT AN AR FILE\n"), 0o600); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	if err := extractDeb(path, t.TempDir()); err == nil {
		t.Error("expected error for invalid ar magic, got nil")
	}
}

func TestExtractDeb_MissingDataTar(t *testing.T) {
	// Archive with no data.tar.* member at all.
	deb := buildAr([]arMember{
		{name: "debian-binary", data: []byte("2.0\n")},
	})
	path := filepath.Join(t.TempDir(), "nodatatar.deb")
	if err := os.WriteFile(path, deb, 0o600); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	if err := extractDeb(path, t.TempDir()); err == nil {
		t.Error("expected error when data.tar.* is absent, got nil")
	}
}

// ─── Path functions: baseDir, chromeInstallDir, driverDir, ChromePath, HelperPath ─

func TestBaseDir_UsesXDGCacheHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	got := baseDir()
	if got != filepath.Join(tmp, "vibez") {
		t.Errorf("baseDir() = %q, want %q", got, filepath.Join(tmp, "vibez"))
	}
}

func TestBaseDir_FallsBackToHomeDir(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	got := baseDir()
	if got == "" {
		t.Error("baseDir() should return non-empty path even without XDG_CACHE_HOME")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("baseDir() = %q, want absolute path", got)
	}
}

func TestChromeInstallDir_ContainsChrome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	got := chromeInstallDir()
	if !filepath.IsAbs(got) {
		t.Errorf("chromeInstallDir() = %q, want absolute path", got)
	}
	if filepath.Base(got) != "chrome" {
		t.Errorf("chromeInstallDir() base = %q, want %q", filepath.Base(got), "chrome")
	}
}

func TestDriverDir_ContainsDriver(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	got := driverDir()
	if filepath.Base(got) != "driver" {
		t.Errorf("driverDir() base = %q, want %q", filepath.Base(got), "driver")
	}
}

func TestChromePath_IsAbsolute(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	got := ChromePath()
	if !filepath.IsAbs(got) {
		t.Errorf("ChromePath() = %q, want absolute path", got)
	}
	if filepath.Base(got) != "chrome" {
		t.Errorf("ChromePath() base = %q, want %q", filepath.Base(got), "chrome")
	}
}

func TestHelperPath_IsAbsolute(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	got := HelperPath()
	if !filepath.IsAbs(got) {
		t.Errorf("HelperPath() = %q, want absolute path", got)
	}
	if filepath.Base(got) != "vibez-helper" {
		t.Errorf("HelperPath() base = %q, want %q", filepath.Base(got), "vibez-helper")
	}
}

// ─── linkHelper ────────────────────────────────────────────────────────────

func TestLinkHelper_CreatesHardLink(t *testing.T) {
	// Set up a fake chrome directory structure in a temp cache dir.
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	// Create the directories and a fake chrome binary.
	chromeBin := ChromePath()
	if err := os.MkdirAll(filepath.Dir(chromeBin), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(chromeBin, []byte("fake chrome"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write chrome: %v", err)
	}

	// Call linkHelper — should create vibez-helper.
	linkHelper()

	if _, err := os.Stat(HelperPath()); err != nil {
		t.Errorf("vibez-helper not created by linkHelper(): %v", err)
	}
}

func TestLinkHelper_IdempotentWhenHelperExists(t *testing.T) {
	// Set up a fake chrome + helper already present.
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	chromeBin := ChromePath()
	if err := os.MkdirAll(filepath.Dir(chromeBin), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(chromeBin, []byte("fake chrome"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write chrome: %v", err)
	}
	if err := os.WriteFile(HelperPath(), []byte("fake helper"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write helper: %v", err)
	}

	// Should not panic and should be a no-op.
	linkHelper()
}

// ─── EnsureBrowser when Chrome already installed ────────────────────────────

func TestEnsureBrowser_AlreadyInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	chromeBin := ChromePath()
	if err := os.MkdirAll(filepath.Dir(chromeBin), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(chromeBin, []byte("fake chrome"), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write chrome: %v", err)
	}

	var progress []string
	err := EnsureBrowser(func(s string) { progress = append(progress, s) })
	if err != nil {
		t.Errorf("EnsureBrowser when already installed should return nil, got: %v", err)
	}
	// No download should have been triggered.
	if len(progress) > 0 {
		t.Errorf("EnsureBrowser when installed should not call onProgress, got: %v", progress)
	}
}

// ─── extractDeb: truncated ar header ───────────────────────────────────────

func TestExtractDeb_TruncatedArHeader(t *testing.T) {
	// Write only the global magic + 30 bytes (header is 60 bytes) → should fail gracefully.
	data := []byte("!<arch>\n" + strings.Repeat(" ", 30))
	path := filepath.Join(t.TempDir(), "truncated.deb")
	if err := os.WriteFile(path, data, 0o600); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	// extractDeb should return an error (missing data.tar.*), not panic.
	if err := extractDeb(path, t.TempDir()); err == nil {
		t.Error("expected error for truncated ar, got nil")
	}
}
