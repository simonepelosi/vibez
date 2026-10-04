package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestCleanLayoutHasNoShortcutFooter(t *testing.T) {
	cfg := testCfg()
	cfg.HideHints = true
	m := New(cfg, &mockProvider{}, nil, Options{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m.introStep = introDone
	for _, mode := range []viewMode{modeNormal, modeSearch} {
		m.mode = mode
		got := m.View().Content
		for _, hint := range []string{"play/pause", "shift+tab", "add to queue", "NORMAL", "SEARCH"} {
			if strings.Contains(strings.ToLower(got), strings.ToLower(hint)) {
				t.Fatalf("shortcut footer still visible: %s", hint)
			}
		}
		if lipgloss.Height(got) != m.height {
			t.Fatalf("layout height %d, want %d", lipgloss.Height(got), m.height)
		}
	}
}
