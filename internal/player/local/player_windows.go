//go:build windows

// Package local provides a Player for the audio files in the user's music
// directory. On Windows it decodes and plays them natively through Media
// Foundation and waveOut with a 10-band equalizer. No browser, WebView, or
// Apple credentials are involved.
package local

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/simone-vibes/vibez/internal/audioquality"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

var (
	errClosed        = errors.New("local player: closed")
	errEmptyQueue    = errors.New("local player: queue is empty")
	errNothingLoaded = errors.New("local player: no track is loaded")
)

// restartThreshold is how far into a track Previous restarts it rather than
// going back a track.
const restartThreshold = 3 * time.Second

// maxEQGain bounds an EQ band's gain in dB either way, the range the
// equalizer panel offers.
const maxEQGain = 12.0

// snapshot is what the native engine reports about the loaded track, both as
// the reply to a command and as an asynchronous event. Gen names the track it
// describes (0 when none is loaded) and Seq orders snapshots across both paths.
type snapshot struct {
	Gen      uint64
	Seq      uint64
	Kind     string // event type ("ended", "error", "timeupdate", ...); empty in command replies
	Playing  bool
	Loading  bool
	Position float64 // seconds
	Duration float64 // seconds; 0 until known
	Error    string  // why the command or the decoder failed
}

// engine runs commands on the native Windows audio sink. A command that fails
// for a media reason returns a snapshot with Error set; an error return means
// the audio engine itself could not be used.
type engine interface {
	call(method string, args ...any) (snapshot, error)
	close()
}

// Player implements player.Player for local audio files played via Media
// Foundation and waveOut on Windows.
type Player struct {
	eng   engine
	bcast player.Broadcast

	cmdMu sync.Mutex
	mu    sync.Mutex

	library map[string]provider.Track // registered tracks by ID
	q       queue
	state   player.State // Error and Logs are never stored, only sent
	gen     uint64       // generation of the loaded track; 0 when none is
	lastGen uint64
	seq     uint64 // newest engine snapshot applied
	ended   bool   // the loaded track finished with nothing queued after it
	eq      []any  // equalizer bands awaiting applyEqualizer
	eqDirty bool
	closed  bool

	closeOnce sync.Once
}

// New starts a native Windows audio engine for local playback. Nothing plays
// until the queue is loaded and started.
func New(_ func(string)) (*Player, error) {
	p := newPlayer()
	eng, err := openNativeEngine(p)
	if err != nil {
		return nil, fmt.Errorf("local player: %w", err)
	}
	p.eng = eng
	return p, nil
}

func newPlayer() *Player {
	return &Player{
		library: map[string]provider.Track{},
		q:       newQueue(),
		state:   player.State{Volume: 1},
	}
}

// LoadTracks registers the scanned library and queues all of it without
// starting playback. Anything loaded before is stopped.
func (p *Player) LoadTracks(tracks []provider.Track) {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.library = make(map[string]provider.Track, len(tracks))
	for _, t := range tracks {
		p.library[t.ID] = t
	}
	p.q.reset(tracks)
	p.mu.Unlock()
	if err := p.unload(); err != nil {
		p.sendError(err)
	}
}

// ── player.Player ─────────────────────────────────────────────────────────

// Play resumes the loaded track, or starts the current queue entry when
// nothing is loaded.
func (p *Player) Play() error {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	gen, err := p.loadedGen()
	if err != nil {
		return err
	}
	if gen == 0 {
		return p.loadCurrent(true)
	}
	return p.command("play")
}

func (p *Player) Pause() error {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	gen, err := p.loadedGen()
	if err != nil || gen == 0 {
		return err
	}
	return p.command("pause")
}

// Stop pauses and rewinds the loaded track.
func (p *Player) Stop() error {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	gen, err := p.loadedGen()
	if err != nil || gen == 0 {
		return err
	}
	return p.command("stop")
}

// Next skips to the next track. At the end of the queue with repeat off it
// leaves playback as it is.
func (p *Player) Next() error {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	p.mu.Lock()
	if err := p.usableLocked(); err != nil {
		p.mu.Unlock()
		return err
	}
	if !p.q.advance(false) {
		s := p.state
		p.mu.Unlock()
		// The TUI marks itself loading on a skip; tell it nothing changed.
		p.bcast.Send(s)
		return nil
	}
	p.mu.Unlock()
	return p.loadCurrent(true)
}

