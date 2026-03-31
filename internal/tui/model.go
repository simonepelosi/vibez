package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
	"github.com/simone-vibes/vibez/internal/tui/views"
)

// ── Modal modes (vim-inspired) ────────────────────────────────────────────

type viewMode int

const (
	modeNormal  viewMode = iota
	modeSearch           // '/' opens — query accumulates in searchQuery
	modeCommand          // ':' opens — command accumulates in cmdBuf
)

// ── Content displayed in the main area ───────────────────────────────────

type contentArea int

const (
	contentEmpty   contentArea = iota
	contentResults             // search results
	contentLibrary             // library browser
)

// ── Messages ──────────────────────────────────────────────────────────────

type playerStateMsg player.State
type searchResultMsg struct {
	result *provider.SearchResult
	err    error
}
type tickMsg time.Time
type glowTickMsg time.Time
type introTickMsg time.Time
type errMsg struct{ err error }

// introFrames: logo types out letter-by-letter, then holds for 8 frames.
var introFrames = func() []string {
	logo := "♪ vibez"
	runes := []rune(logo)
	frames := make([]string, 0, len(runes)+16)
	for i := range runes {
		frames = append(frames, string(runes[:i+1]))
	}
	for range 8 {
		frames = append(frames, logo)
	}
	return frames
}()

const introDone = -1

// ── Model ─────────────────────────────────────────────────────────────────

type Model struct {
	cfg      *config.Config
	provider provider.Provider
	player   player.Player

	width, height int

	playerState player.State
	stateCh     <-chan player.State
	queueIDs    []string // current playback queue (for "add to queue")

	// Views
	library *views.LibraryModel
	search  *views.SearchModel // results-only renderer

	// Modal state
	mode    viewMode
	content contentArea

	// Search accumulation (mode == modeSearch)
	searchQuery string

	// Command accumulation (mode == modeCommand)
	cmdBuf string

	// Double-key tracking (for 'gg')
	lastKey string

	// Errors
	errMsg    string
	errExpiry time.Time

	// Animation
	glowStep  int
	introStep int // introDone (-1) when complete

	// Cursor in results list
	searchCursor int
}

func New(cfg *config.Config, prov provider.Provider, plyr player.Player) *Model {
	m := &Model{
		cfg:      cfg,
		provider: prov,
		player:   plyr,
		content:  contentEmpty,
	}
	if plyr != nil {
		m.stateCh = plyr.Subscribe()
	}
	m.library = views.NewLibrary(prov)
	m.search = views.NewSearch(prov)
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tick(),
		glowTick(), // always run — animates both the logo and the track title
		introTick(),
		m.library.Init(),
	}
	if m.stateCh != nil {
		cmds = append(cmds, waitForState(m.stateCh))
	}
	return tea.Batch(cmds...)
}

// ── Timers ────────────────────────────────────────────────────────────────

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func glowTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg { return glowTickMsg(t) })
}

func introTick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(t time.Time) tea.Msg { return introTickMsg(t) })
}

func waitForState(ch <-chan player.State) tea.Cmd {
	return func() tea.Msg { return playerStateMsg(<-ch) }
}

