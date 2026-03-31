package views

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/simone-vibes/vibez/internal/provider"
)

// --- searchTrackItem ---

func TestSearchTrackItem_Title(t *testing.T) {
	item := searchTrackItem{t: provider.Track{Title: "Search Song"}}
	if item.Title() != "Search Song" {
		t.Errorf("Title() = %q, want %q", item.Title(), "Search Song")
	}
}

func TestSearchTrackItem_Description(t *testing.T) {
	item := searchTrackItem{t: provider.Track{Artist: "Search Artist", Album: "Search Album"}}
	got := item.Description()
	if !strings.Contains(got, "Search Artist") {
		t.Errorf("Description() should contain artist, got %q", got)
	}
	if !strings.Contains(got, "Search Album") {
		t.Errorf("Description() should contain album, got %q", got)
	}
}

func TestSearchTrackItem_FilterValue(t *testing.T) {
	item := searchTrackItem{t: provider.Track{Title: "Filter Track"}}
	if item.FilterValue() != "Filter Track" {
		t.Errorf("FilterValue() = %q, want %q", item.FilterValue(), "Filter Track")
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
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	// Focused() always returns false in new design
	if s.Focused() {
		t.Error("Focused() should always return false")
	}
}

func TestSearch_Update_NonSearchMsg_NoPanic(t *testing.T) {
	s := NewSearch(&mockProvider{})
	s.SetSize(80, 24)
	_, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown}) // should not panic
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
	s, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
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
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})

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
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})
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
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyDown})

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
