package views

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/provider"
)

// --- searchRow ---

func TestSearchRow_IsItem_Header(t *testing.T) {
	row := searchRow{header: true, label: "Tracks"}
	if row.isItem() {
		t.Error("header row should not be an item")
	}
}

func TestSearchRow_IsItem_Track(t *testing.T) {
	tr := provider.Track{Title: "Search Song"}
	row := searchRow{track: &tr}
	if !row.isItem() {
		t.Error("track row should be an item")
	}
}

func TestSearchRow_IsItem_Album(t *testing.T) {
	a := provider.Album{Title: "Search Album"}
	row := searchRow{album: &a}
	if !row.isItem() {
		t.Error("album row should be an item")
	}
}

func TestSearchRow_IsItem_Playlist(t *testing.T) {
	p := provider.Playlist{Name: "Search Playlist"}
	row := searchRow{playlist: &p}
	if !row.isItem() {
		t.Error("playlist row should be an item")
	}
}

func TestSearchRow_View_TrackRendersArtistAndAlbum(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetState([]provider.Track{
		{Title: "Search Song", Artist: "Search Artist", Album: "Search Album"},
	}, false, nil)
	v := s.View()
	if !strings.Contains(v, "Search Artist") {
		t.Errorf("View() should contain artist, got %q", v)
	}
	if !strings.Contains(v, "Search Album") {
		t.Errorf("View() should contain album, got %q", v)
	}
}

// --- searchResultItems ---

func TestSearchResultItems_Empty(t *testing.T) {
	result := &provider.SearchResult{}
	items := searchResultItems(result)
	if len(items) != 0 {
		t.Errorf("searchResultItems(empty) = %d items, want 0", len(items))
	}
}

func TestSearchResultItems_WithTracks(t *testing.T) {
	result := &provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "T1", Artist: "A1"},
			{Title: "T2", Artist: "A2"},
			{Title: "T3", Artist: "A3"},
		},
	}
	items := searchResultItems(result)
	if len(items) != 3 {
		t.Errorf("searchResultItems = %d items, want 3", len(items))
	}
}

// --- SearchModel ---

func TestNewSearch_NilProvider(t *testing.T) {
	s := NewSearch(nil)
	if s == nil {
		t.Fatal("NewSearch(nil) returned nil")
	}
}

func TestSearch_Focus_And_Focused(t *testing.T) {
	s := NewSearch(&mockProvider{})
	// Focus/Focused are no-ops; input is managed by the model
	s.Focus()
	if s.Focused() {
		t.Error("Focused() should always return false (input managed by model)")
	}
}

func TestSearch_SetSize_NoPanic(t *testing.T) {
	s := NewSearch(&mockProvider{})
	s.SetSize(80, 24) // should not panic
}

func TestSearch_Init(t *testing.T) {
	s := NewSearch(&mockProvider{})
	cmd := s.Init()
	if cmd != nil {
		t.Error("Init() should return nil cmd")
	}
}

func TestSearch_View_NonEmpty(t *testing.T) {
	s := NewSearch(&mockProvider{})
	s.SetSize(80, 24)
	s.SetState(nil, true, nil) // loading state → non-empty view
	got := s.View()
	if got == "" {
		t.Error("View() should return non-empty string when loading")
	}
}

func TestSearch_Update_SearchResultMsg(t *testing.T) {
	s := NewSearch(&mockProvider{})
	s.SetSize(80, 24)
	result := &provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "Found Track", Artist: "Found Artist"},
		},
	}
	// In the new design, search results are set via SetState (called by model.go).
	s.SetState(result.Tracks, false, nil)
	got := s.View()
	if got == "" {
		t.Error("View() after search result should return non-empty string")
	}
}

func TestSearch_Update_EscBlursInput(t *testing.T) {
	s := NewSearch(&mockProvider{})
	s.SetSize(80, 24)
	// Esc is now handled by the model; search model Update ignores key msgs
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	// Focused() always returns false in new design
	if s.Focused() {
		t.Error("Focused() should always return false")
	}
}

