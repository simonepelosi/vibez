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

// ── ContentView interface ─────────────────────────────────────────────────

// ContentView is the interface every content panel must implement.
// To register a new panel: implement this interface and append to m.panels in New().
// Nothing else in the model needs to change.
type ContentView interface {
	NavKey() string   // normal-mode key to activate this panel
	NavLabel() string // short label shown in the status bar
	SetSize(w, h int)
	Update(msg tea.KeyMsg) tea.Cmd
	View() string
}

// libraryPanel wraps views.LibraryModel to satisfy ContentView.
type libraryPanel struct{ m *views.LibraryModel }

func (p *libraryPanel) NavKey() string   { return "l" }
func (p *libraryPanel) NavLabel() string { return "library" }
func (p *libraryPanel) SetSize(w, h int) { p.m.SetSize(w, h) }
func (p *libraryPanel) Update(msg tea.KeyMsg) tea.Cmd {
	updated, cmd := p.m.Update(msg)
	p.m = updated
	return cmd
}
func (p *libraryPanel) View() string  { return p.m.View() }
func (p *libraryPanel) Init() tea.Cmd { return p.m.Init() }

// queuePanel wraps views.QueueModel to satisfy ContentView.
type queuePanel struct{ m *views.QueueModel }

func (p *queuePanel) NavKey() string   { return "q" }
func (p *queuePanel) NavLabel() string { return "queue" }
func (p *queuePanel) SetSize(w, h int) { p.m.SetSize(w, h) }
func (p *queuePanel) Update(msg tea.KeyMsg) tea.Cmd {
	p.m.Update(msg)
	return nil
}
func (p *queuePanel) View() string                      { return p.m.View() }
func (p *queuePanel) SetTracks(tracks []provider.Track) { p.m.SetTracks(tracks) }

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
	queueIDs    []string         // current playback queue (for "add to queue")
	queueTracks []provider.Track // full track objects parallel to queueIDs

	// Panels
	panels      []ContentView // registered content panels; add new ones in New()
	activePanel int           // index into panels; -1 = none active
	library     *libraryPanel
	queue       *queuePanel

	// Search popup (not a panel)
	search *views.SearchModel

	// Modal state
	mode viewMode

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
}

func New(cfg *config.Config, prov provider.Provider, plyr player.Player) *Model {
	m := &Model{
		cfg:         cfg,
		provider:    prov,
		player:      plyr,
		activePanel: -1,
	}
	if plyr != nil {
		m.stateCh = plyr.Subscribe()
	}
	m.library = &libraryPanel{m: views.NewLibrary(prov)}
	m.queue = &queuePanel{m: views.NewQueue()}
	m.search = views.NewSearch(prov)
	// Register panels — append here to add new views, nothing else changes:
	m.panels = []ContentView{m.library, m.queue}
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tick(),
		glowTick(),
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
		for _, p := range m.panels {
			p.SetSize(cw, ch)
		}
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

	case views.PlayTracksMsg:
		if msg.Track != nil {
			m.playerState.Track = msg.Track
			m.queueTracks = []provider.Track{*msg.Track}
		}
		m.queueIDs = msg.IDs
		m.queue.SetTracks(m.queueTracks)
		cmds = append(cmds, m.playerCmd(func() error {
			if msg.PlaylistID != "" {
				return m.player.SetPlaylist(msg.PlaylistID, msg.StartIdx)
			}
			return m.player.SetQueue(msg.IDs)
		}))
		m.mode = modeNormal
		m.activePanel = -1

	case errMsg:
		m.errMsg = msg.err.Error()
		m.errExpiry = time.Now().Add(3 * time.Second)

	case tea.KeyMsg:
		cmd := m.handleKey(msg)
		cmds = append(cmds, cmd)

	default:
		// Forward library background loads
		updated, libCmd := m.library.m.Update(msg)
		m.library.m = updated
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
		return m.handleSearchKey(k, msg)
	case modeCommand:
		return m.handleCommandKey(k)
	default:
		return m.handleNormalKey(msg, k)
	}
}

