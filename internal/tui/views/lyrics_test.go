package views

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/lyrics"
)

func TestLyricsModel_WordHighlightingAndSeek(t *testing.T) {
	m := NewLyrics()
	m.SetSize(60, 10)
	m.SetLyrics(&lyrics.Result{
		Synced: true,
		Lines: []lyrics.Line{
			{
				Start: 5 * time.Second,
				Text:  "first timed line",
				Words: []lyrics.Word{
					{Start: 5 * time.Second, Text: "first"},
					{Start: 6 * time.Second, Text: "timed"},
					{Start: 7 * time.Second, Text: "line"},
				},
			},
			{
				Start: 10 * time.Second,
				Text:  "second timed line",
			},
			{
				Start: 15 * time.Second,
				Text:  "third timed line",
			},
		},
	}, nil)

	m.SetPosition(6500 * time.Millisecond)
	view := m.View()
	for _, word := range []string{"first", "timed", "line"} {
		if !strings.Contains(view, word) {
			t.Errorf("view missing word %q:\n%s", word, view)
		}
	}

	// Move cursor down to "second timed line" (unfollows active line).
	_ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	view = m.View()
	if !strings.Contains(view, "▸ second timed line") {
		t.Errorf("expected selected indicator on second line, got:\n%s", view)
	}

	// Press Enter to seek to the selected line.
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil command on Enter")
	}
	msg := cmd()
	seekMsg, ok := msg.(LyricsSeekMsg)
	if !ok {
		t.Fatalf("expected LyricsSeekMsg, got %T", msg)
	}
	if seekMsg.Position != 10*time.Second {
		t.Errorf("seek position = %v, want 10s", seekMsg.Position)
	}
}
