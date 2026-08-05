//go:build darwin && !cgo

package local

import (
	"fmt"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

// Player is a stub for builds where CGo is disabled.
// New() always returns an error, so none of these methods are ever called.
type Player struct{}

func New() (*Player, error) {
	return nil, fmt.Errorf("local playback requires CGo on macOS — build with CGO_ENABLED=1")
}

func (p *Player) Play() error                          { return nil }
func (p *Player) Pause() error                         { return nil }
func (p *Player) Stop() error                          { return nil }
func (p *Player) Next() error                          { return nil }
func (p *Player) Previous() error                      { return nil }
func (p *Player) Seek(_ time.Duration) error           { return nil }
func (p *Player) SetVolume(_ float64) error            { return nil }
func (p *Player) SetAudioBitrate(_ int) error          { return nil }
func (p *Player) SetQueue(_ []string) error            { return nil }
func (p *Player) SetPlaylist(_ string, _ int) error    { return nil }
func (p *Player) AppendQueue(_ []string) error         { return nil }
func (p *Player) SetRepeat(_ int) error                { return nil }
func (p *Player) SetShuffle(_ bool) error              { return nil }
func (p *Player) SetEqualizer(_ []player.EQBand) error { return nil }
func (p *Player) RemoveFromQueue(_ int) error          { return nil }
func (p *Player) MoveInQueue(_, _ int) error           { return nil }
func (p *Player) ClearQueue() error                    { return nil }
func (p *Player) GetState() (*player.State, error)     { return &player.State{}, nil }
func (p *Player) Subscribe() <-chan player.State       { return nil }
func (p *Player) Close() error                         { return nil }
func (p *Player) LoadTracks(_ []provider.Track)        {}
