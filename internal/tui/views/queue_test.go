package views

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/simone-vibes/vibez/internal/provider"
)

// --- queueItem list.Item interface ---

func TestQueueItem_Title(t *testing.T) {
	qi := queueItem{track: provider.Track{Title: "My Song"}, pos: 3}
	got := qi.Title()
	if !strings.Contains(got, "3") {
		t.Errorf("Title() should contain position number, got %q", got)
	}
	if !strings.Contains(got, "My Song") {
		t.Errorf("Title() should contain track title, got %q", got)
	}
}

func TestQueueItem_Description(t *testing.T) {
	qi := queueItem{track: provider.Track{Artist: "The Artist", Album: "The Album"}, pos: 1}
	got := qi.Description()
	if !strings.Contains(got, "The Artist") {
		t.Errorf("Description() should contain artist, got %q", got)
	}
	if !strings.Contains(got, "The Album") {
		t.Errorf("Description() should contain album, got %q", got)
	}
}

func TestQueueItem_FilterValue(t *testing.T) {
	qi := queueItem{track: provider.Track{Title: "Filter Me"}, pos: 1}
	got := qi.FilterValue()
	if got != "Filter Me" {
		t.Errorf("FilterValue() = %q, want %q", got, "Filter Me")
	}
}

// --- QueueModel ---

func TestNewQueue_NotNil(t *testing.T) {
	q := NewQueue()
	if q == nil {
		t.Fatal("NewQueue() returned nil")
	}
}

func TestQueue_SetTracksNil(t *testing.T) {
	q := NewQueue()
	q.SetTracks(nil) // should not panic
}

func TestQueue_SetTracks(t *testing.T) {
	q := NewQueue()
	tracks := []provider.Track{
		{Title: "Track A", Artist: "Artist A", Album: "Album A"},
		{Title: "Track B", Artist: "Artist B", Album: "Album B"},
	}
	q.SetTracks(tracks) // should not panic
}

func TestQueue_SetSize(t *testing.T) {
	q := NewQueue()
	q.SetSize(80, 24) // should not panic
}

func TestQueue_View_Empty(t *testing.T) {
	q := NewQueue()
	q.SetSize(80, 24)
	got := q.View()
	if !strings.Contains(got, "empty") {
		t.Errorf("View() when empty should contain 'empty', got %q", got)
	}
}

func TestQueue_View_WithTracks(t *testing.T) {
	q := NewQueue()
	q.SetSize(80, 24)
	tracks := []provider.Track{
		{Title: "Song One", Artist: "Artist", Album: "Album"},
	}
	q.SetTracks(tracks)
	got := q.View()
	if got == "" {
		t.Error("View() with tracks should return non-empty string")
	}
}

func TestQueue_Update_NoPanic(t *testing.T) {
	q := NewQueue()
	q.SetSize(80, 24)
	q.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // should not panic
}

// --- queueTrackLine ---

func TestQueueTrackLine_NonSelected(t *testing.T) {
	t1 := provider.Track{Title: "My Track", Artist: "My Artist"}
	got := queueTrackLine(t1, 0, 99) // 99 is not 0, so not selected
	if !strings.Contains(got, "My Track") {
		t.Errorf("queueTrackLine should contain title, got %q", got)
	}
}

func TestQueueTrackLine_Selected(t *testing.T) {
	t1 := provider.Track{Title: "Selected Track", Artist: "My Artist"}
	// Both should return non-empty strings and not panic
	notSelected := queueTrackLine(t1, 0, 99)
	selected := queueTrackLine(t1, 0, 0)
	if notSelected == "" {
		t.Error("non-selected track line should not be empty")
	}
	if selected == "" {
		t.Error("selected track line should not be empty")
	}
}