func TestSearch_Update_NonSearchMsg_NoPanic(t *testing.T) {
	s := NewSearch(&mockProvider{})
	s.SetSize(80, 24)
	_, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // should not panic
}

func TestSearch_ScheduleSearch_NonEmpty(t *testing.T) {
	s := NewSearch(&mockProvider{})
	cmd := s.scheduleSearch("hello")
	if cmd == nil {
		t.Error("scheduleSearch with non-empty query should return non-nil cmd")
	}
}

func TestSearch_ScheduleSearch_Empty(t *testing.T) {
	s := NewSearch(&mockProvider{})
	cmd := s.scheduleSearch("")
	if cmd != nil {
		t.Error("scheduleSearch with empty query should return nil cmd")
	}
}

func TestSearch_Update_TypeWhileFocused(t *testing.T) {
	s := NewSearch(&mockProvider{})
	s.SetSize(80, 24)
	// In the new design, typing is handled by the model, not search.
	// Verify Update handles non-searchResultMsg gracefully (no-op, no panic).
	s, cmd := s.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	_ = cmd // cmd may be nil — that is correct in the new design
	_ = s
}

// --- SelectedTrack ────────────────────────────────────────────────────────────

func TestSearch_SelectedTrack_NoResults(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	if got := s.SelectedTrack(); got != nil {
		t.Errorf("SelectedTrack() with no results = %v, want nil", got)
	}
}

func TestSearch_SelectedTrack_ReturnsFirstByDefault(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	tracks := []provider.Track{
		{Title: "First", Artist: "A", CatalogID: "111"},
		{Title: "Second", Artist: "B", CatalogID: "222"},
	}
	s.SetState(tracks, false, nil)

	got := s.SelectedTrack()
	if got == nil {
		t.Fatal("SelectedTrack() returned nil after SetState with tracks")
	}
	if got.Title != "First" {
		t.Errorf("SelectedTrack().Title = %q, want %q", got.Title, "First")
	}
}

func TestSearch_SelectedTrack_ChangesAfterNavigation(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	tracks := []provider.Track{
		{Title: "Alpha", CatalogID: "1"},
		{Title: "Beta", CatalogID: "2"},
		{Title: "Gamma", CatalogID: "3"},
	}
	s.SetState(tracks, false, nil)

	// Move down once — should select "Beta".
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	got := s.SelectedTrack()
	if got == nil {
		t.Fatal("SelectedTrack() returned nil after navigating down")
	}
	if got.Title != "Beta" {
		t.Errorf("SelectedTrack().Title after Down = %q, want %q", got.Title, "Beta")
	}
}

func TestSearch_SelectedIndex_TracksCursorPosition(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetState([]provider.Track{
		{Title: "A", CatalogID: "1"},
		{Title: "B", CatalogID: "2"},
	}, false, nil)

	if s.SelectedIndex() != 0 {
		t.Errorf("initial SelectedIndex() = %d, want 0", s.SelectedIndex())
	}
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.SelectedIndex() != 1 {
		t.Errorf("SelectedIndex() after Down = %d, want 1", s.SelectedIndex())
	}
}

func TestSearch_SetState_ResetsSelection(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetState([]provider.Track{
		{Title: "X", CatalogID: "x"},
		{Title: "Y", CatalogID: "y"},
	}, false, nil)

	// Navigate to second item.
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	// Replace results — cursor should reset to 0.
	s.SetState([]provider.Track{{Title: "New", CatalogID: "n"}}, false, nil)
	if s.SelectedIndex() != 0 {
		t.Errorf("SelectedIndex() after SetState = %d, want 0 (reset)", s.SelectedIndex())
	}
}

