package mpris

import (
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