// Previous restarts a track that has played past restartThreshold and
// otherwise goes back one.
func (p *Player) Previous() error {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	p.mu.Lock()
	if err := p.usableLocked(); err != nil {
		p.mu.Unlock()
		return err
	}
	if p.gen != 0 && p.state.Position > restartThreshold {
		p.mu.Unlock()
		return p.command("seek", 0.0)
	}
	p.q.previous()
	p.mu.Unlock()
	return p.loadCurrent(true)
}

func (p *Player) Seek(position time.Duration) error {
	if position < 0 {
		return fmt.Errorf("local player: seek to negative position %v", position)
	}
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	gen, err := p.loadedGen()
	if err != nil {
		return err
	}
	if gen == 0 {
		return errNothingLoaded
	}
	return p.command("seek", position.Seconds())
}

func (p *Player) SetVolume(v float64) error {
	if math.IsNaN(v) || v < 0 || v > 1 {
		return fmt.Errorf("local player: volume %v is outside 0–1", v)
	}
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	if _, err := p.loadedGen(); err != nil {
		return err
	}
	if err := p.command("setVolume", v); err != nil {
		return err
	}
	p.mu.Lock()
	p.state.Volume = v
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

// SetAudioBitrate validates kbps but cannot apply it: local files play at
// whatever bitrate they were encoded at.
func (p *Player) SetAudioBitrate(kbps int) error {
	if err := audioquality.Validate(kbps); err != nil {
		return err
	}
	return player.ErrAudioBitrateSavedPreferenceOnly
}

// SetQueue replaces the queue with the library tracks ids names and plays the
// first. An ID outside the loaded library is an error and leaves the queue
// untouched, since the TUI's copy of the queue would no longer line up.
func (p *Player) SetQueue(ids []string) error {
	if len(ids) == 0 {
		return errors.New("local player: no tracks to play")
	}
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errClosed
	}
	tracks, err := p.resolveLocked(ids)
	if err != nil {
		p.mu.Unlock()
		return err
	}
	p.q.reset(tracks)
	p.mu.Unlock()
	return p.loadCurrent(true)
}

// SetPlaylist fails: the local library has no playlists, and the TUI never
// offers one in local mode.
func (p *Player) SetPlaylist(playlistID string, _ int) error {
	return fmt.Errorf("local player: playlist %q: the local library has no playlists", playlistID)
}

// AppendQueue adds library tracks to the end of the queue. If nothing is
// loaded, or the queue had already played out, the first of them starts.
func (p *Player) AppendQueue(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errClosed
	}
	tracks, err := p.resolveLocked(ids)
	if err != nil {
		p.mu.Unlock()
		return err
	}
	start := p.gen == 0 || p.ended
	p.q.add(tracks, start)
	p.mu.Unlock()
	if !start {
		return nil
	}
	return p.loadCurrent(true)
}

func (p *Player) SetRepeat(mode int) error {
	if mode < player.RepeatModeOff || mode > player.RepeatModeAll {
		return fmt.Errorf("local player: unknown repeat mode %d", mode)
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errClosed
	}
	p.q.repeat = mode
	p.state.RepeatMode = mode
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

// SetShuffle turns shuffle on or off. Queue indices are unaffected; only the
// order tracks play in changes.
func (p *Player) SetShuffle(on bool) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errClosed
	}
	p.q.setShuffle(on)
	p.state.ShuffleMode = on
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	return nil
}

// SetEqualizer validates bands and applies them in the background. The TUI
// calls it from its update loop, which must not wait on a load that holds the
// command lock.
func (p *Player) SetEqualizer(bands []player.EQBand) error {
	arg, err := eqArg(bands)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errClosed
	}
	p.eq = arg
	p.eqDirty = true
	p.mu.Unlock()
	go p.applyEqualizer()
	return nil
}

// applyEqualizer sends the newest requested bands to the engine. Each call
// takes whatever is newest rather than the bands it was started for, so a
// burst of changes cannot land out of order.
func (p *Player) applyEqualizer() {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	p.mu.Lock()
	if p.closed || !p.eqDirty {
		p.mu.Unlock()
		return
	}
	arg := p.eq
	p.eqDirty = false
	p.mu.Unlock()
	if err := p.command("setEqualizer", arg); err != nil {
		p.sendError(err)
	}
}

