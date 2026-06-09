//go:build linux

// Package local provides a Player backed by GStreamer for local audio files.
// It plays file:// URIs directly so no DRM, no WebView, no credentials needed.
package local

import (
	"fmt"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/audioquality"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/player/gst"
	"github.com/simone-vibes/vibez/internal/provider"
)

// Player implements player.Player for local audio files using GStreamer.
type Player struct {
	gst   *gst.Player
	mu    sync.RWMutex
	state player.State
	subs  []chan player.State
	queue []provider.Track
	idx   int
	done  chan struct{}
}

// New creates a local Player backed by a GStreamer pipeline.
func New() (*Player, error) {
	gstPlayer, err := gst.New()
	if err != nil {
		return nil, fmt.Errorf("local player: %w", err)
	}
	p := &Player{gst: gstPlayer, done: make(chan struct{})}
	p.gst.OnEOS(func() {
		_ = p.Next()
	})
	p.gst.OnError(func(e error) {
		p.mu.Lock()
		p.state.Error = e.Error()
		s := p.state
		p.mu.Unlock()
		p.broadcast(s)
	})
	go p.pollState()
	return p, nil
}

func (p *Player) broadcast(s player.State) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, ch := range p.subs {
		select {
		case ch <- s:
		default:
		}
	}
}

func (p *Player) currentState() player.State {
	p.mu.RLock()
	s := p.state
	p.mu.RUnlock()
	s.Position = p.gst.Position()
	return s
}

func (p *Player) playTrack(t provider.Track) {
	uri := fmt.Sprintf("file://%s", t.ID[len("local:"):])
	p.gst.PlayURI(uri)
	time.Sleep(200 * time.Millisecond)
	if d := p.gst.Duration(); d > 0 {
		t.Duration = d
	}
	p.mu.Lock()
	p.state.Track = &t
	p.state.Playing = true
	p.state.Position = 0
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
}

func (p *Player) Play() error {
	p.gst.Play()
	p.mu.Lock()
	p.state.Playing = true
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
	return nil
}

func (p *Player) Pause() error {
	p.gst.Pause()
	p.mu.Lock()
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
	return nil
}

func (p *Player) Stop() error {
	p.gst.Stop()
	p.mu.Lock()
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
	return nil
}

func (p *Player) Next() error {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}
	p.idx = (p.idx + 1) % len(p.queue)
	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) Previous() error {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}
	if p.gst.Position() > 3*time.Second {
		p.mu.Unlock()
		p.gst.Seek(0)
		return nil
	}
	if p.idx == 0 {
		p.idx = len(p.queue) - 1
	} else {
		p.idx--
	}
	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) Seek(pos time.Duration) error {
	p.gst.Seek(pos)
	return nil
}

func (p *Player) SetVolume(v float64) error {
	p.gst.SetVolume(v)
	return nil
}

func (p *Player) SetAudioBitrate(kbps int) error {
	if err := audioquality.Validate(kbps); err != nil {
		return err
	}
	return player.ErrAudioBitrateSavedPreferenceOnly
}

func (p *Player) SetQueue(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// Finding the first ID in the full queue and starting from there.
	p.mu.Lock()
	found := false
	for i, t := range p.queue {
		if t.ID == ids[0] {
			p.idx = i
			found = true
			break
		}
	}
	if !found || len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}
	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) SetPlaylist(_ string, startIdx int) error {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}
	if startIdx >= 0 && startIdx < len(p.queue) {
		p.idx = startIdx
	} else {
		p.idx = 0
	}
	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) AppendQueue(ids []string) error {
	extra := tracksForIDs(p.queue, ids)
	p.mu.Lock()
	p.queue = append(p.queue, extra...)
	p.mu.Unlock()
	return nil
}

func (p *Player) SetRepeat(mode int) error {
	p.mu.Lock()
	p.state.RepeatMode = mode
	p.mu.Unlock()
	return nil
}

func (p *Player) SetShuffle(on bool) error {
	p.mu.Lock()
	p.state.ShuffleMode = on
	p.mu.Unlock()
	return nil
}

func (p *Player) SetEqualizer(_ []player.EQBand) error { return nil }

func (p *Player) RemoveFromQueue(idx int) error {
	p.mu.Lock()
	if idx >= 0 && idx < len(p.queue) {
		p.queue = append(p.queue[:idx], p.queue[idx+1:]...)
	}
	p.mu.Unlock()
	return nil
}

func (p *Player) MoveInQueue(from, to int) error {
	p.mu.Lock()
	if from >= 0 && from < len(p.queue) && to >= 0 && to < len(p.queue) {
		t := p.queue[from]
		p.queue = append(p.queue[:from], p.queue[from+1:]...)
		if from < to {
			to--
		}
		p.queue = append(p.queue[:to], append([]provider.Track{t}, p.queue[to:]...)...)
	}
	p.mu.Unlock()
	return nil
}

func (p *Player) ClearQueue() error {
	p.gst.Stop()
	p.mu.Lock()
	p.queue = nil
	p.idx = 0
	p.state.Track = nil
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
	return nil
}

func (p *Player) GetState() (*player.State, error) {
	s := p.currentState()
	return &s, nil
}

func (p *Player) Subscribe() <-chan player.State {
	ch := make(chan player.State, 8)
	p.mu.Lock()
	p.subs = append(p.subs, ch)
	p.mu.Unlock()
	return ch
}

func (p *Player) Close() error {
	close(p.done)
	p.gst.Destroy()
	return nil
}

// This replaces the internal queue with the given tracks. Called after
// the provider scans the music directory
func (p *Player) LoadTracks(tracks []provider.Track) {
	p.mu.Lock()
	p.queue = append([]provider.Track{}, tracks...)
	p.idx = 0
	p.mu.Unlock()
}

func tracksForIDs(tracks []provider.Track, ids []string) []provider.Track {
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

func (p *Player) pollState() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.mu.RLock()
			playing := p.state.Playing
			p.mu.RUnlock()
			if !playing {
				continue
			}
			p.mu.Lock()
			p.state.Position = p.gst.Position()
			s := p.state
			p.mu.Unlock()
			p.broadcast(s)
		case <-p.done:
			return
		}
	}
}
