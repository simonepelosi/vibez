//go:build linux

// Package local provides a Player backed by GStreamer for local audio files.
// It plays file:// URIs directly so no DRM, no WebView, no credentials needed.
package local

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/audioquality"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/player/gst"
	"github.com/simone-vibes/vibez/internal/provider"
)

// gstEngine is the minimal GStreamer backend surface required by Player.
type gstEngine interface {
	PlayURI(string)
	Play()
	Pause()
	Stop()
	Seek(time.Duration)
	SetVolume(float64)
	SetEqualizer([]float64)
	Position() time.Duration
	Destroy()
}

// Player implements player.Player for local audio files using GStreamer.
type Player struct {
	gst        gstEngine
	mu         sync.RWMutex
	state      player.State
	bcast      player.Broadcast
	closed     bool
	queue      []provider.Track
	allTracks  []provider.Track
	idx        int
	ended      bool
	done       chan struct{}
	pollWakeCh chan struct{}
	closeOnce  sync.Once
}

func newWithEngine(engine gstEngine) *Player {
	p := &Player{
		gst:        engine,
		done:       make(chan struct{}),
		pollWakeCh: make(chan struct{}, 1),
	}
	go p.pollState()
	return p
}

// New creates a local Player backed by a GStreamer pipeline.
func New(_ func(string)) (*Player, error) {
	gstPlayer, err := gst.New()
	if err != nil {
		return nil, fmt.Errorf("local player: %w", err)
	}
	p := newWithEngine(gstPlayer)
	gstPlayer.OnEOS(func() {
		_ = p.Next()
	})
	gstPlayer.OnError(func(e error) {
		p.mu.Lock()
		p.state.Error = provider.Clean(e.Error())
		s := p.state
		p.mu.Unlock()
		p.broadcast(s)
	})
	return p, nil
}

func (p *Player) signalPoll() {
	if p.pollWakeCh == nil {
		return
	}
	select {
	case p.pollWakeCh <- struct{}{}:
	default:
	}
}

func (p *Player) broadcast(s player.State) {
	p.mu.RLock()
	closed := p.closed
	p.mu.RUnlock()
	if closed {
		return
	}
	p.signalPoll()
	p.bcast.Send(s)
}

func (p *Player) currentState() player.State {
	p.mu.RLock()
	s := p.state
	p.mu.RUnlock()
	s.Position = p.gst.Position()
	return s
}

func (p *Player) playTrack(t provider.Track) {
	rawPath, ok := strings.CutPrefix(t.ID, "local:")
	if !ok || rawPath == "" {
		p.mu.Lock()
		p.state.Error = fmt.Sprintf("local player: invalid track ID %q", provider.Clean(t.ID))
		s := p.state
		p.mu.Unlock()
		p.broadcast(s)
		return
	}
	cleanPath, err := verifyRegularAudioPath(rawPath)
	if err != nil {
		p.mu.Lock()
		p.state.Track = nil
		p.state.Playing = false
		p.state.Position = 0
		p.state.Error = provider.Clean(err.Error())
		s := p.state
		p.mu.Unlock()
		p.broadcast(s)
		return
	}
	uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(cleanPath)}).String()
	p.gst.PlayURI(uri)
	p.mu.Lock()
	p.ended = false
	p.state.Error = ""
	p.state.Track = &t
	p.state.Playing = true
	p.state.Position = 0
	s := p.state
	p.mu.Unlock()
	p.broadcast(s)
}

