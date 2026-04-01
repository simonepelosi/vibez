package tui

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/player"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
	"github.com/simone-vibes/vibez/internal/tui/views"
	"github.com/simone-vibes/vibez/internal/vibe"
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
func (p *queuePanel) View() string                          { return p.m.View() }
func (p *queuePanel) SetTracks(tracks []provider.Track)     { p.m.SetTracks(tracks) }
func (p *queuePanel) SelectedTrack() (int, *provider.Track) { return p.m.SelectedTrack() }

// ── Messages ──────────────────────────────────────────────────────────────

type playerStateMsg player.State
type searchResultMsg struct {
	result *provider.SearchResult
	err    error
}
type vibeResultMsg struct {
	query     string
	tracks    []provider.Track
	err       error
	discovery bool // true when result is from a discovery auto-refill
}
type loveSongMsg struct {
	title string
	loved bool
	err   error
}
type songRatingMsg struct {
	trackID string
	loved   bool
}
type tickMsg time.Time
type glowTickMsg time.Time
type introTickMsg time.Time
type memTickMsg struct{ stats string }
type errMsg struct{ err error }
type playlistCreatedMsg struct{ name string }

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

// ── Discovery mode ─────────────────────────────────────────────────────────

// discoveryMode holds state for the continuous-discovery feature.
// When enabled, one song is queued 30 seconds before the current track ends.
// The similarity value (0=very different, 1=very similar) controls how
// adventurous the search is.
type discoveryMode struct {
	enabled        bool
	seed           *provider.Track
	similarity     float64 // 0.0–1.0
	refilling      bool    // background search in progress
	triggeredForID string  // ID of track for which we already fired a search
}

const (
	discoveryTriggerBefore  = 30 * time.Second // queue next song this long before track ends
	discoverySimilarityStep = 0.1
)

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

	// Discovery mode
	discovery discoveryMode

	// Panels
	panels      []ContentView // registered content panels; add new ones in New()
	activePanel int           // index into panels; -1 = none active
	library     *libraryPanel
	queue       *queuePanel

	// Vibe panel (always visible, right split)
	vibe *views.VibeModel

	// Search popup (not a panel)
	search *views.SearchModel

	// Modal state
	mode viewMode

	// Search accumulation (mode == modeSearch)
	searchQuery string

	// Command accumulation (mode == modeCommand)
	cmdBuf     string
	cmdSuggIdx int // currently highlighted suggestion (0-based)

	// Double-key tracking (for 'gg')
	lastKey string

	// Errors
	errMsg    string
	errExpiry time.Time

	// Debug log
	debugLog    []string
	debugView   bool
	debugScroll int // lines scrolled up from tail (0 = show latest)

	// Favorites: track IDs that the user has hearted this session.
	favorites map[string]bool

	// Animation
	glowStep   int
	introStep  int    // introDone (-1) when complete
	initStatus string // status text shown on the loading screen

	// Memory profiling (enabled with --mem-profiling)
	memProfiling bool
	memStats     string
	helperPaths  []string
}

