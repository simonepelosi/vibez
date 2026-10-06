package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
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

func TestExtractBinary_ZipFound(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "vibez_windows_amd64.zip")
	if err := os.WriteFile(zipPath, zipOf(t, "vibez.exe", []byte("MZ vibez")), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "vibez.exe")
	if err := extractBinary(zipPath, "vibez.exe", dst); err != nil {
		t.Fatalf("extractBinary failed: %v", err)
	}
	if got := readFile(t, dst); got != "MZ vibez" {
		t.Errorf("extracted content = %q, want %q", got, "MZ vibez")
	}
}

func TestExtractBinary_ZipNotFound(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "vibez_windows_amd64.zip")
	if err := os.WriteFile(zipPath, zipOf(t, "vibez", []byte("not the Windows binary")), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "vibez.exe")
	if err := extractBinary(zipPath, "vibez.exe", dst); err == nil {
		t.Error("extractBinary should error when binary not in archive")
	}
}

// A zip says how large an entry unpacks to, so one past the limit is refused
// before anything is written rather than cut short into a broken binary.
func TestExtractBinary_ZipOverLimitIsRefused(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(&zip.FileHeader{
		Name:               "vibez.exe",
		Method:             zip.Store,
		CompressedSize64:   1,
		UncompressedSize64: maxBinarySize + 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "vibez_windows_amd64.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "vibez.exe")
	if err := extractBinary(zipPath, "vibez.exe", dst); err == nil {
		t.Error("extractBinary should refuse an entry larger than maxBinarySize")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("extractBinary wrote an oversized entry out")
	}
}

// ── releaseAsset ──────────────────────────────────────────────────────────────

// Each platform must ask for the archive its release publishes, and look in it
// for the executable under that platform's name for it.
func TestReleaseAsset(t *testing.T) {
	cases := []struct {
		goos, goarch, archive, binary string
	}{
		{"linux", "amd64", "vibez_linux_amd64.tar.gz", "vibez"},
		{"darwin", "arm64", "vibez_darwin_arm64.tar.gz", "vibez"},
		{"windows", "amd64", "vibez_windows_amd64.zip", "vibez.exe"},
	}
	for _, tc := range cases {
		archive, binary := releaseAsset(tc.goos, tc.goarch)
		if archive != tc.archive || binary != tc.binary {
			t.Errorf("releaseAsset(%q, %q) = %q, %q; want %q, %q", tc.goos, tc.goarch, archive, binary, tc.archive, tc.binary)
		}
	}
}

// ── shouldCheck / markChecked ─────────────────────────────────────────────────

func TestShouldCheck_NoStamp(t *testing.T) {
	isolateCache(t)
	if !shouldCheck() {
		t.Error("shouldCheck should return true when stamp does not exist")
	}
}

func TestShouldCheck_RecentStamp(t *testing.T) {
	isolateCache(t)
	dir := cacheDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	stamp := filepath.Join(dir, "last_update_check")
	if err := os.WriteFile(stamp, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if shouldCheck() {
		t.Error("shouldCheck should return false when stamp was just written")
	}
}

// ── update ────────────────────────────────────────────────────────────────────

// tarGz builds a gzipped tar holding one regular file.
func tarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Size:     int64(len(content)),
		Mode:     0o755,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// zipOf builds a zip laid out like a Windows release, with a README ahead of
// the one file that matters.
func zipOf(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name    string
		content []byte
	}{{"README.md", []byte("# vibez\n")}, {name, content}} {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// releaseArchive packs binary the way a release publishes it for the platform
// running the test, and names the archive the same way.
func releaseArchive(t *testing.T, binary []byte) (name string, archive []byte) {
	t.Helper()
	name, binaryName := releaseAsset(runtime.GOOS, runtime.GOARCH)
	if strings.HasSuffix(name, ".zip") {
		return name, zipOf(t, binaryName, binary)
	}
	return name, tarGz(t, binaryName, binary)
}

// fakeReleases serves a releases endpoint for tag, plus the archive and
// checksums it advertises. withAsset says whether an archive for this platform
// is published at all, which is how a release that skips a platform is
// modelled.
func fakeReleases(t *testing.T, tag string, withAsset bool, binary []byte) *httptest.Server {
	t.Helper()
	assetName, archive := releaseArchive(t, binary)
	sum := sha256.Sum256(archive)

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		switch r.URL.Path {
		case "/checksums.txt":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + assetName + "\n"))
		case "/asset":
			_, _ = w.Write(archive)
		default:
			assets := []ghAsset{{Name: "checksums.txt", BrowserDownloadURL: base + "/checksums.txt"}}
			if withAsset {
				assets = append(assets, ghAsset{Name: assetName, BrowserDownloadURL: base + "/asset"})
			}
			_ = json.NewEncoder(w).Encode(ghRelease{TagName: tag, Assets: assets})
		}
	}))
}

