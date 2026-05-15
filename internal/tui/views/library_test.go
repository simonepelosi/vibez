package views

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
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
	lib.libraryRequestGeneration = 1
	lib.libraryRequestSection = sectionPlaylists
	lib.libraryRequestKind = libraryRequestPlaylists
	lib.selectedSection = sectionPlaylists
	msg := libraryLoadedMsg{
		generation: 1,
		section:    sectionPlaylists,
		kind:       libraryRequestPlaylists,
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

func TestLibrary_Update_LibraryLoadedMsg_Error(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	lib.loading = true
	lib.libraryRequestGeneration = 1
	lib.libraryRequestSection = sectionPlaylists
	lib.libraryRequestKind = libraryRequestPlaylists
	lib.selectedSection = sectionPlaylists
	msg := libraryLoadedMsg{generation: 1, section: sectionPlaylists, kind: libraryRequestPlaylists, err: errors.New("load failed")}
	lib, _ = lib.Update(msg)
	// loading should be cleared even on error
	if lib.loading {
		t.Error("loading should be false after error msg")
	}
}

func TestLibrary_Update_TabKey(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 24)
	// Tab key has no effect with single-tab library; just verify no panic.
	_, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyTab})
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
	// Load playlists and verify no panic.
	lib.libraryRequestGeneration = 1
	lib.libraryRequestSection = sectionPlaylists
	lib.libraryRequestKind = libraryRequestPlaylists
	lib.selectedSection = sectionPlaylists
	lib, _ = lib.Update(libraryLoadedMsg{
		generation: 1,
		section:    sectionPlaylists,
		kind:       libraryRequestPlaylists,
		playlists:  []provider.Playlist{{Name: "PL", TrackCount: 5}},
	})
	if lib.list.Items() == nil {
		t.Error("expected non-nil items after loading playlists")
	}
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
	lib.selectedSection = sectionPlaylists
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
	if loaded.generation == 0 {
		t.Fatal("libraryLoadedMsg.generation = 0, want non-zero")
	}
	if loaded.section != sectionPlaylists {
		t.Fatalf("libraryLoadedMsg.section = %d, want %d", loaded.section, sectionPlaylists)
	}
	if loaded.kind != libraryRequestPlaylists {
		t.Fatalf("libraryLoadedMsg.kind = %d, want %d", loaded.kind, libraryRequestPlaylists)
	}
}

func TestLibrary_LoadPlaylistTracks_Executes(t *testing.T) {
	prov := &mockProvider{} // returns nil tracks, nil error by default
	lib := NewLibrary(prov)
	pl := provider.Playlist{ID: "pl-123", Name: "Test Playlist"}
	cmd := lib.loadPlaylistTracks(pl)
	if cmd == nil {
		t.Fatal("loadPlaylistTracks() should return non-nil cmd")
	}
	msg := cmd()
	ptm, ok := msg.(playlistTracksMsg)
	if !ok {
		t.Fatalf("loadPlaylistTracks returned %T, want playlistTracksMsg", msg)
	}
	if ptm.playlist.ID != "pl-123" {
		t.Errorf("playlistTracksMsg.playlist.ID = %q, want %q", ptm.playlist.ID, "pl-123")
	}
	if ptm.generation == 0 {
		t.Fatal("playlistTracksMsg.generation = 0, want non-zero")
	}
	if ptm.kind != playlistRequestTracks {
		t.Fatalf("playlistTracksMsg.kind = %d, want %d", ptm.kind, playlistRequestTracks)
	}
}

// --- renderDrillView ---

func TestLibrary_RenderDrillView_Loading(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	lib.pane = paneTracks
	lib.drillLoading = true
	lib.drillPlaylist = provider.Playlist{Name: "My Playlist"}
	view := lib.renderDrillView()
	if view == "" {
		t.Error("renderDrillView() while loading should return non-empty string")
	}
}

