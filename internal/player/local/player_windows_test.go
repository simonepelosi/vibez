//go:build windows

package local

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// fakeEngine stands in for the Chrome page. It keeps just enough element
// state to reply the way web/engine.js does, and records every command.
type fakeEngine struct {
	mu      sync.Mutex
	seq     uint64
	gen     uint64
	playing bool
	calls   []fakeCall
	fail    map[string]string // method → media error to reply with
	closes  int
}

type fakeCall struct {
	method string
	args   []any
}

func (e *fakeEngine) call(method string, args ...any) (snapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closes > 0 {
		return snapshot{}, errors.New("page closed")
	}
	e.calls = append(e.calls, fakeCall{method, args})
	var position float64
	switch method {
	case "load":
		e.gen = args[1].(uint64)
		e.playing = args[2].(bool)
	case "play":
		e.playing = true
	case "pause", "stop":
		e.playing = false
	case "seek":
		position = args[0].(float64)
	case "unload":
		e.gen, e.playing = 0, false
	}
	errMsg := e.fail[method]
	if errMsg != "" {
		e.playing = false
	}
	e.seq++
	return snapshot{Gen: e.gen, Seq: e.seq, Playing: e.playing, Position: position, Duration: 200, Error: errMsg}, nil
}

func (e *fakeEngine) close() {
	e.mu.Lock()
	e.closes++
	e.mu.Unlock()
}

// event builds a page event for generation gen, sequenced after everything
// the fake has replied so far.
func (e *fakeEngine) event(kind string, gen uint64, position float64) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	playing := kind != "ended" && kind != "pause" && kind != "error"
	raw, _ := json.Marshal(snapshot{Gen: gen, Seq: e.seq, Kind: kind, Playing: playing, Position: position, Duration: 200})
	return string(raw)
}

func (e *fakeEngine) loadedGen() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.gen
}

// loads lists the autoplay flag of each load so far.
func (e *fakeEngine) loads() []bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []bool
	for _, c := range e.calls {
		if c.method == "load" {
			out = append(out, c.args[2].(bool))
		}
	}
	return out
}

func (e *fakeEngine) lastCall(method string) (fakeCall, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.calls) - 1; i >= 0; i-- {
		if e.calls[i].method == method {
			return e.calls[i], true
		}
	}
	return fakeCall{}, false
}

func newTestPlayer(t *testing.T, titles ...string) (*Player, *fakeEngine, []provider.Track) {
	t.Helper()
	media, err := newMediaServer()
	if err != nil {
		t.Fatal(err)
	}
	p := newPlayer(media)
	eng := &fakeEngine{fail: map[string]string{}}
	p.eng = eng
	t.Cleanup(func() { _ = p.Close() })
	dir := t.TempDir()
	tracks := make([]provider.Track, len(titles))
	for i, title := range titles {
		tracks[i] = provider.Track{ID: "local:" + filepath.Join(dir, title+".mp3"), Title: title}
	}
	p.LoadTracks(tracks)
	return p, eng, tracks
}

func ids(tracks ...provider.Track) []string {
	out := make([]string, len(tracks))
	for i, t := range tracks {
		out[i] = t.ID
	}
	return out
}

func nowPlaying(t *testing.T, p *Player) string {
	t.Helper()
	s, _ := p.GetState()
	if s.Track == nil {
		return ""
	}
	return s.Track.Title
}

// waitFor polls cond, which the player satisfies from another goroutine.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLoadTracksStartsNothingUntilPlay(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A", "B")
	if n := len(eng.loads()); n != 0 {
		t.Fatalf("LoadTracks made %d loads, want none", n)
	}
	if s, _ := p.GetState(); s.Track != nil || s.Playing {
		t.Fatalf("after LoadTracks: track %v playing %v, want nothing", s.Track, s.Playing)
	}
	if err := p.Play(); err != nil {
		t.Fatal(err)
	}
	call, _ := eng.lastCall("load")
	want, _ := p.media.url(tracks[0].ID)
	if call.args[0] != want || call.args[2] != true {
		t.Fatalf("Play loaded %v, want autoplay of %s", call.args, want)
	}
	if s, _ := p.GetState(); nowPlaying(t, p) != "A" || !s.Playing || s.Track.Duration != 200*time.Second {
		t.Fatalf("state %+v, want A playing with its duration from the page", s)
	}
}