// isolateCache points the update-check stamp at a temp home so a test never
// reads or writes the real one. Windows finds its cache under LOCALAPPDATA,
// falling back to USERPROFILE; elsewhere it is under HOME.
func isolateCache(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
}

// fakeExe writes a stand-in binary and points executable() at it, so a test
// never overwrites the test binary itself.
func fakeExe(t *testing.T, mode os.FileMode) string {
	t.Helper()
	_, binaryName := releaseAsset(runtime.GOOS, runtime.GOARCH)
	path := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(path, []byte("old binary"), mode); err != nil {
		t.Fatal(err)
	}
	orig := executable
	executable = func() (string, error) { return path, nil }
	t.Cleanup(func() { executable = orig })
	return path
}

// failRename makes the renames whose source matches fail, as a locked or
// vanished file would, and leaves every other rename to os.Rename.
func failRename(t *testing.T, match func(src string) bool) {
	t.Helper()
	orig := rename
	rename = func(src, dst string) error {
		if match(src) {
			return &os.LinkError{Op: "rename", Old: src, New: dst, Err: errors.New("injected failure")}
		}
		return orig(src, dst)
	}
	t.Cleanup(func() { rename = orig })
}

func isStaged(src string) bool { return strings.HasSuffix(src, ".new") }

func isAside(src string) bool { return strings.Contains(filepath.Base(src), asideInfix) }

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// dirNames lists what is in dir, sorted.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func discard(string) {}

func TestUpdate_CurrentVersionNeedsNothing(t *testing.T) {
	isolateCache(t)
	srv := fakeReleases(t, "v1.2.3", true, []byte("new binary"))
	defer srv.Close()

	got := update(srv.URL, "v1.2.3", true, discard)
	if got.Outcome != OutcomeCurrent {
		t.Errorf("Outcome = %v, want OutcomeCurrent", got.Outcome)
	}
	if got.Exe != "" {
		t.Errorf("Exe = %q, want empty", got.Exe)
	}
}

