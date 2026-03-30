package views

import (
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
	if s.Focused() {
		t.Error("should not be focused initially")
	}
	s.Focus()
	if !s.Focused() {
		t.Error("should be focused after Focus()")
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
	got := s.View()
	if got == "" {
		t.Error("View() should return non-empty string")
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
	s, _ = s.Update(searchResultMsg{result: result, err: nil})
	got := s.View()
	if got == "" {
		t.Error("View() after search result should return non-empty string")
	}
}

func TestSearch_Update_EscBlursInput(t *testing.T) {
	s := NewSearch(&mockProvider{})
	s.SetSize(80, 24)
	s.Focus()
	if !s.Focused() {
		t.Fatal("should be focused before esc")
	}
	s, _ = s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if s.Focused() {
		t.Error("should not be focused after esc")
	}
}

func TestSearch_Update_NonSearchMsg_NoPanic(t *testing.T) {
	s := NewSearch(&mockProvider{})
	s.SetSize(80, 24)
	_, _ = s.Update(tea.WindowSizeMsg{Width: 80, Height: 24}) // should not panic
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
	s.Focus()
	// Type a character - should trigger scheduleSearch internally
	s, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd == nil {
		t.Error("typing while focused should return a cmd")
	}
	_ = s
}
