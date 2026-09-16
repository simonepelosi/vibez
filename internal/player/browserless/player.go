//go:build linux || darwin

package browserless

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/audioquality"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/player/browserless/cdm"
	"github.com/simone-vibes/vibez/internal/player/browserless/license"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/provider/apple"
)

type audioSink interface {
	PlayURI(uri string)
	Play()
	Pause()
	Stop()
	Seek(d time.Duration)
	SetVolume(v float64)
	Position() time.Duration
	OnEOS(fn func())
	OnError(fn func(error))
	Destroy()
}

// Player implements player.Player without a web browser, using an in-process
// Widevine CDM and audio streaming pipeline.
type Player struct {
	cfg       *config.Config
	provider  *apple.AppleProvider
	cdmEngine *cdm.CDM
	licClient *license.Client
	streamer  *StreamServer
	sink      audioSink

	mu        sync.RWMutex
	state     player.State
	bcast     player.Broadcast
	queue     []provider.Track
	idx       int
	playCtx   context.CancelFunc
	doneCh    chan struct{}
	closeOnce sync.Once
}

// New creates a ready browserless Player.
func New(cfg *config.Config, prov *apple.AppleProvider) (*Player, error) {
	cdmPath, err := EnsureCDM()
	if err != nil {
		return nil, fmt.Errorf("browserless cdm: %w", err)
	}

	cdmEngine, err := cdm.New(cdmPath)
	if err != nil {
		return nil, fmt.Errorf("browserless init cdm: %w", err)
	}

	licClient := license.NewClient(cfg)
	streamer, err := NewStreamServer(cdmEngine, licClient)
	if err != nil {
		_ = cdmEngine.Close()
		return nil, fmt.Errorf("browserless stream server: %w", err)
	}

	sink, err := newAudioSink()
	if err != nil {
		_ = streamer.Close()
		_ = cdmEngine.Close()
		return nil, fmt.Errorf("browserless audio sink: %w", err)
	}

	p := &Player{
		cfg:       cfg,
		provider:  prov,
		cdmEngine: cdmEngine,
		licClient: licClient,
		streamer:  streamer,
		sink:      sink,
		doneCh:    make(chan struct{}),
		state: player.State{
			Volume:  1.0,
			Bitrate: 256,
		},
	}

	p.sink.OnEOS(func() {
		p.mu.RLock()
		playing := p.state.Playing
		pos := p.sink.Position()
		var dur time.Duration
		if p.state.Track != nil {
			dur = p.state.Track.Duration
		}
		p.mu.RUnlock()

		if !playing {
			return
		}
		if dur > 5*time.Second && pos < dur-5*time.Second {
			// Ignore premature EOS
			return
		}
		_ = p.Next()
	})

	p.sink.OnError(func(e error) {
		p.mu.Lock()
		p.state.Error = e.Error()
		p.state.Playing = false
		p.state.Loading = false
		s := p.state
		p.mu.Unlock()
		p.bcast.Send(s)
	})

	go p.pollPosition()

	return p, nil
}

func (p *Player) pollPosition() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.doneCh:
			return
		case <-ticker.C:
			p.mu.RLock()
			playing := p.state.Playing
			loading := p.state.Loading
			p.mu.RUnlock()

			if playing && !loading {
				pos := p.sink.Position()
				p.mu.Lock()
				p.state.Position = pos
				s := p.state
				p.mu.Unlock()
				p.bcast.Send(s)
			}
		}
	}
}