func TestUpdate_CheckFailureKnowsNothing(t *testing.T) {
	isolateCache(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	got := update(srv.URL, "v1.0.0", true, discard)
	if got.Outcome != OutcomeFailed {
		t.Errorf("Outcome = %v, want OutcomeFailed", got.Outcome)
	}
	if got.Tag != "" {
		t.Errorf("Tag = %q, want empty: a failed check learns no version", got.Tag)
	}
	if got.Advice() != "" {
		t.Errorf("Advice = %q, want empty", got.Advice())
	}
}

func TestUpdate_DisabledStillReportsTheRelease(t *testing.T) {
	isolateCache(t)
	srv := fakeReleases(t, "v2.0.0", true, []byte("new binary"))
	defer srv.Close()

	got := update(srv.URL, "v1.0.0", false, discard)
	if got.Outcome != OutcomeDisabled {
		t.Errorf("Outcome = %v, want OutcomeDisabled", got.Outcome)
	}
	if got.Tag != "v2.0.0" {
		t.Errorf("Tag = %q, want v2.0.0", got.Tag)
	}
	if !strings.Contains(got.Advice(), "--no-update") {
		t.Errorf("Advice = %q, should say how to install it", got.Advice())
	}
}

// A platform with no published archive - linux/arm64 at the time of writing -
// must not be told to drop --no-update for a build that is not there.
func TestUpdate_MissingPlatformAssetIsManual(t *testing.T) {
	isolateCache(t)
	srv := fakeReleases(t, "v2.0.0", false, nil)
	defer srv.Close()

	for _, install := range []bool{true, false} {
		got := update(srv.URL, "v1.0.0", install, discard)
		if got.Outcome != OutcomeManual {
			t.Errorf("install=%v: Outcome = %v, want OutcomeManual", install, got.Outcome)
		}
		if got.Tag != "v2.0.0" {
			t.Errorf("install=%v: Tag = %q, want v2.0.0", install, got.Tag)
		}
	}
}

func TestUpdate_UnwritableBinaryIsManual(t *testing.T) {
	isolateCache(t)
	fakeExe(t, 0o400)
	srv := fakeReleases(t, "v2.0.0", true, []byte("new binary"))
	defer srv.Close()

	got := update(srv.URL, "v1.0.0", true, discard)
	if got.Outcome != OutcomeManual {
		t.Errorf("Outcome = %v, want OutcomeManual", got.Outcome)
	}
	if !strings.Contains(got.Advice(), "reinstall") {
		t.Errorf("Advice = %q, should point at reinstalling", got.Advice())
	}
}

// The file an update replaces is always a running executable, and Linux will
// not open one for writing (ETXTBSY), so canReplace must decide without
// opening it. The test binary is the one executable known to be running here.
func TestCanReplace_RunningBinary(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !canReplace(self) {
		t.Fatalf("canReplace(%s) = false for the running test binary", self)
	}
}

func TestUpdate_InstallsOverTheRunningBinary(t *testing.T) {
	isolateCache(t)
	exe := fakeExe(t, 0o755)
	srv := fakeReleases(t, "v2.0.0", true, []byte("new binary"))
	defer srv.Close()

	got := update(srv.URL, "v1.0.0", true, discard)
	if got.Outcome != OutcomeInstalled {
		t.Fatalf("Outcome = %v, want OutcomeInstalled", got.Outcome)
	}

	// The path comes back symlink-resolved, which on macOS means /private/var
	// rather than the /var the test handed in.
	want, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	if got.Exe != want {
		t.Errorf("Exe = %q, want %q", got.Exe, want)
	}
	content, err := os.ReadFile(exe) //nolint:gosec // exe is a path inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new binary" {
		t.Errorf("installed content = %q, want %q", content, "new binary")
	}
	// Neither the staged binary nor, on Windows, the one moved aside - which
	// nothing runs here - is left beside it.
	if names := dirNames(t, filepath.Dir(exe)); !slices.Equal(names, []string{filepath.Base(exe)}) {
		t.Errorf("files beside the installed binary = %v, want only %s", names, filepath.Base(exe))
	}
}

func TestUpdate_ChecksumMismatchInstallsNothing(t *testing.T) {
	isolateCache(t)
	exe := fakeExe(t, 0o755)
	assetName, archive := releaseArchive(t, []byte("new binary"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		switch r.URL.Path {
		case "/checksums.txt":
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + assetName + "\n"))
		case "/asset":
			_, _ = w.Write(archive)
		default:
			_ = json.NewEncoder(w).Encode(ghRelease{TagName: "v2.0.0", Assets: []ghAsset{
				{Name: "checksums.txt", BrowserDownloadURL: base + "/checksums.txt"},
				{Name: assetName, BrowserDownloadURL: base + "/asset"},
			}})
		}
	}))
	defer srv.Close()

	got := update(srv.URL, "v1.0.0", true, discard)
	if got.Outcome != OutcomeManual {
		t.Errorf("Outcome = %v, want OutcomeManual", got.Outcome)
	}
	content, err := os.ReadFile(exe) //nolint:gosec // exe is a path inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old binary" {
		t.Errorf("binary was replaced despite a bad checksum: %q", content)
	}
}

