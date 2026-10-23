package smtc

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// ── fakes ─────────────────────────────────────────────────────────────────

// fakeController records every call on a channel so tests can wait for it.
type fakeController struct {
	calls chan string
	err   error
	block chan struct{} // when non-nil, every call waits for it to close
}

func newFakeController() *fakeController {
	return &fakeController{calls: make(chan string, 64)}
}

func (c *fakeController) record(call string) error {
	if c.block != nil {
		<-c.block
	}
	c.calls <- call
	return c.err
}

func (c *fakeController) Play() error                { return c.record("play") }
func (c *fakeController) Pause() error               { return c.record("pause") }
func (c *fakeController) Next() error                { return c.record("next") }
func (c *fakeController) Previous() error            { return c.record("previous") }
func (c *fakeController) Seek(d time.Duration) error { return c.record("seek " + d.String()) }

// waitCall returns the next controller call, failing the test after timeout.
func (c *fakeController) waitCall(t *testing.T, timeout time.Duration) string {
	t.Helper()
	select {
	case call := <-c.calls:
		return call
	case <-time.After(timeout):
		t.Fatalf("no controller call within %v", timeout)
		return ""
	}
}

func (c *fakeController) assertNoCall(t *testing.T, within time.Duration) {
	t.Helper()
	select {
	case call := <-c.calls:
		t.Fatalf("unexpected controller call %q", call)
	case <-time.After(within):
	}
}

// fakeBridge records updates and lets a test raise SMTC events.
type fakeBridge struct {
	mu      sync.Mutex
	updates []update
	closed  bool
	onEvent func(event)
	onError func(string)
}

func (b *fakeBridge) apply(u update) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		panic("apply after close")
	}
	b.updates = append(b.updates, u)
}

func (b *fakeBridge) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}

// take returns and forgets the updates applied so far.
func (b *fakeBridge) take() []update {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.updates
	b.updates = nil
	return u
}

// fakeClock is a manually advanced clock.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

type harness struct {
	srv    *Server
	bridge *fakeBridge
	ctrl   *fakeController
	clock  *fakeClock
	logs   *logSink
}

type logSink struct {
	mu   sync.Mutex
	msgs []string
}

func (l *logSink) log(msg string) {
	l.mu.Lock()
	l.msgs = append(l.msgs, msg)
	l.mu.Unlock()
}

func (l *logSink) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.msgs, "\n")
}

// waitFor waits until a logged message contains each of subs. Logging is
// asynchronous, so a test cannot check right after the call that logs.
func (l *logSink) waitFor(t *testing.T, subs ...string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		logs := l.all()
		missing := ""
		for _, sub := range subs {
			if !strings.Contains(logs, sub) {
				missing = sub
				break
			}
		}
		if missing == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no log containing %q; logs:\n%s", missing, logs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		bridge: &fakeBridge{},
		ctrl:   newFakeController(),
		clock:  &fakeClock{t: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)},
		logs:   &logSink{},
	}
	factory := func(onEvent func(event), onError func(string)) (bridge, error) {
		h.bridge.onEvent = onEvent
		h.bridge.onError = onError
		return h.bridge, nil
	}
	srv, err := newServer(h.ctrl, h.logs.log, factory, h.clock.now)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	h.srv = srv
	t.Cleanup(func() { _ = srv.Close() })
	return h
}

var testTrack = &provider.Track{
	ID:         "1",
	Title:      "Song",
	Artist:     "Artist",
	Album:      "Album",
	Duration:   3 * time.Minute,
	ArtworkURL: "https://example.com/300x300.jpg",
}

func playing(pos time.Duration) player.State {
	return player.State{Track: testTrack, Playing: true, Position: pos}
}

// ── state → update mapping ─────────────────────────────────────────────────

