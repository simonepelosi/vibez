package player

import (
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
)

// Repeat mode constants matching MusicKit.PlayerRepeatMode values.
const (
	RepeatModeOff = 0 // no repeat
	RepeatModeOne = 1 // repeat current track
	RepeatModeAll = 2 // repeat entire queue
)

type State struct {
	Track       *provider.Track
	Playing     bool
	Loading     bool // true when MusicKit playbackState is loading/buffering
	Position    time.Duration
	Volume      float64 // 0.0–1.0
	RepeatMode  int     // RepeatModeOff / RepeatModeOne / RepeatModeAll
	ShuffleMode bool    // true = shuffle on
	Error       string  // non-empty when JS reports a playback error
	Log         string  // non-empty for debug-only log entries (not shown in status bar)
}

type Player interface {
	Play() error
	Pause() error
	Stop() error
	Next() error
	Previous() error
	Seek(position time.Duration) error
	SetVolume(v float64) error
	// SetQueue replaces the playback queue with the given catalog track IDs and
	// starts playback from the first entry.
	SetQueue(ids []string) error
	// SetPlaylist queues a library playlist by its ID (e.g. "p.XXXXX") and starts
	// playback from startIdx. This avoids per-song catalog ID resolution.
	SetPlaylist(playlistID string, startIdx int) error
	// AppendQueue adds the given track IDs to the end of the current queue
	// without interrupting playback. If nothing is playing it starts playback.
	AppendQueue(ids []string) error
	// SetRepeat sets the repeat mode: 0=off, 1=one, 2=all.
	SetRepeat(mode int) error
	// SetShuffle enables or disables shuffle playback.
	SetShuffle(on bool) error
	GetState() (*State, error)
	Subscribe() <-chan State
	Close() error
}
