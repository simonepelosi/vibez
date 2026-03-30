package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
	"github.com/simone-vibes/vibez/internal/tui/views"
)

type viewID int

const (
	viewNowPlaying viewID = iota
	viewQueue
	viewLibrary
	viewSearch
)

const sidebarWidth = 16

type sidebarEntry struct {
	id    viewID
	label string
}

var sidebarNav = []sidebarEntry{
	{viewNowPlaying, "now playing"},
	{viewSearch, "search"},
	{viewLibrary, "library"},
	{viewQueue, "queue"},
}

type tickMsg time.Time
type glowTickMsg time.Time
type playerStateMsg player.State
type errMsg struct{ err error }

type Model struct {
	cfg      *config.Config
	provider provider.Provider
	player   player.Player

	activeView viewID
	width      int
	height     int

	playerState player.State
	stateCh     <-chan player.State

	nowPlaying *views.NowPlayingModel
	queue      *views.QueueModel
	library    *views.LibraryModel
	search     *views.SearchModel

	errMsg    string
	errExpiry time.Time
	showHelp  bool

	glowStep int
}

func New(cfg *config.Config, prov provider.Provider, plyr player.Player) *Model {
	m := &Model{
		cfg:        cfg,
		provider:   prov,
		player:     plyr,
		activeView: viewNowPlaying,
	}

	if plyr != nil {
		m.stateCh = plyr.Subscribe()
	}

	m.nowPlaying = views.NewNowPlaying(&m.playerState)
	m.queue = views.NewQueue()
	m.library = views.NewLibrary(prov)
	m.search = views.NewSearch(prov)

	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tick(),
		m.library.Init(),
		m.search.Init(),
	}
	if m.stateCh != nil {
		cmds = append(cmds, waitForState(m.stateCh))
	}
	return tea.Batch(cmds...)
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func glowTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return glowTickMsg(t)
	})
}

