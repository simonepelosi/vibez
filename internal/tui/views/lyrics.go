package views

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/simone-vibes/vibez/internal/lyrics"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// LyricsSeekMsg is emitted when the user presses Enter on a synced lyric line
// to seek playback to that line's timestamp.
type LyricsSeekMsg struct {
	Position time.Duration
}

// LyricsModel renders a scrollable lyrics panel.
// Call SetLoading when a fetch starts, SetLyrics when it completes, and
// SetPosition on every player-state update so the current line is highlighted.
type LyricsModel struct {
	lines      []lyrics.Line
	synced     bool
	loading    bool
	errMsg     string
	pos        time.Duration
	currentIdx int  // index of the currently active line (-1 = none)
	cursor     int  // index of the user-selected line (-1 = none)
	follow     bool // true when viewport/cursor follows playback automatically
	scroll     int  // index of the top visible line
	width      int
	height     int
}

func NewLyrics() *LyricsModel {
	return &LyricsModel{currentIdx: -1, cursor: -1, follow: true}
}

// SetLoading transitions the panel into a fetching state.
func (l *LyricsModel) SetLoading() {
	l.loading = true
	l.lines = nil
	l.errMsg = ""
	l.pos = 0
	l.currentIdx = -1
	l.cursor = -1
	l.follow = true
	l.scroll = 0
}

// SetLyrics transitions the panel to the loaded (or error) state.
func (l *LyricsModel) SetLyrics(res *lyrics.Result, err error) {
	l.loading = false
	if err != nil {
		l.errMsg = err.Error()
		l.lines = nil
		return
	}
	l.lines = res.Lines
	l.synced = res.Synced
	l.errMsg = ""
	l.pos = 0
	l.currentIdx = -1
	l.cursor = -1
	l.follow = true
	l.scroll = 0
}

// SetPosition updates which line (and word) is highlighted based on the
// playback position. No-op for plain (unsynced) lyrics.
func (l *LyricsModel) SetPosition(pos time.Duration) {
	if !l.synced || len(l.lines) == 0 {
		return
	}
	l.pos = pos
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
	if l.follow {
		l.cursor = idx
		l.centerOn(idx)
	}
}

func (l *LyricsModel) visibleRows() int {
	return max(l.height-2, 1)
}

func (l *LyricsModel) centerOn(idx int) {
	if idx >= 0 && l.height > 0 {
		target := idx - l.height/2
		maxScroll := max(0, len(l.lines)-l.height)
		l.scroll = max(0, min(target, maxScroll))
	}
}

func (l *LyricsModel) ensureCursorVisible() {
	if l.cursor < 0 {
		return
	}
	rows := l.visibleRows()
	if l.cursor < l.scroll {
		l.scroll = l.cursor
	} else if l.cursor >= l.scroll+rows {
		l.scroll = l.cursor - rows + 1
	}
	maxScroll := max(0, len(l.lines)-l.height)
	l.scroll = max(0, min(l.scroll, maxScroll))
}

// SetSize updates the panel dimensions (called on window resize).
func (l *LyricsModel) SetSize(w, h int) {
	l.width = w
	l.height = h
}

// Update handles j/k scroll/cursor input and Enter to seek when the lyrics
// panel is active.
func (l *LyricsModel) Update(msg tea.KeyPressMsg) tea.Cmd {
	if len(l.lines) == 0 {
		return nil
	}
	if l.synced {
		if l.cursor < 0 {
			l.cursor = max(0, l.currentIdx)
		}
		switch msg.String() {
		case "j", "down":
			if l.cursor < len(l.lines)-1 {
				l.cursor++
			}
			l.follow = l.cursor == l.currentIdx
			l.ensureCursorVisible()
		case "k", "up":
			if l.cursor > 0 {
				l.cursor--
			}
			l.follow = l.cursor == l.currentIdx
			l.ensureCursorVisible()
		case "g":
			l.cursor = 0
			l.follow = l.cursor == l.currentIdx
			l.scroll = 0
		case "G":
			l.cursor = len(l.lines) - 1
			l.follow = l.cursor == l.currentIdx
			l.ensureCursorVisible()
		case "enter":
			if l.cursor >= 0 && l.cursor < len(l.lines) {
				target := l.lines[l.cursor].Start
				l.follow = true
				l.currentIdx = l.cursor
				l.pos = target
				l.centerOn(l.cursor)
				return func() tea.Msg {
					return LyricsSeekMsg{Position: target}
				}
			}
		}
		return nil
	}

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

// renderActiveLine renders the currently playing lyric line, highlighting individual
// words up to l.pos when enhanced LRC word timestamps are available.
func (l *LyricsModel) renderActiveLine(line lyrics.Line, current, upcoming lipgloss.Style) string {
	if len(line.Words) == 0 {
		return current.Render(line.Text)
	}
	var sb strings.Builder
	for i, w := range line.Words {
		if i > 0 {
			sb.WriteByte(' ')
		}
		if w.Start <= l.pos {
			sb.WriteString(current.Render(w.Text))
		} else {
			sb.WriteString(upcoming.Render(w.Text))
		}
	}
	return sb.String()
}

// View renders the lyrics panel as a newline-separated string.
func (l *LyricsModel) View() string {
	muted := styles.QueueItemMuted
	normal := lipgloss.NewStyle().Foreground(styles.ColorFg)
	current := styles.Playing.Bold(true)
	selected := styles.Selected
	header := styles.TabActive

	if l.loading {
		return header.Render("Lyrics") + "\n" +
			strings.Repeat("─", 5) + "\n\n" +
			muted.Render("fetching lyrics…")
	}

	if l.errMsg != "" {
		return header.Render("Lyrics") + "\n" +
			strings.Repeat("─", 5) + "\n\n" +
			muted.Render("You cannot sing this song :(")
	}

	if len(l.lines) == 0 {
		return header.Render("Lyrics") + "\n" +
			strings.Repeat("─", 5) + "\n\n" +
			muted.Render("no lyrics found")
	}

	var sb strings.Builder
	sb.WriteString(header.Render("Lyrics") + "\n")
	sb.WriteString(muted.Render(strings.Repeat("─", 5)) + "\n")

	end := min(l.scroll+l.visibleRows(), len(l.lines))
	for i := l.scroll; i < end; i++ {
		line := l.lines[i]
		text := line.Text
		if text == "" {
			if !l.follow && l.synced && i == l.cursor {
				sb.WriteString(selected.Render("▸ ♪"))
			}
			sb.WriteByte('\n')
			continue
		}
		switch {
		case !l.follow && l.synced && i == l.cursor && i != l.currentIdx:
			sb.WriteString(selected.Render("▸ " + text))
		case i == l.currentIdx:
			sb.WriteString(l.renderActiveLine(line, current, normal))
		case l.synced && i < l.currentIdx:
			sb.WriteString(muted.Render(text))
		default:
			sb.WriteString(normal.Render(text))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