func New(cfg *config.Config, prov provider.Provider, plyr player.Player, opts Options) *Model {
	m := &Model{
		cfg:          cfg,
		provider:     prov,
		player:       plyr,
		activePanel:  -1,
		memProfiling: opts.MemProfiling,
	}
	if plyr != nil {
		m.stateCh = plyr.Subscribe()
	}
	m.library = &libraryPanel{m: views.NewLibrary(prov)}
	m.queue = &queuePanel{m: views.NewQueue()}
	m.vibe = views.NewVibe()
	m.search = views.NewSearch(prov)
	m.favorites = make(map[string]bool)
	m.panels = []ContentView{m.library, m.queue}
	if opts.Backend != "" {
		m.appendLog("[engine] backend: " + opts.Backend)
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tick(),
		glowTick(),
		introTick(),
	}
	if m.provider != nil {
		cmds = append(cmds, m.library.Init())
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

func memTick(helperPaths []string) tea.Cmd {
	return tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
		return memTickMsg{stats: collectMemStats(helperPaths)}
	})
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
		inner := max(0, m.width-2)
		contentW := max(0, inner-2)
		panelH := m.panelHeight()
		m.library.SetSize(contentW, panelH)
		m.search.SetSize(contentW, panelH)

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
				if m.player == nil {
					// Hold at last frame until the engine signals ready.
					m.introStep = len(introFrames) - 1
				} else {
					m.introStep = introDone
				}
			}
			cmds = append(cmds, introTick())
		}

	case playerStateMsg:
		wasPlaying := m.playerState.Playing
		s := player.State(msg)
		if s.Log != "" {
			m.appendLog(s.Log)
			s.Log = ""
		}
		if s.Error != "" {
			if strings.Contains(s.Error, "CONTENT_RESTRICTED") {
				// Track is region-locked or unavailable in this storefront.
				// Log it silently and skip to the next track — same behaviour
				// as any streaming app encountering a restricted title.
				title := "track"
				if s.Track != nil {
					title = s.Track.Artist + " — " + s.Track.Title
				}
				m.appendLog(fmt.Sprintf("[skip] restricted: %s", title))
				if m.player != nil {
					cmds = append(cmds, m.playerCmd(func() error { return m.player.Next() }))
				}
			} else {
				m.appendLog("[error] " + s.Error)
				m.errMsg = s.Error
				m.errExpiry = time.Now().Add(4 * time.Second)
			}
			s.Error = ""
		}
		if s.Track != nil && (m.playerState.Track == nil || m.playerState.Track.Title != s.Track.Title) {
			m.appendLog("[playing] " + s.Track.Artist + " — " + s.Track.Title)
			// Check whether the new track is already loved on Apple Music.
			cmds = append(cmds, m.checkSongRatingCmd(s.Track))
		}
		// Discovery: queue the next song 30 s before the current track ends.
		// We guard with triggeredForID so the search fires exactly once per track.
		if m.discovery.enabled && !m.discovery.refilling &&
			s.Track != nil && s.Track.Duration > discoveryTriggerBefore &&
			s.Position >= s.Track.Duration-discoveryTriggerBefore &&
			m.discovery.triggeredForID != s.Track.ID {
			m.discovery.triggeredForID = s.Track.ID
			m.discovery.refilling = true
			m.syncDiscoveryView()
			cmds = append(cmds, m.runDiscoverySearch())
		}
		m.playerState = s
		if !wasPlaying && m.playerState.Playing {
			cmds = append(cmds, glowTick())
		}
		cmds = append(cmds, waitForState(m.stateCh))

	case songRatingMsg:
		// Update favorite state to match what Apple Music reports.
		if msg.trackID != "" {
			m.favorites[msg.trackID] = msg.loved
		}

	case views.VibeQueryMsg:
		// User submitted a vibe description — start async provider search.
		m.vibe.SetSearching()
		cmds = append(cmds, m.runVibeSearch(msg.Query))

	case vibeResultMsg:
		if msg.err != nil {
			if !msg.discovery {
				m.vibe.SetResult(0, msg.err)
			}
			m.appendLog(fmt.Sprintf("[vibe] search error: %v", msg.err))
		} else {
			ids := make([]string, len(msg.tracks))
			for i, t := range msg.tracks {
				ids[i] = views.PlaybackID(t)
			}
			if m.player != nil {
				if err := m.player.AppendQueue(ids); err != nil {
					if !msg.discovery {
						m.vibe.SetResult(0, err)
					}
					break
				}
			}
			m.queueTracks = append(m.queueTracks, msg.tracks...)
			m.queueIDs = append(m.queueIDs, ids...)
			m.queue.SetTracks(m.queueTracks)
			if msg.discovery {
				m.discovery.refilling = false
				m.syncDiscoveryView()
				m.appendLog(fmt.Sprintf("[discovery] refilled %d tracks", len(msg.tracks)))
			} else {
				m.vibe.SetResult(len(msg.tracks), nil)
				m.appendLog(fmt.Sprintf("[vibe] added %d tracks for %q", len(msg.tracks), msg.query))
			}
		}

	case loveSongMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("[fav] ✗ %s: %v", msg.title, msg.err))
		} else {
			state := "♡"
			if msg.loved {
				state = "♥"
			}
			m.appendLog(fmt.Sprintf("[fav] %s %s synced to Apple Music", state, msg.title))
		}

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

	case InitStatusMsg:
		m.initStatus = string(msg)

	case EngineReadyMsg:
		m.player = msg.Player
		m.provider = msg.Provider
		m.stateCh = msg.Player.Subscribe()
		m.library = &libraryPanel{m: views.NewLibrary(msg.Provider)}
		m.search = views.NewSearch(msg.Provider)
		m.helperPaths = msg.HelperPaths
		m.appendLog("[engine] backend: " + msg.Backend)
		cmds = append(cmds, waitForState(m.stateCh), m.library.Init())
		if m.memProfiling {
			cmds = append(cmds, memTick(m.helperPaths))
		}

	case memTickMsg:
		m.memStats = msg.stats
		if m.memProfiling {
			return m, memTick(m.helperPaths)
		}

	case InitErrMsg:
		m.appendLog("[init error] " + msg.Err.Error())
		m.errMsg = msg.Err.Error()
		m.errExpiry = time.Now().Add(30 * time.Second)
		m.introStep = introDone

	case errMsg:
		m.appendLog("[error] " + msg.err.Error())
		m.errMsg = msg.err.Error()
		m.errExpiry = time.Now().Add(3 * time.Second)

	case playlistCreatedMsg:
		m.errMsg = "✓ Playlist \"" + msg.name + "\" saved"
		m.errExpiry = time.Now().Add(4 * time.Second)

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
			m.appendLog(fmt.Sprintf("[queue] play now: %s — %s", tc.Artist, tc.Title))
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
			m.appendLog(fmt.Sprintf("[queue] append: %s — %s", tc.Artist, tc.Title))
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

// ── Command palette ───────────────────────────────────────────────────────

// cmdEntry describes a single command shown in the command palette.
type cmdEntry struct {
	trigger     string // prefix matched against cmdBuf
	usage       string // full usage shown to the user
	description string
}

// allCommands is the master list shown in the command palette.
var allCommands = []cmdEntry{
	{"save", "save <name>", "Save queue as a playlist in Apple Music"},
	{"debug-logs", "debug-logs", "Toggle debug log panel"},
	{"q", "q", "Quit vibez"},
	{"quit", "quit", "Quit vibez"},
}

// commandSuggestions returns commands whose trigger starts with the current
// cmdBuf, or all commands when the buffer is empty.
func (m *Model) commandSuggestions() []cmdEntry {
	var out []cmdEntry
	for _, c := range allCommands {
		if m.cmdBuf == "" || strings.HasPrefix(c.trigger, m.cmdBuf) ||
			strings.HasPrefix(c.usage, m.cmdBuf) {
			out = append(out, c)
		}
	}
	return out
}

func (m *Model) handleCommandKey(k string) tea.Cmd {
	switch k {
	case "esc":
		m.mode = modeNormal
		m.cmdBuf = ""
		m.cmdSuggIdx = 0
	case "enter":
		cmd := m.cmdBuf
		m.cmdBuf = ""
		m.cmdSuggIdx = 0
		m.mode = modeNormal
		return m.executeCommand(cmd)
	case "tab":
		suggs := m.commandSuggestions()
		if len(suggs) > 0 {
			m.cmdBuf = suggs[m.cmdSuggIdx].usage
			if idx := strings.Index(m.cmdBuf, " <"); idx >= 0 {
				m.cmdBuf = m.cmdBuf[:idx+1]
			}
		}
	case "up", "ctrl+p":
		suggs := m.commandSuggestions()
		if len(suggs) > 0 {
			m.cmdSuggIdx = max(0, m.cmdSuggIdx-1)
		}
	case "down", "ctrl+n":
		suggs := m.commandSuggestions()
		if len(suggs) > 0 {
			m.cmdSuggIdx = min(len(suggs)-1, m.cmdSuggIdx+1)
		}
	case "backspace":
		if len(m.cmdBuf) > 0 {
			m.cmdBuf = m.cmdBuf[:len(m.cmdBuf)-1]
			m.cmdSuggIdx = 0
		}
	default:
		if len(k) == 1 && k[0] >= 32 {
			m.cmdBuf += k
			m.cmdSuggIdx = 0
		}
	}
	return nil
}

