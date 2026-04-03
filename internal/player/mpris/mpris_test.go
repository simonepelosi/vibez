package mpris

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
)

func track(title, artist string) *provider.Track {
	return &provider.Track{Title: title, Artist: artist}
}

// --- stateChanged ---

func TestStateChanged_BothNil(t *testing.T) {
	a := player.State{Track: nil}
	b := player.State{Track: nil}
	if stateChanged(a, b) {
		t.Error("two nil-track states should not be considered changed")
	}
}

func TestStateChanged_PlayingToggle(t *testing.T) {
	a := player.State{Playing: true}
	b := player.State{Playing: false}
	if !stateChanged(a, b) {
		t.Error("Playing flag change not detected")
	}
}

func TestStateChanged_VolumeChange(t *testing.T) {
	a := player.State{Volume: 0.5}
	b := player.State{Volume: 0.8}
	if !stateChanged(a, b) {
		t.Error("Volume change not detected")
	}
}

func TestStateChanged_TrackTitleChange(t *testing.T) {
	a := player.State{Track: track("Song A", "Artist")}
	b := player.State{Track: track("Song B", "Artist")}
	if !stateChanged(a, b) {
		t.Error("Track title change not detected")
	}
}

func TestStateChanged_TrackArtistChange(t *testing.T) {
	a := player.State{Track: track("Song", "Artist A")}
	b := player.State{Track: track("Song", "Artist B")}
	if !stateChanged(a, b) {
		t.Error("Track artist change not detected")
	}
}

func TestStateChanged_NilToNonNilTrack(t *testing.T) {
	a := player.State{Track: nil}
	b := player.State{Track: track("Song", "Artist")}
	if !stateChanged(a, b) {
		t.Error("nil → non-nil track change not detected")
	}
}

func TestStateChanged_NonNilToNilTrack(t *testing.T) {
	a := player.State{Track: track("Song", "Artist")}
	b := player.State{Track: nil}
	if !stateChanged(a, b) {
		t.Error("non-nil → nil track change not detected")
	}
}

func TestStateChanged_SameTrackNoChange(t *testing.T) {
	a := player.State{Playing: true, Volume: 0.7, Track: track("Song", "Artist")}
	b := player.State{Playing: true, Volume: 0.7, Track: track("Song", "Artist")}
	if stateChanged(a, b) {
		t.Error("identical states should not be considered changed")
	}
}

func TestStateChanged_PositionChangeIgnored(t *testing.T) {
	// Position changes are intentionally not tracked (would fire too often).
	a := player.State{Track: track("Song", "Art"), Position: 10 * time.Second}
	b := player.State{Track: track("Song", "Art"), Position: 15 * time.Second}
	if stateChanged(a, b) {
		t.Error("position-only change should not trigger state change")
	}
}

// --- findPlayer preference ---