func TestSearch_Loading_View_NonemptyString(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetState(nil, true, nil)
	v := s.View()
	if v == "" {
		t.Error("View() during loading should return non-empty string")
	}
}

func TestSearch_EmptyResults_View_ReturnsEmpty(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetState(nil, false, nil)
	v := s.View()
	if v != "" {
		t.Errorf("View() with no results/loading/error should be empty, got %q", v)
	}
}

func TestSearch_ErrorState_View_ContainsError(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetState(nil, false, errors.New("network timeout"))
	v := s.View()
	if !strings.Contains(v, "network timeout") {
		t.Errorf("View() with error should contain error text, got %q", v)
	}
}

// --- Results, Loading, Focus, SetCursor, Cursor, PlaybackID ---

func TestSearch_Results_Empty(t *testing.T) {
	s := NewSearch(nil)
	if s.Results() != nil {
		t.Error("Results() on fresh SearchModel should be nil")
	}
}

func TestSearch_Results_AfterSetState(t *testing.T) {
	s := NewSearch(nil)
	tracks := []provider.Track{
		{Title: "Track X", CatalogID: "x"},
		{Title: "Track Y", CatalogID: "y"},
	}
	s.SetState(tracks, false, nil)
	got := s.Results()
	if len(got) != 2 {
		t.Errorf("Results() = %d items, want 2", len(got))
	}
}

func TestSearch_Loading_False(t *testing.T) {
	s := NewSearch(nil)
	if s.Loading() {
		t.Error("Loading() should be false on new SearchModel")
	}
}

func TestSearch_Loading_True(t *testing.T) {
	s := NewSearch(nil)
	s.SetState(nil, true, nil)
	if !s.Loading() {
		t.Error("Loading() should be true after SetState(loading=true)")
	}
}

func TestSearch_Focus_NoPanic(t *testing.T) {
	s := NewSearch(nil)
	s.Focus() // no-op, should not panic
}

func TestSearch_Focused_AlwaysFalse(t *testing.T) {
	s := NewSearch(nil)
	s.Focus()
	if s.Focused() {
		t.Error("Focused() should always return false")
	}
}

func TestSearch_SetCursor_NoPanic(t *testing.T) {
	s := NewSearch(nil)
	s.SetCursor(5) // no-op, should not panic
}

func TestSearch_Cursor_ReturnsListIndex(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetState([]provider.Track{
		{Title: "A", CatalogID: "a"},
		{Title: "B", CatalogID: "b"},
	}, false, nil)
	if s.Cursor() != 0 {
		t.Errorf("Cursor() = %d, want 0 initially", s.Cursor())
	}
}

// --- SetResults ---

func TestSearch_SetResults_TracksAlbumsPlaylists(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 40)
	result := &provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "Night Owl", Artist: "Chet Baker", CatalogID: "c1"},
		},
		Albums: []provider.Album{
			{ID: "a1", Title: "Chet", Artist: "Chet Baker", TrackCount: 11},
		},
		Playlists: []provider.Playlist{
			{ID: "pl1", Name: "Jazz Classics", TrackCount: 20},
		},
	}
	s.SetResults(result, false, nil)

	if s.Loading() {
		t.Error("Loading() should be false after SetResults")
	}
	v := s.View()
	if !strings.Contains(v, "Tracks") {
		t.Errorf("View() should contain Tracks header, got: %q", v)
	}
	if !strings.Contains(v, "Albums") {
		t.Errorf("View() should contain Albums header, got: %q", v)
	}
	if !strings.Contains(v, "Playlists") {
		t.Errorf("View() should contain Playlists header, got: %q", v)
	}
	if !strings.Contains(v, "Night Owl") {
		t.Errorf("View() should contain track title, got: %q", v)
	}
	if !strings.Contains(v, "Chet") {
		t.Errorf("View() should contain album title, got: %q", v)
	}
	if !strings.Contains(v, "Jazz Classics") {
		t.Errorf("View() should contain playlist name, got: %q", v)
	}
	if !strings.Contains(v, "[album]") {
		t.Errorf("View() should contain [album] tag, got: %q", v)
	}
	if !strings.Contains(v, "[playlist]") {
		t.Errorf("View() should contain [playlist] tag, got: %q", v)
	}
}