// RemoveFromQueue removes the entry at idx. Removing the loaded track hands
// over to the entry that takes its place, playing only if it was playing;
// with none to take over, playback stops.
func (p *Player) RemoveFromQueue(idx int) error {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errClosed
	}
	wasCurrent, succeeded, ok := p.q.remove(idx)
	if !ok {
		p.mu.Unlock()
		return fmt.Errorf("local player: queue index %d out of range", idx)
	}
	if !wasCurrent || p.gen == 0 {
		p.mu.Unlock()
		return nil
	}
	autoplay := p.state.Playing || p.state.Loading
	p.mu.Unlock()
	if succeeded {
		return p.loadCurrent(autoplay)
	}
	return p.unload()
}

// MoveInQueue moves the entry at from to index to without touching playback.
func (p *Player) MoveInQueue(from, to int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errClosed
	}
	if !p.q.move(from, to) {
		return fmt.Errorf("local player: cannot move queue entry %d to %d in a queue of %d", from, to, len(p.q.entries))
	}
	return nil
}

func (p *Player) ClearQueue() error {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errClosed
	}
	p.q.clear()
	p.mu.Unlock()
	return p.unload()
}

func (p *Player) GetState() (*player.State, error) {
	p.mu.Lock()
	s := p.state
	p.mu.Unlock()
	return &s, nil
}

func (p *Player) Subscribe() <-chan player.State {
	return p.bcast.Subscribe()
}

// Close stops the native audio sink, then closes every subscription. It is
// safe to call more than once.
func (p *Player) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.gen = 0
		p.mu.Unlock()
		if p.eng != nil {
			p.eng.close()
		}
		p.cmdMu.Lock()
		p.bcast.Close()
		p.cmdMu.Unlock()
	})
	return nil
}

// ── internals ─────────────────────────────────────────────────────────────

// loadedGen reports the loaded generation, failing once the player is closed.
func (p *Player) loadedGen() (uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, errClosed
	}
	return p.gen, nil
}

// usableLocked fails when the player is closed or the queue is empty.
func (p *Player) usableLocked() error {
	if p.closed {
		return errClosed
	}
	if p.q.cur < 0 {
		return errEmptyQueue
	}
	return nil
}

func (p *Player) resolveLocked(ids []string) ([]provider.Track, error) {
	tracks := make([]provider.Track, len(ids))
	for i, id := range ids {
		t, ok := p.library[id]
		if !ok {
			return nil, fmt.Errorf("local player: track %q is not in the local library", id)
		}
		tracks[i] = t
	}
	return tracks, nil
}

// loadCurrent loads the current queue entry under a new generation, which
// retires every snapshot of the track it replaces. The caller holds cmdMu.
func (p *Player) loadCurrent(autoplay bool) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errClosed
	}
	t, ok := p.q.current()
	if !ok {
		p.mu.Unlock()
		return errEmptyQueue
	}
	path, hasPrefix := strings.CutPrefix(t.ID, "local:")
	if !hasPrefix || path == "" {
		p.mu.Unlock()
		if err := p.unload(); err != nil {
			return err
		}
		return fmt.Errorf("local player: %q is not a local library file", t.ID)
	}
	p.lastGen++
	gen := p.lastGen
	p.gen = gen
	p.ended = false
	p.state.Track = &t
	p.state.Playing = false
	p.state.Loading = autoplay
	p.state.Position = 0
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)

	snap, err := p.eng.call("load", path, gen, autoplay)
	if err != nil {
		p.stalled(gen)
		return fmt.Errorf("local player: load %q: %w", t.Title, err)
	}
	p.apply(snap)
	if snap.Error != "" {
		return fmt.Errorf("local player: cannot play %q: %s", t.Title, snap.Error)
	}
	return nil
}

// command runs method on the loaded track and applies the engine's reply. The
// caller holds cmdMu.
func (p *Player) command(method string, args ...any) error {
	p.mu.Lock()
	gen := p.gen
	p.mu.Unlock()
	snap, err := p.eng.call(method, args...)
	if err != nil {
		p.stalled(gen)
		return fmt.Errorf("local player: %s: %w", method, err)
	}
	p.apply(snap)
	if snap.Error != "" {
		return fmt.Errorf("local player: %s: %s", method, snap.Error)
	}
	return nil
}

// unload drops the loaded track, if any. The caller holds cmdMu.
func (p *Player) unload() error {
	p.mu.Lock()
	loaded := p.gen != 0
	p.gen = 0
	p.ended = false
	p.state.Track = nil
	p.state.Playing = false
	p.state.Loading = false
	p.state.Position = 0
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
	if !loaded {
		return nil
	}
	if _, err := p.eng.call("unload"); err != nil {
		return fmt.Errorf("local player: unload: %w", err)
	}
	return nil
}