func (m *Model) handleSearchKey(k string, msg tea.KeyMsg) tea.Cmd {
	switch k {
	case "esc":
		m.mode = modeNormal
		m.searchQuery = ""
		return nil
	case "enter":
		// Play now — replaces queue and starts immediately.
		if t := m.search.SelectedTrack(); t != nil {
			tc := *t
			m.queueTracks = []provider.Track{tc}
			m.queueIDs = []string{views.PlaybackID(tc)}
			m.playerState.Track = &tc
			m.queue.SetTracks(m.queueTracks)
			m.mode = modeNormal
			return m.playerCmd(func() error { return m.player.SetQueue(m.queueIDs) })
		}
		return nil
	case "tab":
		// Add to queue — appends without interrupting playback.
		if t := m.search.SelectedTrack(); t != nil {
			tc := *t
			m.queueTracks = append(m.queueTracks, tc)
			m.queueIDs = append(m.queueIDs, views.PlaybackID(tc))
			m.queue.SetTracks(m.queueTracks)
			return m.playerCmd(func() error { return m.player.AppendQueue([]string{views.PlaybackID(tc)}) })
		}
		return nil
	case "up", "down", "pgup", "pgdown":
		_, cmd := m.search.Update(msg)
		return cmd
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
	// Check if key activates a panel (toggle off if already active)
	for i, p := range m.panels {
		if k == p.NavKey() {
			if m.activePanel == i {
				m.activePanel = -1 // toggle off
			} else {
				m.activePanel = i
			}
			m.lastKey = ""
			return nil
		}
	}

	// Forward keys to the active panel (e.g. library list navigation)
	if m.activePanel >= 0 {
		if k == "esc" {
			m.activePanel = -1
			m.lastKey = ""
			return nil
		}
		return m.panels[m.activePanel].Update(msg)
	}

	switch k {
	case "/":
		m.mode = modeSearch
		m.searchQuery = ""
		m.search.SetState(nil, false, nil)

	case ":":
		m.mode = modeCommand
		m.cmdBuf = ""

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

	case "k", "up":
		m.lastKey = ""

	case "g":
		if m.lastKey == "g" {
			m.lastKey = ""
		} else {
			m.lastKey = "g"
		}

	case "G":
		m.lastKey = ""

	case "enter":
		m.lastKey = ""
		if t := m.search.SelectedTrack(); t != nil {
			tc := *t
			m.queueTracks = []provider.Track{tc}
			m.queueIDs = []string{views.PlaybackID(tc)}
			m.playerState.Track = &tc
			m.queue.SetTracks(m.queueTracks)
			return m.playerCmd(func() error { return m.player.SetQueue(m.queueIDs) })
		}

	case "a":
		m.lastKey = ""
		if t := m.search.SelectedTrack(); t != nil {
			tc := *t
			m.queueTracks = append(m.queueTracks, tc)
			m.queueIDs = append(m.queueIDs, views.PlaybackID(tc))
			m.queue.SetTracks(m.queueTracks)
			return m.playerCmd(func() error { return m.player.AppendQueue([]string{views.PlaybackID(tc)}) })
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
			"",
			centerStr(styles.QueueItemMuted.Render("silence is not a vibe"), m.width),
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

	// Search popup overlays everything when in search mode
	if m.mode == modeSearch {
		return m.renderSearchPopup(h)
	}

	var raw string
	if m.activePanel >= 0 && m.activePanel < len(m.panels) {
		raw = m.panels[m.activePanel].View()
	}
	// Default idle state — animated bear + search hint, vertically centred.
	if raw == "" {
		const bearContent = views.BearLines + 2 // bear(3) + blank + hint
		topPad := max(0, (h-bearContent)/2)
		var sb strings.Builder
		sb.WriteString(strings.Repeat("\n", topPad))
		sb.WriteString(views.RenderBear(m.glowStep, m.playerState.Playing, m.width))
		sb.WriteString("\n\n")
		sb.WriteString(centerStr(styles.QueueItemMuted.Render("press / to search"), m.width))
		raw = sb.String()
	}

	if m.errMsg != "" {
		raw = styles.ErrorStyle.Render("⚠  "+m.errMsg) + "\n" + raw
	}

	lines := strings.Split(raw, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:h], "\n")
}

func (m *Model) renderSearchPopup(h int) string {
	accent := lipgloss.NewStyle().Foreground(styles.ColorAccent)
	muted := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	cursor := accent.Render("█")

	inputLine := accent.Render("/") + "  " +
		lipgloss.NewStyle().Foreground(styles.ColorFg).Render(m.searchQuery) +
		cursor

	popupW := max(40, m.width-16)
	sep := muted.Render(strings.Repeat("─", popupW-2))

	// Footer: 2 lines (footerSep + footer text). Fixed: border(2)+input(1)+sep(1)+footer(2) = 6.
	listH := max(1, h-6)
	m.search.SetSize(popupW-4, listH) // -4 for border(2)+padding(2)

	listView := m.search.View()
	if listView == "" && !m.search.Loading() && m.searchQuery != "" {
		listView = "  " + muted.Render("no results")
	}

	// Pad list view to exactly listH lines so the footer stays pinned.
	listLines := strings.Split(listView, "\n")
	for len(listLines) < listH {
		listLines = append(listLines, "")
	}
	listBlock := strings.Join(listLines[:listH], "\n")

	footerSep := muted.Render(strings.Repeat("─", popupW-2))
	footer := "  " + accent.Render("Enter") + muted.Render(" play now") +
		"  ·  " + accent.Render("Tab") + muted.Render(" add to queue") +
		"  ·  " + accent.Render("Esc") + muted.Render(" close")

	inner := inputLine + "\n" + sep + "\n" + listBlock + "\n" + footerSep + "\n" + footer

	popup := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Width(popupW).
		Padding(0, 1).
		Render(inner)

	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, popup,
		lipgloss.WithWhitespaceBackground(styles.ColorBg))
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
		}
		// Append one hint per registered panel (auto-generated)
		for _, p := range m.panels {
			label := p.NavLabel()
			if m.activePanel >= 0 && m.panels[m.activePanel] == p {
				label = lipgloss.NewStyle().Foreground(styles.ColorAccent).Underline(true).Render(label)
				parts = append(parts, accent.Render(p.NavKey())+" "+label)
			} else {
				parts = append(parts, accent.Render(p.NavKey())+muted.Render(" "+label))
			}
		}
		parts = append(parts, accent.Render(":q")+muted.Render(" quit"))
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