func TestSearch_SetResults_OnlyAlbums(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	result := &provider.SearchResult{
		Albums: []provider.Album{
			{ID: "a1", Title: "Kind of Blue", Artist: "Miles Davis", TrackCount: 5},
			{ID: "a2", Title: "A Love Supreme", Artist: "John Coltrane", TrackCount: 4},
		},
	}
	s.SetResults(result, false, nil)

	v := s.View()
	if !strings.Contains(v, "Albums") {
		t.Errorf("View() should contain Albums header, got: %q", v)
	}
	if strings.Contains(v, "Tracks") {
		t.Errorf("View() should NOT contain Tracks header when there are no tracks, got: %q", v)
	}
	if !strings.Contains(v, "Kind of Blue") {
		t.Errorf("View() should contain album title, got: %q", v)
	}
}

func TestSearch_SetResults_OnlyPlaylists(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	result := &provider.SearchResult{
		Playlists: []provider.Playlist{
			{ID: "pl1", Name: "Morning Mix", TrackCount: 15},
		},
	}
	s.SetResults(result, false, nil)

	v := s.View()
	if !strings.Contains(v, "Playlists") {
		t.Errorf("View() should contain Playlists header, got: %q", v)
	}
	if !strings.Contains(v, "Morning Mix") {
		t.Errorf("View() should contain playlist name, got: %q", v)
	}
	if strings.Contains(v, "Tracks") {
		t.Errorf("View() should NOT contain Tracks header when there are no tracks, got: %q", v)
	}
}

func TestSearch_SetResults_ResetsSelectionToFirstItem(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	// First load: tracks only; navigate down.
	s.SetResults(&provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "A", CatalogID: "a"},
			{Title: "B", CatalogID: "b"},
		},
	}, false, nil)
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.SelectedIndex() != 1 {
		t.Fatalf("expected index 1 before reset, got %d", s.SelectedIndex())
	}

	// Second load: new results should reset cursor to first item.
	s.SetResults(&provider.SearchResult{
		Tracks: []provider.Track{{Title: "X", CatalogID: "x"}},
	}, false, nil)
	if s.SelectedIndex() != 0 {
		t.Errorf("SetResults should reset SelectedIndex to 0, got %d", s.SelectedIndex())
	}
}

// --- SelectedAlbum ---

func TestSearch_SelectedAlbum_NoResults(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	if got := s.SelectedAlbum(); got != nil {
		t.Errorf("SelectedAlbum() with no results = %v, want nil", got)
	}
}

func TestSearch_SelectedAlbum_FirstByDefault(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	// Albums only — first item should be auto-selected.
	s.SetResults(&provider.SearchResult{
		Albums: []provider.Album{
			{ID: "a1", Title: "Blue Train", Artist: "Coltrane"},
			{ID: "a2", Title: "Giant Steps", Artist: "Coltrane"},
		},
	}, false, nil)

	got := s.SelectedAlbum()
	if got == nil {
		t.Fatal("SelectedAlbum() returned nil when albums are present")
	}
	if got.Title != "Blue Train" {
		t.Errorf("SelectedAlbum().Title = %q, want %q", got.Title, "Blue Train")
	}
	if s.SelectedTrack() != nil {
		t.Error("SelectedTrack() should be nil when an album is selected")
	}
	if s.SelectedPlaylist() != nil {
		t.Error("SelectedPlaylist() should be nil when an album is selected")
	}
}

