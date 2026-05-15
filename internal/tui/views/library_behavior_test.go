package views

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/provider"
)

func TestLibrary_InitialViewShowsLibrarySections(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	view := lib.View()
	for _, want := range []string{"Songs", "Albums", "Artists", "Playlists"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q: %q", want, view)
		}
	}
}

func TestLibrary_SongsSectionLoadsAndPlaysSongs(t *testing.T) {
	tracks := []provider.Track{{ID: "i.1", Title: "One", Artist: "A", Album: "X"}, {ID: "i.2", Title: "Two", Artist: "B", Album: "Y"}}
	lib := NewLibrary(&mockProvider{libraryTracks: tracks})
	lib.SetSize(80, 24)
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(lib.View(), "Loading songs") {
		t.Fatalf("loading view missing songs copy: %q", lib.View())
	}
	lib, _ = lib.Update(libraryTracksLoadedMsg{tracks: tracks})
	view := lib.View()
	if !strings.Contains(view, "One") || !strings.Contains(view, "Two") {
		t.Fatalf("songs not rendered: %q", view)
	}
	_, cmd := lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	assertPlayTracksMsg(t, cmd, []string{"i.1", "i.2"}, tracks)
}

func TestLibrary_AlbumsSectionGroupsSongsAndPlaysAlbumTracks(t *testing.T) {
	tracks := []provider.Track{{ID: "i.1", Title: "One", Artist: "A", Album: "Alpha"}, {ID: "i.2", Title: "Two", Artist: "A", Album: "Alpha"}, {ID: "i.3", Title: "Three", Artist: "B", Album: "Beta"}}
	lib := NewLibrary(&mockProvider{libraryTracks: tracks})
	lib.SetSize(80, 24)
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	lib, _ = lib.Update(libraryTracksLoadedMsg{tracks: tracks})
	if view := lib.View(); !strings.Contains(view, "Alpha") || !strings.Contains(view, "Beta") {
		t.Fatalf("albums not rendered: %q", view)
	}
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := lib.View(); !strings.Contains(view, "One") || !strings.Contains(view, "Two") || strings.Contains(view, "Three") {
		t.Fatalf("album tracks wrong: %q", view)
	}
	_, cmd := lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	assertPlayTracksMsg(t, cmd, []string{"i.1", "i.2"}, tracks[:2])
}

func TestLibrary_ArtistsSectionGroupsSongsAndPlaysArtistTracks(t *testing.T) {
	tracks := []provider.Track{{ID: "i.1", Title: "One", Artist: "A", Album: "Alpha"}, {ID: "i.2", Title: "Two", Artist: "A", Album: "Beta"}, {ID: "i.3", Title: "Three", Artist: "B", Album: "Beta"}}
	lib := NewLibrary(&mockProvider{libraryTracks: tracks})
	lib.SetSize(80, 24)
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	lib, _ = lib.Update(libraryTracksLoadedMsg{tracks: tracks})
	if view := lib.View(); !strings.Contains(view, "A") || !strings.Contains(view, "B") {
		t.Fatalf("artists not rendered: %q", view)
	}
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := lib.View(); !strings.Contains(view, "One") || !strings.Contains(view, "Two") || strings.Contains(view, "Three") {
		t.Fatalf("artist tracks wrong: %q", view)
	}
	_, cmd := lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	assertPlayTracksMsg(t, cmd, []string{"i.1", "i.2"}, tracks[:2])
}

func TestLibrary_PlaylistsSectionLoadsPlaylistThenPlaysTracks(t *testing.T) {
	playlists := []provider.Playlist{{ID: "p.1", Name: "Mix", TrackCount: 2}}
	tracks := []provider.Track{{ID: "i.1", Title: "One", Artist: "A"}, {ID: "i.2", Title: "Two", Artist: "B"}}
	lib := NewLibrary(&mockProvider{playlists: playlists, playlistTracks: map[string][]provider.Track{"p.1": tracks}})
	lib.SetSize(80, 24)
	for range 3 {
		lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	lib, _ = lib.Update(libraryLoadedMsg{playlists: playlists})
	if view := lib.View(); !strings.Contains(view, "Mix") {
		t.Fatalf("playlists not rendered: %q", view)
	}
	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	lib, _ = lib.Update(playlistTracksMsg{playlist: playlists[0], tracks: tracks})
	if view := lib.View(); !strings.Contains(view, "One") || !strings.Contains(view, "Two") {
		t.Fatalf("playlist tracks not rendered: %q", view)
	}
	_, cmd := lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	assertPlayTracksMsg(t, cmd, []string{"i.1", "i.2"}, tracks)
}

func assertPlayTracksMsg(t *testing.T, cmd tea.Cmd, ids []string, tracks []provider.Track) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected play command")
	}
	msg, ok := cmd().(PlayTracksMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want PlayTracksMsg", cmd())
	}
	if strings.Join(msg.IDs, ",") != strings.Join(ids, ",") {
		t.Fatalf("IDs = %#v, want %#v", msg.IDs, ids)
	}
	if len(msg.Tracks) != len(tracks) {
		t.Fatalf("Tracks len = %d, want %d", len(msg.Tracks), len(tracks))
	}
	if msg.Track == nil || msg.Track.ID != tracks[0].ID {
		t.Fatalf("Track = %#v, want first %#v", msg.Track, tracks[0])
	}
}