func (m *Model) executeCommand(cmd string) tea.Cmd {
	switch {
	case cmd == "q" || cmd == "quit":
		if m.player != nil {
			_ = m.player.Close()
		}
		return tea.Quit
	case cmd == "debug-logs":
		m.debugView = !m.debugView
		m.debugScroll = 0
		return nil
	case strings.HasPrefix(cmd, "save "), strings.HasPrefix(cmd, "save-playlist "):
		name := cmd
		for _, prefix := range []string{"save-playlist ", "save "} {
			name = strings.TrimPrefix(name, prefix)
		}
		name = strings.TrimSpace(name)
		if name == "" {
			m.errMsg = ":save requires a playlist name"
			m.errExpiry = time.Now().Add(3 * time.Second)
			return nil
		}
		ids := make([]string, len(m.queueTracks))
		for i, t := range m.queueTracks {
			ids[i] = views.PlaybackID(t)
		}
		return m.createPlaylistCmd(name, ids)
	}
	m.errMsg = fmt.Sprintf("unknown command: %s", cmd)
	m.errExpiry = time.Now().Add(3 * time.Second)
	return nil
}

func (m *Model) createPlaylistCmd(name string, ids []string) tea.Cmd {
	m.appendLog(fmt.Sprintf("[playlist] creating %q with %d tracks", name, len(ids)))
	return func() tea.Msg {
		_, err := m.provider.CreatePlaylist(context.Background(), name, ids)
		if err != nil {
			return errMsg{fmt.Errorf("save-playlist: %w", err)}
		}
		return playlistCreatedMsg{name: name}
	}
}

