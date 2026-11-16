//go:build linux

package local

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

type fakeGST struct {
	mu      sync.Mutex
	uris    []string
	stopped int
	pos     time.Duration
}

func (f *fakeGST) PlayURI(u string) {
	f.mu.Lock()
	f.uris = append(f.uris, u)
	f.mu.Unlock()
}
func (f *fakeGST) Play()                   {}
func (f *fakeGST) Pause()                  {}
func (f *fakeGST) Seek(time.Duration)      {}
func (f *fakeGST) SetVolume(float64)       {}
func (f *fakeGST) SetEqualizer([]float64)  {}
func (f *fakeGST) Destroy()                {}
func (f *fakeGST) Position() time.Duration { f.mu.Lock(); defer f.mu.Unlock(); return f.pos }
func (f *fakeGST) Stop()                   { f.mu.Lock(); f.stopped++; f.mu.Unlock() }

func TestLinuxRemoveLastTrackResetsIndexAndNextDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.flac")
	f2 := filepath.Join(dir, "b.flac")
	for _, f := range []string{f1, f2} {
		if err := os.WriteFile(f, []byte("fLaC"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	fg := &fakeGST{}
	p := newWithEngine(fg)
	defer func() { _ = p.Close() }()

	tracks := []provider.Track{
		{ID: "local:" + f1, Title: "A"},
		{ID: "local:" + f2, Title: "B"},
	}
	p.LoadTracks(tracks)
	if err := p.SetRepeat(player.RepeatModeOne); err != nil {
		t.Fatal(err)
	}
	// Advance to last track (index 1) via SetPlaylist, then remove it.
	if err := p.SetPlaylist("", 1); err != nil {
		t.Fatal(err)
	}
	if err := p.RemoveFromQueue(1); err != nil {
		t.Fatal(err)
	}
	// RepeatModeOne Next() used to panic with index out of range [1] with length 1.
	if err := p.Next(); err != nil {
		t.Fatal(err)
	}
	st, _ := p.GetState()
	if st.Track == nil || st.Track.ID != tracks[0].ID {
		t.Fatalf("expected wrap/reset to first track, got %+v", st.Track)
	}
}

func TestLinuxAppendQueueConcurrentWithLoadTracks(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.flac")
	if err := os.WriteFile(f1, []byte("fLaC"), 0o600); err != nil {
		t.Fatal(err)
	}

	fg := &fakeGST{}
	p := newWithEngine(fg)
	defer func() { _ = p.Close() }()

	t1 := provider.Track{ID: "local:" + f1, Title: "A"}
	p.LoadTracks([]provider.Track{t1})

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 25 {
				_ = p.AppendQueue([]string{t1.ID})
				p.LoadTracks([]provider.Track{t1})
			}
		})
	}
	wg.Wait()
}

func TestLinuxPlayTrackRejectsNonRegularPaths(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "valid.wav")
	if err := os.WriteFile(reg, []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}

	fg := &fakeGST{}
	p := newWithEngine(fg)
	defer func() { _ = p.Close() }()

	bad := []provider.Track{
		{ID: "local:" + dir, Title: "dir"},
		{ID: "local:relative/path.wav", Title: "rel"},
		{ID: "local:" + reg + "\x00bad", Title: "nul"},
		{ID: "other:" + reg, Title: "scheme"},
	}
	p.LoadTracks(bad)
	for i := range bad {
		if err := p.SetPlaylist("", i); err != nil {
			t.Fatal(err)
		}
		st, _ := p.GetState()
		if st.Error == "" {
			t.Fatalf("expected error for bad track %+v", bad[i])
		}
	}

	fg.mu.Lock()
	gotBad := len(fg.uris)
	fg.mu.Unlock()
	if gotBad != 0 {
		t.Fatalf("expected 0 PlayURI calls for bad paths, got %d", gotBad)
	}

	p.LoadTracks([]provider.Track{{ID: "local:" + reg, Title: "ok"}})
	if err := p.SetPlaylist("", 0); err != nil {
		t.Fatal(err)
	}
	fg.mu.Lock()
	gotGood := len(fg.uris)
	fg.mu.Unlock()
	if gotGood != 1 {
		t.Fatalf("expected 1 PlayURI call for regular file, got %d", gotGood)
	}
}

func TestLinuxCloseClosesSubscriptions(t *testing.T) {
	p := newWithEngine(&fakeGST{})
	ch := p.Subscribe()
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected subscription channel to close on Player.Close")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed subscription channel")
	}
}