func TestSearch_SelectedAlbum_NavigateDown(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetResults(&provider.SearchResult{
		Albums: []provider.Album{
			{ID: "a1", Title: "First Album"},
			{ID: "a2", Title: "Second Album"},
		},
	}, false, nil)

	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	got := s.SelectedAlbum()
	if got == nil {
		t.Fatal("SelectedAlbum() returned nil after navigating down")
	}
	if got.Title != "Second Album" {
		t.Errorf("SelectedAlbum().Title = %q, want %q", got.Title, "Second Album")
	}
}

// --- SelectedPlaylist ---

func TestSearch_SelectedPlaylist_NoResults(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	if got := s.SelectedPlaylist(); got != nil {
		t.Errorf("SelectedPlaylist() with no results = %v, want nil", got)
	}
}

func TestSearch_SelectedPlaylist_FirstByDefault(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetResults(&provider.SearchResult{
		Playlists: []provider.Playlist{
			{ID: "pl1", Name: "Chill Vibes", TrackCount: 10},
			{ID: "pl2", Name: "Workout", TrackCount: 20},
		},
	}, false, nil)

	got := s.SelectedPlaylist()
	if got == nil {
		t.Fatal("SelectedPlaylist() returned nil when playlists are present")
	}
	if got.Name != "Chill Vibes" {
		t.Errorf("SelectedPlaylist().Name = %q, want %q", got.Name, "Chill Vibes")
	}
	if s.SelectedTrack() != nil {
		t.Error("SelectedTrack() should be nil when a playlist is selected")
	}
	if s.SelectedAlbum() != nil {
		t.Error("SelectedAlbum() should be nil when a playlist is selected")
	}
}

// --- Cross-section navigation ---

func TestSearch_Navigation_CrossesSectionBoundary(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 60)
	s.SetResults(&provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "Only Track", CatalogID: "t1"},
		},
		Albums: []provider.Album{
			{ID: "a1", Title: "Only Album"},
		},
	}, false, nil)

	// Initially on the track.
	if s.SelectedTrack() == nil {
		t.Fatal("expected track to be selected initially")
	}

	// One down: should move to the album (crosses the Albums header).
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.SelectedAlbum() == nil {
		t.Fatalf("expected album to be selected after navigating down past the Tracks section")
	}
	if s.SelectedAlbum().Title != "Only Album" {
		t.Errorf("SelectedAlbum().Title = %q, want %q", s.SelectedAlbum().Title, "Only Album")
	}

	// One up: should go back to the track.
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if s.SelectedTrack() == nil {
		t.Fatalf("expected track to be re-selected after navigating up")
	}
	if s.SelectedTrack().Title != "Only Track" {
		t.Errorf("SelectedTrack().Title = %q, want %q", s.SelectedTrack().Title, "Only Track")
	}
}

func TestSearch_Navigation_AllThreeSections(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 60)
	s.SetResults(&provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "Track One", CatalogID: "t1"},
		},
		Albums: []provider.Album{
			{ID: "a1", Title: "Album One"},
		},
		Playlists: []provider.Playlist{
			{ID: "pl1", Name: "Playlist One"},
		},
	}, false, nil)

	// Start: track selected.
	if s.SelectedTrack() == nil {
		t.Fatal("step 0: expected track")
	}

	// Step 1: album.
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.SelectedAlbum() == nil {
		t.Fatal("step 1: expected album")
	}

	// Step 2: playlist.
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.SelectedPlaylist() == nil {
		t.Fatal("step 2: expected playlist")
	}

	// Step 3: no further — stays on playlist.
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.SelectedPlaylist() == nil {
		t.Fatal("step 3: expected still playlist at end of list")
	}
}

func TestSearch_Navigation_PgDown_SkipsMultipleItems(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 60)
	tracks := make([]provider.Track, 8)
	for i := range tracks {
		tracks[i] = provider.Track{Title: fmt.Sprintf("Track %d", i+1), CatalogID: fmt.Sprintf("t%d", i+1)}
	}
	s.SetResults(&provider.SearchResult{Tracks: tracks}, false, nil)

	startIdx := s.SelectedIndex()
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if s.SelectedIndex() <= startIdx {
		t.Errorf("PgDown should advance cursor; got index %d (was %d)", s.SelectedIndex(), startIdx)
	}
}

