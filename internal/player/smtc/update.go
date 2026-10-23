package smtc

import (
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// playbackStatus is the status SMTC shows. The values equal
// Windows.Media.MediaPlaybackStatus.
type playbackStatus int

const (
	// statusClosed means no track. The bridge also disables the controls, so
	// the session leaves the system UI and media keys go to other apps.
	statusClosed   playbackStatus = 0
	statusChanging playbackStatus = 1 // a track is loading
	statusStopped  playbackStatus = 2
	statusPlaying  playbackStatus = 3
	statusPaused   playbackStatus = 4
)

func (s playbackStatus) String() string {
	switch s {
	case statusClosed:
		return "Closed"
	case statusChanging:
		return "Changing"
	case statusStopped:
		return "Stopped"
	case statusPlaying:
		return "Playing"
	case statusPaused:
		return "Paused"
	}
	return "Unknown"
}

// metadata is what the DisplayUpdater shows for a track.
type metadata struct {
	Title      string
	Artist     string
	Album      string
	ArtworkURL string
}

// timeline is the position and length a reader draws a progress bar from.
type timeline struct {
	Position time.Duration
	Duration time.Duration
}

// update is one batch for the bridge. A nil field is left as it was.
type update struct {
	Metadata *metadata
	Status   *playbackStatus
	Timeline *timeline
}

const (
	// timelineRefresh is how often the timeline is re-sent while playing.
	// Readers extrapolate the position from the last update's timestamp, so
	// this only corrects drift; jumps are sent at once.
	timelineRefresh = 5 * time.Second

	// seekThreshold is how far the position may stray from where steady
	// playback would put it before the change counts as a seek. Position
	// ticks arrive every 250 ms and vibez seeks in 10 s steps, so this sits
	// between the two, as it does for MPRIS.
	seekThreshold = 2 * time.Second
)

// tracker remembers what the bridge was last told and turns each player State
// into the smallest update that brings readers up to date. Its zero value
// matches a new bridge: no metadata, status Closed, no timeline.
type tracker struct {
	meta   metadata
	status playbackStatus

	hasTimeline bool
	tl          timeline
	tlAt        time.Time // when tl was sent
}

func statusOf(st player.State) playbackStatus {
	switch {
	case st.Track == nil:
		return statusClosed
	case st.Playing:
		return statusPlaying
	case st.Loading:
		return statusChanging
	default:
		return statusPaused
	}
}

func metadataOf(t *provider.Track) metadata {
	if t == nil {
		return metadata{}
	}
	return metadata{Title: t.Title, Artist: t.Artist, Album: t.Album, ArtworkURL: t.ArtworkURL}
}

func timelineOf(t *provider.Track, pos time.Duration) timeline {
	dur := max(t.Duration, 0)
	pos = max(pos, 0)
	if dur > 0 && pos > dur {
		pos = dur
	}
	return timeline{Position: pos, Duration: dur}
}

// next returns the update that st calls for at time now, or false when readers
// already show everything st holds.
func (t *tracker) next(st player.State, now time.Time) (update, bool) {
	var u update
	status := statusOf(st)
	meta := metadataOf(st.Track)

	metaChanged := meta != t.meta
	statusChanged := status != t.status
	if metaChanged && st.Track != nil {
		m := meta
		u.Metadata = &m
	}
	if statusChanged {
		s := status
		u.Status = &s
	}
	t.meta = meta
	t.status = status

	if st.Track == nil {
		// Closed clears the display along with the timeline.
		t.hasTimeline = false
		return u, statusChanged
	}

	tl := timelineOf(st.Track, st.Position)
	if t.timelineDue(tl, metaChanged || statusChanged, now) {
		u.Timeline = &tl
		t.hasTimeline = true
		t.tl = tl
		t.tlAt = now
	}
	return u, u.Metadata != nil || u.Status != nil || u.Timeline != nil
}

// timelineDue reports whether tl has to be sent: there is none yet, the track
// or status changed, the length changed, the position jumped, or the refresh
// interval has passed while playing.
func (t *tracker) timelineDue(tl timeline, changed bool, now time.Time) bool {
	if !t.hasTimeline || changed || tl.Duration != t.tl.Duration {
		return true
	}
	elapsed := now.Sub(t.tlAt)
	expected := t.tl.Position
	if t.status == statusPlaying {
		expected += elapsed
	}
	drift := tl.Position - expected
	if drift < 0 {
		drift = -drift
	}
	if drift > seekThreshold {
		return true
	}
	return t.status == statusPlaying && elapsed >= timelineRefresh
}
