//go:build linux

package mpris

// Server exports vibez as an MPRIS2 media player on the D-Bus session bus.
// Desktop environments (GNOME, KDE, …) discover it as "vibez" in their
// media panels and send play/pause/next/prev actions to it.
//
// Usage:
//
//	srv, err := NewServer(audioEngine)
//	if err != nil { /* D-Bus unavailable — non-fatal */ }
//	defer srv.Close()
//	go func() { for st := range audioEngine.Subscribe() { srv.Update(st) } }()

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

const (
	mprisServiceName = "org.mpris.MediaPlayer2.vibez"
	mprisObjectPath  = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisRootIface   = "org.mpris.MediaPlayer2"
	mprisPlayerIface = "org.mpris.MediaPlayer2.Player"
	noTrackPath      = dbus.ObjectPath("/org/vibez/track/none")
)

// Controller is the subset of player.Player that the MPRIS server delegates to.
type Controller interface {
	Play() error
	Pause() error
	Next() error
	Previous() error
	Seek(time.Duration) error
	SetRepeat(mode int) error
	SetShuffle(on bool) error
}

// Server is the MPRIS D-Bus server for vibez. Create with NewServer.
type Server struct {
	conn  *dbus.Conn
	props *prop.Properties

	mu                   sync.Mutex
	flushMu              sync.Mutex
	pos                  time.Duration
	lastStatus           string
	lastTrackID          string
	lastPos              time.Duration // position at the previous flush, for seek detection
	lastPosAt            time.Time     // wall-clock time lastPos was sampled
	currentTrackPath     dbus.ObjectPath
	currentTrackDuration time.Duration
	hasCurrentTrack      bool
	debounce             *time.Timer
	pending              *player.State
	unavailable          bool
	closed               bool
}

// MusicKit can expose a partial now-playing item with no ID during a track
// transition. Hashing the strongest available identity keeps paths valid and
// bounded without conflating IDs that differ only by D-Bus path punctuation.
func trackObjectPath(track *provider.Track) dbus.ObjectPath {
	if track == nil {
		return noTrackPath
	}

	namespace := "id"
	identity := track.ID
	if identity == "" {
		namespace = "catalog"
		identity = track.CatalogID
	}
	var sum [sha256.Size]byte
	if identity == "" {
		namespace = "unknown"
		hash := sha256.New()
		var fieldLength [8]byte
		for _, field := range []string{track.Title, track.Artist, track.Album} {
			binary.BigEndian.PutUint64(fieldLength[:], uint64(len(field)))
			_, _ = hash.Write(fieldLength[:])
			_, _ = hash.Write([]byte(field))
		}
		// int64 -> uint64 reinterprets the bits rather than losing them, so the
		// hash stays injective over Duration. Clamping a negative value would
		// instead fold distinct tracks onto the same digest.
		binary.BigEndian.PutUint64(fieldLength[:], uint64(track.Duration)) //nolint:gosec // bijective conversion; hash input only
		_, _ = hash.Write(fieldLength[:])
		copy(sum[:], hash.Sum(nil))
	} else {
		sum = sha256.Sum256([]byte(identity))
	}
	path := dbus.ObjectPath(fmt.Sprintf("/org/vibez/track/%s_%x", namespace, sum))
	if !path.IsValid() {
		panic(fmt.Sprintf("mpris: generated invalid track path %q", path))
	}
	return path
}

// ── D-Bus method objects ──────────────────────────────────────────────────

type rootObj struct{}

func (*rootObj) Raise() *dbus.Error { return nil }
func (*rootObj) Quit() *dbus.Error  { return nil }

type playerObj struct {
	ctrl Controller
	srv  *Server
}