func (p *Player) Play() error {
	p.mu.Lock()
	if (p.ended || p.state.Track == nil) && len(p.queue) > 0 {
		if p.ended || p.idx < 0 || p.idx >= len(p.queue) {
			p.idx = 0
		}
		p.ended = false
		t := p.queue[p.idx]
		p.mu.Unlock()
		p.playTrack(t)
		return nil
	}
	p.state.Playing = true
	s := p.state
	p.mu.Unlock()
	p.gst.Play()
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
	if p.idx < 0 || p.idx >= len(p.queue) {
		p.idx = 0
	}
	if p.state.RepeatMode == player.RepeatModeOne {
		t := p.queue[p.idx]
		p.mu.Unlock()
		p.playTrack(t)
		return nil
	}
	if p.state.RepeatMode == player.RepeatModeOff && p.idx >= len(p.queue)-1 {
		p.ended = true
		p.state.Playing = false
		p.state.Position = 0
		s := p.state
		p.mu.Unlock()
		p.gst.Stop()
		p.broadcast(s)
		return nil
	}
	p.idx = (p.idx + 1) % len(p.queue)
	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) Previous() error {
	p.mu.RLock()
	empty := len(p.queue) == 0
	p.mu.RUnlock()
	if empty {
		return nil
	}
	if p.gst.Position() > 3*time.Second {
		p.gst.Seek(0)
		return nil
	}
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}
	if p.idx <= 0 || p.idx >= len(p.queue) {
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
	p.mu.Lock()
	newQueue := tracksForIDs(p.allTracks, ids)
	if len(newQueue) == 0 {
		p.mu.Unlock()
		return nil
	}
	p.queue = newQueue
	p.idx = 0
	t := p.queue[0]
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
	p.mu.Lock()
	extra := tracksForIDs(p.allTracks, ids)
	if len(extra) == 0 {
		p.mu.Unlock()
		return nil
	}
	wasEmpty := len(p.queue) == 0
	wasEnded := p.ended && !p.state.Playing
	firstNewIdx := len(p.queue)
	p.queue = append(p.queue, extra...)
	if wasEmpty {
		p.idx = 0
		p.ended = false
	} else if wasEnded {
		p.idx = firstNewIdx
		p.ended = false
	}
	t := extra[0]
	p.mu.Unlock()
	if wasEmpty || wasEnded {
		p.playTrack(t)
	}
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

func (p *Player) SetEqualizer(bands []player.EQBand) error {
	if p.gst != nil {
		gains := make([]float64, len(bands))
		for i, b := range bands {
			gains[i] = b.Gain
		}
		p.gst.SetEqualizer(gains)
	}
	return nil
}

func (p *Player) RemoveFromQueue(idx int) error {
	p.mu.Lock()
	var stopPlayback bool
	if idx >= 0 && idx < len(p.queue) {
		p.queue = append(p.queue[:idx], p.queue[idx+1:]...)
		switch {
		case idx == p.idx:
			p.state.Track = nil
			p.state.Playing = false
			p.state.Position = 0
			if len(p.queue) == 0 || p.idx >= len(p.queue) {
				p.idx = 0
			}
			stopPlayback = true
		case idx < p.idx:
			p.idx--
		}
	}
	s := p.state
	p.mu.Unlock()
	if stopPlayback {
		p.gst.Stop()
	}
	p.broadcast(s)
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

		switch {
		case from == p.idx:
			p.idx = to
		case from < p.idx && to >= p.idx:
			p.idx--
		case from > p.idx && to <= p.idx:
			p.idx++
		}
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
	return p.bcast.Subscribe()
}

func (p *Player) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		close(p.done)
		p.bcast.Close()
		if p.gst != nil {
			p.gst.Destroy()
		}
	})
	return nil
}

// This replaces the internal queue with the given tracks. Called after
// the provider scans the music directory
func (p *Player) LoadTracks(tracks []provider.Track) {
	p.mu.Lock()
	p.allTracks = append([]provider.Track{}, tracks...)
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
	var ticker *time.Ticker
	var tickC <-chan time.Time
	stopTicker := func() {
		if ticker != nil {
			ticker.Stop()
			ticker = nil
			tickC = nil
		}
	}
	defer stopTicker()

	syncTicker := func() {
		p.mu.RLock()
		playing := p.state.Playing
		p.mu.RUnlock()
		if playing {
			if ticker == nil {
				ticker = time.NewTicker(500 * time.Millisecond)
				tickC = ticker.C
			}
		} else {
			stopTicker()
		}
	}

	syncTicker()
	for {
		select {
		case <-p.done:
			return
		case <-p.pollWakeCh:
			syncTicker()
		case <-tickC:
			p.mu.RLock()
			playing := p.state.Playing
			p.mu.RUnlock()
			if !playing {
				stopTicker()
				continue
			}
			pos := p.gst.Position()
			p.mu.Lock()
			if p.state.Playing {
				p.state.Position = pos
			}
			s := p.state
			p.mu.Unlock()
			p.broadcast(s)
		}
	}
}