// The TUI's copy of the queue has to stay index-aligned with the player's,
// so a queue naming a track outside the library is refused whole.
func TestSetQueueRejectsTracksOutsideTheLibrary(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A", "B", "C")
	err := p.SetQueue([]string{tracks[2].ID, "local:C:/elsewhere/x.mp3"})
	if err == nil || !strings.Contains(err.Error(), "not in the local library") {
		t.Fatalf("SetQueue with an unknown ID: %v", err)
	}
	if len(eng.loads()) != 0 {
		t.Fatal("a refused SetQueue still loaded a track")
	}
	if err := p.SetQueue(ids(tracks[2], tracks[0])); err != nil {
		t.Fatal(err)
	}
	if err := p.Next(); err != nil || nowPlaying(t, p) != "A" {
		t.Fatalf("Next after SetQueue(C, A): %v, playing %q", err, nowPlaying(t, p))
	}
}

func TestAppendQueueStartsOnlyWhenNothingIsLoaded(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A", "B", "C")
	if err := p.SetQueue(ids(tracks[0])); err != nil {
		t.Fatal(err)
	}
	if err := p.AppendQueue(ids(tracks[1])); err != nil {
		t.Fatal(err)
	}
	if n := len(eng.loads()); n != 1 || nowPlaying(t, p) != "A" {
		t.Fatalf("append while playing: %d loads, playing %q; want A undisturbed", n, nowPlaying(t, p))
	}
	if err := p.ClearQueue(); err != nil {
		t.Fatal(err)
	}
	if nowPlaying(t, p) != "" {
		t.Fatal("ClearQueue left a track loaded")
	}
	if err := p.AppendQueue(ids(tracks[2])); err != nil {
		t.Fatal(err)
	}
	if nowPlaying(t, p) != "C" {
		t.Fatalf("append to an idle player: playing %q, want C", nowPlaying(t, p))
	}
}

func TestEndOfTrackAdvancesAndStaleEventsAreIgnored(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A", "B", "C")
	if err := p.SetQueue(ids(tracks...)); err != nil {
		t.Fatal(err)
	}
	genA := eng.loadedGen()
	p.onEvent(eng.event("ended", genA, 200))
	waitFor(t, "B to load", func() bool { return len(eng.loads()) == 2 })
	if nowPlaying(t, p) != "B" {
		t.Fatalf("after A ended: playing %q, want B", nowPlaying(t, p))
	}

	// A's element can still deliver events after B replaced it.
	p.onEvent(eng.event("ended", genA, 200))
	p.onEvent(eng.event("timeupdate", genA, 150))
	time.Sleep(50 * time.Millisecond)
	if n := len(eng.loads()); n != 2 {
		t.Fatalf("a stale end of A caused %d loads, want 2", n)
	}
	if s, _ := p.GetState(); s.Position != 0 || nowPlaying(t, p) != "B" {
		t.Fatalf("a stale snapshot of A changed the state: %q at %v", nowPlaying(t, p), s.Position)
	}
}

// Snapshots race each other through Playwright; an older one arriving late
// must not roll the state back.
func TestOlderSnapshotIsIgnored(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A")
	if err := p.SetQueue(ids(tracks...)); err != nil {
		t.Fatal(err)
	}
	gen := eng.loadedGen()
	older := eng.event("timeupdate", gen, 10)
	p.onEvent(eng.event("timeupdate", gen, 20))
	p.onEvent(older)
	if s, _ := p.GetState(); s.Position != 20*time.Second {
		t.Fatalf("position %v, want the newer 20s", s.Position)
	}
}

func TestEndOfQueueStopsAndAppendCarriesOn(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A", "B")
	if err := p.SetQueue(ids(tracks[0])); err != nil {
		t.Fatal(err)
	}
	p.onEvent(eng.event("ended", eng.loadedGen(), 200))
	waitFor(t, "the queue to run out", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.ended
	})
	s, _ := p.GetState()
	if len(eng.loads()) != 1 || s.Playing || nowPlaying(t, p) != "A" {
		t.Fatalf("end of queue: %d loads, playing=%v on %q; want A stopped", len(eng.loads()), s.Playing, nowPlaying(t, p))
	}
	if err := p.Next(); err != nil || len(eng.loads()) != 1 {
		t.Fatalf("Next at the end of the queue: %v, %d loads; want a no-op", err, len(eng.loads()))
	}
	if err := p.AppendQueue(ids(tracks[1])); err != nil {
		t.Fatal(err)
	}
	if nowPlaying(t, p) != "B" {
		t.Fatalf("append after the queue ended: playing %q, want B", nowPlaying(t, p))
	}
}