func (m *Model) handleNormalKey(msg tea.KeyMsg, k string) tea.Cmd {
	// When debug log is open, j/k/G scroll it; esc closes it.
	if m.debugView {
		switch k {
		case "j", "down":
			if m.debugScroll > 0 {
				m.debugScroll--
			}
			return nil
		case "k", "up":
			m.debugScroll++
			return nil
		case "G":
			m.debugScroll = 0
			return nil
		case "esc":
			m.debugView = false
			return nil
		}
	}

	// When vibe panel has text-input focus, route all keys to it.
	if m.vibe.IsFocused() {
		return m.vibe.Update(msg)
	}

	// Panel nav keys toggle their panel (always checked first).
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

	// ':' opens command mode from anywhere — checked before any panel handler.
	if k == ":" {
		m.mode = modeCommand
		m.cmdBuf = ""
		return nil
	}

	// Queue panel keys take priority over global controls when queue is active.
	if m.activePanel >= 0 && m.panels[m.activePanel] == m.queue {
		if k == "esc" {
			m.activePanel = -1
			m.lastKey = ""
			return nil
		}
		switch k {
		case "enter":
			if idx, t := m.queue.SelectedTrack(); t != nil {
				ids := m.queueIDs[idx:]
				m.queueTracks = m.queueTracks[idx:]
				m.queueIDs = ids
				m.queue.SetTracks(m.queueTracks)
				m.appendLog(fmt.Sprintf("[queue] playing from position %d: %s — %s", idx+1, t.Artist, t.Title))
				return m.playerCmd(func() error { return m.player.SetQueue(ids) })
			}
		case "d":
			if idx, t := m.queue.SelectedTrack(); idx >= 0 {
				title := ""
				if t != nil {
					title = t.Artist + " — " + t.Title
				}
				m.queueTracks = append(m.queueTracks[:idx], m.queueTracks[idx+1:]...)
				m.queueIDs = append(m.queueIDs[:idx], m.queueIDs[idx+1:]...)
				m.queue.SetTracks(m.queueTracks)
				m.appendLog(fmt.Sprintf("[queue] removed #%d: %s", idx+1, title))
				i := idx
				return m.playerCmd(func() error { return m.player.RemoveFromQueue(i) })
			}
		case "K":
			if idx, _ := m.queue.SelectedTrack(); idx > 0 {
				m.queueTracks[idx-1], m.queueTracks[idx] = m.queueTracks[idx], m.queueTracks[idx-1]
				m.queueIDs[idx-1], m.queueIDs[idx] = m.queueIDs[idx], m.queueIDs[idx-1]
				m.queue.SetTracks(m.queueTracks)
				m.appendLog(fmt.Sprintf("[queue] moved #%d up", idx+1))
				from, to := idx, idx-1
				return m.playerCmd(func() error { return m.player.MoveInQueue(from, to) })
			}
		case "J":
			if idx, _ := m.queue.SelectedTrack(); idx >= 0 && idx < len(m.queueTracks)-1 {
				m.queueTracks[idx], m.queueTracks[idx+1] = m.queueTracks[idx+1], m.queueTracks[idx]
				m.queueIDs[idx], m.queueIDs[idx+1] = m.queueIDs[idx+1], m.queueIDs[idx]
				m.queue.SetTracks(m.queueTracks)
				m.appendLog(fmt.Sprintf("[queue] moved #%d down", idx+1))
				from, to := idx, idx+1
				return m.playerCmd(func() error { return m.player.MoveInQueue(from, to) })
			}
		case "c":
			m.appendLog("[queue] cleared")
			m.queueTracks = nil
			m.queueIDs = nil
			m.queue.SetTracks(nil)
			return m.playerCmd(func() error { return m.player.ClearQueue() })
		case "s":
			m.mode = modeCommand
			m.cmdBuf = "save "
			return nil
		default:
			return m.queue.Update(msg)
		}
		return nil
	}

	// Player control keys always work, even when other panels are active.
	switch k {
	case " ":
		return m.togglePlayPause()

	case "n":
		m.lastKey = ""
		m.appendLog("[player] next")
		return m.playerCmd(func() error { return m.player.Next() })

	case "p":
		m.lastKey = ""
		m.appendLog("[player] previous")
		return m.playerCmd(func() error { return m.player.Previous() })

	case "+", "=":
		m.lastKey = ""
		if m.discovery.enabled {
			// Adjust discovery similarity toward "more similar".
			m.discovery.similarity = min(1.0, m.discovery.similarity+discoverySimilarityStep)
			m.syncDiscoveryView()
			m.appendLog(fmt.Sprintf("[discovery] similarity → %.0f%%", m.discovery.similarity*100))
			return nil
		}
		return m.adjustVolume(0.05)

	case "-":
		m.lastKey = ""
		if m.discovery.enabled {
			// Adjust discovery similarity toward "more different".
			m.discovery.similarity = max(0.0, m.discovery.similarity-discoverySimilarityStep)
			m.syncDiscoveryView()
			m.appendLog(fmt.Sprintf("[discovery] similarity → %.0f%%", m.discovery.similarity*100))
			return nil
		}
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
		m.appendLog(fmt.Sprintf("[player] repeat → %d", next))
		return m.playerCmd(func() error { return m.player.SetRepeat(next) })

	case "s":
		m.lastKey = ""
		on := !m.playerState.ShuffleMode
		m.playerState.ShuffleMode = on
		m.appendLog(fmt.Sprintf("[player] shuffle → %v", on))
		return m.playerCmd(func() error { return m.player.SetShuffle(on) })

	case "f":
		m.lastKey = ""
		if m.playerState.Track != nil {
			id := m.playerState.Track.ID
			m.favorites[id] = !m.favorites[id]
			loved := m.favorites[id]
			m.appendLog(fmt.Sprintf("[fav] %s → %v", m.playerState.Track.Title, loved))
			// Sync to Apple Music asynchronously.
			t := m.playerState.Track
			return m.loveSongCmd(t, loved)
		}

	case "d":
		m.lastKey = ""
		if m.discovery.enabled {
			// Toggle off.
			m.discovery.enabled = false
			m.discovery.seed = nil
			m.discovery.refilling = false
			m.discovery.triggeredForID = ""
			m.syncDiscoveryView()
			m.appendLog("[discovery] stopped")
		} else if m.playerState.Track != nil {
			// Activate with current song as seed; default similarity = 0.7.
			m.discovery.enabled = true
			m.discovery.seed = m.playerState.Track
			m.discovery.similarity = 0.7
			m.discovery.refilling = false
			m.discovery.triggeredForID = ""
			m.syncDiscoveryView()
			m.appendLog(fmt.Sprintf("[discovery] started from %q (similarity %.0f%%)",
				m.playerState.Track.Title, m.discovery.similarity*100))
		}
		return nil

	case "v":
		m.lastKey = ""
		m.vibe.Focus()
		return nil
	}

	// Forward remaining keys to other active panels (e.g. library).
	if m.activePanel >= 0 {
		if k == "esc" {
			m.activePanel = -1
			m.lastKey = ""
			return nil
		}
		return m.panels[m.activePanel].Update(msg)
	}

	// Keys that only work when no panel is covering the content area.
	switch k {
	case "/":
		m.mode = modeSearch
		m.searchQuery = ""
		m.search.SetState(nil, false, nil)

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

// runVibeSearch uses the KeywordAgent to interpret the user's vibe description.
// It fires multiple parallel searches using different query variants from the
// agent, merges results, deduplicates, shuffles, and returns up to 15 tracks.
func (m *Model) runVibeSearch(query string) tea.Cmd {
	agent := &vibe.KeywordAgent{}
	v := agent.Parse(query)
	// Get diverse query variants. Pick up to 3 randomly (already shuffled by the agent).
	allQueries := agent.ToSearchQueries(v)
	const maxQueries = 3
	if len(allQueries) > maxQueries {
		allQueries = allQueries[:maxQueries]
	}
	if len(allQueries) == 0 {
		allQueries = []string{query}
	}
	m.appendLog(fmt.Sprintf("[vibe] %q → queries: %v", query, allQueries))
	prov := m.provider

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		type searchOut struct {
			tracks []provider.Track
			err    error
		}
		chs := make([]chan searchOut, len(allQueries))
		for i, q := range allQueries {
			ch := make(chan searchOut, 1)
			chs[i] = ch
			go func(term string, out chan searchOut) {
				res, err := prov.Search(ctx, term)
				if err != nil || res == nil {
					out <- searchOut{err: err}
					return
				}
				out <- searchOut{tracks: res.Tracks}
			}(q, ch)
		}

		// Merge results and deduplicate by (artist, title).
		seen := map[string]bool{}
		var merged []provider.Track
		for _, ch := range chs {
			r := <-ch
			for _, t := range r.tracks {
				key := strings.ToLower(t.Artist + "||" + t.Title)
				if !seen[key] {
					seen[key] = true
					merged = append(merged, t)
				}
			}
		}

		if len(merged) == 0 {
			// Fallback to raw input.
			res, err := prov.Search(ctx, query)
			if err != nil || res == nil || len(res.Tracks) == 0 {
				return vibeResultMsg{query: query, err: fmt.Errorf("no results for %q", query)}
			}
			merged = res.Tracks
		}

		rand.Shuffle(len(merged), func(i, j int) { //nolint:gosec // music shuffle
			merged[i], merged[j] = merged[j], merged[i]
		})
		const cap = 15
		if len(merged) > cap {
			merged = merged[:cap]
		}
		return vibeResultMsg{query: query, tracks: merged}
	}
}

// loveSongCmd calls provider.LoveSong asynchronously and returns a loveSongMsg.
func (m *Model) loveSongCmd(t *provider.Track, loved bool) tea.Cmd {
	if m.provider == nil || t == nil {
		return nil
	}
	catalogID := t.CatalogID
	if catalogID == "" {
		catalogID = t.ID
	}
	title := t.Title
	prov := m.provider
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := prov.LoveSong(ctx, catalogID, loved)
		return loveSongMsg{title: title, loved: loved, err: err}
	}
}

