package views

import (
	"image/color"
	"sort"
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
	lines       []lyrics.Line
	synced      bool
	sorted      bool
	loading     bool
	errMsg      string
	pos         time.Duration
	currentIdx  int  // index of the currently active line (-1 = none)
	activeWords int  // number of highlighted words on active line
	cursor      int  // index of the user-selected line (-1 = none)
	follow      bool // true when viewport/cursor follows playback automatically
	scroll      int  // index of the top visible line
	width       int
	height      int

	viewDirty          bool
	cachedView         string
	cachedFg           color.Color
	cachedPlayingFg    color.Color
	cachedCurrentStyle lipgloss.Style
}

func NewLyrics() *LyricsModel {
	return &LyricsModel{currentIdx: -1, cursor: -1, follow: true, viewDirty: true}
}

func (l *LyricsModel) invalidateView() {
	l.viewDirty = true
}

func linesAreSorted(lines []lyrics.Line) bool {
	for i := 1; i < len(lines); i++ {
		if lines[i].Start < lines[i-1].Start {
			return false
		}
	}
	return true
}

func countActiveWords(words []lyrics.Word, pos time.Duration) int {
	n := 0
	for _, w := range words {
		if w.Start <= pos {
			n++
		}
	}
	return n
}

// SetLoading transitions the panel into a fetching state.
func (l *LyricsModel) SetLoading() {
	l.loading = true
	l.lines = nil
	l.sorted = false
	l.errMsg = ""
	l.pos = 0
	l.currentIdx = -1
	l.activeWords = 0
	l.cursor = -1
	l.follow = true
	l.scroll = 0
	l.invalidateView()
}

// SetLyrics transitions the panel to the loaded (or error) state.
func (l *LyricsModel) SetLyrics(res *lyrics.Result, err error) {
	l.loading = false
	if err != nil {
		l.errMsg = err.Error()
		l.lines = nil
		l.sorted = false
		l.invalidateView()
		return
	}
	l.lines = res.Lines
	l.synced = res.Synced
	l.sorted = linesAreSorted(res.Lines)
	l.errMsg = ""
	l.pos = 0
	l.currentIdx = -1
	l.activeWords = 0
	l.cursor = -1
	l.follow = true
	l.scroll = 0
	l.invalidateView()
}

func (l *LyricsModel) findLineIndex(pos time.Duration) int {
	n := len(l.lines)
	if n == 0 {
		return -1
	}
	if !l.sorted {
		idx := -1
		for i, line := range l.lines {
			if line.Start > pos {
				break
			}
			idx = i
		}
		return idx
	}
	// Fast path: unchanged or advanced by a single line.
	cur := l.currentIdx
	if cur >= 0 && cur < n && l.lines[cur].Start <= pos {
		if cur+1 == n || l.lines[cur+1].Start > pos {
			return cur
		}
		if cur+2 == n || l.lines[cur+2].Start > pos {
			return cur + 1
		}
	}
	return sort.Search(n, func(i int) bool { return l.lines[i].Start > pos }) - 1
}

// SetPosition updates which line (and word) is highlighted based on the
// playback position. No-op for plain (unsynced) lyrics.
func (l *LyricsModel) SetPosition(pos time.Duration) {
	if !l.synced || len(l.lines) == 0 {
		return
	}
	l.pos = pos
	idx := l.findLineIndex(pos)
	if idx == l.currentIdx {
		if idx >= 0 && len(l.lines[idx].Words) > 0 {
			if wc := countActiveWords(l.lines[idx].Words, pos); wc != l.activeWords {
				l.activeWords = wc
				l.invalidateView()
			}
		}
		return
	}
	l.currentIdx = idx
	if idx >= 0 && len(l.lines[idx].Words) > 0 {
		l.activeWords = countActiveWords(l.lines[idx].Words, pos)
	} else {
		l.activeWords = 0
	}
	if !l.follow && l.cursor >= 0 && idx == l.cursor {
		l.follow = true
	}
	if l.follow {
		l.cursor = idx
		l.centerOn(idx)
	}
	l.invalidateView()
}

// Back resumes auto-following the active lyric line when the user has scrolled
// away; returns false when already following so the caller can close the panel.
func (l *LyricsModel) Back() bool {
	if !l.synced || l.follow || len(l.lines) == 0 {
		return false
	}
	l.follow = true
	if l.currentIdx >= 0 {
		l.cursor = l.currentIdx
		l.centerOn(l.currentIdx)
	} else {
		l.cursor = 0
		l.scroll = 0
	}
	l.invalidateView()
	return true
}

func (l *LyricsModel) visibleRows() int {
	return max(l.height-2, 1)
}

func (l *LyricsModel) centerOn(idx int) {
	if idx >= 0 && l.height > 0 {
		rows := l.visibleRows()
		target := idx - rows/2
		maxScroll := max(0, len(l.lines)-rows)
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
	maxScroll := max(0, len(l.lines)-rows)
	l.scroll = max(0, min(l.scroll, maxScroll))
}

// SetSize updates the panel dimensions (called on window resize).
func (l *LyricsModel) SetSize(w, h int) {
	if l.width == w && l.height == h {
		return
	}
	l.width = w
	l.height = h
	l.invalidateView()
}

// Update handles j/k scroll/cursor input and Enter to seek when the lyrics
// panel is active.
func (l *LyricsModel) Update(msg tea.KeyPressMsg) tea.Cmd {
	if len(l.lines) == 0 {
		return nil
	}
	prevIdx, prevCursor, prevFollow, prevScroll := l.currentIdx, l.cursor, l.follow, l.scroll
	defer func() {
		if l.currentIdx != prevIdx || l.cursor != prevCursor || l.follow != prevFollow || l.scroll != prevScroll {
			l.invalidateView()
		}
	}()
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

	maxScroll := max(0, len(l.lines)-l.visibleRows())
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
	for _, w := range line.Words {
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
	playingFg := styles.Playing.GetForeground()
	if l.cachedFg != styles.ColorFg || l.cachedPlayingFg != playingFg {
		l.cachedFg = styles.ColorFg
		l.cachedPlayingFg = playingFg
		l.cachedCurrentStyle = styles.Playing.Bold(true)
		l.viewDirty = true
	}
	if !l.viewDirty {
		return l.cachedView
	}

	muted := styles.QueueItemMuted
	normal := styles.QueueItem
	current := l.cachedCurrentStyle
	selected := styles.Selected
	header := styles.TabActive

	var rendered string
	switch {
	case l.loading:
		rendered = header.Render("Lyrics") + "\n" +
			strings.Repeat("─", 5) + "\n\n" +
			muted.Render("fetching lyrics…")
	case l.errMsg != "":
		rendered = header.Render("Lyrics") + "\n" +
			strings.Repeat("─", 5) + "\n\n" +
			muted.Render("You cannot sing this song :(")
	case len(l.lines) == 0:
		rendered = header.Render("Lyrics") + "\n" +
			strings.Repeat("─", 5) + "\n\n" +
			muted.Render("no lyrics found")
	default:
		end := min(l.scroll+l.visibleRows(), len(l.lines))
		var sb strings.Builder
		sb.Grow((end - l.scroll) * 48)
		sb.WriteString(header.Render("Lyrics") + "\n")
		sb.WriteString(muted.Render(strings.Repeat("─", 5)) + "\n")

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
		rendered = sb.String()
	}

	l.cachedView = rendered
	l.viewDirty = false
	return rendered
}
