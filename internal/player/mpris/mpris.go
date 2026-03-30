package mpris

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

const (
	mprisInterface = "org.mpris.MediaPlayer2.Player"
	dbusProperties = "org.freedesktop.DBus.Properties"
	pollInterval   = 500 * time.Millisecond
)

// MPRISPlayer controls an MPRIS-compatible media player over D-Bus.
type MPRISPlayer struct {
	conn *dbus.Conn
	name string // e.g. org.mpris.MediaPlayer2.Cider
	obj  dbus.BusObject
	subs []chan player.State
	mu   sync.Mutex
	done chan struct{}
}

// New connects to the D-Bus session bus and finds an available MPRIS player.
func New() (*MPRISPlayer, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connecting to D-Bus session bus: %w", err)
	}

	name, err := findPlayer(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	obj := conn.Object(name, "/org/mpris/MediaPlayer2")
	p := &MPRISPlayer{
		conn: conn,
		name: name,
		obj:  obj,
		done: make(chan struct{}),
	}
	go p.poll()
	return p, nil
}

// findPlayer returns the bus name of the best available MPRIS player.
func findPlayer(conn *dbus.Conn) (string, error) {
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return "", fmt.Errorf("listing D-Bus names: %w", err)
	}

	var players []string
	for _, n := range names {
		if strings.HasPrefix(n, "org.mpris.MediaPlayer2.") {
			players = append(players, n)
		}
	}
	if len(players) == 0 {
		return "", errors.New("no MPRIS player found — start Cider or another MPRIS-compatible player")
	}
	return selectPlayer(players), nil
}

// selectPlayer picks the best player from a list of MPRIS bus names,
// preferring Cider/Apple Music players over generic ones.
func selectPlayer(players []string) string {
	preferred := []string{"cider", "apple"}
	for _, pref := range preferred {
		for _, p := range players {
			if strings.Contains(strings.ToLower(p), pref) {
				return p
			}
		}
	}
	return players[0]
}

func (m *MPRISPlayer) call(method string, args ...any) error {
	return m.obj.Call(mprisInterface+"."+method, 0, args...).Err
}

func (m *MPRISPlayer) getProperty(prop string) (dbus.Variant, error) {
	var v dbus.Variant
	err := m.obj.Call(dbusProperties+".Get", 0, mprisInterface, prop).Store(&v)
	return v, err
}

func (m *MPRISPlayer) setProperty(prop string, val any) error {
	return m.obj.Call(dbusProperties+".Set", 0, mprisInterface, prop, dbus.MakeVariant(val)).Err
}

func (m *MPRISPlayer) Play() error     { return m.call("Play") }
func (m *MPRISPlayer) Pause() error    { return m.call("Pause") }
func (m *MPRISPlayer) Stop() error     { return m.call("Stop") }
func (m *MPRISPlayer) Next() error     { return m.call("Next") }
func (m *MPRISPlayer) Previous() error { return m.call("Previous") }
func (m *MPRISPlayer) SetQueue(_ []string) error {
	return errors.New("SetQueue is not supported via MPRIS")
}

func (m *MPRISPlayer) Seek(pos time.Duration) error {
	// MPRIS Seek moves relative to current position; use SetPosition for absolute.
	// We use the track ID from metadata for SetPosition.
	meta, err := m.metadata()
	if err != nil {
		return err
	}
	trackID, _ := meta["mpris:trackid"].Value().(dbus.ObjectPath)
	return m.obj.Call(mprisInterface+".SetPosition", 0, trackID, pos.Microseconds()).Err
}

func (m *MPRISPlayer) SetVolume(v float64) error {
	return m.setProperty("Volume", v)
}

func (m *MPRISPlayer) GetState() (*player.State, error) {
	return m.readState()
}

func (m *MPRISPlayer) Subscribe() <-chan player.State {
	ch := make(chan player.State, 4)
	m.mu.Lock()
	m.subs = append(m.subs, ch)
	m.mu.Unlock()
	return ch
}

func (m *MPRISPlayer) Close() error {
	close(m.done)
	return m.conn.Close()
}

// --- Internal helpers ---

// parseMetadataMap converts a raw MPRIS metadata map into a Track.
func parseMetadataMap(meta map[string]dbus.Variant) *provider.Track {
	t := &provider.Track{}
	if v, ok := meta["xesam:title"]; ok {
		t.Title, _ = v.Value().(string)
	}
	if v, ok := meta["xesam:album"]; ok {
		t.Album, _ = v.Value().(string)
	}
	if v, ok := meta["mpris:artUrl"]; ok {
		t.ArtworkURL, _ = v.Value().(string)
	}
	if v, ok := meta["mpris:length"]; ok {
		dur, _ := v.Value().(int64)
		t.Duration = time.Duration(dur) * time.Microsecond
	}
	if v, ok := meta["xesam:artist"]; ok {
		artists, _ := v.Value().([]string)
		if len(artists) > 0 {
			t.Artist = strings.Join(artists, ", ")
		}
	}
	return t
}

func (m *MPRISPlayer) metadata() (map[string]dbus.Variant, error) {
	v, err := m.getProperty("Metadata")
	if err != nil {
		return nil, err
	}
	meta, ok := v.Value().(map[string]dbus.Variant)
	if !ok {
		return nil, errors.New("unexpected metadata type")
	}
	return meta, nil
}

func (m *MPRISPlayer) readState() (*player.State, error) {
	statusV, err := m.getProperty("PlaybackStatus")
	if err != nil {
		return nil, fmt.Errorf("reading PlaybackStatus: %w", err)
	}
	status, _ := statusV.Value().(string)

	posV, _ := m.getProperty("Position")
	posUs, _ := posV.Value().(int64)

	volV, _ := m.getProperty("Volume")
	vol, _ := volV.Value().(float64)

	meta, _ := m.metadata()

	state := &player.State{
		Playing:  status == "Playing",
		Position: time.Duration(posUs) * time.Microsecond,
		Volume:   vol,
	}

	if meta != nil {
		state.Track = parseMetadataMap(meta)
	}

	return state, nil
}

func (m *MPRISPlayer) poll() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var last player.State
	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			s, err := m.readState()
			if err != nil {
				continue
			}
			if stateChanged(last, *s) {
				last = *s
				m.mu.Lock()
				for _, ch := range m.subs {
					select {
					case ch <- *s:
					default:
					}
				}
				m.mu.Unlock()
			}
		}
	}
}

func stateChanged(a, b player.State) bool {
	if a.Playing != b.Playing || a.Volume != b.Volume {
		return true
	}
	if a.Track == nil && b.Track == nil {
		return false
	}
	if a.Track == nil || b.Track == nil {
		return true
	}
	return a.Track.Title != b.Track.Title || a.Track.Artist != b.Track.Artist
}
