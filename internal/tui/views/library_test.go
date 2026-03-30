package views

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/simone-vibes/vibez/internal/provider"
)

// --- playlistItem ---

func TestPlaylistItem_Title(t *testing.T) {
	pl := playlistItem(provider.Playlist{Name: "My Playlist"})
	if pl.Title() != "My Playlist" {
		t.Errorf("Title() = %q, want %q", pl.Title(), "My Playlist")
	}
}

func TestPlaylistItem_Description(t *testing.T) {
	pl := playlistItem(provider.Playlist{TrackCount: 42})
	got := pl.Description()
	if !strings.Contains(got, "42") {
		t.Errorf("Description() should contain track count, got %q", got)
	}
}

func TestPlaylistItem_FilterValue(t *testing.T) {
	pl := playlistItem(provider.Playlist{Name: "Filter Playlist"})
	if pl.FilterValue() != "Filter Playlist" {
		t.Errorf("FilterValue() = %q, want %q", pl.FilterValue(), "Filter Playlist")
	}
}

// --- trackListItem ---

func TestTrackListItem_Title(t *testing.T) {
	item := trackListItem{t: provider.Track{Title: "Library Track"}}
	if item.Title() != "Library Track" {
		t.Errorf("Title() = %q, want %q", item.Title(), "Library Track")
	}
}

func TestTrackListItem_Description(t *testing.T) {
	item := trackListItem{t: provider.Track{Artist: "Some Artist", Album: "Some Album"}}
	got := item.Description()
	if !strings.Contains(got, "Some Artist") {
		t.Errorf("Description() should contain artist, got %q", got)
	}
	if !strings.Contains(got, "Some Album") {
		t.Errorf("Description() should contain album, got %q", got)
	}
}

func TestTrackListItem_FilterValue(t *testing.T) {
	item := trackListItem{t: provider.Track{Title: "Track Title", Artist: "Track Artist"}}
	got := item.FilterValue()
	if !strings.Contains(got, "Track Title") {
		t.Errorf("FilterValue() should contain title, got %q", got)
	}
	if !strings.Contains(got, "Track Artist") {
		t.Errorf("FilterValue() should contain artist, got %q", got)
	}
}

// --- LibraryModel ---

func TestNewLibrary_NotNil(t *testing.T) {
	lib := NewLibrary(nil)
	if lib == nil {
		t.Fatal("NewLibrary(nil) returned nil")
	}
}

func TestNewLibrary_WithProvider(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	if lib == nil {
		t.Fatal("NewLibrary(provider) returned nil")
	}
}

func TestLibrary_SetSize_NoPanic(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24) // should not panic
}

func TestLibrary_View_Loading(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	lib.loading = true
	got := lib.View()
	if !strings.Contains(got, "Loading") {
		t.Errorf("View() when loading should contain 'Loading', got %q", got)
	}
}

func TestLibrary_View_NotLoading(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	lib.loading = false
	got := lib.View()
	if got == "" {
		t.Error("View() should return non-empty string")
	}
}

func TestLibrary_Update_LibraryLoadedMsg_Playlists(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	msg := libraryLoadedMsg{
		tab: tabPlaylists,
		playlists: []provider.Playlist{
			{ID: "p1", Name: "Playlist One", TrackCount: 10},
			{ID: "p2", Name: "Playlist Two", TrackCount: 20},
		},
	}
	lib, _ = lib.Update(msg)
	if len(lib.playlists) != 2 {
		t.Errorf("playlists not set: got %d, want 2", len(lib.playlists))
	}
}

func TestLibrary_Update_LibraryLoadedMsg_Tracks(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	msg := libraryLoadedMsg{
		tab: tabTracks,
		tracks: []provider.Track{
			{ID: "t1", Title: "Track One"},
		},
	}
	lib, _ = lib.Update(msg)
	if len(lib.tracks) != 1 {
		t.Errorf("tracks not set: got %d, want 1", len(lib.tracks))
	}
}

func TestLibrary_Update_LibraryLoadedMsg_Error(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	lib.loading = true
	msg := libraryLoadedMsg{err: errors.New("load failed")}
	lib, _ = lib.Update(msg)
	// loading should be cleared even on error
	if lib.loading {
		t.Error("loading should be false after error msg")
	}
}

func TestLibrary_Update_TabKey(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	// Just verify it doesn't panic; tab assertion is in TestLibrary_Update_TabString.
	_, _ = lib.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\t")})
}

func TestLibrary_Update_TabString(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	lib.activeTab = tabPlaylists
	// Send "tab" key (the string "tab" matches msg.String() == "tab")
	lib, _ = lib.Update(tea.KeyMsg{Type: tea.KeyTab})
	if lib.activeTab != tabAlbums {
		t.Errorf("after tab key: activeTab = %d, want %d (tabAlbums)", lib.activeTab, tabAlbums)
	}
}

func TestLibrary_Update_SpinnerTick_WhenLoading(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	lib.loading = true
	// Send a spinner tick message - should not panic
	tickMsg := spinner.TickMsg{}
	_, _ = lib.Update(tickMsg)
}

func TestLibrary_RefreshList_Indirect(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	// Load playlists and then switch to tracks tab to exercise refreshList
	lib, _ = lib.Update(libraryLoadedMsg{
		tab:       tabPlaylists,
		playlists: []provider.Playlist{{Name: "PL", TrackCount: 5}},
	})
	// Switch to albums tab
	lib, _ = lib.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Switch to tracks tab — final result unused, test verifies no panic
	_, _ = lib.Update(tea.KeyMsg{Type: tea.KeyTab})
	// No panic = success
}

func TestLibrary_Init_NoPanic(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	cmd := lib.Init()
	if cmd == nil {
		t.Error("Init() should return non-nil cmd")
	}
}

func TestLibrary_LoadPlaylists_Executes(t *testing.T) {
	prov := &mockProvider{
		playlists: []provider.Playlist{
			{ID: "x", Name: "X Playlist", TrackCount: 3},
		},
	}
	lib := NewLibrary(prov)
	cmd := lib.loadPlaylists()
	if cmd == nil {
		t.Fatal("loadPlaylists() should return non-nil cmd")
	}
	msg := cmd() // execute the cmd
	loaded, ok := msg.(libraryLoadedMsg)
	if !ok {
		t.Fatalf("loadPlaylists cmd returned %T, want libraryLoadedMsg", msg)
	}
	if len(loaded.playlists) != 1 {
		t.Errorf("got %d playlists, want 1", len(loaded.playlists))
	}
}

func TestLibrary_LoadTracks_Executes(t *testing.T) {
	prov := &mockProvider{
		libraryTracks: []provider.Track{
			{ID: "t1", Title: "Track One", Artist: "Artist One"},
		},
	}
	lib := NewLibrary(prov)
	cmd := lib.loadTracks()
	if cmd == nil {
		t.Fatal("loadTracks() should return non-nil cmd")
	}
	msg := cmd() // execute the cmd
	loaded, ok := msg.(libraryLoadedMsg)
	if !ok {
		t.Fatalf("loadTracks cmd returned %T, want libraryLoadedMsg", msg)
	}
	if len(loaded.tracks) != 1 {
		t.Errorf("got %d tracks, want 1", len(loaded.tracks))
	}
}
