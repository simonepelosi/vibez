// Package demo provides a fully functional in-memory Player that runs without
// any Apple credentials. It is intended for UI development and contribution
// testing. Every Player interface method works; playback state advances in
// real time so animations, progress bars and queue interactions can be tested.
package demo

import (
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
	demodp "github.com/simone-vibes/vibez/internal/provider/demo"
)

// tracks is the local alias for the shared demo track list.
var tracks = demodp.Tracks

// Player is the demo Player implementation.
type Player struct {
	mu    sync.RWMutex
	state player.State
	queue []provider.Track
	idx   int

	subs   []chan player.State
	stopCh chan struct{}
	done   chan struct{}
}

// New creates a demo Player pre-loaded with the built-in track list and
// starts simulated playback from the first track.
func New() *Player {
	p := &Player{
		queue:  append([]provider.Track{}, tracks...),
		idx:    0,
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	p.state = player.State{
		Track:   &p.queue[0],
		Playing: true,
		Volume:  0.8,
	}
	go p.run()
	return p
}

// run advances the playback position every second while playing.
func (p *Player) run() {
	defer close(p.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.mu.Lock()
			if p.state.Playing && p.state.Track != nil {
				p.state.Position += time.Second
				if p.state.Position >= p.state.Track.Duration {
					p.advance()
				}
			}
			st := p.state
			p.mu.Unlock()
			p.broadcast(st)
		}
	}
}

// advance moves to the next track (wraps around). Must be called with mu held.
func (p *Player) advance() {
	p.idx = (p.idx + 1) % len(p.queue)
	t := p.queue[p.idx]
	p.state.Track = &t
	p.state.Position = 0
}

func (p *Player) broadcast(st player.State) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, ch := range p.subs {
		select {
		case ch <- st:
		default:
		}
	}
}

func (p *Player) Play() error {
	p.mu.Lock()
	p.state.Playing = true
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) Pause() error {
	p.mu.Lock()
	p.state.Playing = false
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) Stop() error  { return p.Pause() }
func (p *Player) Close() error { close(p.stopCh); <-p.done; return nil }

func (p *Player) Next() error {
	p.mu.Lock()
	p.advance()
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) Previous() error {
	p.mu.Lock()
	if p.state.Position > 3*time.Second {
		p.state.Position = 0
	} else {
		if p.idx == 0 {
			p.idx = len(p.queue) - 1
		} else {
			p.idx--
		}
		t := p.queue[p.idx]
		p.state.Track = &t
		p.state.Position = 0
	}
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) Seek(pos time.Duration) error {
	p.mu.Lock()
	if p.state.Track != nil && pos <= p.state.Track.Duration {
		p.state.Position = pos
	}
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) SetVolume(v float64) error {
	p.mu.Lock()
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	p.state.Volume = v
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) SetQueue(ids []string) error {
	p.mu.Lock()
	p.queue = tracksForIDs(ids)
	if len(p.queue) == 0 {
		p.queue = append([]provider.Track{}, tracks...)
	}
	p.idx = 0
	t := p.queue[0]
	p.state.Track = &t
	p.state.Position = 0
	p.state.Playing = true
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) SetPlaylist(_ string, startIdx int) error {
	p.mu.Lock()
	p.queue = append([]provider.Track{}, tracks...)
	if startIdx >= 0 && startIdx < len(p.queue) {
		p.idx = startIdx
	} else {
		p.idx = 0
	}
	t := p.queue[p.idx]
	p.state.Track = &t
	p.state.Position = 0
	p.state.Playing = true
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) AppendQueue(ids []string) error {
	extra := tracksForIDs(ids)
	p.mu.Lock()
	p.queue = append(p.queue, extra...)
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) SetRepeat(mode int) error {
	p.mu.Lock()
	p.state.RepeatMode = mode
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) SetShuffle(on bool) error {
	p.mu.Lock()
	p.state.ShuffleMode = on
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) RemoveFromQueue(idx int) error {
	p.mu.Lock()
	if idx >= 0 && idx < len(p.queue) {
		p.queue = append(p.queue[:idx], p.queue[idx+1:]...)
		if p.idx >= len(p.queue) {
			p.idx = len(p.queue) - 1
		}
	}
	p.mu.Unlock()
	return nil
}

func (p *Player) MoveInQueue(from, to int) error {
	p.mu.Lock()
	if from >= 0 && from < len(p.queue) && to >= 0 && to < len(p.queue) {
		t := p.queue[from]
		p.queue = append(p.queue[:from], p.queue[from+1:]...)
		p.queue = append(p.queue[:to], append([]provider.Track{t}, p.queue[to:]...)...)
	}
	p.mu.Unlock()
	return nil
}

func (p *Player) ClearQueue() error {
	p.mu.Lock()
	p.queue = []provider.Track{}
	p.state.Track = nil
	p.state.Playing = false
	st := p.state
	p.mu.Unlock()
	p.broadcast(st)
	return nil
}

func (p *Player) GetState() (*player.State, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	st := p.state
	return &st, nil
}

func (p *Player) Subscribe() <-chan player.State {
	ch := make(chan player.State, 8)
	p.mu.Lock()
	p.subs = append(p.subs, ch)
	p.mu.Unlock()
	return ch
}

// tracksForIDs looks up demo tracks by ID; unknown IDs are silently skipped.
func tracksForIDs(ids []string) []provider.Track {
	byID := make(map[string]provider.Track, len(tracks))
	for _, t := range tracks {
		byID[t.ID] = t
	}
	out := make([]provider.Track, 0, len(ids))
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out
}