func TestRepeatOneReplaysOnEndButSkipMovesOn(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A", "B")
	if err := p.SetRepeat(player.RepeatModeOne); err != nil {
		t.Fatal(err)
	}
	if err := p.SetQueue(ids(tracks...)); err != nil {
		t.Fatal(err)
	}
	genA := eng.loadedGen()
	p.onEvent(eng.event("ended", genA, 200))
	waitFor(t, "A to reload", func() bool { return len(eng.loads()) == 2 })
	if nowPlaying(t, p) != "A" || eng.loadedGen() == genA {
		t.Fatalf("repeat-one: playing %q gen %d; want A under a new generation", nowPlaying(t, p), eng.loadedGen())
	}
	if err := p.Next(); err != nil || nowPlaying(t, p) != "B" {
		t.Fatalf("Next under repeat-one: %v, playing %q; want B", err, nowPlaying(t, p))
	}
	if err := p.SetRepeat(7); err == nil {
		t.Fatal("SetRepeat accepted an unknown mode")
	}
}

func TestRemoveFromQueue(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A", "B", "C")
	if err := p.SetQueue(ids(tracks...)); err != nil {
		t.Fatal(err)
	}
	if err := p.RemoveFromQueue(2); err != nil || len(eng.loads()) != 1 {
		t.Fatalf("removing a queued track: %v, %d loads; want playback undisturbed", err, len(eng.loads()))
	}
	// [A B], A playing: B takes over and keeps playing.
	if err := p.RemoveFromQueue(0); err != nil {
		t.Fatal(err)
	}
	if loads := eng.loads(); nowPlaying(t, p) != "B" || !loads[len(loads)-1] {
		t.Fatalf("removing the playing track: playing %q, autoplay %v; want B playing", nowPlaying(t, p), loads)
	}
	// [B], B paused and removed: nothing is left to take over.
	if err := p.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := p.RemoveFromQueue(0); err != nil {
		t.Fatal(err)
	}
	if _, ok := eng.lastCall("unload"); !ok || nowPlaying(t, p) != "" {
		t.Fatalf("removing the last track: playing %q, unloaded %v", nowPlaying(t, p), ok)
	}
	if err := p.RemoveFromQueue(0); err == nil {
		t.Fatal("RemoveFromQueue on an empty queue succeeded")
	}
}

// The TUI inserts "play next" tracks by appending and then moving them to just
// after the current one; the next track must then be the inserted one.
func TestMoveInQueueFollowsTheTUIsPlayNext(t *testing.T) {
	p, _, tracks := newTestPlayer(t, "A", "B", "C", "D")
	if err := p.SetQueue(ids(tracks[:3]...)); err != nil {
		t.Fatal(err)
	}
	if err := p.AppendQueue(ids(tracks[3])); err != nil {
		t.Fatal(err)
	}
	if err := p.MoveInQueue(3, 1); err != nil {
		t.Fatal(err)
	}
	if err := p.Next(); err != nil || nowPlaying(t, p) != "D" {
		t.Fatalf("Next after play-next of D: %v, playing %q", err, nowPlaying(t, p))
	}
	if err := p.MoveInQueue(1, 4); err == nil {
		t.Fatal("MoveInQueue accepted an out-of-range index")
	}
}

func TestLoadFailureIsReportedNotPlayed(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A")
	eng.fail["load"] = "unsupported format (DEMUXER_ERROR_NO_SUPPORTED_STREAMS)"
	err := p.SetQueue(ids(tracks...))
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("SetQueue of an undecodable file: %v", err)
	}
	if s, _ := p.GetState(); s.Playing || s.Loading || nowPlaying(t, p) != "A" {
		t.Fatalf("state after a failed load: %+v; want A, not playing", s)
	}
}