// stalled records that the engine could not be reached about generation gen,
// so the state stops claiming it plays.
func (p *Player) stalled(gen uint64) {
	p.mu.Lock()
	if p.gen != gen || (!p.state.Playing && !p.state.Loading) {
		p.mu.Unlock()
		return
	}
	p.state.Playing = false
	p.state.Loading = false
	s := p.state
	p.mu.Unlock()
	p.bcast.Send(s)
}

func (p *Player) apply(snap snapshot) {
	p.mu.Lock()
	applied := p.applyLocked(snap)
	s := p.state
	p.mu.Unlock()
	if applied {
		p.bcast.Send(s)
	}
}

// applyLocked copies a snapshot of the loaded generation into the state
// unless a newer one has been applied already. It reports whether it did.
func (p *Player) applyLocked(snap snapshot) bool {
	if snap.Gen == 0 || snap.Gen != p.gen || snap.Seq <= p.seq {
		return false
	}
	p.seq = snap.Seq
	p.state.Playing = snap.Playing
	p.state.Loading = snap.Loading
	p.state.Position = seconds(snap.Position)
	if d := seconds(snap.Duration); d > 0 && p.state.Track != nil && p.state.Track.Duration != d {
		// Published states share the Track pointer, so it is replaced, never
		// written through.
		t := *p.state.Track
		t.Duration = d
		p.state.Track = &t
	}
	if snap.Playing || snap.Loading {
		p.ended = false
	}
	return true
}

// onEvent handles an event the engine pushes. The advance after a track ends
// is handed to its own goroutine so callbacks never contend on cmdMu.
func (p *Player) onEvent(snap snapshot) {
	p.mu.Lock()
	if p.closed || snap.Gen == 0 || snap.Gen != p.gen {
		p.mu.Unlock()
		return
	}
	applied := p.applyLocked(snap)
	s := p.state
	p.mu.Unlock()
	switch snap.Kind {
	case "error":
		title := ""
		if s.Track != nil {
			title = s.Track.Title
		}
		s.Error = fmt.Sprintf("local player: %q: %s", title, snap.Error)
		p.bcast.Send(s)
		return
	case "ended":
		go p.advanceAfterEnd(snap.Gen)
	}
	if applied {
		p.bcast.Send(s)
	}
}

// advanceAfterEnd moves on from generation gen, which played to its end,
// unless something else has taken over since: another load, or a Play that
// restarted it.
func (p *Player) advanceAfterEnd(gen uint64) {
	p.cmdMu.Lock()
	defer p.cmdMu.Unlock()
	p.mu.Lock()
	if p.closed || p.gen != gen || p.state.Playing || p.state.Loading {
		p.mu.Unlock()
		return
	}
	if !p.q.advance(true) {
		p.ended = true
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	if err := p.loadCurrent(true); err != nil {
		p.sendError(err)
	}
}

// sendError broadcasts err once without storing it in the state.
func (p *Player) sendError(err error) {
	p.mu.Lock()
	s := p.state
	p.mu.Unlock()
	s.Error = err.Error()
	p.bcast.Send(s)
}

// eqArg validates bands and copies them into a fresh slice. The copy matters:
// the TUI keeps mutating the slice it passes in.
func eqArg(bands []player.EQBand) ([]any, error) {
	out := make([]any, len(bands))
	for i, b := range bands {
		switch {
		case !finite(b.Frequency) || b.Frequency <= 0:
			return nil, fmt.Errorf("local player: EQ band %d: frequency %v Hz must be positive", i, b.Frequency)
		case !finite(b.Q) || b.Q < 0:
			return nil, fmt.Errorf("local player: EQ band %d: Q %v must not be negative", i, b.Q)
		case !finite(b.Gain) || math.Abs(b.Gain) > maxEQGain:
			return nil, fmt.Errorf("local player: EQ band %d: gain %v dB is outside ±%v dB", i, b.Gain, maxEQGain)
		}
		out[i] = map[string]any{"frequency": b.Frequency, "q": b.Q, "gain": b.Gain}
	}
	return out, nil
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func seconds(s float64) time.Duration {
	if !finite(s) || s <= 0 {
		return 0
	}
	return time.Duration(s * float64(time.Second))
}
