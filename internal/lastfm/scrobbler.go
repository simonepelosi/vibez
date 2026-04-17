package lastfm

import (
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

const (
	minScrobbleDuration  = 30 * time.Second
	maxScrobbleThreshold = 4 * time.Minute
)

// Scrobbler watches a player.State stream and applies Last.fm scrobbling rules:
//
//   - Sends a Now Playing update when a new track starts playing.
//   - Scrobbles the track once the user has listened for at least 50% of its
//     duration, or for 4 minutes — whichever comes first.
//   - Tracks with a duration below 30 seconds are never scrobbled.
//   - Play time is measured only while the player is actually playing (pauses
//     are excluded from the accumulated total).
type Scrobbler struct {
	client *Client
	logFn  func(string) // optional; set via SetLogger before first Update

	mu             sync.Mutex
	lastTrack      *provider.Track
	trackStart     time.Time     // wall time when this track was first seen playing
	resumeTime     time.Time     // wall time when the current play segment started
	playedTime     time.Duration // accumulated play time, pauses excluded
	isPlaying      bool
	scrobbled      bool
	nowPlayingSent bool
}

// NewScrobbler creates a Scrobbler backed by client.
func NewScrobbler(client *Client) *Scrobbler {
	return &Scrobbler{client: client}
}

// SetLogger registers a function called for each debug event. Pass nil to
// disable. Must be called before the first Update.
func (s *Scrobbler) SetLogger(fn func(string)) {
	s.logFn = fn
}

func (s *Scrobbler) log(msg string) {
	if s.logFn != nil {
		s.logFn(msg)
	}
}

// Update processes a new player state. It should be called for every state
// delivered by player.Player.Subscribe.
func (s *Scrobbler) Update(st player.State) {
	s.mu.Lock()
	defer s.mu.Unlock()

	trackChanged := st.Track != nil && (s.lastTrack == nil ||
		s.lastTrack.Title != st.Track.Title ||
		s.lastTrack.Artist != st.Track.Artist)

	if trackChanged {
		// Finalise the previous track before switching.
		if s.lastTrack != nil && !s.scrobbled {
			if s.isPlaying {
				s.playedTime += time.Since(s.resumeTime)
			}
			s.maybeScrobble(s.lastTrack, s.trackStart, s.playedTime)
		}

		// Initialise state for the new track.
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
		// Playback stopped entirely — finalise and clear.
		if s.lastTrack != nil && !s.scrobbled && s.isPlaying {
			s.playedTime += time.Since(s.resumeTime)
			s.maybeScrobble(s.lastTrack, s.trackStart, s.playedTime)
		}
		s.lastTrack = nil
		s.isPlaying = false
		return
	}

	// Handle play / pause transitions on the same track.
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

	// Send Now Playing once per track, at the moment it starts playing.
	if !s.nowPlayingSent && st.Playing && s.lastTrack != nil {
		s.nowPlayingSent = true
		track := s.lastTrack
		s.log("[lastfm] now playing: " + track.Artist + " — " + track.Title)
		go func() {
			if err := s.client.UpdateNowPlaying(track.Artist, track.Title, track.Album, track.Duration); err != nil {
				s.log("[lastfm] now playing error: " + err.Error())
			}
		}()
	}

	// Check whether the scrobble threshold has been reached.
	if !s.scrobbled && s.lastTrack != nil {
		current := s.playedTime
		if s.isPlaying {
			current += time.Since(s.resumeTime)
		}
		s.checkScrobble(s.lastTrack, s.trackStart, current)
	}
}

func (s *Scrobbler) checkScrobble(track *provider.Track, start time.Time, played time.Duration) {
	if s.scrobbled || track.Duration < minScrobbleDuration || start.IsZero() {
		return
	}
	threshold := min(track.Duration/2, maxScrobbleThreshold)
	if played >= threshold {
		s.scrobbled = true
		s.log("[lastfm] scrobbling: " + track.Artist + " — " + track.Title)
		dur := track.Duration
		go func() {
			if err := s.client.Scrobble(track.Artist, track.Title, track.Album, start, dur); err != nil {
				s.log("[lastfm] scrobble error: " + err.Error())
			} else {
				s.log("[lastfm] scrobbled: " + track.Artist + " — " + track.Title)
			}
		}()
	}
}

func (s *Scrobbler) maybeScrobble(track *provider.Track, start time.Time, played time.Duration) {
	if track == nil || track.Duration < minScrobbleDuration || start.IsZero() {
		return
	}
	threshold := min(track.Duration/2, maxScrobbleThreshold)
	if played >= threshold {
		s.log("[lastfm] scrobbling: " + track.Artist + " — " + track.Title)
		dur := track.Duration
		go func() {
			if err := s.client.Scrobble(track.Artist, track.Title, track.Album, start, dur); err != nil {
				s.log("[lastfm] scrobble error: " + err.Error())
			} else {
				s.log("[lastfm] scrobbled: " + track.Artist + " — " + track.Title)
			}
		}()
	}
}