// A new binary that cannot take the old one's place must leave the old one
// installed and nothing else behind, however the platform replaces it.
func TestUpdate_FailedInstallKeepsTheOldBinary(t *testing.T) {
	isolateCache(t)
	exe := fakeExe(t, 0o755)
	srv := fakeReleases(t, "v2.0.0", true, []byte("new binary"))
	defer srv.Close()
	failRename(t, isStaged)

	got := update(srv.URL, "v1.0.0", true, discard)
	if got.Outcome != OutcomeManual {
		t.Errorf("Outcome = %v, want OutcomeManual", got.Outcome)
	}
	if content := readFile(t, exe); content != "old binary" {
		t.Errorf("binary after a failed install = %q, want the old one", content)
	}
	if names := dirNames(t, filepath.Dir(exe)); !slices.Equal(names, []string{filepath.Base(exe)}) {
		t.Errorf("files beside the binary = %v, want only %s", names, filepath.Base(exe))
	}
}

// ── replaceRunningExe / removeAside ───────────────────────────────────────────

// stageReplacement lays out an installed binary with an update staged beside
// it, as installBinary hands them to replaceRunningExe.
func stageReplacement(t *testing.T) (exe, staged string) {
	t.Helper()
	exe = filepath.Join(t.TempDir(), "vibez.exe")
	staged = exe + ".new"
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil { //nolint:gosec // executable permissions required
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new binary"), 0o755); err != nil { //nolint:gosec // executable permissions required
		t.Fatal(err)
	}
	return exe, staged
}

func TestReplaceRunningExe_Installs(t *testing.T) {
	exe, staged := stageReplacement(t)

	if err := replaceRunningExe(staged, exe); err != nil {
		t.Fatalf("replaceRunningExe: %v", err)
	}
	if content := readFile(t, exe); content != "new binary" {
		t.Errorf("installed content = %q, want %q", content, "new binary")
	}
	if names := dirNames(t, filepath.Dir(exe)); !slices.Equal(names, []string{"vibez.exe"}) {
		t.Errorf("files beside the binary = %v, want only vibez.exe", names)
	}
}

// When the update cannot move in, the binary moved aside for it goes back.
func TestReplaceRunningExe_RestoresTheOldBinary(t *testing.T) {
	exe, staged := stageReplacement(t)
	failRename(t, isStaged)

	if err := replaceRunningExe(staged, exe); err == nil {
		t.Fatal("replaceRunningExe reported success for an install that failed")
	}
	if content := readFile(t, exe); content != "old binary" {
		t.Errorf("binary after a failed install = %q, want the old one", content)
	}
	if names := dirNames(t, filepath.Dir(exe)); !slices.Equal(names, []string{"vibez.exe", "vibez.exe.new"}) {
		t.Errorf("files = %v, want the binary and the staged update only", names)
	}
}

// If even moving the old binary back fails, it must still be intact, and the
// error must say where it is so the user can put it back.
func TestReplaceRunningExe_ReportsWhereTheOldBinaryIs(t *testing.T) {
	exe, staged := stageReplacement(t)
	failRename(t, func(src string) bool { return isStaged(src) || isAside(src) })

	err := replaceRunningExe(staged, exe)
	if err == nil {
		t.Fatal("replaceRunningExe reported success for an install that failed")
	}
	var aside string
	for _, name := range dirNames(t, filepath.Dir(exe)) {
		if isAsideName("vibez.exe", name) {
			aside = filepath.Join(filepath.Dir(exe), name)
		}
	}
	if aside == "" {
		t.Fatal("the old binary is gone")
	}
	if content := readFile(t, aside); content != "old binary" {
		t.Errorf("old binary moved aside = %q, want it unchanged", content)
	}
	if !strings.Contains(err.Error(), aside) {
		t.Errorf("error %q does not say the old binary is at %s", err, aside)
	}
}

// Only names replaceRunningExe makes are swept, so a user's own files beside
// the binary survive.
func TestRemoveAside_TakesOnlyWhatWasMovedAside(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "vibez.exe")
	keep := []string{ // in the order ReadDir lists them
		"other.exe" + asideInfix + strings.Repeat("0", 2*asideIDBytes),
		"vibez.exe",
		"vibez.exe.new",
		"vibez.exe" + asideInfix + "0123",
		"vibez.exe.old-backup",
	}
	sweep := []string{filepath.Base(asideName(exe)), filepath.Base(asideName(exe))}
	for _, name := range slices.Concat(keep, sweep) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	removeAside(exe)
	if names := dirNames(t, dir); !slices.Equal(names, keep) {
		t.Errorf("after sweeping, files = %v, want %v", names, keep)
	}
}