// checkSongRatingCmd checks whether the track is already Loved on Apple Music
// and returns a songRatingMsg so the model can update m.favorites accordingly.
func (m *Model) checkSongRatingCmd(t *provider.Track) tea.Cmd {
	if m.provider == nil || t == nil {
		return nil
	}
	catalogID := t.CatalogID
	if catalogID == "" {
		catalogID = t.ID
	}
	trackID := t.ID
	prov := m.provider
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		loved, _ := prov.GetSongRating(ctx, catalogID)
		return songRatingMsg{trackID: trackID, loved: loved}
	}
}

// syncDiscoveryView pushes the current discovery state into the vibe view.
func (m *Model) syncDiscoveryView() {
	info := views.DiscoveryInfo{}
	if m.discovery.enabled && m.discovery.seed != nil {
		info.Active = true
		info.SeedArtist = m.discovery.seed.Artist
		info.SeedTitle = m.discovery.seed.Title
		info.Similarity = m.discovery.similarity
		info.Refilling = m.discovery.refilling
	}
	m.vibe.SetDiscovery(info)
}

// runDiscoverySearch builds search queries from the seed track and similarity
// value, fetches results in parallel, and returns a vibeResultMsg tagged as a
// discovery refill so the model knows not to update the vibe panel state.
func (m *Model) runDiscoverySearch() tea.Cmd {
	if !m.discovery.enabled || m.discovery.seed == nil {
		return nil
	}
	seed := m.discovery.seed
	similarity := m.discovery.similarity
	prov := m.provider
	queries := discoveryQueries(seed, similarity)
	m.appendLog(fmt.Sprintf("[discovery] queries (sim=%.0f%%): %v", similarity*100, queries))

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		type out struct{ tracks []provider.Track }
		chs := make([]chan out, len(queries))
		for i, q := range queries {
			ch := make(chan out, 1)
			chs[i] = ch
			go func(term string, c chan out) {
				res, err := prov.Search(ctx, term)
				if err != nil || res == nil {
					c <- out{}
					return
				}
				c <- out{tracks: res.Tracks}
			}(q, ch)
		}

		seen := map[string]bool{}
		var merged []provider.Track
		for _, ch := range chs {
			r := <-ch
			for _, t := range r.tracks {
				key := strings.ToLower(t.Artist + "||" + t.Title)
				if !seen[key] {
					seen[key] = true
					merged = append(merged, t)
				}
			}
		}

		rand.Shuffle(len(merged), func(i, j int) { //nolint:gosec
			merged[i], merged[j] = merged[j], merged[i]
		})
		const refillCap = 1
		if len(merged) > refillCap {
			merged = merged[:refillCap]
		}
		if len(merged) == 0 {
			return vibeResultMsg{discovery: true, err: fmt.Errorf("no results")}
		}
		return vibeResultMsg{discovery: true, tracks: merged}
	}
}