func TestLibrary_RenderDrillView_EmptyTracks(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	lib.pane = paneTracks
	lib.drillLoading = false
	lib.drillPlaylist = provider.Playlist{Name: "Empty Playlist"}
	lib.drillTracks = nil
	view := lib.renderDrillView()
	if view == "" {
		t.Error("renderDrillView() with empty tracks should return non-empty string")
	}
}

func TestLibrary_RenderDrillView_WithTracks(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	lib.pane = paneTracks
	lib.drillLoading = false
	lib.drillPlaylist = provider.Playlist{Name: "Full Playlist", ID: "pl1"}
	lib.drillTracks = []provider.Track{
		{Title: "Track 1", Artist: "Artist 1"},
		{Title: "Track 2", Artist: "Artist 2"},
	}
	// Set drill list items.
	lib.drillRequestGeneration = 1
	lib.drillRequestKind = playlistRequestTracks
	lib.Update(playlistTracksMsg{
		playlist:   lib.drillPlaylist,
		generation: 1,
		kind:       playlistRequestTracks,
		tracks:     lib.drillTracks,
	})
	view := lib.renderDrillView()
	if view == "" {
		t.Error("renderDrillView() with tracks should return non-empty string")
	}
}

// --- Library.View() selecting drill pane ---

func TestLibrary_View_DrillPane(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	lib.pane = paneTracks
	lib.drillPlaylist = provider.Playlist{Name: "Drill Playlist"}
	view := lib.View()
	if view == "" {
		t.Error("View() in drill pane should return non-empty string")
	}
}

// --- Library.Update with playlistTracksMsg (drill pane) ---

func TestLibrary_Update_PlaylistTracksMsg_Success(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	pl := provider.Playlist{ID: "pl1", Name: "Playlist"}
	tracks := []provider.Track{{Title: "Song", Artist: "Artist"}}
	lib.pane = paneTracks
	lib.drillPlaylist = pl
	lib.drillRequestGeneration = 1
	lib.drillRequestKind = playlistRequestTracks
	updated, _ := lib.Update(playlistTracksMsg{playlist: pl, generation: 1, kind: playlistRequestTracks, tracks: tracks})
	if len(updated.drillTracks) != 1 {
		t.Errorf("drillTracks after playlistTracksMsg = %d, want 1", len(updated.drillTracks))
	}
}

func TestLibrary_Update_PlaylistTracksMsg_Error(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	pl := provider.Playlist{ID: "pl1", Name: "Playlist"}
	lib.pane = paneTracks
	lib.drillPlaylist = pl
	lib.drillLoading = true
	lib.drillRequestGeneration = 1
	lib.drillRequestKind = playlistRequestTracks
	updated, _ := lib.Update(playlistTracksMsg{playlist: pl, generation: 1, kind: playlistRequestTracks, err: errors.New("load error")})
	if updated.drillLoading {
		t.Error("drillLoading should be false after error")
	}
}

func TestLibrary_Update_PlaylistTracksMsg_DropsStaleResponse(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	pl := provider.Playlist{ID: "pl1", Name: "Playlist"}
	tracks := []provider.Track{{Title: "Song", Artist: "Artist"}}
	lib.pane = paneItems
	lib.drillPlaylist = pl
	lib.drillLoading = true
	lib.drillRequestGeneration = 1
	lib.drillRequestKind = playlistRequestTracks

	updated, _ := lib.Update(playlistTracksMsg{playlist: pl, generation: 1, kind: playlistRequestTracks, tracks: tracks})
	if updated.pane != paneItems {
		t.Fatalf("stale playlist response changed pane = %d, want %d", updated.pane, paneItems)
	}
	if len(updated.drillTracks) != 0 {
		t.Fatalf("stale playlist response set %d drill tracks, want 0", len(updated.drillTracks))
	}
	if !updated.drillLoading {
		t.Fatal("stale playlist response should not clear current drill loading state")
	}
}