// --- Album track count display ---

func TestSearch_AlbumWithTrackCount_ShowsCount(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetResults(&provider.SearchResult{
		Albums: []provider.Album{
			{ID: "a1", Title: "Thriller", Artist: "Michael Jackson", TrackCount: 9},
		},
	}, false, nil)

	v := s.View()
	if !strings.Contains(v, "9 tracks") {
		t.Errorf("View() should show track count, got: %q", v)
	}
	if !strings.Contains(v, "Michael Jackson") {
		t.Errorf("View() should show artist name, got: %q", v)
	}
}

func TestSearch_AlbumWithoutTrackCount_NoCountShown(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetResults(&provider.SearchResult{
		Albums: []provider.Album{
			{ID: "a1", Title: "Unknown Album", Artist: "Someone", TrackCount: 0},
		},
	}, false, nil)

	v := s.View()
	if strings.Contains(v, "tracks") {
		t.Errorf("View() should NOT show '0 tracks', got: %q", v)
	}
}

func TestSearch_PlaylistWithTrackCount_ShowsCount(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 20)
	s.SetResults(&provider.SearchResult{
		Playlists: []provider.Playlist{
			{ID: "pl1", Name: "Study Mix", TrackCount: 42},
		},
	}, false, nil)

	v := s.View()
	if !strings.Contains(v, "42 tracks") {
		t.Errorf("View() should show track count for playlist, got: %q", v)
	}
}

// --- SelectedIndex with mixed sections ---

// --- Scroll regression tests ---

func TestSearch_ScrollUp_SectionHeaderRemainsVisible(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 6) // tight height: only room for header + 2 items
	tracks := []provider.Track{
		{Title: "Alpha", CatalogID: "t1"},
		{Title: "Beta", CatalogID: "t2"},
		{Title: "Gamma", CatalogID: "t3"},
		{Title: "Delta", CatalogID: "t4"},
	}
	s.SetResults(&provider.SearchResult{Tracks: tracks}, false, nil)

	// Navigate down far enough that the Tracks header scrolls out of view.
	for range 3 {
		s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	// Now navigate back up to the first item.
	for range 3 {
		s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	}

	// The scroll position must be 0 so the "Tracks" header is visible.
	if s.scroll != 0 {
		t.Errorf("after scrolling up to first item, scroll = %d, want 0 (header must be visible)", s.scroll)
	}
	v := s.View()
	if !strings.Contains(v, "Tracks") {
		t.Errorf("section header should be visible after scrolling back to the top, view: %q", v)
	}
}

func TestSearch_ScrollUp_AlbumSectionHeaderRemainsVisible(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 6)
	s.SetResults(&provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "T1", CatalogID: "t1"},
		},
		Albums: []provider.Album{
			{ID: "a1", Title: "Album One"},
			{ID: "a2", Title: "Album Two"},
			{ID: "a3", Title: "Album Three"},
		},
	}, false, nil)

	// Navigate into the Albums section and then scroll down a bit.
	for range 4 {
		s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	// Navigate back up to the first album (index just after the Albums header).
	for range 2 {
		s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	}

	v := s.View()
	if !strings.Contains(v, "Albums") {
		t.Errorf("Albums header should be visible after scrolling back up, view: %q", v)
	}
}