// discoveryQueries returns search terms based on seed + similarity.
// similarity 1.0 = same artist; 0.0 = completely random genre exploration.
func discoveryQueries(seed *provider.Track, similarity float64) []string {
	// Determine the seed's primary genre.
	genre := "indie"
	if len(seed.Genres) > 0 {
		genre = seed.Genres[0]
	}

	explorationGenres := []string{
		"electronic", "indie pop", "r&b", "jazz", "folk", "hip-hop",
		"ambient", "soul", "funk", "alternative", "dream pop", "lofi",
	}

	switch {
	case similarity >= 0.85:
		return []string{seed.Artist, seed.Artist + " similar artists"}
	case similarity >= 0.65:
		return []string{seed.Artist, genre}
	case similarity >= 0.45:
		return []string{genre, genre + " playlist"}
	case similarity >= 0.20:
		// Mix seed genre with a random exploration genre.
		other := explorationGenres[rand.Intn(len(explorationGenres))] //nolint:gosec
		return []string{genre, other}
	default:
		// Pure discovery: two random unrelated genres.
		i := rand.Intn(len(explorationGenres))     //nolint:gosec
		j := rand.Intn(len(explorationGenres) - 1) //nolint:gosec
		if j >= i {
			j++
		}
		return []string{explorationGenres[i], explorationGenres[j]}
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
	return m.renderBoxLayout()
}

// renderBoxLayout renders the full boxed UI.
//
//	┌─────────────────────────────────────┐
//	│ ʕ•ᴥ•ʔ vibez ♪               ♪ 72% │
//	├─────────────────────────────────────┤
//	│  Now Playing                        │  (12 lines)
//	│  …progress bar, controls, bear…     │
//	├──────────────────┬──────────────────┤
//	│ Queue            │ Vibe             │  (panelH lines)
//	├──────────────────┴──────────────────┤
//	│ ʕ•ᴥ•ʔ > / search  n next  :q quit  │
//	└─────────────────────────────────────┘
func (m *Model) renderBoxLayout() string {
	inner := m.width - 2 // visual width between the │ border chars
	npH := 12            // now playing section height (fixed)
	panelH := m.panelHeight()

	splitW := inner / 2          // left column inner width (includes padding)
	rightW := inner - splitW - 1 // right column inner width (-1 for │ divider)

	libraryActive := m.activePanel >= 0 && m.panels[m.activePanel] == m.library
	queueActive := m.activePanel >= 0 && m.panels[m.activePanel] == m.queue
	fullWidth := libraryActive || queueActive || m.mode == modeSearch || m.mode == modeCommand || m.debugView

	var sb strings.Builder

	// ── Top border ──
	sb.WriteString("┌" + strings.Repeat("─", inner) + "┐\n")

	// ── Header ──
	sb.WriteString(m.renderBoxHeader(inner) + "\n")

	// ── Header divider ──
	sb.WriteString("├" + strings.Repeat("─", inner) + "┤\n")

	// ── Now Playing (12 lines) ──
	for _, line := range m.nowPlayingLines(inner-2, npH) {
		sb.WriteString("│ " + padRight(line, inner-2) + " │\n")
	}

	// ── Split or full divider ──
	if fullWidth {
		sb.WriteString("├" + strings.Repeat("─", inner) + "┤\n")
	} else {
		sb.WriteString("├" + strings.Repeat("─", splitW) + "┬" + strings.Repeat("─", rightW) + "┤\n")
	}

	// ── Panel content ──
	switch {
	case m.debugView:
		for _, line := range m.debugLogLines(inner-2, panelH) {
			sb.WriteString("│ " + padRight(line, inner-2) + " │\n")
		}
	case m.mode == modeSearch:
		for _, line := range m.searchLines(inner-2, panelH) {
			sb.WriteString("│ " + padRight(line, inner-2) + " │\n")
		}
	case m.mode == modeCommand:
		for _, line := range m.commandLines(inner-2, panelH) {
			sb.WriteString("│ " + padRight(line, inner-2) + " │\n")
		}
	case libraryActive:
		for _, line := range toLines(m.library.View(), panelH) {
			sb.WriteString("│ " + padRight(line, inner-2) + " │\n")
		}
	case queueActive:
		m.queue.m.SetSize(inner-2, panelH)
		for _, line := range toLines(m.queue.View(), panelH) {
			sb.WriteString("│ " + padRight(line, inner-2) + " │\n")
		}
	default:
		qLines := m.queuePanelLines(splitW-2, panelH)
		vLines := m.vibe.Lines(rightW-2, panelH, m.glowStep)
		for i := range panelH {
			left := safeIdx(qLines, i)
			right := safeIdx(vLines, i)
			sb.WriteString("│ " + padRight(left, splitW-2) + " │ " + padRight(right, rightW-2) + " │\n")
		}
	}

	// ── Join or full divider ──
	if fullWidth {
		sb.WriteString("├" + strings.Repeat("─", inner) + "┤\n")
	} else {
		sb.WriteString("├" + strings.Repeat("─", splitW) + "┴" + strings.Repeat("─", rightW) + "┤\n")
	}

	// ── Status bar (two lines: context/mode + playback) ──
	sb.WriteString("│ " + padRight(m.statusNavContent(inner-2), inner-2) + " │\n")
	sb.WriteString("│ " + padRight(m.statusPlayContent(inner-2), inner-2) + " │\n")

	// ── Bottom border ──
	sb.WriteString("└" + strings.Repeat("─", inner) + "┘")

	return sb.String()
}

func (m *Model) renderIntro() string {
	if m.introStep < 0 || m.introStep >= len(introFrames) {
		return ""
	}

	frame := introFrames[m.introStep]
	glowIdx := m.glowStep % len(styles.GlowPalette)

	var logo strings.Builder
	for _, r := range frame {
		logo.WriteString(lipgloss.NewStyle().
			Foreground(styles.GlowPalette[glowIdx]).
			Render(string(r)))
	}

	var subtitle string
	if m.introStep >= len("♪ vibez") {
		statusText := m.initStatus
		if statusText == "" {
			statusText = "connecting…"
		}
		subtitle = "\n" + centerStr(
			lipgloss.NewStyle().Foreground(styles.ColorMuted).Render(statusText),
			m.width,
		)
	}

	topPad := max(0, (m.height-3)/2)
	return strings.Repeat("\n", topPad) +
		centerStr(logo.String(), m.width) +
		subtitle
}

// renderBoxHeader builds the header line including the border chars.
func (m *Model) renderBoxHeader(inner int) string {
	bear := views.BearExpr(m.glowStep, m.playerState.Playing)
	title := views.RenderGlowTitle("vibez ♪", m.glowStep)

	vol := int(m.playerState.Volume * 100)
	volStr := styles.QueueItemMuted.Render(fmt.Sprintf("♪ %d%%", vol))

	rightStr := volStr
	if m.memStats != "" {
		rightStr = styles.QueueItemMuted.Render(m.memStats) + "  " + volStr
	}

	bearW := lipgloss.Width(bear)
	titleW := lipgloss.Width(title)
	rightW := lipgloss.Width(rightStr)
	contentW := inner - 2 // for " " padding on each side

	// Place title at the horizontal centre of the header.
	titleStart := max(bearW+1, (contentW-titleW)/2)
	leftPad := titleStart - bearW
	rightPad := max(1, contentW-bearW-leftPad-titleW-rightW)

	return "│ " + bear + strings.Repeat(" ", leftPad) + title + strings.Repeat(" ", rightPad) + rightStr + " │"
}

// nowPlayingLines returns exactly h lines for the Now Playing section.
func (m *Model) nowPlayingLines(contentW, h int) []string {
	muted := styles.QueueItemMuted

	t := m.playerState.Track
	if t == nil {
		lines := make([]string, h)
		mid := h / 2
		lines[mid] = centerStr(muted.Render("silence is not a vibe"), contentW)
		if m.errMsg != "" {
			lines[max(0, mid-2)] = centerStr(styles.ErrorStyle.Render("⚠  "+m.errMsg), contentW)
		}
		return lines
	}

	// Title: bright lavender while playing, softer gray while paused.
	var titleStr string
	if m.playerState.Playing || m.playerState.Loading {
		titleStr = styles.NowPlayingTitlePlaying.Render(t.Title)
	} else {
		titleStr = styles.NowPlayingTitle.Render(t.Title)
	}

	// "Artist — Title" — centred
	trackLine := centerStr(
		styles.NowPlayingArtist.Render(t.Artist)+muted.Render(" — ")+titleStr,
		contentW,
	)

	// "Album • elapsed / total" — centred
	elapsed := views.FormatDuration(m.playerState.Position)
	total := views.FormatDuration(t.Duration)
	albumLine := centerStr(
		styles.NowPlayingAlbum.Render(t.Album+" • ")+styles.TimeStyle.Render(elapsed+" / "+total),
		contentW,
	)

	// Progress bar — centred, slightly narrower than full width
	barW := max(10, contentW-8)
	progressLine := centerStr(views.RenderProgressBar(m.playerState.Position, t.Duration, barW), contentW)

	// Controls: ↺  ⇄  ▶/⏸  ♡/♥
	var playIcon string
	var playStyle lipgloss.Style
	switch {
	case m.playerState.Loading:
		spinnerFrames := [10]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		playIcon = spinnerFrames[m.glowStep%10]
		playStyle = styles.ControlActive
	case m.playerState.Playing:
		playIcon = "⏸"
		playStyle = styles.ControlActive
	default:
		playIcon = "▶"
		playStyle = styles.Paused
	}

	repeatIcon, repeatStyle := "↺", muted
	switch m.playerState.RepeatMode {
	case player.RepeatModeAll:
		repeatIcon, repeatStyle = "↺", styles.ControlActive
	case player.RepeatModeOne:
		repeatIcon, repeatStyle = "↻", styles.ControlActive
	}
	shuffleStyle := muted
	if m.playerState.ShuffleMode {
		shuffleStyle = styles.ControlActive
	}

	heartIcon, heartStyle := "♡", muted
	if m.favorites[t.ID] {
		heartIcon, heartStyle = "♥", styles.FavoriteActive
	}

	controls := centerStr(
		repeatStyle.Render(repeatIcon)+"   "+
			shuffleStyle.Render("⇄")+"   "+
			playStyle.Render(playIcon)+"   "+
			heartStyle.Render(heartIcon),
		contentW,
	)

	// Error line — centred, or blank.
	errLine := ""
	if m.errMsg != "" {
		const prefix = "⚠  "
		errText := truncateStr(m.errMsg, max(10, contentW-len([]rune(prefix))))
		errLine = centerStr(styles.ErrorStyle.Render(prefix+errText), contentW)
	}

	lines := []string{
		"",
		centerStr(styles.NowPlayingLabel.Render("Now Playing"), contentW),
		centerStr(muted.Render(strings.Repeat("─", 11)), contentW),
		trackLine,
		albumLine,
		"",
		progressLine,
		"",
		"",
		controls,
		"",
		errLine,
	}

	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

// queuePanelLines returns the Queue panel lines for the left split.
func (m *Model) queuePanelLines(w, h int) []string {
	header := styles.Header.Render("Queue")
	sep := styles.QueueItemMuted.Render(strings.Repeat("─", 5))

	currentTitle := ""
	if m.playerState.Track != nil {
		currentTitle = m.playerState.Track.Title
	}

	tracks := m.queue.m.Tracks()
	var trackLines []string
	for _, t := range tracks {
		label := t.Artist + " — " + t.Title
		if t.Title == currentTitle {
			prefix := styles.ControlActive.Render("▶ ")
			line := truncateStr(label, w-3)
			trackLines = append(trackLines, prefix+styles.ControlActive.Render(line))
		} else {
			line := truncateStr(label, w-3)
			trackLines = append(trackLines, "  "+styles.QueueItem.Render(line))
		}
	}
	if len(trackLines) == 0 {
		trackLines = []string{styles.QueueItemMuted.Render("  Queue is empty")}
	}

	result := append([]string{header, sep}, trackLines...)
	for len(result) < h {
		result = append(result, "")
	}
	return result[:h]
}

// searchLines renders the search popup inline (full-width in the split area).
func (m *Model) searchLines(contentW, h int) []string {
	accent := lipgloss.NewStyle().Foreground(styles.ColorAccent)
	muted := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	cursor := accent.Render("█")

	inputLine := accent.Render("/") + "  " +
		lipgloss.NewStyle().Foreground(styles.ColorFg).Render(m.searchQuery) + cursor
	sep := muted.Render(strings.Repeat("─", contentW))

	// Reserve input(1) + sep(1) + footerSep(1) + footer(1) = 4 lines.
	listH := max(1, h-4)
	m.search.SetSize(contentW, listH)
	listView := m.search.View()
	if listView == "" && !m.search.Loading() && m.searchQuery != "" {
		listView = "  " + muted.Render("no results")
	}

	listLines := toLines(listView, listH)
	footerSep := muted.Render(strings.Repeat("─", contentW))
	footer := "  " + accent.Render("Enter") + muted.Render(" play now") +
		"  ·  " + accent.Render("Tab") + muted.Render(" add to queue") +
		"  ·  " + accent.Render("Esc") + muted.Render(" close")

	result := append([]string{inputLine, sep}, listLines...)
	result = append(result, footerSep, footer)
	for len(result) < h {
		result = append(result, "")
	}
	return result[:h]
}

// statusNavContent is the top status line: mode chip + context-aware shortcuts.
func (m *Model) statusNavContent(_ int) string {
	muted := styles.QueueItemMuted
	accent := styles.KeyName
	dot := muted.Render("  ·  ")

	switch m.mode {
	case modeSearch:
		return styles.ModeSearch.Render("SEARCH") + "  " +
			accent.Render("/") + styles.Header.Render(m.searchQuery) + accent.Render("_")
	case modeCommand:
		return styles.ModeCommand.Render("CMD") + "  " +
			muted.Render(":") + m.cmdBuf + accent.Render("_") +
			muted.Render("  Tab complete · ↑/↓ navigate · Esc cancel")
	default:
		var parts []string
		switch {
		case m.debugView:
			parts = []string{
				styles.ModeNormal.Render("DEBUG"),
				accent.Render("j/k") + muted.Render(" scroll"),
				accent.Render("esc") + muted.Render(" close"),
			}
		case m.vibe.IsFocused():
			parts = []string{
				styles.ModeNormal.Render("VIBE"),
				accent.Render("Enter") + muted.Render(" search"),
				accent.Render("esc") + muted.Render(" cancel"),
			}
		case m.activePanel >= 0 && m.panels[m.activePanel] == m.queue:
			parts = []string{
				styles.ModeNormal.Render("QUEUE"),
				accent.Render("Enter") + muted.Render(" play"),
				accent.Render("d") + muted.Render(" remove"),
				accent.Render("K/J") + muted.Render(" move up/down"),
				accent.Render("c") + muted.Render(" clear"),
				accent.Render("s") + muted.Render(" :save"),
				accent.Render("esc") + muted.Render(" close"),
			}
		case m.activePanel >= 0 && m.panels[m.activePanel] == m.library:
			parts = []string{
				styles.ModeNormal.Render("LIBRARY"),
				accent.Render("Enter") + muted.Render(" play"),
				accent.Render("Tab") + muted.Render(" add to queue"),
				accent.Render("esc") + muted.Render(" close"),
			}
		default:
			parts = []string{
				styles.ModeNormal.Render("NORMAL"),
				accent.Render(":") + muted.Render(" command"),
				accent.Render("/") + muted.Render(" search"),
				accent.Render("l") + muted.Render(" library"),
				accent.Render("q") + muted.Render(" queue"),
				accent.Render("v") + muted.Render(" vibe"),
				accent.Render(":q") + muted.Render(" quit"),
			}
		}
		return strings.Join(parts, dot)
	}
}

// statusPlayContent is the bottom status line: always shows playback controls.
func (m *Model) statusPlayContent(_ int) string {
	muted := styles.QueueItemMuted
	accent := styles.KeyName
	dot := muted.Render("  ·  ")

	discoverHint := accent.Render("d") + muted.Render(" discovery")
	if m.discovery.enabled {
		discoverHint = accent.Render("d") + styles.Playing.Render(" ● discovery")
	}

	parts := []string{
		accent.Render("spc") + muted.Render(" play/pause"),
		accent.Render("n/p") + muted.Render(" next/prev"),
		accent.Render("f") + muted.Render(" fav"),
		accent.Render("s") + muted.Render(" shuffle"),
		accent.Render("r") + muted.Render(" repeat"),
		discoverHint,
	}
	return strings.Join(parts, dot)
}

// commandLines renders the command palette in the panel area when CMD mode is active.
func (m *Model) commandLines(w, h int) []string {
	muted := styles.QueueItemMuted
	accent := styles.KeyName
	header := accent.Render("Commands")
	sep := muted.Render(strings.Repeat("─", 8))

	suggs := m.commandSuggestions()
	var rows []string
	for i, c := range suggs {
		cursor := "  "
		nameStyle := styles.QueueItem
		descStyle := muted
		if i == m.cmdSuggIdx {
			cursor = styles.Playing.Render("▶ ")
			nameStyle = styles.Playing
			descStyle = styles.QueueItem
		}
		usage := nameStyle.Render(fmt.Sprintf("%-20s", c.usage))
		desc := descStyle.Render(c.description)
		rows = append(rows, cursor+usage+" "+desc)
	}
	if len(rows) == 0 {
		rows = []string{"  " + muted.Render("no matching commands")}
	}

	result := append([]string{"", header, sep, ""}, rows...)
	for len(result) < h {
		result = append(result, "")
	}
	return result[:h]
}

// panelHeight returns the number of rows available for the split panel section.
// Fixed overhead = top(1)+hdr(1)+hdrdiv(1)+np(12)+splitdiv(1)+joindiv(1)+status(2)+bottom(1) = 20.
func (m *Model) panelHeight() int {
	return max(3, m.height-20)
}

// ── Helpers ───────────────────────────────────────────────────────────────

// padRight pads s on the right with spaces to reach visual width w.
func padRight(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw >= w {
		return s
	}
	return s + strings.Repeat(" ", w-sw)
}

// toLines splits s into exactly h lines, padding/truncating as needed.
func toLines(s string, h int) []string {
	lines := strings.Split(s, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

// safeIdx returns lines[i] or "" if i is out of range.
func safeIdx(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// truncateStr truncates s to at most maxW runes, adding "…" if cut.
func truncateStr(s string, maxW int) string {
	if maxW <= 1 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxW {
		return s
	}
	return string(runes[:maxW-1]) + "…"
}

// appendLog adds a timestamped entry to the debug log, capped at 500 lines.
func (m *Model) appendLog(line string) {
	const maxLines = 500
	ts := time.Now().Format("15:04:05")
	m.debugLog = append(m.debugLog, ts+"  "+line)
	if len(m.debugLog) > maxLines {
		m.debugLog = m.debugLog[len(m.debugLog)-maxLines:]
	}
}

// debugLogLines renders the debug log as exactly h lines for the split area.
func (m *Model) debugLogLines(w, h int) []string {
	accent := styles.Header
	muted := styles.QueueItemMuted

	header := accent.Render("Debug Log") + "  " + muted.Render("esc / :debug-logs to close · k/j scroll")
	sep := muted.Render(strings.Repeat("─", 9))
	contentH := max(0, h-2)

	total := len(m.debugLog)
	scroll := min(m.debugScroll, max(0, total-contentH))
	start := max(0, total-contentH-scroll)
	end := min(total, start+contentH)

	// Maximum text width: panel width minus the 2-char indent and 1-char safety margin.
	maxW := max(1, w-3)

	truncate := func(s string) string {
		r := []rune(s)
		if len(r) > maxW {
			return string(r[:maxW-1]) + "…"
		}
		return s
	}

	lines := []string{header, sep}
	if total == 0 {
		lines = append(lines, "  "+muted.Render("no log entries yet"))
	} else {
		for _, entry := range m.debugLog[start:end] {
			clipped := truncate(entry)
			var rendered string
			switch {
			case strings.Contains(entry, "[error]"):
				rendered = styles.ErrorStyle.Render(clipped)
			case strings.Contains(entry, "[playing]"):
				rendered = styles.Playing.Render(clipped)
			case strings.Contains(entry, "[js:"):
				rendered = styles.Header.Render(clipped)
			default:
				rendered = muted.Render(clipped)
			}
			lines = append(lines, "  "+rendered)
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
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