func waitForState(ch <-chan player.State) tea.Cmd {
	return func() tea.Msg {
		return playerStateMsg(<-ch)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.nowPlaying.SetSize(m.contentWidth(), m.contentHeight())
		m.queue.SetSize(m.contentWidth(), m.contentHeight())
		m.library.SetSize(m.contentWidth(), m.contentHeight())
		m.search.SetSize(m.contentWidth(), m.contentHeight())

	case tickMsg:
		if m.errMsg != "" && time.Now().After(m.errExpiry) {
			m.errMsg = ""
		}
		cmds = append(cmds, tick())

	case glowTickMsg:
		// Only advance and reschedule while playing — stops automatically on pause.
		if m.playerState.Playing {
			m.glowStep++
			m.nowPlaying.SetGlowStep(m.glowStep)
			cmds = append(cmds, glowTick())
		}

	case playerStateMsg:
		wasPlaying := m.playerState.Playing
		s := player.State(msg)
		if s.Error != "" {
			m.errMsg = s.Error
			m.errExpiry = time.Now().Add(4 * time.Second)
			s.Error = ""
		}
		m.playerState = s
		m.nowPlaying.SetState(&m.playerState)
		// Keep queue model in sync with currently playing track.
		if m.playerState.Track != nil {
			if len(m.queue.Tracks()) == 0 || m.queue.Tracks()[0].ID != m.playerState.Track.ID {
				m.queue.SetTracks([]provider.Track{*m.playerState.Track})
			}
		}
		// Restart the glow animation when playback begins.
		if !wasPlaying && m.playerState.Playing {
			cmds = append(cmds, glowTick())
		}
		cmds = append(cmds, waitForState(m.stateCh))

	case views.PlayTracksMsg:
		cmds = append(cmds, m.playerCmd(func() error {
			return m.player.SetQueue(msg.IDs)
		}))
		// Switch to Now Playing so the user sees what started.
		m.activeView = viewNowPlaying

	case errMsg:
		m.errMsg = msg.err.Error()
		m.errExpiry = time.Now().Add(3 * time.Second)

	case tea.KeyMsg:
		// Let the search view consume typing keys when focused.
		if m.activeView == viewSearch && m.search.Focused() {
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, keys.Quit):
			if m.player != nil {
				_ = m.player.Close()
			}
			return m, tea.Quit

		case key.Matches(msg, keys.Help):
			m.showHelp = !m.showHelp

		case key.Matches(msg, keys.ViewNow):
			m.activeView = viewNowPlaying
		case key.Matches(msg, keys.ViewQueue):
			m.activeView = viewQueue
		case key.Matches(msg, keys.ViewLib):
			m.activeView = viewLibrary
		case key.Matches(msg, keys.ViewSearch):
			m.activeView = viewSearch

		case key.Matches(msg, keys.Search):
			m.activeView = viewSearch
			m.search.Focus()

		case key.Matches(msg, keys.PlayPause):
			cmds = append(cmds, m.togglePlayPause())
		case key.Matches(msg, keys.Next):
			cmds = append(cmds, m.playerCmd(func() error { return m.player.Next() }))
		case key.Matches(msg, keys.Previous):
			cmds = append(cmds, m.playerCmd(func() error { return m.player.Previous() }))
		case key.Matches(msg, keys.VolumeUp):
			cmds = append(cmds, m.adjustVolume(0.05))
		case key.Matches(msg, keys.VolumeDown):
			cmds = append(cmds, m.adjustVolume(-0.05))

		case key.Matches(msg, keys.Up):
			if m.activeView == viewNowPlaying {
				for i, entry := range sidebarNav {
					if entry.id == m.activeView && i > 0 {
						m.activeView = sidebarNav[i-1].id
						break
					}
				}
			} else {
				cmd := m.updateActiveView(msg)
				cmds = append(cmds, cmd)
			}
		case key.Matches(msg, keys.Down):
			if m.activeView == viewNowPlaying {
				for i, entry := range sidebarNav {
					if entry.id == m.activeView && i < len(sidebarNav)-1 {
						m.activeView = sidebarNav[i+1].id
						break
					}
				}
			} else {
				cmd := m.updateActiveView(msg)
				cmds = append(cmds, cmd)
			}

		default:
			cmd := m.updateActiveView(msg)
			cmds = append(cmds, cmd)
		}

	default:
		// Forward any unrecognised message (e.g. searchResultMsg from the
		// async search goroutine) to the active view so it can handle it.
		// This is the standard BubbleTea parent→child delegation pattern.
		cmd := m.forwardMsgToActive(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) togglePlayPause() tea.Cmd {
	return func() tea.Msg {
		if m.player == nil {
			return errMsg{fmt.Errorf("no player connected")}
		}
		if m.playerState.Playing {
			if err := m.player.Pause(); err != nil {
				return errMsg{err}
			}
		} else {
			if err := m.player.Play(); err != nil {
				return errMsg{err}
			}
		}
		return nil
	}
}

func (m *Model) playerCmd(fn func() error) tea.Cmd {
	return func() tea.Msg {
		if m.player == nil {
			return errMsg{fmt.Errorf("no player connected")}
		}
		if err := fn(); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m *Model) adjustVolume(delta float64) tea.Cmd {
	return func() tea.Msg {
		if m.player == nil {
			return errMsg{fmt.Errorf("no player connected")}
		}
		newVol := clamp(m.playerState.Volume+delta, 0, 1)
		if err := m.player.SetVolume(newVol); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m *Model) updateActiveView(msg tea.KeyMsg) tea.Cmd {
	switch m.activeView {
	case viewNowPlaying:
		m.nowPlaying.Update(msg)
	case viewQueue:
		m.queue.Update(msg)
	case viewLibrary:
		var cmd tea.Cmd
		m.library, cmd = m.library.Update(msg)
		return cmd
	case viewSearch:
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return cmd
	}
	return nil
}

// forwardMsgToActive delivers async result messages to the views that need them.
// Internal timer ticks (glowTickMsg, tickMsg) are filtered out — no child view
// needs them and forwarding them caused unnecessary redraws on every frame.
// Library receives messages regardless of active view (for background loading).
// Search only receives messages when it is the active view.
func (m *Model) forwardMsgToActive(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case glowTickMsg, tickMsg:
		return nil
	}
	var libCmd tea.Cmd
	m.library, libCmd = m.library.Update(msg)
	if m.activeView != viewSearch {
		return libCmd
	}
	var searchCmd tea.Cmd
	m.search, searchCmd = m.search.Update(msg)
	return tea.Batch(libCmd, searchCmd)
}

func (m *Model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	sep := m.renderSeparator()
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		sep,
		m.renderContent(),
		sep,
		m.renderFooter(),
	)
}

func (m *Model) renderHeader() string {
	logo := styles.Header.Render("♪ vibez")

	var trackInfo string
	if m.playerState.Track != nil {
		t := m.playerState.Track
		icon := "⏸"
		if m.playerState.Playing {
			icon = "▶"
		}
		elapsed := views.FormatDuration(m.playerState.Position)
		total := views.FormatDuration(t.Duration)
		raw := fmt.Sprintf("%s %s — %s  %s/%s", icon, t.Title, t.Artist, elapsed, total)
		if m.playerState.Playing {
			idx := m.glowStep % len(styles.GlowPalette)
			trackInfo = lipgloss.NewStyle().
				Foreground(styles.GlowPalette[idx]).
				Render(raw)
		} else {
			trackInfo = styles.QueueItemMuted.Render(raw)
		}
	}

	logoW := lipgloss.Width(logo)
	trackW := lipgloss.Width(trackInfo)
	spacerW := max(0, m.width-logoW-trackW)
	return logo + strings.Repeat(" ", spacerW) + trackInfo
}

func (m *Model) renderSeparator() string {
	return styles.Separator.Render(strings.Repeat("─", m.width))
}

// renderSidebar builds the sidebar lines, one per navigation entry, padded to
// sidebarWidth. Extra lines (when contentHeight > len(sidebarNav)) are blank.
func (m *Model) renderSidebar(h int) []string {
	lines := make([]string, h)
	blank := strings.Repeat(" ", sidebarWidth)

	for i := range h {
		if i < len(sidebarNav) {
			entry := sidebarNav[i]
			prefix := "   "
			if entry.id == m.activeView {
				prefix = " ▶ "
			}
			text := prefix + entry.label
			pad := max(0, sidebarWidth-lipgloss.Width(text))
			text += strings.Repeat(" ", pad)
			if entry.id == m.activeView {
				lines[i] = styles.SidebarActive.Render(text)
			} else {
				lines[i] = styles.SidebarInactive.Render(text)
			}
		} else {
			lines[i] = blank
		}
	}
	return lines
}

// renderContent renders the sidebar and the active view side by side.
func (m *Model) renderContent() string {
	h := m.contentHeight()
	if h <= 0 {
		return ""
	}

	// Active view content.
	var viewContent string
	switch m.activeView {
	case viewNowPlaying:
		viewContent = m.nowPlaying.View()
	case viewQueue:
		viewContent = m.queue.View()
	case viewLibrary:
		viewContent = m.library.View()
	case viewSearch:
		viewContent = m.search.View()
	}

	if m.errMsg != "" {
		viewContent = styles.ErrorStyle.Render("⚠ "+m.errMsg) + "\n" + viewContent
	}

	sidebarLines := m.renderSidebar(h)

	// Split view content into exactly h lines.
	contentLines := strings.Split(viewContent, "\n")
	for len(contentLines) < h {
		contentLines = append(contentLines, "")
	}
	contentLines = contentLines[:h]

	divider := styles.Separator.Render("│")
	rows := make([]string, h)
	for i := range h {
		rows[i] = sidebarLines[i] + divider + contentLines[i]
	}
	return strings.Join(rows, "\n")
}

func (m *Model) renderFooter() string {
	base := "space · n · p · q quit"
	var extra string
	switch m.activeView {
	case viewNowPlaying:
		extra = "1-4 switch view · / search"
	case viewSearch:
		extra = "/ search · enter play · esc back"
	case viewLibrary:
		extra = "tab switch tab · enter play · / search"
	case viewQueue:
		extra = "1-4 switch view"
	}
	hint := extra + "  " + base
	return styles.KeyHint.Render(hint)
}

// contentHeight is the number of rows available for the sidebar+content area.
// Layout: header(1) + separator(1) + content(N) + separator(1) + footer(1) = 4 fixed lines.
func (m *Model) contentHeight() int {
	return max(0, m.height-4)
}

// contentWidth is the width available for the active view panel.
// Layout: sidebar(sidebarWidth) + divider(1) + content(N).
func (m *Model) contentWidth() int {
	return max(0, m.width-sidebarWidth-1)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