func TestStatusOf(t *testing.T) {
	cases := []struct {
		name string
		st   player.State
		want playbackStatus
	}{
		{"no track", player.State{}, statusClosed},
		{"no track but playing flag", player.State{Playing: true}, statusClosed},
		{"playing", player.State{Track: testTrack, Playing: true}, statusPlaying},
		{"loading", player.State{Track: testTrack, Loading: true}, statusChanging},
		{"paused", player.State{Track: testTrack}, statusPaused},
	}
	for _, tc := range cases {
		if got := statusOf(tc.st); got != tc.want {
			t.Errorf("%s: statusOf = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFirstTrackSendsEverything(t *testing.T) {
	var tr tracker
	u, ok := tr.next(playing(10*time.Second), time.Now())
	if !ok {
		t.Fatal("first track produced no update")
	}
	want := metadata{Title: "Song", Artist: "Artist", Album: "Album", ArtworkURL: "https://example.com/300x300.jpg"}
	if u.Metadata == nil || *u.Metadata != want {
		t.Errorf("Metadata = %+v, want %+v", u.Metadata, want)
	}
	if u.Status == nil || *u.Status != statusPlaying {
		t.Errorf("Status = %v, want Playing", u.Status)
	}
	if u.Timeline == nil || *u.Timeline != (timeline{Position: 10 * time.Second, Duration: 3 * time.Minute}) {
		t.Errorf("Timeline = %+v, want 10s of 3m", u.Timeline)
	}
}

func TestNoTrackAtStartupSendsNothing(t *testing.T) {
	var tr tracker
	if u, ok := tr.next(player.State{Volume: 1}, time.Now()); ok {
		t.Errorf("idle player produced update %+v", u)
	}
}

func TestTrackRemovedClosesSession(t *testing.T) {
	var tr tracker
	now := time.Now()
	tr.next(playing(0), now)
	u, ok := tr.next(player.State{}, now.Add(time.Second))
	if !ok || u.Status == nil || *u.Status != statusClosed {
		t.Fatalf("update = %+v, %v; want status Closed", u, ok)
	}
	if u.Metadata != nil || u.Timeline != nil {
		t.Errorf("Closed update carries metadata or timeline: %+v", u)
	}
	// The same track again must be re-sent in full: Closed cleared the display.
	u, ok = tr.next(playing(0), now.Add(2*time.Second))
	if !ok || u.Metadata == nil || u.Status == nil || u.Timeline == nil {
		t.Errorf("track after Closed = %+v, want metadata, status and timeline", u)
	}
}

func TestPositionClampedToTrack(t *testing.T) {
	var tr tracker
	u, _ := tr.next(playing(5*time.Minute), time.Now())
	if u.Timeline == nil || u.Timeline.Position != 3*time.Minute {
		t.Errorf("Timeline = %+v, want position clamped to 3m", u.Timeline)
	}
	var tr2 tracker
	u, _ = tr2.next(playing(-time.Second), time.Now())
	if u.Timeline == nil || u.Timeline.Position != 0 {
		t.Errorf("Timeline = %+v, want negative position clamped to 0", u.Timeline)
	}
}

// ── throttling ────────────────────────────────────────────────────────────

func TestPositionTicksAreThrottled(t *testing.T) {
	var tr tracker
	now := time.Now()
	tr.next(playing(0), now)

	sent := 0
	var sentAt []time.Duration
	// Twenty seconds of 250 ms ticks with the position advancing in step.
	for i := 1; i <= 80; i++ {
		elapsed := time.Duration(i) * 250 * time.Millisecond
		u, ok := tr.next(playing(elapsed), now.Add(elapsed))
		if !ok {
			continue
		}
		if u.Metadata != nil || u.Status != nil {
			t.Fatalf("tick %d re-sent metadata or status: %+v", i, u)
		}
		if u.Timeline == nil {
			t.Fatalf("tick %d sent an empty update", i)
		}
		sent++
		sentAt = append(sentAt, elapsed)
	}
	if sent != 4 {
		t.Errorf("sent %d timeline refreshes in 20 s (at %v), want 4 (every %v)", sent, sentAt, timelineRefresh)
	}
}

func TestPausedPositionSendsNothing(t *testing.T) {
	var tr tracker
	now := time.Now()
	st := player.State{Track: testTrack, Position: 30 * time.Second}
	tr.next(st, now)
	for i := 1; i <= 100; i++ {
		if u, ok := tr.next(st, now.Add(time.Duration(i)*time.Second)); ok {
			t.Fatalf("paused tick %d produced %+v", i, u)
		}
	}
}

func TestSeekSendsTimelineAtOnce(t *testing.T) {
	var tr tracker
	now := time.Now()
	tr.next(playing(10*time.Second), now)
	u, ok := tr.next(playing(70*time.Second), now.Add(250*time.Millisecond))
	if !ok || u.Timeline == nil || u.Timeline.Position != 70*time.Second {
		t.Fatalf("forward seek: update = %+v, %v; want timeline at 70s", u, ok)
	}
	u, ok = tr.next(playing(5*time.Second), now.Add(500*time.Millisecond))
	if !ok || u.Timeline == nil || u.Timeline.Position != 5*time.Second {
		t.Fatalf("backward seek: update = %+v, %v; want timeline at 5s", u, ok)
	}
}

func TestSeekWhilePausedSendsTimeline(t *testing.T) {
	var tr tracker
	now := time.Now()
	tr.next(player.State{Track: testTrack, Position: 10 * time.Second}, now)
	u, ok := tr.next(player.State{Track: testTrack, Position: 100 * time.Second}, now.Add(time.Minute))
	if !ok || u.Timeline == nil || u.Timeline.Position != 100*time.Second {
		t.Fatalf("update = %+v, %v; want timeline at 100s", u, ok)
	}
}

func TestPauseSendsStatusAndTimeline(t *testing.T) {
	var tr tracker
	now := time.Now()
	tr.next(playing(10*time.Second), now)
	u, ok := tr.next(player.State{Track: testTrack, Position: 11 * time.Second}, now.Add(time.Second))
	if !ok || u.Status == nil || *u.Status != statusPaused {
		t.Fatalf("update = %+v, %v; want status Paused", u, ok)
	}
	if u.Metadata != nil {
		t.Errorf("pause re-sent metadata")
	}
	if u.Timeline == nil || u.Timeline.Position != 11*time.Second {
		t.Errorf("Timeline = %+v, want the paused position so readers stop extrapolating", u.Timeline)
	}
}

func TestTrackChangeSendsMetadata(t *testing.T) {
	var tr tracker
	now := time.Now()
	tr.next(playing(170*time.Second), now)

	next := &provider.Track{ID: "2", Title: "Other", Artist: "Artist", Album: "Album", Duration: 4 * time.Minute}
	// Loading the next track: new metadata, status Changing.
	u, ok := tr.next(player.State{Track: next, Loading: true}, now.Add(time.Second))
	if !ok || u.Metadata == nil || u.Metadata.Title != "Other" || u.Metadata.ArtworkURL != "" {
		t.Fatalf("update = %+v, %v; want metadata for Other with no artwork", u, ok)
	}
	if u.Status == nil || *u.Status != statusChanging {
		t.Errorf("Status = %v, want Changing", u.Status)
	}
	if u.Timeline == nil || *u.Timeline != (timeline{Duration: 4 * time.Minute}) {
		t.Errorf("Timeline = %+v, want 0 of 4m", u.Timeline)
	}
	// Loaded: only the status changes.
	u, ok = tr.next(player.State{Track: next, Playing: true}, now.Add(2*time.Second))
	if !ok || u.Metadata != nil || u.Status == nil || *u.Status != statusPlaying {
		t.Errorf("update = %+v, %v; want status Playing only", u, ok)
	}
}

func TestBackfilledMetadataIsSent(t *testing.T) {
	var tr tracker
	now := time.Now()
	placeholder := &provider.Track{ID: "i.abc"}
	tr.next(player.State{Track: placeholder, Loading: true}, now)

	filled := &provider.Track{ID: "i.abc", Title: "Real", Artist: "A", Album: "B", Duration: time.Minute}
	u, ok := tr.next(player.State{Track: filled, Loading: true}, now.Add(time.Second))
	if !ok || u.Metadata == nil || u.Metadata.Title != "Real" {
		t.Fatalf("update = %+v, %v; want the backfilled title", u, ok)
	}
	if u.Timeline == nil || u.Timeline.Duration != time.Minute {
		t.Errorf("Timeline = %+v, want the backfilled duration", u.Timeline)
	}
}

func TestDurationChangeSendsTimeline(t *testing.T) {
	var tr tracker
	now := time.Now()
	noDur := &provider.Track{ID: "1", Title: "Song"}
	tr.next(player.State{Track: noDur, Playing: true}, now)
	withDur := &provider.Track{ID: "1", Title: "Song", Duration: 2 * time.Minute}
	u, ok := tr.next(player.State{Track: withDur, Playing: true, Position: 250 * time.Millisecond}, now.Add(250*time.Millisecond))
	if !ok || u.Timeline == nil || u.Timeline.Duration != 2*time.Minute {
		t.Fatalf("update = %+v, %v; want the new duration", u, ok)
	}
	if u.Metadata != nil || u.Status != nil {
		t.Errorf("duration change re-sent metadata or status: %+v", u)
	}
}

func TestRepeatedTrackRestartIsSeek(t *testing.T) {
	// Repeat-one replays the same track: metadata is unchanged, and only the
	// position jumping back to 0 tells readers.
	var tr tracker
	now := time.Now()
	tr.next(playing(179*time.Second), now)
	u, ok := tr.next(playing(0), now.Add(time.Second))
	if !ok || u.Timeline == nil || u.Timeline.Position != 0 {
		t.Fatalf("update = %+v, %v; want timeline back at 0", u, ok)
	}
}

// ── Server ────────────────────────────────────────────────────────────────

func TestServerAppliesOnlyChanges(t *testing.T) {
	h := newHarness(t)

	h.srv.Update(player.State{})
	if got := h.bridge.take(); len(got) != 0 {
		t.Fatalf("idle state applied %+v", got)
	}

	h.srv.Update(playing(0))
	got := h.bridge.take()
	if len(got) != 1 || got[0].Metadata == nil || got[0].Status == nil || got[0].Timeline == nil {
		t.Fatalf("first track applied %+v, want one full update", got)
	}

	for i := 1; i <= 8; i++ { // two seconds of ticks
		h.clock.advance(250 * time.Millisecond)
		h.srv.Update(playing(time.Duration(i) * 250 * time.Millisecond))
	}
	if got := h.bridge.take(); len(got) != 0 {
		t.Fatalf("position ticks applied %d updates, want none before the refresh interval", len(got))
	}

	h.clock.advance(250 * time.Millisecond)
	h.srv.Update(player.State{Track: testTrack, Position: 2250 * time.Millisecond})
	got = h.bridge.take()
	if len(got) != 1 || got[0].Status == nil || *got[0].Status != statusPaused {
		t.Fatalf("pause applied %+v, want status Paused", got)
	}
}

func TestServerRoutesEventsToController(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		ev   event
		want string
	}{
		{event{kind: eventPlay}, "play"},
		{event{kind: eventPause}, "pause"},
		{event{kind: eventStop}, "pause"},
		{event{kind: eventNext}, "next"},
		{event{kind: eventPrevious}, "previous"},
		{event{kind: eventSeek, position: 95 * time.Second}, "seek 1m35s"},
	}
	for _, tc := range cases {
		h.bridge.onEvent(tc.ev)
		if got := h.ctrl.waitCall(t, 2*time.Second); got != tc.want {
			t.Errorf("%v routed to %q, want %q", tc.ev.kind, got, tc.want)
		}
	}
}

func TestServerEventsKeepOrder(t *testing.T) {
	h := newHarness(t)
	want := []string{"next", "next", "previous", "pause", "play"}
	for _, k := range []eventKind{eventNext, eventNext, eventPrevious, eventPause, eventPlay} {
		h.bridge.onEvent(event{kind: k})
	}
	for i, w := range want {
		if got := h.ctrl.waitCall(t, 2*time.Second); got != w {
			t.Fatalf("call %d = %q, want %q", i, got, w)
		}
	}
}

func TestServerEventNeverBlocksWindows(t *testing.T) {
	h := newHarness(t)
	h.ctrl.block = make(chan struct{})

	done := make(chan struct{})
	go func() {
		// Far more presses than the queue holds, with the controller stuck.
		for range eventQueueSize * 4 {
			h.bridge.onEvent(event{kind: eventNext})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onEvent blocked while the controller was busy")
	}
	close(h.ctrl.block)
	h.logs.waitFor(t, "[smtc] dropped next: controller is busy")
}

func TestServerLogsControllerAndNativeErrors(t *testing.T) {
	h := newHarness(t)
	h.ctrl.err = errors.New("no queue")
	h.bridge.onEvent(event{kind: eventNext})
	h.ctrl.waitCall(t, 2*time.Second)
	h.bridge.onError("smtc: put_Title failed: HRESULT 0x80004005")
	h.logs.waitFor(t, "[smtc] next: no queue", "[smtc] smtc: put_Title failed: HRESULT 0x80004005")
}

func TestServerSlowLoggerNeverBlocksWindows(t *testing.T) {
	// The TUI's Send blocks while the TUI is busy. Native callbacks log, and
	// they must return at once regardless.
	release := make(chan struct{})
	var logged sync.WaitGroup
	logged.Add(1)
	var once sync.Once
	logf := func(string) {
		once.Do(logged.Done)
		<-release
	}
	b := &fakeBridge{}
	factory := func(onEvent func(event), onError func(string)) (bridge, error) {
		b.onEvent, b.onError = onEvent, onError
		return b, nil
	}
	srv, err := newServer(newFakeController(), logf, factory, time.Now)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		for range logQueueSize * 4 {
			b.onError("smtc: Update failed: HRESULT 0x80004005")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("onError blocked on a slow logger")
	}
	logged.Wait()
	close(release)
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServerClose(t *testing.T) {
	h := newHarness(t)
	h.srv.Update(playing(0))
	if err := h.srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !h.bridge.closed {
		t.Error("Close did not close the bridge")
	}
	// After Close: no update reaches the bridge (fakeBridge panics if one
	// does), events are ignored, and Close is idempotent.
	h.srv.Update(playing(time.Minute))
	h.bridge.onEvent(event{kind: eventNext})
	h.ctrl.assertNoCall(t, 100*time.Millisecond)
	if err := h.srv.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestServerConcurrentUse(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range 200 {
				h.srv.Update(playing(time.Duration(g*1000+i) * time.Millisecond))
				h.bridge.onEvent(event{kind: eventPlay})
			}
		})
	}
	drained := make(chan struct{})
	go func() {
		for range h.ctrl.calls {
		}
		close(drained)
	}()
	wg.Wait()
	if err := h.srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close waited for the worker, so nothing sends on calls any more.
	close(h.ctrl.calls)
	<-drained
}

func TestNewServerReportsBridgeError(t *testing.T) {
	want := errors.New("boom")
	factory := func(func(event), func(string)) (bridge, error) { return nil, want }
	if _, err := newServer(newFakeController(), nil, factory, time.Now); !errors.Is(err, want) {
		t.Errorf("newServer error = %v, want %v", err, want)
	}
}
