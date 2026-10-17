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
					{Start: 5 * time.Second, Text: "first "},
					{Start: 6 * time.Second, Text: "timed "},
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

func TestLyricsModel_BottomScrollAndResumeFollow(t *testing.T) {
	lines := make([]lyrics.Line, 20)
	for i := range lines {
		lines[i] = lyrics.Line{
			Start: time.Duration(i) * time.Second,
			Text:  "line-" + string(rune('A'+i)),
		}
	}
	m := NewLyrics()
	m.SetSize(40, 10) // visibleRows = 8
	m.SetLyrics(&lyrics.Result{Synced: true, Lines: lines}, nil)
	m.SetPosition(0)

	// Jump to bottom with G: last line (index 19, "line-T") must be visible.
	_ = m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	view := m.View()
	if !strings.Contains(view, "▸ line-T") {
		t.Errorf("expected last line visible after G, got:\n%s", view)
	}

	// Back() (triggered on Esc) should re-attach follow mode without closing.
	if !m.Back() {
		t.Error("expected Back() to return true when detached from follow")
	}
	if m.Back() {
		t.Error("expected second Back() to return false when already following")
	}

	// Detach again onto line 2 ("line-C"); when playback reaches line 2, follow resumes.
	_ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	_ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.follow {
		t.Fatal("expected follow=false after scrolling ahead")
	}
	m.SetPosition(2 * time.Second)
	if !m.follow {
		t.Error("expected follow to resume when active line catches up to cursor")
	}
}