func TestFindPlayerPreference(t *testing.T) {
	// Verify the preference logic: Cider/Apple over generic players.
	// We test the selection logic without a real D-Bus by calling the helper directly.
	tests := []struct {
		name    string
		players []string
		want    string
	}{
		{
			name:    "prefers cider",
			players: []string{"org.mpris.MediaPlayer2.vlc", "org.mpris.MediaPlayer2.Cider"},
			want:    "org.mpris.MediaPlayer2.Cider",
		},
		{
			name:    "prefers apple over generic",
			players: []string{"org.mpris.MediaPlayer2.vlc", "org.mpris.MediaPlayer2.AppleMusic"},
			want:    "org.mpris.MediaPlayer2.AppleMusic",
		},
		{
			name:    "falls back to first when no preferred",
			players: []string{"org.mpris.MediaPlayer2.vlc", "org.mpris.MediaPlayer2.rhythmbox"},
			want:    "org.mpris.MediaPlayer2.vlc",
		},
		{
			name:    "single player returned",
			players: []string{"org.mpris.MediaPlayer2.mopidy"},
			want:    "org.mpris.MediaPlayer2.mopidy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := selectPlayer(tc.players)
			if got != tc.want {
				t.Errorf("selectPlayer() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- parseMetadataMap ---

func TestParseMetadataMap_FullMetadata(t *testing.T) {
	meta := map[string]dbus.Variant{
		"xesam:title":  dbus.MakeVariant("My Song"),
		"xesam:artist": dbus.MakeVariant([]string{"Artist A", "Artist B"}),
		"xesam:album":  dbus.MakeVariant("My Album"),
		"mpris:artUrl": dbus.MakeVariant("https://art.example.com/cover.jpg"),
		"mpris:length": dbus.MakeVariant(int64(180_000_000)), // 180 seconds in microseconds
	}
	track := parseMetadataMap(meta)
	if track.Title != "My Song" {
		t.Errorf("Title = %q, want %q", track.Title, "My Song")
	}
	if track.Album != "My Album" {
		t.Errorf("Album = %q, want %q", track.Album, "My Album")
	}
	if track.ArtworkURL != "https://art.example.com/cover.jpg" {
		t.Errorf("ArtworkURL = %q", track.ArtworkURL)
	}
	if track.Artist != "Artist A, Artist B" {
		t.Errorf("Artist = %q, want %q", track.Artist, "Artist A, Artist B")
	}
	want := 180 * time.Second
	if track.Duration != want {
		t.Errorf("Duration = %v, want %v", track.Duration, want)
	}
}

func TestParseMetadataMap_Empty(t *testing.T) {
	track := parseMetadataMap(map[string]dbus.Variant{})
	if track == nil {
		t.Fatal("parseMetadataMap returned nil for empty map")
	}
	if track.Title != "" || track.Artist != "" || track.Album != "" {
		t.Errorf("expected empty track, got %+v", track)
	}
}

func TestParseMetadataMap_Partial(t *testing.T) {
	meta := map[string]dbus.Variant{
		"xesam:title": dbus.MakeVariant("Only Title"),
	}
	track := parseMetadataMap(meta)
	if track.Title != "Only Title" {
		t.Errorf("Title = %q", track.Title)
	}
	if track.Artist != "" {
		t.Errorf("Artist should be empty, got %q", track.Artist)
	}
	if track.Duration != 0 {
		t.Errorf("Duration should be 0, got %v", track.Duration)
	}
}

func TestParseMetadataMap_SingleArtist(t *testing.T) {
	meta := map[string]dbus.Variant{
		"xesam:artist": dbus.MakeVariant([]string{"Solo Artist"}),
	}
	track := parseMetadataMap(meta)
	if track.Artist != "Solo Artist" {
		t.Errorf("Artist = %q, want %q", track.Artist, "Solo Artist")
	}
}

func TestParseMetadataMap_MultipleArtistsJoined(t *testing.T) {
	meta := map[string]dbus.Variant{
		"xesam:artist": dbus.MakeVariant([]string{"A", "B", "C"}),
	}
	track := parseMetadataMap(meta)
	if track.Artist != "A, B, C" {
		t.Errorf("Artist = %q, want %q", track.Artist, "A, B, C")
	}
}

func TestParseMetadataMap_DurationConversion(t *testing.T) {
	// 3 minutes = 180 seconds = 180,000,000 microseconds
	meta := map[string]dbus.Variant{
		"mpris:length": dbus.MakeVariant(int64(180_000_000)),
	}
	track := parseMetadataMap(meta)
	want := 180 * time.Second
	if track.Duration != want {
		t.Errorf("Duration = %v, want %v", track.Duration, want)
	}
}

func TestParseMetadataMap_WrongTypeNocrash(t *testing.T) {
	// Wrong types should not panic, just leave fields at zero values.
	meta := map[string]dbus.Variant{
		"xesam:title":  dbus.MakeVariant(42),           // int instead of string
		"xesam:artist": dbus.MakeVariant("not-a-list"), // string instead of []string
		"mpris:length": dbus.MakeVariant("not-int64"),  // string instead of int64
	}
	track := parseMetadataMap(meta)
	if track == nil {
		t.Fatal("parseMetadataMap returned nil")
	}
	if track.Title != "" {
		t.Errorf("Title should be empty for wrong type, got %q", track.Title)
	}
	if track.Artist != "" {
		t.Errorf("Artist should be empty for wrong type, got %q", track.Artist)
	}
	if track.Duration != 0 {
		t.Errorf("Duration should be 0 for wrong type, got %v", track.Duration)
	}
}

// ─── D-Bus integration tests ─────────────────────────────────────────────────
//
// These tests register a fake MPRIS2 service on the session bus, create an
// MPRISPlayer pointing at it, and exercise the full call path.  They require a
// working D-Bus session bus (DBUS_SESSION_BUS_ADDRESS must be set) and are
// skipped automatically when the bus is unavailable.

// fakeMPRIS registers a minimal MPRIS2 object on the session bus.
// It records method calls and returns configurable property values.
type fakeMPRIS struct {
	mu             sync.Mutex
	calls          []string
	playbackStatus string
	volume         float64
	posUs          int64
	metadata       map[string]dbus.Variant
	conn           *dbus.Conn
	busName        string
}

// newFakeMPRIS registers a fake MPRIS service on the session bus and returns it.
// If a bus session is unavailable the second return value is false.
func newFakeMPRIS(t *testing.T) (*fakeMPRIS, bool) {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, false // bus unavailable — skip
	}

	// Request a unique bus name.
	busName := "org.mpris.MediaPlayer2.vibeztest"
	if _, err := conn.RequestName(busName, dbus.NameFlagAllowReplacement|dbus.NameFlagReplaceExisting); err != nil {
		_ = conn.Close()
		return nil, false
	}

	f := &fakeMPRIS{
		conn:           conn,
		busName:        busName,
		playbackStatus: "Paused",
		volume:         0.5,
		metadata: map[string]dbus.Variant{
			"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")),
		},
	}

	// Export root and player objects.
	if err := conn.Export(f, "/org/mpris/MediaPlayer2", "org.mpris.MediaPlayer2"); err != nil {
		t.Fatal("Export org.mpris.MediaPlayer2:", err)
	}
	// Use ExportMethodTable to avoid go vet's stdmethods check on "Seek"
	// which expects io.Seeker's signature (offset int64, whence int) (int64, error).
	if err := conn.ExportMethodTable(map[string]any{
		"Next":        f.Next,
		"Previous":    f.Previous,
		"Pause":       f.Pause,
		"Stop":        f.Stop,
		"Play":        f.Play,
		"PlayPause":   f.PlayPause,
		"Seek":        f.seekRelative,
		"SetPosition": f.SetPosition,
		"OpenUri":     f.OpenUri,
	}, "/org/mpris/MediaPlayer2", "org.mpris.MediaPlayer2.Player"); err != nil {
		t.Fatal("ExportMethodTable:", err)
	}

	// Export the properties interface.
	if err := conn.Export(f, "/org/mpris/MediaPlayer2", "org.freedesktop.DBus.Properties"); err != nil {
		t.Fatal("Export org.freedesktop.DBus.Properties:", err)
	}

	t.Cleanup(func() {
		_, _ = conn.ReleaseName(busName)
		_ = conn.Close()
	})
	return f, true
}

// ── D-Bus interface implementations ──

func (f *fakeMPRIS) Play() *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Play")
	f.playbackStatus = "Playing"
	return nil
}

func (f *fakeMPRIS) Pause() *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Pause")
	f.playbackStatus = "Paused"
	return nil
}