// ── Update ────────────────────────────────────────────────────────────────

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		cw, ch := m.contentDimensions()
		m.library.SetSize(cw, ch)
		m.search.SetSize(cw, ch)

	case tickMsg:
		if m.errMsg != "" && time.Now().After(m.errExpiry) {
			m.errMsg = ""
		}
		cmds = append(cmds, tick())

	case glowTickMsg:
		m.glowStep++
		cmds = append(cmds, glowTick())

	case introTickMsg:
		if m.introStep != introDone {
			m.introStep++
			if m.introStep >= len(introFrames) {
				m.introStep = introDone
			} else {
				cmds = append(cmds, introTick())
			}
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
		if !wasPlaying && m.playerState.Playing {
			cmds = append(cmds, glowTick())
		}
		cmds = append(cmds, waitForState(m.stateCh))

	case searchResultMsg:
		m.search.SetState(
			func() []provider.Track {
				if msg.result != nil {
					return msg.result.Tracks
				}
				return nil
			}(),
			false,
			msg.err,
		)
		m.searchCursor = 0
		m.content = contentResults

	case views.PlayTracksMsg:
		if msg.Track != nil {
			m.playerState.Track = msg.Track
		}
		m.queueIDs = msg.IDs
		cmds = append(cmds, m.playerCmd(func() error {
			if msg.PlaylistID != "" {
				return m.player.SetPlaylist(msg.PlaylistID, msg.StartIdx)
			}
			return m.player.SetQueue(msg.IDs)
		}))
		m.mode = modeNormal
		m.content = contentResults

	case errMsg:
		m.errMsg = msg.err.Error()
		m.errExpiry = time.Now().Add(3 * time.Second)

	case tea.KeyMsg:
		cmd := m.handleKey(msg)
		cmds = append(cmds, cmd)

	default:
		// Forward library background loads
		var libCmd tea.Cmd
		m.library, libCmd = m.library.Update(msg)
		cmds = append(cmds, libCmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()

	// ctrl+c always quits
	if k == "ctrl+c" {
		if m.player != nil {
			_ = m.player.Close()
		}
		return tea.Quit
	}

	switch m.mode {
	case modeSearch:
		return m.handleSearchKey(k)
	case modeCommand:
		return m.handleCommandKey(k)
	default:
		return m.handleNormalKey(msg, k)
	}
}

func (m *Model) handleSearchKey(k string) tea.Cmd {
	switch k {
	case "esc":
		m.mode = modeNormal
		m.searchQuery = ""
		return nil
	case "enter":
		if m.content == contentResults {
			results := m.search.Results()
			if len(results) > 0 && m.searchCursor < len(results) {
				t := results[m.searchCursor]
				tc := t
				m.queueIDs = []string{views.PlaybackID(tc)}
				m.playerState.Track = &tc
				m.mode = modeNormal
				return m.playerCmd(func() error { return m.player.SetQueue(m.queueIDs) })
			}
		}
		return nil
	case "up", "k":
		if m.searchCursor > 0 {
			m.searchCursor--
		}
		return nil
	case "down", "j":
		results := m.search.Results()
		if m.searchCursor < len(results)-1 {
			m.searchCursor++
		}
		return nil
	case "a":
		results := m.search.Results()
		if len(results) > 0 && m.searchCursor < len(results) {
			t := results[m.searchCursor]
			m.queueIDs = append(m.queueIDs, views.PlaybackID(t))
			return m.playerCmd(func() error { return m.player.SetQueue(m.queueIDs) })
		}
		return nil
	case "backspace":
		if len(m.searchQuery) > 0 {
			runes := []rune(m.searchQuery)
			m.searchQuery = string(runes[:len(runes)-1])
			return m.scheduleSearch(m.searchQuery)
		}
		return nil
	default:
		if len(k) == 1 && k[0] >= 32 {
			m.searchQuery += k
			return m.scheduleSearch(m.searchQuery)
		}
	}
	return nil
}

func (m *Model) handleCommandKey(k string) tea.Cmd {
	switch k {
	case "esc":
		m.mode = modeNormal
		m.cmdBuf = ""
	case "enter":
		cmd := m.executeCommand(m.cmdBuf)
		m.cmdBuf = ""
		m.mode = modeNormal
		return cmd
	case "backspace":
		if len(m.cmdBuf) > 0 {
			m.cmdBuf = m.cmdBuf[:len(m.cmdBuf)-1]
		}
	default:
		if len(k) == 1 && k[0] >= 32 {
			m.cmdBuf += k
		}
	}
	return nil
}

func (m *Model) executeCommand(cmd string) tea.Cmd {
	switch cmd {
	case "q", "quit":
		if m.player != nil {
			_ = m.player.Close()
		}
		return tea.Quit
	}
	m.errMsg = fmt.Sprintf("unknown command: %s", cmd)
	m.errExpiry = time.Now().Add(3 * time.Second)
	return nil
}

func (m *Model) handleNormalKey(msg tea.KeyMsg, k string) tea.Cmd {
	// Library mode: forward navigation to library
	if m.content == contentLibrary {
		switch k {
		case "esc", "backspace":
			m.content = contentResults
			return nil
		}
		var cmd tea.Cmd
		m.library, cmd = m.library.Update(msg)
		return cmd
	}

	switch k {
	case "/":
		m.mode = modeSearch
		m.searchQuery = ""
		m.search.SetState(nil, false, nil)
		m.content = contentResults

	case ":":
		m.mode = modeCommand
		m.cmdBuf = ""

	case "l":
		m.content = contentLibrary

	case "q":
		if m.player != nil {
			_ = m.player.Close()
		}
		return tea.Quit

	case " ":
		return m.togglePlayPause()

	case "n":
		m.lastKey = ""
		return m.playerCmd(func() error { return m.player.Next() })

	case "p":
		m.lastKey = ""
		return m.playerCmd(func() error { return m.player.Previous() })

	case "+", "=":
		m.lastKey = ""
		return m.adjustVolume(0.05)

	case "-":
		m.lastKey = ""
		return m.adjustVolume(-0.05)

	case "r":
		m.lastKey = ""
		// Cycle: off → all → one → off
		next := player.RepeatModeOff
		switch m.playerState.RepeatMode {
		case player.RepeatModeOff:
			next = player.RepeatModeAll
		case player.RepeatModeAll:
			next = player.RepeatModeOne
		case player.RepeatModeOne:
			next = player.RepeatModeOff
		}
		m.playerState.RepeatMode = next
		return m.playerCmd(func() error { return m.player.SetRepeat(next) })

	case "s":
		m.lastKey = ""
		on := !m.playerState.ShuffleMode
		m.playerState.ShuffleMode = on
		return m.playerCmd(func() error { return m.player.SetShuffle(on) })

	case "j", "down":
		m.lastKey = ""
		results := m.search.Results()
		if len(results) > 0 && m.searchCursor < len(results)-1 {
			m.searchCursor++
		}

	case "k", "up":
		m.lastKey = ""
		if m.searchCursor > 0 {
			m.searchCursor--
		}

	case "g":
		if m.lastKey == "g" {
			m.searchCursor = 0
			m.lastKey = ""
		} else {
			m.lastKey = "g"
		}

	case "G":
		m.lastKey = ""
		results := m.search.Results()
		if len(results) > 0 {
			m.searchCursor = len(results) - 1
		}

	case "enter":
		m.lastKey = ""
		results := m.search.Results()
		if len(results) > 0 && m.searchCursor < len(results) {
			t := results[m.searchCursor]
			tc := t
			m.queueIDs = []string{views.PlaybackID(tc)}
			m.playerState.Track = &tc
			return m.playerCmd(func() error { return m.player.SetQueue(m.queueIDs) })
		}

	case "a":
		m.lastKey = ""
		results := m.search.Results()
		if len(results) > 0 && m.searchCursor < len(results) {
			t := results[m.searchCursor]
			m.queueIDs = append(m.queueIDs, views.PlaybackID(t))
			return m.playerCmd(func() error { return m.player.SetQueue(m.queueIDs) })
		}

	default:
		m.lastKey = ""
	}

	return nil
}

func (m *Model) scheduleSearch(query string) tea.Cmd {
	if query == "" {
		m.search.SetState(nil, false, nil)
		return nil
	}
	m.search.SetState(nil, true, nil)
	prov := m.provider
	return func() tea.Msg {
		time.Sleep(300 * time.Millisecond)
		result, err := prov.Search(context.Background(), query)
		return searchResultMsg{result: result, err: err}
	}
}

func (m *Model) togglePlayPause() tea.Cmd {
	return func() tea.Msg {
		if m.player == nil {
			return errMsg{fmt.Errorf("no player")}
		}
		var err error
		if m.playerState.Playing {
			err = m.player.Pause()
		} else {
			err = m.player.Play()
		}
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m *Model) playerCmd(fn func() error) tea.Cmd {
	return func() tea.Msg {
		if m.player == nil {
			return errMsg{fmt.Errorf("no player")}
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
			return errMsg{fmt.Errorf("no player")}
		}
		newVol := clamp(m.playerState.Volume+delta, 0, 1)
		if err := m.player.SetVolume(newVol); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

// ── View ──────────────────────────────────────────────────────────────────

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.introStep != introDone {
		return m.renderIntro()
	}

	sep := m.renderSeparator()
	contentH := max(0, m.height-10) // fixed overhead: logo(1)+blank(1)+nowplaying(4)+blank(1)+sep(1)+sep(1)+status(1)

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderLogoLine(),        // line 1
		"",                        // line 2 blank
		m.renderNowPlaying(),      // lines 3-5 (3 lines)
		"",                        // line 6 blank
		sep,                       // line 7
		m.renderContent(contentH), // variable
		sep,
		m.renderStatusBar(),
	)
}

func (m *Model) renderIntro() string {
	if m.introStep < 0 || m.introStep >= len(introFrames) {
		return ""
	}

	frame := introFrames[m.introStep]
	glowIdx := m.introStep % len(styles.GlowPalette)

	var logo strings.Builder
	for _, r := range frame {
		logo.WriteString(lipgloss.NewStyle().
			Foreground(styles.GlowPalette[glowIdx]).
			Render(string(r)))
	}

	var subtitle string
	if m.introStep >= len("♪ vibez") {
		subtitle = "\n" + centerStr(
			lipgloss.NewStyle().Foreground(styles.ColorMuted).Render("connecting…"),
			m.width,
		)
	}

	topPad := max(0, (m.height-3)/2)
	return strings.Repeat("\n", topPad) +
		centerStr(logo.String(), m.width) +
		subtitle
}

func (m *Model) renderLogoLine() string {
	return centerStr(views.RenderGlowTitle("♪ vibez", m.glowStep), m.width)
}

func (m *Model) renderNowPlaying() string {
	t := m.playerState.Track
	if t == nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			centerStr(styles.QueueItemMuted.Render("♪"), m.width),
			centerStr(styles.QueueItemMuted.Render("press / to search"), m.width),
			"",
			"",
		)
	}

	// Title — glow sweep when playing, static italic when paused.
	var titleStr string
	if m.playerState.Playing {
		titleStr = views.RenderGlowTitle(t.Title, m.glowStep)
	} else {
		titleStr = styles.NowPlayingTitle.Render(t.Title)
	}

	// Status line: play icon + elapsed/total + repeat/shuffle mode indicators.
	icon, statusStyle := "⏸", styles.Paused
	if m.playerState.Playing {
		icon, statusStyle = "▶", styles.Playing
	}
	elapsed := views.FormatDuration(m.playerState.Position)
	total := views.FormatDuration(t.Duration)
	timeStr := statusStyle.Render(fmt.Sprintf("%s  %s / %s", icon, elapsed, total))

	repeatIcon, repeatStyle := "↺", styles.QueueItemMuted
	switch m.playerState.RepeatMode {
	case player.RepeatModeAll:
		repeatIcon, repeatStyle = "↺", styles.Playing
	case player.RepeatModeOne:
		repeatIcon, repeatStyle = "↻", styles.Playing
	}
	shuffleIcon, shuffleStyle := "⇄", styles.QueueItemMuted
	if m.playerState.ShuffleMode {
		shuffleStyle = styles.Playing
	}
	modes := repeatStyle.Render(repeatIcon) + "  " + shuffleStyle.Render(shuffleIcon)
	statusLine := timeStr + "   " + modes

	return lipgloss.JoinVertical(lipgloss.Left,
		centerStr(titleStr, m.width),
		centerStr(styles.NowPlayingArtist.Render(t.Artist), m.width),
		centerStr(styles.NowPlayingAlbum.Render(t.Album), m.width),
		centerStr(statusLine, m.width),
	)
}

