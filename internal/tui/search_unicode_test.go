package tui

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

func TestSearchChineseCommittedText(t *testing.T) {
	for _, text := range []string{"周", "周杰伦", "范特西", "Jay 周杰伦 🎵"} {
		t.Run(text, func(t *testing.T) {
			m := newModel(nil)
			m.mode = modeSearch
			_, cmd := m.Update(tea.KeyPressMsg{Code: []rune(text)[0], Text: text})
			if m.searchQuery != text || m.searchCursor != len([]rune(text)) || cmd == nil {
				t.Fatalf("query=%q cursor=%d searchScheduled=%t", m.searchQuery, m.searchCursor, cmd != nil)
			}
		})
	}
}

func TestSearchChinesePasteAtCursor(t *testing.T) {
	m := newModel(nil)
	m.mode = modeSearch
	m.searchQuery = "周 范特西"
	m.searchCursor = 1
	_, cmd := m.Update(tea.PasteMsg{Content: "杰伦"})
	if m.searchQuery != "周杰伦 范特西" || m.searchCursor != 3 || cmd == nil {
		t.Fatalf("query=%q cursor=%d searchScheduled=%t", m.searchQuery, m.searchCursor, cmd != nil)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.searchQuery != "周杰 范特西" || m.searchCursor != 2 {
		t.Fatalf("after backspace query=%q cursor=%d", m.searchQuery, m.searchCursor)
	}
}

func TestSearchPasteCannotExecuteKeys(t *testing.T) {
	m := newModel(nil)
	m.mode = modeSearch
	m.Update(tea.PasteMsg{Content: "周杰伦\n:q\x1b"})
	if m.mode != modeSearch || m.searchQuery != "周杰伦:q" {
		t.Fatalf("mode=%v query=%q", m.mode, m.searchQuery)
	}
}