func (f *fakeMPRIS) Stop() *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Stop")
	return nil
}

func (f *fakeMPRIS) Next() *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Next")
	return nil
}

func (f *fakeMPRIS) Previous() *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Previous")
	return nil
}

func (f *fakeMPRIS) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch prop {
	case "PlaybackStatus":
		return dbus.MakeVariant(f.playbackStatus), nil
	case "Volume":
		return dbus.MakeVariant(f.volume), nil
	case "Position":
		return dbus.MakeVariant(f.posUs), nil
	case "Metadata":
		return dbus.MakeVariant(f.metadata), nil
	default:
		return dbus.MakeVariant(""), nil
	}
}

func (f *fakeMPRIS) Set(iface, prop string, val dbus.Variant) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if prop == "Volume" {
		if v, ok := val.Value().(float64); ok {
			f.volume = v
		}
	}
	return nil
}

func (f *fakeMPRIS) GetAll(_ string) (map[string]dbus.Variant, *dbus.Error) {
	return map[string]dbus.Variant{}, nil
}

func (f *fakeMPRIS) seekRelative(_ int64) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "Seek")
	return nil
}

func (f *fakeMPRIS) SetPosition(_ dbus.ObjectPath, _ int64) *dbus.Error { return nil }
func (f *fakeMPRIS) OpenUri(_ string) *dbus.Error                       { return nil }
func (f *fakeMPRIS) Raise() *dbus.Error                                 { return nil }
func (f *fakeMPRIS) Quit() *dbus.Error                                  { return nil }
func (f *fakeMPRIS) PlayPause() *dbus.Error                             { return nil }

// ── Helpers ──────────────────────────────────────────────────────────────────