func TestSearch_ColorSeeding_PlaylistsWhenHeaderScrolledPast(t *testing.T) {
	s := NewSearch(nil)
	// Give enough height to see items but not the full list.
	s.SetSize(80, 6)
	s.SetResults(&provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "T1", CatalogID: "t1"},
			{Title: "T2", CatalogID: "t2"},
			{Title: "T3", CatalogID: "t3"},
		},
		Playlists: []provider.Playlist{
			{ID: "pl1", Name: "My Playlist", TrackCount: 5},
		},
	}, false, nil)

	// Navigate all the way down to the playlist.
	for range 4 {
		s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	// The selected item must be the playlist.
	if s.SelectedPlaylist() == nil {
		t.Fatal("expected playlist to be selected after navigating down")
	}

	// View() must render without panic and contain the playlist name.
	// (The colour-seeding fix ensures currentAccent is Playlists green,
	// not the default Tracks amber, even when the Playlists header has
	// scrolled out of the viewport.)
	v := s.View()
	if !strings.Contains(v, "My Playlist") {
		t.Errorf("playlist name should be visible in the view, got: %q", v)
	}
}

func TestSearch_ColorSeeding_AlbumsWhenHeaderScrolledPast(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 4) // very tight: forces the Albums header to scroll past
	s.SetResults(&provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "T1", CatalogID: "t1"},
			{Title: "T2", CatalogID: "t2"},
			{Title: "T3", CatalogID: "t3"},
		},
		Albums: []provider.Album{
			{ID: "a1", Title: "Album Visible"},
		},
	}, false, nil)

	// Navigate to the album (past all tracks and their header).
	for range 4 {
		s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}

	if s.SelectedAlbum() == nil {
		t.Fatal("expected album to be selected")
	}

	// Must not panic and must contain the album title.
	v := s.View()
	if !strings.Contains(v, "Album Visible") {
		t.Errorf("album title should be visible, got: %q", v)
	}
}

func TestSearch_SelectedIndex_MixedSections(t *testing.T) {
	s := NewSearch(nil)
	s.SetSize(80, 60)
	s.SetResults(&provider.SearchResult{
		Tracks: []provider.Track{
			{Title: "T1", CatalogID: "t1"},
			{Title: "T2", CatalogID: "t2"},
		},
		Albums: []provider.Album{
			{ID: "a1", Title: "A1"},
		},
	}, false, nil)

	// Item 0 = T1
	if s.SelectedIndex() != 0 {
		t.Errorf("initial SelectedIndex = %d, want 0", s.SelectedIndex())
	}
	// Item 1 = T2
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.SelectedIndex() != 1 {
		t.Errorf("after 1 down SelectedIndex = %d, want 1", s.SelectedIndex())
	}
	// Item 2 = A1 (crosses the Albums header)
	s, _ = s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.SelectedIndex() != 2 {
		t.Errorf("after 2 down SelectedIndex = %d, want 2", s.SelectedIndex())
	}
	if s.SelectedAlbum() == nil {
		t.Error("item 2 should be an album")
	}
}

func TestPlaybackID_CatalogID(t *testing.T) {
	// Non-library track with a CatalogID set: return catalog ID.
	track := provider.Track{ID: "library-id", CatalogID: "catalog-id"}
	got := PlaybackID(track)
	if got != "catalog-id" {
		t.Errorf("PlaybackID(with catalogID) = %q, want %q", got, "catalog-id")
	}
}

func TestPlaybackID_LibraryTrackWithCatalogMatch(t *testing.T) {
	// Library track (i. prefix) that has been matched to a catalog entry:
	// must return the library ID to avoid CONTENT_RESTRICTED on the catalog copy.
	track := provider.Track{ID: "i.library-id", CatalogID: "catalog-id"}
	got := PlaybackID(track)
	if got != "i.library-id" {
		t.Errorf("PlaybackID(library+catalogID) = %q, want %q", got, "i.library-id")
	}
}

func TestPlaybackID_LibraryID(t *testing.T) {
	track := provider.Track{ID: "i.library-id", CatalogID: ""}
	got := PlaybackID(track)
	if got != "i.library-id" {
		t.Errorf("PlaybackID(no catalogID) = %q, want %q", got, "i.library-id")
	}
}
