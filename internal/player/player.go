package player

import (
	"time"

	"github.com/simone-vibes/vibez/internal/provider"
)

type State struct {
	Track    *provider.Track
	Playing  bool
	Position time.Duration
	Volume   float64 // 0.0–1.0
	Error    string  // non-empty when JS reports a playback error
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
	GetState() (*State, error)
	Subscribe() <-chan State
	Close() error
}