func (p *playerObj) Next() *dbus.Error     { _ = p.ctrl.Next(); return nil }
func (p *playerObj) Previous() *dbus.Error { _ = p.ctrl.Previous(); return nil }
func (p *playerObj) Pause() *dbus.Error    { _ = p.ctrl.Pause(); return nil }
func (p *playerObj) Stop() *dbus.Error     { _ = p.ctrl.Pause(); return nil }
func (p *playerObj) Play() *dbus.Error     { _ = p.ctrl.Play(); return nil }

func (p *playerObj) PlayPause() *dbus.Error {
	status, _ := p.srv.props.GetMust(mprisPlayerIface, "PlaybackStatus").(string)
	if status == "Playing" {
		return p.Pause()
	}
	return p.Play()
}

// seekRelative moves the playhead by offsetUs microseconds (relative, per MPRIS spec).
// Registered as D-Bus method "Seek" via ExportMethodTable to avoid go vet
// clash with the io.Seeker interface signature.
func (p *playerObj) seekRelative(offsetUs int64) *dbus.Error {
	p.srv.mu.Lock()
	newPos := p.srv.pos + time.Duration(offsetUs)*time.Microsecond
	p.srv.mu.Unlock()
	_ = p.ctrl.Seek(newPos)
	return nil
}

// setPosition seeks to an absolute position (µs) only when the supplied track
// is still current. MPRIS clients can race a track transition with this call.
func (p *playerObj) setPosition(trackPath dbus.ObjectPath, posUs int64) *dbus.Error {
	const maxPositionUs = int64(time.Duration(1<<63-1) / time.Microsecond)
	if posUs < 0 || posUs > maxPositionUs {
		return nil
	}
	position := time.Duration(posUs) * time.Microsecond

	p.srv.mu.Lock()
	isCurrent := p.srv.hasCurrentTrack && trackPath == p.srv.currentTrackPath
	withinTrack := p.srv.currentTrackDuration == 0 || position <= p.srv.currentTrackDuration
	p.srv.mu.Unlock()
	if isCurrent && withinTrack {
		_ = p.ctrl.Seek(position)
	}
	return nil
}

func (p *playerObj) openUri(_ string) *dbus.Error { return nil }

// ── Constructor ───────────────────────────────────────────────────────────