func TestLibrary_PlaylistResponseAfterBackAndSongsDoesNotOverwriteSongs(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	playlist := provider.Playlist{ID: "pl1", Name: "Playlist"}
	playlistTracks := []provider.Track{{ID: "p.1", Title: "Playlist Song"}}
	songTracks := []provider.Track{{ID: "s.1", Title: "Song Pane Track"}}

	cmd := lib.loadPlaylistTracks(playlist)
	msg := cmd().(playlistTracksMsg)
	lib.pane = paneTracks
	lib.drillPlaylist = playlist
	lib.drillTitle = playlist.Name
	lib.tracksBackPane = paneItems

	lib, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	lib.showTrackPane("Songs", songTracks, paneSections)
	lib, _ = lib.Update(playlistTracksMsg{playlist: playlist, generation: msg.generation, kind: msg.kind, tracks: playlistTracks})

	if lib.pane != paneTracks {
		t.Fatalf("pane = %d, want %d", lib.pane, paneTracks)
	}
	if lib.drillLoading {
		t.Fatal("stale playlist response left drill loading stuck")
	}
	if len(lib.drillTracks) != 1 || lib.drillTracks[0].ID != "s.1" {
		t.Fatalf("drillTracks = %+v, want songs pane tracks", lib.drillTracks)
	}
}

func TestLibrary_PlaylistAResponseIgnoredAfterOpeningPlaylistB(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	playlistA := provider.Playlist{ID: "a", Name: "Playlist A"}
	playlistB := provider.Playlist{ID: "b", Name: "Playlist B"}
	tracksA := []provider.Track{{ID: "a.1", Title: "A"}}
	tracksB := []provider.Track{{ID: "b.1", Title: "B"}}

	cmdA := lib.loadPlaylistTracks(playlistA)
	msgA := cmdA().(playlistTracksMsg)
	lib.pane = paneTracks
	lib.drillPlaylist = playlistA

	cmdB := lib.loadPlaylistTracks(playlistB)
	msgB := cmdB().(playlistTracksMsg)
	lib.drillPlaylist = playlistB

	lib, _ = lib.Update(playlistTracksMsg{playlist: playlistA, generation: msgA.generation, kind: msgA.kind, tracks: tracksA})
	if len(lib.drillTracks) != 0 {
		t.Fatalf("stale playlist A set tracks: %+v", lib.drillTracks)
	}
	if !lib.drillLoading {
		t.Fatal("stale playlist A cleared playlist B loading")
	}

	lib, _ = lib.Update(playlistTracksMsg{playlist: playlistB, generation: msgB.generation, kind: msgB.kind, tracks: tracksB})
	if lib.drillLoading {
		t.Fatal("valid playlist B response should clear loading")
	}
	if len(lib.drillTracks) != 1 || lib.drillTracks[0].ID != "b.1" {
		t.Fatalf("drillTracks = %+v, want playlist B tracks", lib.drillTracks)
	}
}

func TestLibrary_CurrentPlaylistResponseAccepted(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	playlist := provider.Playlist{ID: "pl1", Name: "Playlist"}
	tracks := []provider.Track{{ID: "t.1", Title: "Track"}}

	cmd := lib.loadPlaylistTracks(playlist)
	msg := cmd().(playlistTracksMsg)
	lib.pane = paneTracks
	lib.drillPlaylist = playlist

	lib, _ = lib.Update(playlistTracksMsg{playlist: playlist, generation: msg.generation, kind: msg.kind, tracks: tracks})
	if lib.drillLoading {
		t.Fatal("valid playlist response should clear loading")
	}
	if len(lib.drillTracks) != 1 || lib.drillTracks[0].ID != "t.1" {
		t.Fatalf("drillTracks = %+v, want current playlist tracks", lib.drillTracks)
	}
}