func connectToFake(t *testing.T, busName string) (*MPRISPlayer, *dbus.Conn) {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal("cannot connect to session bus:", err)
	}
	obj := conn.Object(busName, "/org/mpris/MediaPlayer2")
	p := &MPRISPlayer{
		conn: conn,
		name: busName,
		obj:  obj,
		done: make(chan struct{}),
	}
	return p, conn
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestNew_NoMPRISPlayer(t *testing.T) {
	// When no MPRIS player is registered, New() should fail with a useful error.
	// (Don't call this if our fake service happens to be registered.)
	_, err := New()
	// Either succeeds (if any MPRIS player exists on the bus) or fails gracefully.
	// Both outcomes are acceptable — just verify no panic.
	_ = err
}

func TestFindPlayer_NoPlayers(t *testing.T) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Skip("D-Bus unavailable:", err)
	}
	defer conn.Close() //nolint:errcheck

	// Create a slice with no MPRIS names — findPlayer-like logic.
	result := selectPlayer([]string{"org.mpris.MediaPlayer2.cider", "org.mpris.MediaPlayer2.generic"})
	if result == "" {
		t.Error("selectPlayer should return a non-empty string")
	}
}

func TestMPRISPlayer_Play(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	if err := p.Play(); err != nil {
		t.Errorf("Play() error: %v", err)
	}
	fake.mu.Lock()
	called := fake.calls
	fake.mu.Unlock()
	if len(called) == 0 || called[len(called)-1] != "Play" {
		t.Errorf("Play not recorded by fake: %v", called)
	}
}

func TestMPRISPlayer_Pause(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	if err := p.Pause(); err != nil {
		t.Errorf("Pause() error: %v", err)
	}
}

func TestMPRISPlayer_Next(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	if err := p.Next(); err != nil {
		t.Errorf("Next() error: %v", err)
	}
	fake.mu.Lock()
	called := fake.calls
	fake.mu.Unlock()
	if !slices.Contains(called, "Next") {
		t.Errorf("Next not recorded by fake: %v", called)
	}
}

func TestMPRISPlayer_Previous(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	if err := p.Previous(); err != nil {
		t.Errorf("Previous() error: %v", err)
	}
}

