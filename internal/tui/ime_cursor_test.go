package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSearchCursorAnchorsIMEToInput(t *testing.T) {
	m := newModel(nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.introStep = introDone
	m.mode = modeSearch
	m.searchQuery = "周杰伦 album"
	m.searchCursor = 3
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("real cursor missing; IME attaches to stale terminal position")
	}
	if v.Cursor.X != 11 || v.Cursor.Y != m.nowPlayingHeight()+4 {
		t.Fatalf("cursor at %v; want search input column 11 row %d", v.Cursor.Position, m.nowPlayingHeight()+4)
	}
	m.mode = modeNormal
	if m.View().Cursor != nil {
		t.Fatal("normal mode must hide editing cursor")
	}
}