func TestLibrary_StalePlaylistsResponseAfterNavigatingSongsDoesNotReplaceSongs(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	playlists := []provider.Playlist{{ID: "p.1", Name: "Slow Playlist"}}
	songs := []provider.Track{{ID: "s.1", Title: "Current Song"}}

	lib.selectedSection = sectionPlaylists
	playlistsCmd := lib.loadPlaylists()
	playlistsMsg := playlistsCmd().(libraryLoadedMsg)

	lib.selectedSection = sectionSongs
	songsCmd := lib.loadLibraryTracks()
	songsMsg := songsCmd().(libraryTracksLoadedMsg)
	lib, _ = lib.Update(libraryTracksLoadedMsg{generation: songsMsg.generation, section: songsMsg.section, kind: songsMsg.kind, tracks: songs})
	lib, _ = lib.Update(libraryLoadedMsg{generation: playlistsMsg.generation, section: playlistsMsg.section, kind: playlistsMsg.kind, playlists: playlists})

	if lib.pane != paneTracks {
		t.Fatalf("pane = %d, want %d", lib.pane, paneTracks)
	}
	if len(lib.drillTracks) != 1 || lib.drillTracks[0].ID != "s.1" {
		t.Fatalf("drillTracks = %+v, want current songs", lib.drillTracks)
	}
	if lib.loading {
		t.Fatal("stale playlists response cleared/changed current loading state")
	}
}

func TestLibrary_StaleSongsResponseAfterNavigatingPlaylistsDoesNotOverwritePlaylists(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	songs := []provider.Track{{ID: "s.1", Title: "Slow Song"}}
	playlists := []provider.Playlist{{ID: "p.1", Name: "Current Playlist"}}

	lib.selectedSection = sectionSongs
	songsCmd := lib.loadLibraryTracks()
	songsMsg := songsCmd().(libraryTracksLoadedMsg)

	lib.selectedSection = sectionPlaylists
	playlistsCmd := lib.loadPlaylists()
	playlistsMsg := playlistsCmd().(libraryLoadedMsg)
	lib, _ = lib.Update(libraryTracksLoadedMsg{generation: songsMsg.generation, section: songsMsg.section, kind: songsMsg.kind, tracks: songs})

	if !lib.loading {
		t.Fatal("stale songs response cleared current playlists loading")
	}
	if lib.pane != paneSections {
		t.Fatalf("pane = %d, want sections while playlists load", lib.pane)
	}

	lib, _ = lib.Update(libraryLoadedMsg{generation: playlistsMsg.generation, section: playlistsMsg.section, kind: playlistsMsg.kind, playlists: playlists})
	if lib.loading {
		t.Fatal("valid playlists response should clear loading")
	}
	if lib.pane != paneItems || len(lib.playlists) != 1 || lib.playlists[0].ID != "p.1" {
		t.Fatalf("playlists pane = %d playlists = %+v, want current playlists", lib.pane, lib.playlists)
	}
}

func TestLibrary_CurrentTopLevelResponseAccepted(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	tracks := []provider.Track{{ID: "s.1", Title: "Accepted Song"}}

	lib.selectedSection = sectionSongs
	cmd := lib.loadLibraryTracks()
	msg := cmd().(libraryTracksLoadedMsg)
	lib, _ = lib.Update(libraryTracksLoadedMsg{generation: msg.generation, section: msg.section, kind: msg.kind, tracks: tracks})

	if lib.loading {
		t.Fatal("valid tracks response should clear loading")
	}
	if lib.pane != paneTracks || len(lib.drillTracks) != 1 || lib.drillTracks[0].ID != "s.1" {
		t.Fatalf("tracks pane = %d drillTracks = %+v, want accepted songs", lib.pane, lib.drillTracks)

	}
}

// --- Library.Update: drill pane key handling ---

func TestLibrary_Update_DrillPane_Esc_ReturnsToList(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	lib.pane = paneTracks
	lib.drillTracks = []provider.Track{{Title: "T1"}}

	updated, _ := lib.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if updated.pane != paneItems {
		t.Error("esc in drill pane should return to list pane")
	}
}

