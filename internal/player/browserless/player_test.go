//go:build linux || darwin || windows

package browserless

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player/browserless/license"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/provider/apple"
)

func TestPlayerQueueIndexAdjustments(t *testing.T) {
	tracks := []provider.Track{
		{ID: "0"},
		{ID: "1"},
		{ID: "2"},
		{ID: "3"},
	}

	p := &Player{
		queue: tracks,
		idx:   2, // playing track "2"
	}

	// Move playing item (index 2) to 0
	_ = p.MoveInQueue(2, 0)
	if p.idx != 0 {
		t.Errorf("expected idx 0 after moving playing item to 0, got %d", p.idx)
	}
	if p.queue[p.idx].ID != "2" {
		t.Errorf("expected playing track to remain ID '2', got ID %s", p.queue[p.idx].ID)
	}

	// Queue is now: [2, 0, 1, 3], idx is 0 ("2")
	// Move item from 3 ("3") to 0. It shifts playing item rightwards!
	_ = p.MoveInQueue(3, 0)
	// Queue is now: [3, 2, 0, 1], idx should have incremented to 1
	if p.idx != 1 {
		t.Errorf("expected idx 1 after inserting before playing item, got %d", p.idx)
	}
	if p.queue[p.idx].ID != "2" {
		t.Errorf("expected playing track to remain ID '2', got ID %s", p.queue[p.idx].ID)
	}

	// Move item from 0 ("3") to 3 (after playing item). It shifts playing item leftwards!
	_ = p.MoveInQueue(0, 3)
	// Queue is now: [2, 0, 3, 1] or similar, idx should have decremented to 0
	if p.idx != 0 {
		t.Errorf("expected idx 0 after moving item past playing item, got %d", p.idx)
	}
	if p.queue[p.idx].ID != "2" {
		t.Errorf("expected playing track to remain ID '2', got ID %s", p.queue[p.idx].ID)
	}

	// Remove item before playing index
	p.idx = 2 // say playing item at index 2
	_ = p.RemoveFromQueue(0)
	// Item before index removed, playing item shifts left: idx becomes 1
	if p.idx != 1 {
		t.Errorf("expected idx 1 after removing prior item, got %d", p.idx)
	}

	// Remove current playing item
	_ = p.RemoveFromQueue(1)
	if p.state.Track != nil {
		t.Errorf("expected Track to be cleared after removing playing item, got %+v", p.state.Track)
	}
}