func (m *Model) renderContent(h int) string {
	if h <= 0 {
		return ""
	}

	var raw string
	switch m.content {
	case contentLibrary:
		raw = m.library.View()
	case contentResults:
		m.search.SetCursor(m.searchCursor)
		raw = m.search.View()
		if raw == "" && !m.search.Loading() {
			raw = centerStr(styles.QueueItemMuted.Render("♪"), m.width) + "\n" +
				centerStr(styles.QueueItemMuted.Render("press / to search"), m.width)
		}
	default:
		raw = centerStr(styles.QueueItemMuted.Render("♪"), m.width) + "\n" +
			centerStr(styles.QueueItemMuted.Render("press / to search"), m.width)
	}

	if m.errMsg != "" {
		raw = styles.ErrorStyle.Render("⚠  "+m.errMsg) + "\n" + raw
	}

	// Pad/trim to exactly h lines
	lines := strings.Split(raw, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:h], "\n")
}

func (m *Model) renderStatusBar() string {
	muted := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	accent := lipgloss.NewStyle().Foreground(styles.ColorAccent)
	dot := muted.Render("  ·  ")

	switch m.mode {
	case modeSearch:
		cursor := lipgloss.NewStyle().Foreground(styles.ColorAccent).Render("_")
		return " " + styles.ModeSearch.Render("SEARCH") + "  " +
			accent.Render("/") + " " +
			styles.Header.Render(m.searchQuery) + cursor
	case modeCommand:
		cursor := lipgloss.NewStyle().Foreground(styles.ColorError).Render("_")
		return " " + styles.ModeCommand.Render("COMMAND") + "  " +
			accent.Render(":") + m.cmdBuf + cursor
	default:
		parts := []string{
			accent.Render("/") + muted.Render(" search"),
			accent.Render("a") + muted.Render(" add"),
			accent.Render("n") + muted.Render(" next"),
			accent.Render("r") + muted.Render(" repeat"),
			accent.Render("s") + muted.Render(" shuffle"),
			accent.Render(":q") + muted.Render(" quit"),
		}
		return " " + styles.ModeNormal.Render("NORMAL") + "  " + strings.Join(parts, dot)
	}
}

func (m *Model) renderSeparator() string {
	return styles.Separator.Render(strings.Repeat("─", m.width))
}

// contentDimensions returns the width and height for the content area.
func (m *Model) contentDimensions() (int, int) {
	w := max(0, m.width)
	h := max(0, m.height-10)
	return w, h
}

// contentHeight returns the number of rows available for the content area.
// Fixed overhead = 10 lines: logo(1)+blank(1)+nowplaying(4)+blank(1)+sep(1)+sep(1)+status(1).
func (m *Model) contentHeight() int {
	_, h := m.contentDimensions()
	return h
}

func centerStr(s string, width int) string {
	w := lipgloss.Width(s)
	pad := max(0, (width-w)/2)
	return strings.Repeat(" ", pad) + s
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