func TestLibrary_LoadCommandsUseTimeoutContexts(t *testing.T) {
	prov := &mockProvider{}
	lib := NewLibrary(prov)

	lib.loadLibraryTracks()()
	if _, ok := prov.libraryTrackCtx.Deadline(); !ok {
		t.Fatal("GetLibraryTracks context missing deadline")
	}

	lib.loadPlaylists()()
	if _, ok := prov.playlistCtx.Deadline(); !ok {
		t.Fatal("GetLibraryPlaylists context missing deadline")
	}

	lib.loadPlaylistTracks(provider.Playlist{ID: "p1"})()
	if _, ok := prov.playlistTrackCtx.Deadline(); !ok {
		t.Fatal("GetPlaylistTracks context missing deadline")
	}
}

func TestLibrary_OpenSelectedSectionRefreshesExpiredTracks(t *testing.T) {
	prov := &mockProvider{libraryTracks: []provider.Track{{ID: "i.1", Title: "One"}}}
	lib := NewLibrary(prov)
	lib.selectedSection = sectionSongs
	lib.tracksLoaded = true
	lib.libraryTracksTime = time.Now().Add(-libraryTracksTTL)

	_, cmd := lib.openSelectedSection()
	if cmd == nil {
		t.Fatal("expired library tracks should trigger reload")
	}
}

func TestLibrary_UnknownSectionDoesNotPanic(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.selectedSection = librarySection(99)

	updated, cmd := lib.openSelectedSection()
	if cmd != nil {
		t.Fatal("unknown section should not return load command")
	}
	if updated.LoadErr() == nil {
		t.Fatal("unknown section should record load error")
	}
	if got := sectionTitle(librarySection(99)); got != "Library" {
		t.Fatalf("sectionTitle(unknown) = %q, want Library", got)
	}
}

func TestLibrary_Update_DrillPane_Enter_NoSelection(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	lib.pane = paneTracks
	lib.drillTracks = nil

	updated, cmd := lib.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = cmd
	if updated.pane != paneTracks {
		t.Error("enter with no selection in drill pane should stay in paneTracks")
	}
}

// --- Library.Update: tab cycling ---

func TestLibrary_Update_Tab_CyclesTabs(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	// Tab key has no effect (single tab); verify no panic.
	_, _ = lib.Update(tea.KeyPressMsg{Code: tea.KeyTab})
}

// --- Library: playlistItem.Description no TrackCount ---

func TestPlaylistItem_Description_NoTrackCount(t *testing.T) {
	pl := playlistItem(provider.Playlist{Name: "No Count Playlist", TrackCount: 0})
	got := pl.Description()
	if got == "" {
		t.Error("Description() with 0 track count should return non-empty")
	}
}

// --- Library: spinner tick msg (loading) ---

func TestLibrary_Update_SpinnerTick_WhenLoadingDrillList(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.drillLoading = true
	// Should not panic.
	lib.Update(spinner.TickMsg{ID: 1, Time: time.Now()})
}

func TestLibrary_Back_ReturnsFromDrillPane(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.pane = paneTracks
	lib.tracksBackPane = paneItems
	if !lib.Back() {
		t.Fatal("Back() = false, want true")
	}
	if lib.pane != paneItems {
		t.Fatalf("pane = %v, want paneItems", lib.pane)
	}
	if lib.Back() {
		t.Fatal("Back() at items pane = true, want false")
	}
}

func TestLibrary_RenderDrillView_Error(t *testing.T) {
	lib := NewLibrary(&mockProvider{})
	lib.SetSize(80, 20)
	lib.pane = paneTracks
	lib.drillErr = errors.New("apple 404")
	view := lib.renderDrillView()
	if !strings.Contains(view, "Could not load tracks: apple 404") {
		t.Fatalf("error not rendered: %q", view)
	}
}

func TestLibrary_Update_DrillPane_BackKeys(t *testing.T) {
	for _, key := range []string{"left", "h", "esc", "backspace"} {
		lib := NewLibrary(&mockProvider{})
		lib.pane = paneTracks
		lib.tracksBackPane = paneItems
		updated, _ := lib.Update(tea.KeyPressMsg{Text: key})
		if updated.pane != paneItems {
			t.Fatalf("%s pane = %v, want paneItems", key, updated.pane)
		}
	}
}
