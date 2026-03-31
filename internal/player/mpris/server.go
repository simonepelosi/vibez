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
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/simone-vibes/vibez/internal/player"
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

	mu  sync.Mutex
	pos time.Duration
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

// setPosition seeks to an absolute position (µs) for the given track ID.
func (p *playerObj) setPosition(_ dbus.ObjectPath, posUs int64) *dbus.Error {
	_ = p.ctrl.Seek(time.Duration(posUs) * time.Microsecond)
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
			"DesktopEntry":        {Value: "vibez", Writable: false, Emit: prop.EmitFalse},
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
	status := "Paused"
	if st.Playing {
		status = "Playing"
	}

	s.mu.Lock()
	s.pos = st.Position
	s.mu.Unlock()

	meta := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(noTrackPath),
	}
	if t := st.Track; t != nil {
		meta["mpris:trackid"] = dbus.MakeVariant(dbus.ObjectPath("/org/vibez/track/" + t.ID))
		meta["xesam:title"] = dbus.MakeVariant(t.Title)
		meta["xesam:artist"] = dbus.MakeVariant([]string{t.Artist})
		meta["xesam:album"] = dbus.MakeVariant(t.Album)
		meta["mpris:length"] = dbus.MakeVariant(t.Duration.Microseconds())
		if t.ArtworkURL != "" {
			meta["mpris:artUrl"] = dbus.MakeVariant(t.ArtworkURL)
		}
	}

	s.props.SetMust(mprisPlayerIface, "PlaybackStatus", status)
	s.props.SetMust(mprisPlayerIface, "Metadata", meta)
	s.props.SetMust(mprisPlayerIface, "Position", st.Position.Microseconds())
	if st.Volume > 0 {
		s.props.SetMust(mprisPlayerIface, "Volume", st.Volume)
	}

	loopStatus := "None"
	switch st.RepeatMode {
	case player.RepeatModeOne:
		loopStatus = "Track"
	case player.RepeatModeAll:
		loopStatus = "Playlist"
	}
	s.props.SetMust(mprisPlayerIface, "LoopStatus", loopStatus)
	s.props.SetMust(mprisPlayerIface, "Shuffle", st.ShuffleMode)
}

// Close releases the session bus connection.
func (s *Server) Close() error {
	return s.conn.Close()
}