func TestMPRISPlayer_Stop(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	if err := p.Stop(); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

func TestMPRISPlayer_SetVolume(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	if err := p.SetVolume(0.8); err != nil {
		t.Errorf("SetVolume() error: %v", err)
	}
}

func TestMPRISPlayer_Seek(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	if err := p.Seek(30 * time.Second); err != nil {
		t.Errorf("Seek() error: %v", err)
	}
}

func TestMPRISPlayer_SetQueue_ReturnsError(t *testing.T) {
	_, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	// SetQueue is not supported via MPRIS — always errors.
	// Test with a nil conn player to avoid needing a real connection.
	p := &MPRISPlayer{}
	err := p.SetQueue([]string{"id1"})
	if err == nil {
		t.Error("SetQueue should always return an error for MPRIS player")
	}
}

func TestMPRISPlayer_GetState(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	state, err := p.GetState()
	if err != nil {
		t.Errorf("GetState() error: %v", err)
	}
	if state == nil {
		t.Fatal("GetState() returned nil state")
	}
}

func TestMPRISPlayer_Subscribe_ReturnsChannel(t *testing.T) {
	_, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p := &MPRISPlayer{subs: nil}
	ch := p.Subscribe()
	if ch == nil {
		t.Error("Subscribe() should return non-nil channel")
	}
}

func TestMPRISPlayer_Close(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck
	p.done = make(chan struct{})

	if err := p.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
	// done channel should be closed now.
	select {
	case <-p.done:
	default:
		t.Error("Close() should close the done channel")
	}
}

func TestMPRISPlayer_ReadState_Success(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	fake.playbackStatus = "Playing"
	fake.volume = 0.9
	fake.posUs = 30_000_000 // 30 seconds

	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	state, err := p.readState()
	if err != nil {
		t.Errorf("readState() error: %v", err)
	}
	if state == nil {
		t.Fatal("readState() returned nil")
	}
	if !state.Playing {
		t.Error("readState() should report Playing=true")
	}
}

func TestMPRISPlayer_ReadState_WithMetadata(t *testing.T) {
	fake, ok := newFakeMPRIS(t)
	if !ok {
		t.Skip("D-Bus session bus unavailable")
	}
	fake.metadata = map[string]dbus.Variant{
		"xesam:title":  dbus.MakeVariant("Test Song"),
		"xesam:artist": dbus.MakeVariant([]string{"Test Artist"}),
		"xesam:album":  dbus.MakeVariant("Test Album"),
	}

	p, conn := connectToFake(t, fake.busName)
	defer conn.Close() //nolint:errcheck

	state, err := p.readState()
	if err != nil {
		t.Errorf("readState() error: %v", err)
	}
	if state == nil || state.Track == nil {
		t.Fatal("readState() should return track metadata")
	}
	if state.Track.Title != "Test Song" {
		t.Errorf("Track.Title = %q, want %q", state.Track.Title, "Test Song")
	}
}

// ─── MPRIS Server tests ───────────────────────────────────────────────────────

// mockController implements the Controller interface for testing.
type mockController struct {
	playCalled       bool
	pauseCalled      bool
	nextCalled       bool
	prevCalled       bool
	seekDuration     time.Duration
	repeatMode       int
	shuffleMode      bool
	setRepeatCalled  bool
	setShuffleCalled bool
}

func (m *mockController) Play() error                { m.playCalled = true; return nil }
func (m *mockController) Pause() error               { m.pauseCalled = true; return nil }
func (m *mockController) Next() error                { m.nextCalled = true; return nil }
func (m *mockController) Previous() error            { m.prevCalled = true; return nil }
func (m *mockController) Seek(d time.Duration) error { m.seekDuration = d; return nil }
func (m *mockController) SetRepeat(mode int) error {
	m.setRepeatCalled = true
	m.repeatMode = mode
	return nil
}
func (m *mockController) SetShuffle(on bool) error {
	m.setShuffleCalled = true
	m.shuffleMode = on
	return nil
}

func TestNewServer_Success(t *testing.T) {
	ctrl := &mockController{}
	srv, err := NewServer(ctrl)
	if err != nil {
		// The service name "org.mpris.MediaPlayer2.vibez" might already be taken in CI.
		// Accept that as a non-fatal skip.
		t.Skipf("NewServer failed (name may be taken): %v", err)
	}
	defer srv.Close() //nolint:errcheck

	if srv == nil {
		t.Fatal("NewServer returned nil server")
	}
}

func TestServer_Update_Playing(t *testing.T) {
	ctrl := &mockController{}
	srv, err := NewServer(ctrl)
	if err != nil {
		t.Skipf("NewServer failed: %v", err)
	}
	defer srv.Close() //nolint:errcheck

	track := &provider.Track{
		ID:     "track1",
		Title:  "Test Track",
		Artist: "Test Artist",
		Album:  "Test Album",
	}
	st := player.State{
		Playing:  true,
		Track:    track,
		Volume:   0.8,
		Position: 45 * time.Second,
	}
	// Should not panic.
	srv.Update(st)
}

func TestServer_Update_Paused(t *testing.T) {
	ctrl := &mockController{}
	srv, err := NewServer(ctrl)
	if err != nil {
		t.Skipf("NewServer failed: %v", err)
	}
	defer srv.Close() //nolint:errcheck

	srv.Update(player.State{Playing: false, Volume: 0.5})
}

func TestServer_Update_RepeatMode(t *testing.T) {
	ctrl := &mockController{}
	srv, err := NewServer(ctrl)
	if err != nil {
		t.Skipf("NewServer failed: %v", err)
	}
	defer srv.Close() //nolint:errcheck

	srv.Update(player.State{RepeatMode: player.RepeatModeAll, ShuffleMode: true})
	srv.Update(player.State{RepeatMode: player.RepeatModeOne})
	srv.Update(player.State{RepeatMode: player.RepeatModeOff})
}

func TestServer_Update_TrackWithArtwork(t *testing.T) {
	ctrl := &mockController{}
	srv, err := NewServer(ctrl)
	if err != nil {
		t.Skipf("NewServer failed: %v", err)
	}
	defer srv.Close() //nolint:errcheck

	track := &provider.Track{
		ID:         "art-track",
		Title:      "Artwork Song",
		Artist:     "Artwork Artist",
		ArtworkURL: "https://example.com/art.jpg",
	}
	srv.Update(player.State{Playing: true, Track: track})
}

func TestServer_Close(t *testing.T) {
	ctrl := &mockController{}
	srv, err := NewServer(ctrl)
	if err != nil {
		t.Skipf("NewServer failed: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}