func (p *Player) playTrack(t provider.Track) {
	p.mu.Lock()
	if p.playCtx != nil {
		p.playCtx()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.playCtx = cancel

	p.state.Track = &t
	p.state.Loading = true
	p.state.Playing = false
	p.state.Position = 0
	p.state.Error = ""
	s := p.state
	prefer256k := p.state.Bitrate >= 256
	p.mu.Unlock()
	p.bcast.Send(s)

	go func() {
		catalogID := t.CatalogID
		if catalogID == "" {
			catalogID = t.ID
		}
		// If it's a library ID (starts with "i."), resolve catalog ID
		if strings.HasPrefix(catalogID, "i.") && p.provider != nil {
			resolved, err := p.provider.ResolveCatalogSongID(ctx, catalogID)
			if err == nil && resolved != "" {
				catalogID = resolved
			}
		}

		streamURL, duration, err := p.streamer.PrepareTrack(ctx, catalogID, prefer256k)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			p.mu.Lock()
			p.state.Loading = false
			p.state.Error = err.Error()
			s := p.state
			p.mu.Unlock()
			p.bcast.Send(s)
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		p.sink.PlayURI(streamURL)

		p.mu.Lock()
		p.state.Loading = false
		p.state.Playing = true
		if duration > 0 && p.state.Track != nil && p.state.Track.Duration == 0 {
			p.state.Track.Duration = duration
		}
		s = p.state
		p.mu.Unlock()
		p.bcast.Send(s)
	}()
}

// ── player.Player Implementation ───────────────────────────────────────────

func (p *Player) Play() error {
	p.sink.Play()
	p.mu.Lock()
	p.state.Playing = true
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

func (p *Player) Pause() error {
	p.sink.Pause()
	p.mu.Lock()
	p.state.Playing = false
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

func (p *Player) Stop() error {
	p.sink.Stop()
	p.mu.Lock()
	if p.playCtx != nil {
		p.playCtx()
	}
	p.state.Playing = false
	p.state.Loading = false
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

func (p *Player) Next() error {
	p.mu.Lock()
	if len(p.queue) == 0 {
		p.mu.Unlock()
		return nil
	}

	if p.state.RepeatMode == player.RepeatModeOne {
		t := p.queue[p.idx]
		p.mu.Unlock()
		p.playTrack(t)
		return nil
	}

	switch {
	case p.state.ShuffleMode && len(p.queue) > 1:
		p.idx = rand.Intn(len(p.queue)) //nolint:gosec // G404: weak random is sufficient for music shuffle
	case p.state.RepeatMode == player.RepeatModeOff && p.idx >= len(p.queue)-1:
		p.state.Playing = false
		s := p.state
		p.mu.Unlock()
		p.bcast.Send(s)
		return nil
	default:
		p.idx = (p.idx + 1) % len(p.queue)
	}

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

	if p.sink.Position() > 3*time.Second {
		p.mu.Unlock()
		p.sink.Seek(0)
		return nil
	}

	if p.idx <= 0 {
		p.idx = len(p.queue) - 1
	} else {
		p.idx--
	}

	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) Seek(position time.Duration) error {
	p.sink.Seek(position)
	return nil
}

func (p *Player) SetVolume(v float64) error {
	p.sink.SetVolume(v)
	p.mu.Lock()
	p.state.Volume = v
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

func (p *Player) SetAudioBitrate(kbps int) error {
	if err := audioquality.Validate(kbps); err != nil {
		return err
	}
	p.mu.Lock()
	p.state.Bitrate = kbps
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

func (p *Player) SetQueue(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var tracks []provider.Track
	if p.provider != nil {
		fetched, err := p.provider.GetCatalogTracks(ctx, ids)
		if err == nil && len(fetched) > 0 {
			tracks = fetched
		}
	}

	if len(tracks) == 0 {
		// Fallback placeholder track objects with just IDs
		tracks = make([]provider.Track, len(ids))
		for i, id := range ids {
			tracks[i] = provider.Track{ID: id, CatalogID: id}
		}
	}

	p.mu.Lock()
	p.queue = tracks
	p.idx = 0
	t := p.queue[0]
	p.mu.Unlock()

	p.playTrack(t)
	return nil
}

func (p *Player) SetPlaylist(playlistID string, startIdx int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var tracks []provider.Track
	if p.provider != nil {
		fetched, err := p.provider.GetPlaylistTracks(ctx, playlistID)
		if err == nil && len(fetched) > 0 {
			tracks = fetched
		}
	}

	p.mu.Lock()
	if len(tracks) > 0 {
		p.queue = tracks
	}
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
	if len(ids) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var tracks []provider.Track
	if p.provider != nil {
		fetched, err := p.provider.GetCatalogTracks(ctx, ids)
		if err == nil {
			tracks = fetched
		}
	}
	if len(tracks) == 0 {
		tracks = make([]provider.Track, len(ids))
		for i, id := range ids {
			tracks[i] = provider.Track{ID: id, CatalogID: id}
		}
	}

	p.mu.Lock()
	wasEmpty := len(p.queue) == 0
	p.queue = append(p.queue, tracks...)
	p.mu.Unlock()

	if wasEmpty && len(tracks) > 0 {
		p.playTrack(tracks[0])
	}
	return nil
}

func (p *Player) SetRepeat(mode int) error {
	p.mu.Lock()
	p.state.RepeatMode = mode
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

func (p *Player) SetShuffle(on bool) error {
	p.mu.Lock()
	p.state.ShuffleMode = on
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

func (p *Player) SetEqualizer(_ []player.EQBand) error {
	return nil
}

func (p *Player) RemoveFromQueue(idx int) error {
	p.mu.Lock()
	if idx >= 0 && idx < len(p.queue) {
		p.queue = append(p.queue[:idx], p.queue[idx+1:]...)
		switch {
		case idx == p.idx:
			p.state.Track = nil
			p.state.Playing = false
			if p.sink != nil {
				p.sink.Stop()
			}
			if p.idx >= len(p.queue) && len(p.queue) > 0 {
				p.idx = len(p.queue) - 1
			}
		case idx < p.idx:
			p.idx--
		}
		if p.idx >= len(p.queue) && len(p.queue) > 0 {
			p.idx = len(p.queue) - 1
		}
	}
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
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
		case p.idx == from:
			p.idx = to
		case from < p.idx && to >= p.idx:
			p.idx--
		case from > p.idx && to <= p.idx:
			p.idx++
		}
	}
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

func (p *Player) Queue() []provider.Track {
	p.mu.RLock()
	defer p.mu.RUnlock()
	res := make([]provider.Track, len(p.queue))
	copy(res, p.queue)
	return res
}

func (p *Player) QueueIndex() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.idx
}

func (p *Player) PlayQueueIndex(idx int) error {
	p.mu.Lock()
	if idx < 0 || idx >= len(p.queue) {
		p.mu.Unlock()
		return fmt.Errorf("queue index out of bounds: %d", idx)
	}
	p.idx = idx
	t := p.queue[p.idx]
	p.mu.Unlock()
	p.playTrack(t)
	return nil
}

func (p *Player) ClearQueue() error {
	p.mu.Lock()
	p.queue = nil
	p.idx = 0
	p.state.Track = nil
	p.state.Playing = false
	if p.sink != nil {
		p.sink.Stop()
	}
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

func (p *Player) GetState() (*player.State, error) {
	p.mu.RLock()
	s := p.state
	p.mu.RUnlock()
	return &s, nil
}

func (p *Player) Subscribe() <-chan player.State {
	return p.bcast.Subscribe()
}

func (p *Player) Close() error {
	p.closeOnce.Do(func() {
		close(p.doneCh)
		if p.sink != nil {
			p.sink.Destroy()
		}
		if p.streamer != nil {
			_ = p.streamer.Close()
		}
		if p.cdmEngine != nil {
			_ = p.cdmEngine.Close()
		}
	})
	return nil
}
