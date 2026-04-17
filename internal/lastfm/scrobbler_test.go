package lastfm

import (
	"testing"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// fakeClient records calls without hitting the network.
type fakeClient struct {
	nowPlayingCalls []string
	scrobbleCalls   []string
}

func (f *fakeClient) UpdateNowPlaying(artist, track, _ string, _ time.Duration) error {
	f.nowPlayingCalls = append(f.nowPlayingCalls, artist+" — "+track)
	return nil
}

func (f *fakeClient) Scrobble(artist, track, _ string, _ time.Time, _ time.Duration) error {
	f.scrobbleCalls = append(f.scrobbleCalls, artist+" — "+track)
	return nil
}

// scrabblerIface lets us swap the real Client for the fake in tests.
type scrobblerClient interface {
	UpdateNowPlaying(artist, track, album string, duration time.Duration) error
	Scrobble(artist, track, album string, startTime time.Time, duration time.Duration) error
}

// testScrobbler is a Scrobbler variant that uses scrobblerClient instead of *Client,
// allowing injection of fakeClient in tests.
type testScrobbler struct {
	client scrobblerClient

	lastTrack      *provider.Track
	trackStart     time.Time
	resumeTime     time.Time
	playedTime     time.Duration
	isPlaying      bool
	scrobbled      bool
	nowPlayingSent bool
}

func newTestScrobbler(c scrobblerClient) *testScrobbler {
	return &testScrobbler{client: c}
}

func (s *testScrobbler) update(st player.State) {
	trackChanged := st.Track != nil && (s.lastTrack == nil ||
		s.lastTrack.Title != st.Track.Title ||
		s.lastTrack.Artist != st.Track.Artist)

	if trackChanged {
		if s.lastTrack != nil && !s.scrobbled {
			if s.isPlaying {
				s.playedTime += time.Since(s.resumeTime)
			}
			s.maybeScrob(s.lastTrack, s.trackStart, s.playedTime)
		}
		s.lastTrack = st.Track
		s.scrobbled = false
		s.nowPlayingSent = false
		s.playedTime = 0
		s.isPlaying = false
		s.trackStart = time.Time{}
		if st.Playing {
			s.isPlaying = true
			now := time.Now()
			s.trackStart = now
			s.resumeTime = now
		}
	} else if st.Track == nil {
		if s.lastTrack != nil && !s.scrobbled && s.isPlaying {
			s.playedTime += time.Since(s.resumeTime)
			s.maybeScrob(s.lastTrack, s.trackStart, s.playedTime)
		}
		s.lastTrack = nil
		s.isPlaying = false
		return
	}

	if !trackChanged {
		if st.Playing && !s.isPlaying {
			s.isPlaying = true
			s.resumeTime = time.Now()
			if s.trackStart.IsZero() {
				s.trackStart = time.Now()
			}
		} else if !st.Playing && s.isPlaying {
			s.isPlaying = false
			s.playedTime += time.Since(s.resumeTime)
		}
	}

	if !s.nowPlayingSent && st.Playing && s.lastTrack != nil {
		s.nowPlayingSent = true
		_ = s.client.UpdateNowPlaying(s.lastTrack.Artist, s.lastTrack.Title, s.lastTrack.Album, s.lastTrack.Duration)
	}

	if !s.scrobbled && s.lastTrack != nil {
		current := s.playedTime
		if s.isPlaying {
			current += time.Since(s.resumeTime)
		}
		s.checkScrob(s.lastTrack, s.trackStart, current)
	}
}

func (s *testScrobbler) checkScrob(track *provider.Track, start time.Time, played time.Duration) {
	if s.scrobbled || track.Duration < minScrobbleDuration || start.IsZero() {
		return
	}
	threshold := min(track.Duration/2, maxScrobbleThreshold)
	if played >= threshold {
		s.scrobbled = true
		_ = s.client.Scrobble(track.Artist, track.Title, track.Album, start, track.Duration)
	}
}

func (s *testScrobbler) maybeScrob(track *provider.Track, start time.Time, played time.Duration) {
	if track == nil || track.Duration < minScrobbleDuration || start.IsZero() {
		return
	}
	threshold := min(track.Duration/2, maxScrobbleThreshold)
	if played >= threshold {
		_ = s.client.Scrobble(track.Artist, track.Title, track.Album, start, track.Duration)
	}
}

// ── Tests ─────────────────────────────────────────────────────────────────

func TestNowPlayingSentOnTrackStart(t *testing.T) {
	fc := &fakeClient{}
	s := newTestScrobbler(fc)

	track := &provider.Track{Title: "Song A", Artist: "Artist A", Duration: 3 * time.Minute}
	s.update(player.State{Track: track, Playing: true})

	if len(fc.nowPlayingCalls) != 1 {
		t.Fatalf("expected 1 now-playing call, got %d", len(fc.nowPlayingCalls))
	}
	if fc.nowPlayingCalls[0] != "Artist A — Song A" {
		t.Errorf("unexpected now-playing call: %q", fc.nowPlayingCalls[0])
	}
}

func TestNowPlayingSentOnlyOnce(t *testing.T) {
	fc := &fakeClient{}
	s := newTestScrobbler(fc)

	track := &provider.Track{Title: "Song A", Artist: "Artist A", Duration: 3 * time.Minute}
	s.update(player.State{Track: track, Playing: true})
	s.update(player.State{Track: track, Playing: true, Position: time.Second})
	s.update(player.State{Track: track, Playing: true, Position: 2 * time.Second})

	if len(fc.nowPlayingCalls) != 1 {
		t.Errorf("now-playing should be sent only once, got %d calls", len(fc.nowPlayingCalls))
	}
}

func TestShortTrackNotScrobbled(t *testing.T) {
	fc := &fakeClient{}
	s := newTestScrobbler(fc)

	// Track is only 20 seconds — below the 30 s minimum.
	track := &provider.Track{Title: "Short", Artist: "Artist", Duration: 20 * time.Second}
	s.update(player.State{Track: track, Playing: true})

	// Manually fast-forward played time past 50%.
	s.playedTime = 15 * time.Second
	s.isPlaying = false
	s.checkScrob(track, s.trackStart, s.playedTime)

	if len(fc.scrobbleCalls) != 0 {
		t.Errorf("short track should not be scrobbled, got %d scrobble calls", len(fc.scrobbleCalls))
	}
}

func TestTrackScrobblesAtHalfDuration(t *testing.T) {
	fc := &fakeClient{}
	s := newTestScrobbler(fc)

	track := &provider.Track{Title: "Song B", Artist: "Artist B", Duration: 4 * time.Minute}
	s.update(player.State{Track: track, Playing: true})

	// Simulate 2 minutes played (exactly 50%).
	s.playedTime = 2 * time.Minute
	s.isPlaying = false
	s.checkScrob(track, s.trackStart, s.playedTime)

	if len(fc.scrobbleCalls) != 1 {
		t.Fatalf("expected 1 scrobble call at 50%%, got %d", len(fc.scrobbleCalls))
	}
}

func TestFourMinuteCap(t *testing.T) {
	fc := &fakeClient{}
	s := newTestScrobbler(fc)

	// 10-minute track: threshold should be capped at 4 minutes, not 5.
	track := &provider.Track{Title: "Long Song", Artist: "Artist C", Duration: 10 * time.Minute}
	s.update(player.State{Track: track, Playing: true})

	// 4 minutes played — should scrobble (capped threshold).
	s.playedTime = 4 * time.Minute
	s.isPlaying = false
	s.checkScrob(track, s.trackStart, s.playedTime)

	if len(fc.scrobbleCalls) != 1 {
		t.Fatalf("expected scrobble at 4-minute cap, got %d scrobble calls", len(fc.scrobbleCalls))
	}
}

func TestNoScrobbleBeforeThreshold(t *testing.T) {
	fc := &fakeClient{}
	s := newTestScrobbler(fc)

	track := &provider.Track{Title: "Song C", Artist: "Artist C", Duration: 4 * time.Minute}
	s.update(player.State{Track: track, Playing: true})

	// Only 1 minute played (25%) — should not scrobble yet.
	s.playedTime = time.Minute
	s.isPlaying = false
	s.checkScrob(track, s.trackStart, s.playedTime)

	if len(fc.scrobbleCalls) != 0 {
		t.Errorf("should not scrobble before threshold, got %d scrobble calls", len(fc.scrobbleCalls))
	}
}

func TestScrobbleOnTrackChange(t *testing.T) {
	fc := &fakeClient{}
	s := newTestScrobbler(fc)

	track1 := &provider.Track{Title: "Song 1", Artist: "Artist", Duration: 3 * time.Minute}
	s.update(player.State{Track: track1, Playing: true})

	// Simulate enough play time for track1.
	s.playedTime = 2 * time.Minute
	s.isPlaying = false

	// New track starts — should scrobble track1.
	track2 := &provider.Track{Title: "Song 2", Artist: "Artist", Duration: 3 * time.Minute}
	s.update(player.State{Track: track2, Playing: true})

	if len(fc.scrobbleCalls) != 1 {
		t.Fatalf("expected track1 to be scrobbled on track change, got %d scrobble calls", len(fc.scrobbleCalls))
	}
	if fc.scrobbleCalls[0] != "Artist — Song 1" {
		t.Errorf("unexpected scrobble: %q", fc.scrobbleCalls[0])
	}
}

func TestScrobbleOnlyOnce(t *testing.T) {
	fc := &fakeClient{}
	s := newTestScrobbler(fc)

	track := &provider.Track{Title: "Song D", Artist: "Artist D", Duration: 3 * time.Minute}
	s.update(player.State{Track: track, Playing: true})

	s.playedTime = 2 * time.Minute
	s.isPlaying = false

	// Trigger checkScrob twice — scrobble must fire only once.
	s.checkScrob(track, s.trackStart, s.playedTime)
	s.checkScrob(track, s.trackStart, s.playedTime)

	if len(fc.scrobbleCalls) != 1 {
		t.Errorf("track should be scrobbled exactly once, got %d", len(fc.scrobbleCalls))
	}
}
