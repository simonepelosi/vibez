package local_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simone-vibes/vibez/internal/provider/local"
)

// noticeDir creates a temp directory holding the named empty files and returns
// a Provider scanning it, plus the directory.
func noticeDir(t *testing.T, names ...string) (*local.Provider, string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte{}, 0o600); err != nil {
			t.Fatalf("creating %s: %v", n, err)
		}
	}
	p, err := local.New(dir)
	if err != nil {
		t.Fatalf("local.New: %v", err)
	}
	return p, dir
}

func TestScanNotice_SilentWhenEverythingIndexed(t *testing.T) {
	p, _ := noticeDir(t, "a.mp3", "b.flac", "c.m4a")
	if got := p.ScanNotice(); got != "" {
		t.Errorf("ScanNotice() = %q, want empty when nothing was left out", got)
	}
}

// A music directory can hold a long tail of odd extensions. The list names the
// three commonest and counts the rest, so the line stays inside a status bar.
func TestScanNotice_CapsTheExtensionList(t *testing.T) {
	p, _ := noticeDir(t, "a.aa", "b.bb", "c.cc", "d.dd", "e.ee")
	got := p.ScanNotice()
	if !strings.Contains(got, "skipped 5 files (1 .aa, 1 .bb, 1 .cc, +2 more)") {
		t.Errorf("ScanNotice() = %q, want the list capped at three extensions", got)
	}
}

// The path is the part of the line the user already knows, so it gives up its
// room to the part they do not.
func TestScanNotice_ElidesTheHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, "Music")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sleeve.pdf"), []byte{}, 0o600); err != nil {
		t.Fatalf("creating file: %v", err)
	}

	p, err := local.New(dir)
	if err != nil {
		t.Fatalf("local.New: %v", err)
	}
	if got := p.ScanNotice(); !strings.HasPrefix(got, "no playable tracks in "+filepath.Join("~", "Music")+": ") {
		t.Errorf("ScanNotice() = %q, want the home directory elided to ~", got)
	}
}