// updaterHelperEnv makes a copy of this test binary sit in
// TestInstallBinary_ReplacesARunningBinary until its stdin closes.
const updaterHelperEnv = "VIBEZ_TEST_UPDATER_HELPER"

// Every self-update replaces the binary that is running, which Windows only
// allows by moving it aside. This installs over a copy of the test binary while
// that copy runs, then checks the sweep clears what was moved aside once the
// copy has exited.
func TestInstallBinary_ReplacesARunningBinary(t *testing.T) {
	if os.Getenv(updaterHelperEnv) == "run" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self) //nolint:gosec // the running test binary
	if err != nil {
		t.Fatal(err)
	}
	_, binaryName := releaseAsset(runtime.GOOS, runtime.GOARCH)
	dir := t.TempDir()
	exe := filepath.Join(dir, binaryName)
	if err := os.WriteFile(exe, image, 0o755); err != nil { //nolint:gosec // executable permissions required
		t.Fatal(err)
	}

	running := exec.Command(exe, "-test.run=^TestInstallBinary_ReplacesARunningBinary$") //nolint:gosec // a copy of this test binary
	running.Env = append(os.Environ(), updaterHelperEnv+"=run")
	stdin, err := running.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	installErr := installBinary(exe, []byte("new binary"))
	_ = stdin.Close()
	if err := running.Wait(); err != nil {
		t.Errorf("running copy: %v", err)
	}
	if installErr != nil {
		t.Fatalf("installBinary over a running binary: %v", installErr)
	}
	if content := readFile(t, exe); content != "new binary" {
		t.Errorf("installed content = %q, want %q", content, "new binary")
	}

	// Windows can hold on to an image for a moment after its process exits.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		removeAside(exe)
		names := dirNames(t, dir)
		if slices.Equal(names, []string{binaryName}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("files beside the binary after its old copy exited = %v, want only %s", names, binaryName)
		}
	}
}

// UpdateNow must ask even when CheckAndUpdate would have skipped the day's
// check, since the caller already knows this build cannot work.
func TestUpdateNow_IgnoresTheDailyWindow(t *testing.T) {
	isolateCache(t)
	dir := cacheDir()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last_update_check"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if shouldCheck() {
		t.Fatal("stamp was just written, so the daily window should be closed")
	}

	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked = true
		_ = json.NewEncoder(w).Encode(ghRelease{TagName: "v1.0.0"})
	}))
	defer srv.Close()

	if got := update(srv.URL, "v1.0.0", false, discard); got.Outcome != OutcomeCurrent {
		t.Errorf("Outcome = %v, want OutcomeCurrent", got.Outcome)
	}
	if !asked {
		t.Error("update did not reach the server")
	}
}

func TestAdvice(t *testing.T) {
	cases := []struct {
		name string
		r    Result
		want string
	}{
		{"failed", Result{Outcome: OutcomeFailed}, ""},
		{"current", Result{Outcome: OutcomeCurrent, Tag: "v1.0.0"}, "this is already the newest release, so a newer build has to be published first"},
		{"installed", Result{Outcome: OutcomeInstalled, Tag: "v2.0.0"}, "updated to v2.0.0"},
		{"disabled", Result{Outcome: OutcomeDisabled, Tag: "v2.0.0"}, "v2.0.0 is available; restart without --no-update to install it"},
		{"manual", Result{Outcome: OutcomeManual, Tag: "v2.0.0"}, "v2.0.0 is available, but this install cannot replace itself; reinstall to update"},
	}
	for _, tc := range cases {
		if got := tc.r.Advice(); got != tc.want {
			t.Errorf("%s: Advice = %q, want %q", tc.name, got, tc.want)
		}
	}
}