func TestPreviousRestartsATrackPastTheThreshold(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A", "B")
	if err := p.SetQueue(ids(tracks...)); err != nil {
		t.Fatal(err)
	}
	if err := p.Next(); err != nil {
		t.Fatal(err)
	}
	p.onEvent(eng.event("timeupdate", eng.loadedGen(), 30))
	if err := p.Previous(); err != nil {
		t.Fatal(err)
	}
	if call, _ := eng.lastCall("seek"); nowPlaying(t, p) != "B" || call.args[0] != 0.0 {
		t.Fatalf("Previous 30s in: playing %q, seek %v; want B rewound", nowPlaying(t, p), call.args)
	}
	if err := p.Previous(); err != nil || nowPlaying(t, p) != "A" {
		t.Fatalf("Previous at the start of B: %v, playing %q; want A", err, nowPlaying(t, p))
	}
}

func TestSetEqualizer(t *testing.T) {
	p, eng, _ := newTestPlayer(t, "A")
	for _, bad := range []player.EQBand{
		{Frequency: 1000, Q: 1, Gain: 12.5},
		{Frequency: 0, Q: 1, Gain: 0},
		{Frequency: 1000, Q: -1, Gain: 0},
	} {
		if err := p.SetEqualizer([]player.EQBand{bad}); err == nil {
			t.Errorf("SetEqualizer accepted %+v", bad)
		}
	}

	// The TUI keeps editing the slice it passed; what reaches the page is the
	// value at the time of the call.
	bands := player.DefaultEQBands()
	bands[0].Gain = 6
	if err := p.SetEqualizer(bands); err != nil {
		t.Fatal(err)
	}
	bands[0].Gain = -6
	var sent []any
	waitFor(t, "the EQ to reach the page", func() bool {
		call, ok := eng.lastCall("setEqualizer")
		if ok {
			sent = call.args[0].([]any)
		}
		return ok
	})
	if len(sent) != 10 || sent[0].(map[string]any)["gain"] != 6.0 {
		t.Fatalf("page got %v, want 10 bands with the first at +6 dB", sent)
	}

	if err := p.SetEqualizer(nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the EQ bypass", func() bool {
		call, _ := eng.lastCall("setEqualizer")
		return len(call.args[0].([]any)) == 0
	})
}

func TestSetAudioBitrateIsAPreferenceOnly(t *testing.T) {
	p, _, _ := newTestPlayer(t)
	if err := p.SetAudioBitrate(256); !errors.Is(err, player.ErrAudioBitrateSavedPreferenceOnly) {
		t.Fatalf("SetAudioBitrate(256) = %v", err)
	}
	if err := p.SetAudioBitrate(12345); err == nil || errors.Is(err, player.ErrAudioBitrateSavedPreferenceOnly) {
		t.Fatalf("SetAudioBitrate(12345) = %v, want a validation error", err)
	}
}

func TestPlayOnAnEmptyQueueFails(t *testing.T) {
	p, _, _ := newTestPlayer(t)
	if err := p.Play(); !errors.Is(err, errEmptyQueue) {
		t.Fatalf("Play = %v, want errEmptyQueue", err)
	}
	if err := p.Seek(time.Second); !errors.Is(err, errNothingLoaded) {
		t.Fatalf("Seek = %v, want errNothingLoaded", err)
	}
}

func TestCloseReleasesEverythingOnce(t *testing.T) {
	p, eng, tracks := newTestPlayer(t, "A")
	sub := p.Subscribe()
	if err := p.SetQueue(ids(tracks...)); err != nil {
		t.Fatal(err)
	}
	pageURL := p.media.pageURL()
	for range 2 {
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if eng.closes != 1 {
		t.Fatalf("page closed %d times, want once", eng.closes)
	}
	if resp, err := http.Get(pageURL); err == nil { //nolint:gosec // G107: loopback test server URL
		_ = resp.Body.Close()
		t.Fatal("media server still answering after Close")
	}
	deadline := time.After(2 * time.Second)
	for open := true; open; {
		select {
		case _, open = <-sub:
		case <-deadline:
			t.Fatal("subscription still open after Close")
		}
	}
	if err := p.Play(); !errors.Is(err, errClosed) {
		t.Fatalf("Play after Close = %v, want errClosed", err)
	}
}