func TestStreamServerSecurity(t *testing.T) {
	dummyHex := "0123456789abcdef0123456789abcdef"
	s := &StreamServer{
		authToken: dummyHex,
		streams:   make(map[string]*trackStream),
	}

	// Non-loopback RemoteAddr should be forbidden
	req := httptest.NewRequest(http.MethodGet, "/stream/fake/"+dummyHex+".aac", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rr := httptest.NewRecorder()
	s.handleStream(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for non-loopback remote addr, got %d", rr.Code)
	}

	// Foreign Host header should be forbidden
	req = httptest.NewRequest(http.MethodGet, "/stream/fake/"+dummyHex+".aac", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "evil-rebinding-domain.com"
	rr = httptest.NewRecorder()
	s.handleStream(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for invalid host header, got %d", rr.Code)
	}

	// Valid host + loopback, but invalid auth token
	req = httptest.NewRequest(http.MethodGet, "/stream/fake/wrong-hex.aac", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "127.0.0.1:8080"
	rr = httptest.NewRecorder()
	s.handleStream(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for wrong token, got %d", rr.Code)
	}

	// Valid host, loopback, and auth token, but stream does not exist
	req = httptest.NewRequest(http.MethodGet, "/stream/fake/"+dummyHex+".aac", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "127.0.0.1:8080"
	rr = httptest.NewRecorder()
	s.handleStream(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for non-existent stream, got %d", rr.Code)
	}
}

type noopSink struct{}

func (noopSink) PlayURI(string)             {}
func (noopSink) SetNextURI(string)          {}
func (noopSink) Play()                      {}
func (noopSink) Pause()                     {}
func (noopSink) Stop()                      {}
func (noopSink) Seek(time.Duration)         {}
func (noopSink) SetVolume(float64)          {}
func (noopSink) SetEqualizer([]float64)     {}
func (noopSink) Position() time.Duration    { return 0 }
func (noopSink) OnEOS(func())               {}
func (noopSink) OnError(func(error))        {}
func (noopSink) OnStreamStart(func(string)) {}
func (noopSink) Destroy()                   {}

type rewriteTransport struct {
	target string
	rt     http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	u, _ := url.Parse(t.target)
	clone.URL.Scheme = u.Scheme
	clone.URL.Host = u.Host
	return t.rt.RoundTrip(clone)
}

func TestPlayTrack_TriggersOnSessionExpiredOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &config.Config{AppleDeveloperToken: "dev", AppleUserToken: "expired"}
	lic := license.NewClient(cfg)
	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteTransport{target: srv.URL, rt: origTransport}
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	streamer, err := NewStreamServer(nil, lic)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = streamer.Close() }()

	var expiredCalls atomic.Int32
	done := make(chan struct{}, 1)
	p := &Player{
		cfg:       cfg,
		licClient: lic,
		streamer:  streamer,
		sink:      noopSink{},
		OnSessionExpired: func() {
			expiredCalls.Add(1)
			select {
			case done <- struct{}{}:
			default:
			}
		},
	}

	p.playTrack(provider.Track{ID: "123", Title: "Song"})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for OnSessionExpired")
	}
	if got := expiredCalls.Load(); got != 1 {
		t.Errorf("OnSessionExpired called %d times, want 1", got)
	}
}

func TestPlayTrack_SkipsUnavailableTrackAndAdvances(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"songList":[]}`))
	}))
	defer srv.Close()

	cfg := &config.Config{AppleDeveloperToken: "dev", AppleUserToken: "user"}
	lic := license.NewClient(cfg)
	origTransport := http.DefaultTransport
	http.DefaultTransport = rewriteTransport{target: srv.URL, rt: origTransport}
	t.Cleanup(func() { http.DefaultTransport = origTransport })

	streamer, err := NewStreamServer(nil, lic)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = streamer.Close() }()

	p := &Player{
		cfg:       cfg,
		licClient: lic,
		streamer:  streamer,
		sink:      noopSink{},
		queue: []provider.Track{
			{ID: "unavail-1", Title: "Blocked"},
			{ID: "unavail-2", Title: "Next"},
		},
		idx: 0,
	}
	sub := p.Subscribe()
	p.playTrack(p.queue[0])

	deadline := time.After(2 * time.Second)
	sawSkipped := false
	for !sawSkipped {
		select {
		case st := <-sub:
			if st.SkippedID == "unavail-1" {
				sawSkipped = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for SkippedID=unavail-1 broadcast")
		}
	}
}

type spySink struct {
	noopSink
	plays         atomic.Int32
	pauses        atomic.Int32
	mu            sync.Mutex
	nextURI       string
	onStreamStart func(string)
}

func (s *spySink) Play()  { s.plays.Add(1) }
func (s *spySink) Pause() { s.pauses.Add(1) }

func (s *spySink) SetNextURI(uri string) {
	s.mu.Lock()
	s.nextURI = uri
	s.mu.Unlock()
}

func (s *spySink) OnStreamStart(fn func(string)) {
	s.mu.Lock()
	s.onStreamStart = fn
	s.mu.Unlock()
}
func TestPlayWhileLoadingDoesNotCallSinkPlay(t *testing.T) {
	sink := &spySink{}
	p := &Player{sink: sink}
	p.state.Loading = true

	if err := p.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if got := sink.plays.Load(); got != 0 {
		t.Errorf("sink.Play() called %d times while Loading=true, want 0 (avoid GStreamer empty-URI error)", got)
	}
}

func TestSinkErrorDoesNotPersistInPlayerState(t *testing.T) {
	p := &Player{sink: noopSink{}}
	p.mu.Lock()
	p.streamErr = "stream 123: segment 1/10: decrypt: status 2"
	p.mu.Unlock()

	sub := p.Subscribe()
	// Simulate sink reporting a generic error after stream failure.
	p.mu.Lock()
	s := p.state
	s.Error = p.streamErr
	p.mu.Unlock()
	p.bcast.Send(s)

	got := <-sub
	if got.Error != "stream 123: segment 1/10: decrypt: status 2" {
		t.Errorf("broadcast Error = %q, want streamErr", got.Error)
	}
	st, _ := p.GetState()
	if st.Error != "" {
		t.Errorf("stored p.state.Error = %q, want empty so pollPosition does not re-broadcast it", st.Error)
	}
}

func TestGaplessStreamStartTransitionsToPreparedNextTrack(t *testing.T) {
	sink := &spySink{}
	breatheURL := "http://127.0.0.1:1234/stream/2-999/tok.aac"
	prep := &preparedNext{
		idx:       1,
		track:     provider.Track{ID: "2", Title: "Breathe"},
		streamURL: breatheURL,
		duration:  169 * time.Second,
	}
	p := &Player{
		sink: sink,
		queue: []provider.Track{
			{ID: "1", Title: "Speak to Me"},
			{ID: "2", Title: "Breathe"},
		},
		idx:         0,
		nextPrep:    prep,
		stagedPreps: map[string]*preparedNext{breatheURL: prep},
	}
	p.state.Track = &p.queue[0]
	p.state.Playing = true
	sink.OnStreamStart(p.handleStreamStart)

	sub := p.Subscribe()
	sink.onStreamStart(breatheURL)

	st := <-sub
	if st.Track == nil || st.Track.Title != "Breathe" || st.Track.Duration != 169*time.Second {
		t.Fatalf("after stream-start: Track = %+v, want Breathe (169s)", st.Track)
	}
	if p.QueueIndex() != 1 {
		t.Errorf("QueueIndex = %d, want 1", p.QueueIndex())
	}
}

func TestGaplessStreamStartUsesCommittedURIAfterLateQueueRemoval(t *testing.T) {
	sink := &spySink{}
	breatheURL := "http://127.0.0.1:1234/stream/2-999/tok.aac"
	onTheRunURL := "http://127.0.0.1:1234/stream/3-999/tok.aac"

	breathePrep := &preparedNext{
		idx:       1,
		track:     provider.Track{ID: "2", Title: "Breathe"},
		streamURL: breatheURL,
		duration:  169 * time.Second,
	}
	onTheRunPrep := &preparedNext{
		idx:       1,
		track:     provider.Track{ID: "3", Title: "On the Run"},
		streamURL: onTheRunURL,
		duration:  225 * time.Second,
	}

	p := &Player{
		sink: sink,
		queue: []provider.Track{
			{ID: "1", Title: "Speak to Me"},
			{ID: "2", Title: "Breathe"},
			{ID: "3", Title: "On the Run"},
		},
		idx:         0,
		nextPrep:    breathePrep,
		stagedPreps: map[string]*preparedNext{breatheURL: breathePrep},
	}
	p.state.Track = &p.queue[0]
	p.state.Playing = true
	sink.OnStreamStart(p.handleStreamStart)

	// Remove "Breathe" after playbin already committed to breatheURL on about-to-finish,
	// and simulate re-prefetching "On the Run" before the audible join completes.
	if err := p.RemoveFromQueue(1); err != nil {
		t.Fatalf("RemoveFromQueue(1): %v", err)
	}
	p.mu.Lock()
	p.nextPrep = onTheRunPrep
	p.stagedPreps[onTheRunURL] = onTheRunPrep
	p.mu.Unlock()

	sub := p.Subscribe()
	sink.onStreamStart(breatheURL)

	st := <-sub
	if st.Track == nil || st.Track.Title != "Breathe" {
		t.Fatalf("after stream-start for committed Breathe URI: Track = %+v, want Breathe", st.Track)
	}
	// Because Breathe was removed from the queue, idx must stay at 0 so the
	// subsequent join advances to On the Run (now at index 1) rather than skipping it.
	if gotIdx := p.QueueIndex(); gotIdx != 0 {
		t.Errorf("QueueIndex = %d, want 0 so On the Run (idx 1) plays next", gotIdx)
	}
	p.mu.RLock()
	nextIdx, nextTrack, ok := p.nextCandidateLocked()
	p.mu.RUnlock()
	if !ok || nextIdx != 1 || nextTrack.ID != "3" {
		t.Errorf("nextCandidate after committed removed track = (%d, %q, %v), want (1, \"3\", true)", nextIdx, nextTrack.ID, ok)
	}
}

func TestEndOfQueue_PlayRestartsFromBeginning(t *testing.T) {
	sink := &spySink{}
	p := &Player{
		sink: sink,
		queue: []provider.Track{
			{ID: "track-1", Title: "Song 1"},
			{ID: "track-2", Title: "Song 2"},
		},
		idx:          1,
		curStreamURL: "http://127.0.0.1:1234/stream/track-2/tok.aac",
	}
	p.state.Track = &p.queue[1]
	p.state.Playing = true
	p.state.Position = 180 * time.Second

	sub := p.Subscribe()

	// Reaching the end of the queue with RepeatModeOff stops playback and resets Position to 0.
	if err := p.Next(); err != nil {
		t.Fatalf("Next at end of queue: %v", err)
	}
	st := <-sub
	if st.Playing {
		t.Errorf("after Next at end of queue: Playing = true, want false")
	}
	if st.Position != 0 {
		t.Errorf("after Next at end of queue: Position = %v, want 0", st.Position)
	}
	if !p.ended || p.curStreamURL != "" {
		t.Errorf("after Next at end of queue: ended=%v curStreamURL=%q, want true and empty", p.ended, p.curStreamURL)
	}

	// Pressing Play after the queue ended restarts from index 0 via playTrack
	// instead of calling sink.Play() on an exhausted pipeline.
	if err := p.Play(); err != nil {
		t.Fatalf("Play after end of queue: %v", err)
	}
	st = <-sub
	if !st.Loading || st.Track == nil || st.Track.ID != "track-1" {
		t.Errorf("after Play at end of queue: Loading=%v Track=%+v, want Loading=true Track=track-1", st.Loading, st.Track)
	}
	if p.QueueIndex() != 0 {
		t.Errorf("QueueIndex after Play at end of queue = %d, want 0", p.QueueIndex())
	}
	if got := sink.plays.Load(); got != 0 {
		t.Errorf("sink.Play() called %d times on exhausted stream, want 0", got)
	}
	if p.playCtx != nil {
		p.playCtx()
	}
}

func TestEndOfQueue_AppendQueueStartsFirstAppendedTrack(t *testing.T) {
	sink := &spySink{}
	p := &Player{
		sink: sink,
		queue: []provider.Track{
			{ID: "seed-1", Title: "Seed"},
		},
		idx:   0,
		ended: true,
	}
	p.state.Track = &p.queue[0]
	p.state.Playing = false

	sub := p.Subscribe()
	if err := p.AppendQueue([]string{"radio-1", "radio-2"}); err != nil {
		t.Fatalf("AppendQueue: %v", err)
	}
	st := <-sub
	if !st.Loading || st.Track == nil || st.Track.ID != "radio-1" {
		t.Errorf("after AppendQueue on ended queue: Loading=%v Track=%+v, want Loading=true Track=radio-1", st.Loading, st.Track)
	}
	if got := p.QueueIndex(); got != 1 {
		t.Errorf("QueueIndex = %d, want 1 (first appended track)", got)
	}
	if p.playCtx != nil {
		p.playCtx()
	}
}

// A library song with no catalog equivalent (uploaded or unmatched) cannot be
// resolved to a catalog ID. Playback then asks Apple for the library ID,
// which comes back unavailable, so the failed lookup must be logged or the
// skip has no visible cause.
func TestPlayLogsLibraryResolveFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me/library/songs/i.uploaded" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"i.uploaded","type":"library-songs","attributes":{"name":"Home Demo","playParams":{"id":"i.uploaded","kind":"song","isLibrary":true}}}]}`))
	}))
	defer srv.Close()

	prov := apple.New(&config.Config{ //nolint:gosec // G101: test credentials, not real secrets
		AppleDeveloperToken: "test-dev-token",
		AppleUserToken:      "test-user-token",
		StoreFront:          "us",
	})
	prov.SetBaseURL(srv.URL)

	p := &Player{provider: prov, sink: noopSink{}}
	states := p.bcast.Subscribe()
	if err := p.SetQueue([]string{"i.uploaded"}); err != nil {
		t.Fatalf("SetQueue: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case s := <-states:
			for _, line := range s.Logs {
				if strings.HasPrefix(line, "[resolve] i.uploaded: ") && strings.Contains(line, "no Apple Music catalog equivalent") {
					return
				}
				if strings.HasPrefix(line, "[resolve] ") {
					t.Fatalf("unexpected resolve log %q", line)
				}
			}
		case <-deadline:
			t.Fatal("no [resolve] log for a library song without a catalog ID")
		}
	}
}
