package views

import (
	"errors"
	"github.com/simone-vibes/vibez/internal/tui/locale"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/simone-vibes/vibez/internal/lyrics"
)

// LyricsModel renders a scrollable lyrics panel.
// Call SetLoading when a fetch starts, SetLyrics when it completes, and
// SetPosition on every player-state update so the current line is highlighted.
type LyricsModel struct {
	notFound       bool
	Locale         locale.Locale
	lines          []lyrics.Line
	synced         bool
	loading        bool
	errMsg         string
	viewportOffset int
	viewportTarget int
	viewportReady  bool
	viewportWidth  int
	viewportHeight int
	currentIdx     int // index of the currently active line (-1 = none)
	scroll         int // index of the top visible line
	width          int
	height         int
}

func NewLyrics() *LyricsModel {
	return &LyricsModel{currentIdx: -1}
}

// SetLoading transitions the panel into a fetching state.
func (l *LyricsModel) SetLoading() {
	l.viewportReady = false
	l.loading = true
	l.lines = nil
	l.errMsg = ""
	l.currentIdx = -1
	l.scroll = 0
}

// SetLyrics transitions the panel to the loaded (or error) state.
func (l *LyricsModel) SetLyrics(res *lyrics.Result, err error) {
	l.viewportReady = false
	l.loading = false
	if err != nil {
		l.errMsg = err.Error()
		l.notFound = errors.Is(err, lyrics.ErrNotFound)
		l.lines = nil
		return
	}
	l.lines = res.Lines
	l.synced = res.Synced
	l.errMsg = ""
	l.currentIdx = -1
	l.scroll = 0
}

// SetPosition updates which line is highlighted based on the playback position.
// No-op for plain (unsynced) lyrics.
func (l *LyricsModel) SetPosition(pos time.Duration) {
	if !l.synced || len(l.lines) == 0 {
		return
	}
	// Walk forward until a line starts after pos; the one before it is current.
	idx := -1
	for i, line := range l.lines {
		if line.Start > pos {
			break
		}
		idx = i
	}
	if idx == l.currentIdx {
		return
	}
	l.currentIdx = idx
	// Auto-scroll: keep the current line in the middle of the viewport.
	if idx >= 0 && l.height > 0 {
		target := idx - l.height/2
		maxScroll := max(0, len(l.lines)-l.height)
		l.scroll = max(0, min(target, maxScroll))
	}
}

// SetSize updates the panel dimensions (called on window resize).
func (l *LyricsModel) SetSize(w, h int) {
	l.width = w
	l.height = h
}

// Update handles j/k scroll input when the lyrics panel is active.
func (l *LyricsModel) Update(msg tea.KeyPressMsg) tea.Cmd {
	maxScroll := max(0, len(l.lines)-l.height)
	switch msg.String() {
	case "j", "down":
		if l.scroll < maxScroll {
			l.scroll++
		}
	case "k", "up":
		if l.scroll > 0 {
			l.scroll--
		}
	case "g":
		l.scroll = 0
	case "G":
		l.scroll = maxScroll
	}
	return nil
}

// View renders the lyrics panel as a newline-separated string.
func (l *LyricsModel) View() string {
	return strings.Join(l.InlineLines(l.width, l.height), "\n")
}

func (l *LyricsModel) HasError() bool { return l.errMsg != "" }