// NewServer registers org.mpris.MediaPlayer2.vibez on the session bus and
// returns the server. Call Close when done.
func NewServer(ctrl Controller) (*Server, error) {
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		return nil, fmt.Errorf("mpris: session bus: %w", err)
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mpris: auth: %w", err)
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mpris: hello: %w", err)
	}

	srv := &Server{conn: conn}
	pobj := &playerObj{ctrl: ctrl, srv: srv}

	if err := conn.Export(&rootObj{}, mprisObjectPath, mprisRootIface); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mpris: export root: %w", err)
	}
	// Use ExportMethodTable to explicitly map D-Bus method names → Go functions.
	// This avoids go vet's stdmethods check on "Seek" which expects io.Seeker's
	// signature (offset int64, whence int) (int64, error).
	if err := conn.ExportMethodTable(map[string]any{
		"Next":        pobj.Next,
		"Previous":    pobj.Previous,
		"Pause":       pobj.Pause,
		"Stop":        pobj.Stop,
		"Play":        pobj.Play,
		"PlayPause":   pobj.PlayPause,
		"Seek":        pobj.seekRelative,
		"SetPosition": pobj.setPosition,
		"OpenUri":     pobj.openUri,
	}, mprisObjectPath, mprisPlayerIface); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mpris: export player: %w", err)
	}

	emptyMeta := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(noTrackPath),
	}
	propsSpec := prop.Map{
		mprisRootIface: {
			"CanQuit":             {Value: false, Writable: false, Emit: prop.EmitFalse},
			"CanRaise":            {Value: false, Writable: false, Emit: prop.EmitFalse},
			"HasTrackList":        {Value: false, Writable: false, Emit: prop.EmitFalse},
			"Identity":            {Value: "vibez", Writable: false, Emit: prop.EmitFalse},
			"DesktopEntry":        {Value: "io.github.simonepelosi.vibez", Writable: false, Emit: prop.EmitFalse},
			"SupportedUriSchemes": {Value: []string{}, Writable: false, Emit: prop.EmitFalse},
			"SupportedMimeTypes":  {Value: []string{}, Writable: false, Emit: prop.EmitFalse},
		},
		mprisPlayerIface: {
			"PlaybackStatus": {Value: "Stopped", Writable: false, Emit: prop.EmitTrue},
			"LoopStatus": {
				Value:    "None",
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: func(c *prop.Change) *dbus.Error {
					ls, _ := c.Value.(string)
					mode := player.RepeatModeOff
					switch ls {
					case "Track":
						mode = player.RepeatModeOne
					case "Playlist":
						mode = player.RepeatModeAll
					}
					_ = ctrl.SetRepeat(mode)
					return nil
				},
			},
			"Rate": {Value: float64(1), Writable: false, Emit: prop.EmitFalse},
			"Shuffle": {
				Value:    false,
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: func(c *prop.Change) *dbus.Error {
					on, _ := c.Value.(bool)
					_ = ctrl.SetShuffle(on)
					return nil
				},
			},
			"Metadata":      {Value: emptyMeta, Writable: false, Emit: prop.EmitTrue},
			"Volume":        {Value: float64(1), Writable: false, Emit: prop.EmitTrue},
			"Position":      {Value: int64(0), Writable: false, Emit: prop.EmitInvalidates},
			"MinimumRate":   {Value: float64(1), Writable: false, Emit: prop.EmitFalse},
			"MaximumRate":   {Value: float64(1), Writable: false, Emit: prop.EmitFalse},
			"CanGoNext":     {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanGoPrevious": {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanPlay":       {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanPause":      {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanSeek":       {Value: true, Writable: false, Emit: prop.EmitTrue},
			"CanControl":    {Value: true, Writable: false, Emit: prop.EmitFalse},
		},
	}
	props, err := prop.Export(conn, mprisObjectPath, propsSpec)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mpris: export props: %w", err)
	}
	srv.props = props

	reply, err := conn.RequestName(mprisServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("mpris: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		_ = conn.Close()
		return nil, fmt.Errorf("mpris: name %q already taken", mprisServiceName)
	}

	return srv, nil
}

// ── State update ──────────────────────────────────────────────────────────

// Update pushes fresh playback state to MPRIS clients. Call this whenever
// the audio engine emits a new State on its Subscribe channel.
func (s *Server) Update(st player.State) {
	s.mu.Lock()
	if s.closed || s.unavailable {
		s.mu.Unlock()
		return
	}
	s.pos = st.Position
	s.currentTrackPath = trackObjectPath(st.Track)
	s.hasCurrentTrack = st.Track != nil
	if st.Track == nil {
		s.currentTrackDuration = 0
	} else {
		s.currentTrackDuration = st.Track.Duration
	}
	s.pending = &st
	if s.debounce == nil {
		s.debounce = time.AfterFunc(100*time.Millisecond, s.flush)
	} else {
		s.debounce.Reset(100 * time.Millisecond)
	}
	s.mu.Unlock()
}

// seekThreshold is the position jump beyond which a change is treated as a
// deliberate seek rather than normal playback drift or timing jitter. Position
// updates arrive roughly once per second and vibez seeks in ±10s steps, so this
// sits safely between the two.
const seekThreshold = 2 * time.Second

// isSeek reports whether newPos is a discontinuous jump away from where
// uninterrupted playback would have reached: lastPos sampled at lastAt, plus
// the elapsed wall-clock time when playback was advancing. With no prior sample
// (zero lastAt) it is never a seek.
func isSeek(playing bool, lastPos time.Duration, lastAt time.Time, newPos time.Duration, now time.Time) bool {
	if lastAt.IsZero() {
		return false
	}
	expected := lastPos
	if playing {
		expected += now.Sub(lastAt)
	}
	delta := newPos - expected
	if delta < 0 {
		delta = -delta
	}
	return delta > seekThreshold
}

// trySetProperty contains godbus's panic-on-error API. MPRIS is optional, so
// malformed provider metadata or a lost session bus must not terminate vibez.
func (s *Server) trySetProperty(name string, value any) (ok bool) {
	defer func() {
		if recover() != nil {
			s.markUnavailable()
			ok = false
		}
	}()
	s.props.SetMust(mprisPlayerIface, name, value)
	return true
}

func (s *Server) markUnavailable() {
	s.mu.Lock()
	s.unavailable = true
	if s.debounce != nil {
		s.debounce.Stop()
		s.debounce = nil
	}
	s.pending = nil
	s.mu.Unlock()
	_ = s.conn.Close()
}

func (s *Server) flush() {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()

	s.mu.Lock()
	st := s.pending
	if st == nil || s.closed {
		s.mu.Unlock()
		return
	}
	s.pending = nil

	status := "Paused"
	if st.Playing {
		status = "Playing"
	}
	trackPath := trackObjectPath(st.Track)
	trackID := string(trackPath)

	statusChanged := status != s.lastStatus
	trackChanged := trackID != s.lastTrackID

	// A seek within the current track surfaces only as a position jump; without
	// a Seeked signal, clients extrapolate from a stale Position and desync.
	now := time.Now()
	seeked := !trackChanged && isSeek(s.lastStatus == "Playing", s.lastPos, s.lastPosAt, st.Position, now)
	s.lastPos = st.Position
	s.lastPosAt = now

	if !statusChanged && !trackChanged && !seeked {
		s.mu.Unlock()
		return
	}
	s.lastStatus = status
	s.lastTrackID = trackID
	s.mu.Unlock()

	// Per the MPRIS spec, discontinuous position changes are announced via the
	// Seeked signal (the Position property does not emit PropertiesChanged).
	// Refresh Position first so a client reading it in response gets the new value.
	if seeked {
		if !s.trySetProperty("Position", st.Position.Microseconds()) {
			return
		}
		if err := s.conn.Emit(mprisObjectPath, mprisPlayerIface+".Seeked", st.Position.Microseconds()); err != nil {
			s.markUnavailable()
			return
		}
		if !statusChanged && !trackChanged {
			return
		}
	}

	meta := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(noTrackPath),
	}
	if t := st.Track; t != nil {
		meta["mpris:trackid"] = dbus.MakeVariant(trackPath)
		meta["xesam:title"] = dbus.MakeVariant(t.Title)
		meta["xesam:artist"] = dbus.MakeVariant([]string{t.Artist})
		meta["xesam:album"] = dbus.MakeVariant(t.Album)
		meta["mpris:length"] = dbus.MakeVariant(t.Duration.Microseconds())
		if t.ArtworkURL != "" {
			meta["mpris:artUrl"] = dbus.MakeVariant(t.ArtworkURL)
		}
	}

	if !s.trySetProperty("PlaybackStatus", status) {
		return
	}
	if !s.trySetProperty("Metadata", meta) {
		return
	}
	if !s.trySetProperty("Position", st.Position.Microseconds()) {
		return
	}
	if st.Volume > 0 && !s.trySetProperty("Volume", st.Volume) {
		return
	}

	loopStatus := "None"
	switch st.RepeatMode {
	case player.RepeatModeOne:
		loopStatus = "Track"
	case player.RepeatModeAll:
		loopStatus = "Playlist"
	}
	if !s.trySetProperty("LoopStatus", loopStatus) {
		return
	}
	_ = s.trySetProperty("Shuffle", st.ShuffleMode)
}

// Close releases the session bus connection.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.debounce != nil {
		s.debounce.Stop()
		s.debounce = nil
	}
	s.pending = nil
	s.mu.Unlock()

	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	return s.conn.Close()
}
