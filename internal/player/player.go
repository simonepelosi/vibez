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
}

type Player interface {
	Play() error
	Pause() error
	Stop() error
	Next() error
	Previous() error
	Seek(position time.Duration) error
	SetVolume(v float64) error
	// SetQueue replaces the playback queue with the given track IDs and starts
	// playback. Implementations that do not support queue management return an error.
	SetQueue(ids []string) error
	GetState() (*State, error)
	Subscribe() <-chan State
	Close() error
}
