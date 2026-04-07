package tui

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/config"
	"github.com/simone-vibes/vibez/internal/lyrics"
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

// lyricsPanel wraps views.LyricsModel to satisfy ContentView.
type lyricsPanel struct{ m *views.LyricsModel }

func (p *lyricsPanel) NavKey() string                { return "y" }
func (p *lyricsPanel) NavLabel() string              { return "lyrics" }
func (p *lyricsPanel) SetSize(w, h int)              { p.m.SetSize(w, h) }
func (p *lyricsPanel) Update(msg tea.KeyMsg) tea.Cmd { return p.m.Update(msg) }
func (p *lyricsPanel) View() string                  { return p.m.View() }

// feedPanel wraps views.FeedModel to satisfy ContentView.
type feedPanel struct{ m *views.FeedModel }

func (p *feedPanel) NavKey() string                { return "F" }
func (p *feedPanel) NavLabel() string              { return "feed" }
func (p *feedPanel) SetSize(w, h int)              { p.m.SetSize(w, h) }
func (p *feedPanel) Update(msg tea.KeyMsg) tea.Cmd { return p.m.Update(msg) }
func (p *feedPanel) View() string                  { return p.m.View() }

// ── Messages ──────────────────────────────────────────────────────────────

type playerStateMsg player.State
type searchResultMsg struct {
	result *provider.SearchResult
	query  string
	err    error
}

// searchDebounceMsg is emitted after the debounce delay. The gen field lets
// Update discard messages that belong to earlier keystrokes.
type searchDebounceMsg struct {
	query string
	gen   int
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
type SessionExpiredMsg struct{}
type SessionRestoredMsg struct{}
type lyricsResultMsg struct {
	trackID string
	result  *lyrics.Result
	err     error
}
type feedResultMsg struct {
	groups []provider.RecommendationGroup
	err    error
}
type feedTracksMsg struct {
	item   provider.RecommendationItem
	tracks []provider.Track
	play   bool // true = replace queue & play; false = append
	err    error
}

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
// When enabled, songs are queued according to autoMode and refillCap.
// The similarity value (0=very different, 1=very similar) controls how
// adventurous the search is and is set via the metric picker (d key).
type discoveryMode struct {
	enabled        bool
	autoMode       bool // true = auto-refill on last song; false = one-shot
	refillCap      int  // songs to add per cycle
	seed           *provider.Track
	similarity     float64         // 0.0–1.0; persists across stop/start
	refilling      bool            // background search in progress
	triggeredForID string          // ID of track for which we already fired a search
	skipped        map[string]bool // IDs/keys of tracks skipped due to unavailability
	retries        int             // consecutive failed refill attempts (circuit breaker)
}

const discoveryMaxRetries = 5 // give up re-arming after this many consecutive failures

const (
	discoverySimilarityStep = 0.1
)

// ── Model ─────────────────────────────────────────────────────────────────

type Model struct {
	cfg      *config.Config
	provider provider.Provider
	player   player.Player

	width, height int

	playerState     player.State
	stateCh         <-chan player.State
	queueIDs        []string         // current playback queue (for "add to queue")
	queueTracks     []provider.Track // full track objects parallel to queueIDs
	queueMiniOffset int              // scroll offset for the mini-queue in the split view

	// Discovery mode
	discovery discoveryMode

	// Panels
	panels      []ContentView // registered content panels; add new ones in New()
	activePanel int           // index into panels; -1 = none active
	library     *libraryPanel
	queue       *queuePanel
	lyricsP     *lyricsPanel
	feedP       *feedPanel

	// Lyrics
	lyricsClient      *lyrics.Client
	lastLyricsTrackID string // ID of the track for which lyrics were last fetched

	// Vibe panel (always visible, right split)
	vibe *views.VibeModel

	// Search popup (not a panel)
	search *views.SearchModel

	// Modal state
	mode viewMode

	// Search accumulation (mode == modeSearch)
	searchQuery string
	searchGen   int // incremented on every keystroke; used to discard stale results

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

	// Mute: preMuteVol holds the volume before mute so it can be restored.
	// -1 means not muted.
	preMuteVol float64
}

func New(cfg *config.Config, prov provider.Provider, plyr player.Player, opts Options) *Model {
	m := &Model{
		cfg:          cfg,
		provider:     prov,
		player:       plyr,
		activePanel:  -1,
		memProfiling: opts.MemProfiling,
		preMuteVol:   -1,
	}
	if plyr != nil {
		m.stateCh = plyr.Subscribe()
	}
	m.library = &libraryPanel{m: views.NewLibrary(prov)}
	m.queue = &queuePanel{m: views.NewQueue()}
	m.lyricsP = &lyricsPanel{m: views.NewLyrics()}
	m.lyricsClient = lyrics.NewClient()
	m.feedP = &feedPanel{m: views.NewFeed()}
	m.vibe = views.NewVibe()
	m.search = views.NewSearch(prov)
	m.favorites = make(map[string]bool)
	m.panels = []ContentView{m.library, m.queue, m.lyricsP, m.feedP}
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
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return glowTickMsg(t) })
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
		m.lyricsP.SetSize(contentW, panelH)
		m.feedP.SetSize(contentW, panelH)

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
		if s.SkippedID != "" {
			// JS silently skipped a track (CONTENT_RESTRICTED / unavailable).
			// Record the ID in the discovery blacklist so it won't be proposed
			// again this session, purge ALL blacklisted entries from the queue
			// (there may be duplicates from earlier discovery cycles), then
			// re-arm discovery so music keeps flowing without interruption.
			skippedID := s.SkippedID
			s.SkippedID = ""
			if m.discovery.skipped == nil {
				m.discovery.skipped = make(map[string]bool)
			}
			m.discovery.skipped[skippedID] = true
			m.purgeSkippedFromQueue()
			if m.discovery.enabled && !m.discovery.refilling &&
				m.discovery.retries < discoveryMaxRetries {
				m.discovery.retries++
				m.discovery.triggeredForID = ""
				m.discovery.refilling = true
				m.syncDiscoveryView()
				cmds = append(cmds, m.runDiscoverySearch())
			} else if m.discovery.retries >= discoveryMaxRetries {
				m.appendLog("[discovery] max retries reached — giving up")
			}
		}
		if s.Track != nil && (m.playerState.Track == nil || m.playerState.Track.Title != s.Track.Title) {
			m.appendLog("[playing] " + s.Track.Artist + " — " + s.Track.Title)
			// Log playParams so we can confirm which ID path MusicKit will use.
			trackType := "catalog"
			if strings.HasPrefix(s.Track.ID, "i.") {
				trackType = "library"
			}
			pp := fmt.Sprintf("[playParams] id=%s type=%s", s.Track.ID, trackType)
			if s.Track.CatalogID != "" {
				pp += " catalogId=" + s.Track.CatalogID
			}
			m.appendLog(pp)
			// Check whether the new track is already loved on Apple Music.
			cmds = append(cmds, m.checkSongRatingCmd(s.Track))
			// Fetch lyrics for the new track.
			if id := views.PlaybackID(*s.Track); id != m.lastLyricsTrackID {
				m.lastLyricsTrackID = id
				m.lyricsP.m.SetLoading()
				cmds = append(cmds, m.fetchLyricsCmd(s.Track))
			}
			// Auto-scroll mini-queue to keep the current track visible.
			for i, t := range m.queueTracks {
				if t.Title == s.Track.Title {
					visibleRows := max(0, m.panelHeight()-2)
					if visibleRows > 0 && (i < m.queueMiniOffset || i >= m.queueMiniOffset+visibleRows) {
						m.queueMiniOffset = max(0, i-visibleRows/2)
					}
					break
				}
			}
		}
		// Always sync playback position so the current lyrics line stays highlighted.
		if s.Track != nil {
			m.lyricsP.m.SetPosition(s.Position)
		}
		// Discovery: in auto mode, fire as soon as the last track in the queue
		// starts playing. Triggering at the start of the last track gives the
		// search the maximum possible time to complete before the queue runs dry.
		// triggeredForID ensures we fire exactly once per track.
		isLastQueued := len(m.queueTracks) > 0 && s.Track != nil &&
			views.PlaybackID(*s.Track) == views.PlaybackID(m.queueTracks[len(m.queueTracks)-1])
		if m.discovery.enabled && m.discovery.autoMode && !m.discovery.refilling &&
			isLastQueued &&
			m.discovery.triggeredForID != s.Track.ID {
			m.discovery.triggeredForID = s.Track.ID
			m.discovery.refilling = true
			m.syncDiscoveryView()
			cmds = append(cmds, m.runDiscoverySearch())
		}
		m.playerState = s
		if !wasPlaying && m.playerState.Playing {
			m.appendLog("[player] playing")
			cmds = append(cmds, glowTick())
		} else if wasPlaying && !m.playerState.Playing && !m.playerState.Loading {
			m.appendLog("[player] paused")
		}
		cmds = append(cmds, waitForState(m.stateCh))

	case songRatingMsg:
		// Update favorite state to match what Apple Music reports.
		if msg.trackID != "" {
			m.favorites[msg.trackID] = msg.loved
		}

	case lyricsResultMsg:
		// Discard stale results if the user skipped to a different track.
		if msg.trackID == m.lastLyricsTrackID {
			m.lyricsP.m.SetLyrics(msg.result, msg.err)
			if msg.err != nil {
				m.appendLog(fmt.Sprintf("[lyrics] not found: %v", msg.err))
			} else {
				m.appendLog("[lyrics] loaded")
			}
		}

	case feedResultMsg:
		if msg.err != nil {
			m.feedP.m.SetError(msg.err)
			m.appendLog(fmt.Sprintf("[feed] error: %v", msg.err))
		} else {
			m.feedP.m.SetRecommendations(msg.groups)
			m.appendLog(fmt.Sprintf("[feed] loaded %d groups", len(msg.groups)))
		}

	case feedTracksMsg:
		if msg.err != nil {
			m.errMsg = fmt.Sprintf("feed: %v", msg.err)
			m.errExpiry = time.Now().Add(4 * time.Second)
			m.appendLog(fmt.Sprintf("[feed] track fetch error: %v", msg.err))
			break
		}
		if len(msg.tracks) == 0 {
			m.errMsg = "feed: no playable tracks"
			m.errExpiry = time.Now().Add(3 * time.Second)
			break
		}
		ids := make([]string, len(msg.tracks))
		for i, t := range msg.tracks {
			ids[i] = views.PlaybackID(t)
		}
		if msg.play {
			m.queueTracks = msg.tracks
			m.queueIDs = ids
			m.queue.SetTracks(m.queueTracks)
			m.appendLog(fmt.Sprintf("[feed] playing %q (%d tracks)", msg.item.Title, len(ids)))
			return m, m.playerCmd(func() error { return m.player.SetQueue(ids) })
		}
		m.queueTracks = append(m.queueTracks, msg.tracks...)
		m.queueIDs = append(m.queueIDs, ids...)
		m.queue.SetTracks(m.queueTracks)
		m.appendLog(fmt.Sprintf("[feed] queued %q (%d tracks)", msg.item.Title, len(ids)))
		return m, m.playerCmd(func() error { return m.player.AppendQueue(ids) })

	case views.VibeQueryMsg:
		// User submitted a vibe description — start async provider search.
		m.vibe.SetSearching()
		cmds = append(cmds, m.runVibeSearch(msg.Query))

	case views.DiscoveryMetricSelectedMsg:
		// User confirmed a metric in the picker — store it and immediately start
		// discovery in continuous auto mode (mirrors the old single-key behaviour).
		m.discovery.similarity = msg.Similarity
		m.appendLog(fmt.Sprintf("[discovery] metric set: %.0f%% similarity", msg.Similarity*100))
		if m.playerState.Track != nil {
			cmds = append(cmds, m.startDiscovery(true, 1))
		}

	case vibeResultMsg:
		if msg.err != nil {
			if !msg.discovery {
				m.vibe.SetResult(0, msg.err)
			}
			m.appendLog(fmt.Sprintf("[vibe] search error: %v", msg.err))
			if msg.discovery {
				m.discovery.refilling = false
				m.syncDiscoveryView()
			}
			break
		}
		// For discovery results, drop any track that arrived in the blacklist
		// while the search was in flight (race between search goroutine and a
		// concurrent goSkipped notification), and also drop any track already
		// present in the queue (dedup by ID and artist||title).
		tracks := msg.tracks
		if msg.discovery {
			filtered := tracks[:0]
			for _, t := range tracks {
				id := views.PlaybackID(t)
				key := strings.ToLower(t.Artist + "||" + t.Title)
				if m.discovery.skipped[id] {
					continue
				}
				dup := slices.Contains(m.queueIDs, id)
				if !dup {
					for _, qt := range m.queueTracks {
						if strings.ToLower(qt.Artist+"||"+qt.Title) == key {
							dup = true
							break
						}
					}
				}
				if !dup {
					filtered = append(filtered, t)
				}
			}
			tracks = filtered
		}
		if len(tracks) == 0 {
			if msg.discovery && m.discovery.retries < discoveryMaxRetries {
				m.discovery.retries++
				m.discovery.refilling = true
				m.syncDiscoveryView()
				cmds = append(cmds, m.runDiscoverySearch())
			} else {
				if msg.discovery {
					m.discovery.refilling = false
					m.syncDiscoveryView()
				} else {
					m.vibe.SetResult(0, fmt.Errorf("no streamable results"))
				}
			}
			break
		}
		ids := make([]string, len(tracks))
		for i, t := range tracks {
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
		m.queueTracks = append(m.queueTracks, tracks...)
		m.queueIDs = append(m.queueIDs, ids...)
		m.queue.SetTracks(m.queueTracks)
		if msg.discovery {
			m.discovery.refilling = false
			m.discovery.retries = 0 // successful refill — reset circuit breaker
			if !m.discovery.autoMode {
				// One-shot: disable discovery after this successful refill.
				m.discovery.enabled = false
				m.discovery.seed = nil
			}
			m.syncDiscoveryView()
			m.appendLog(fmt.Sprintf("[discovery] refilled %d tracks", len(tracks)))
		} else {
			m.vibe.SetResult(len(tracks), nil)
			m.appendLog(fmt.Sprintf("[vibe] added %d tracks for %q", len(tracks), msg.query))
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

	case searchDebounceMsg:
		// Drop stale debounce ticks — only the latest keystroke wins.
		if msg.gen != m.searchGen || m.provider == nil {
			return m, nil
		}
		m.appendLog(fmt.Sprintf("[search] %q…", msg.query))
		prov := m.provider
		query := msg.query
		return m, func() tea.Msg {
			result, err := prov.Search(context.Background(), query)
			return searchResultMsg{result: result, query: query, err: err}
		}

	case searchResultMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("[search] error: %v", msg.err))
		} else if msg.result != nil {
			m.appendLog(fmt.Sprintf("[search] %d track(s), %d album(s), %d playlist(s)",
				len(msg.result.Tracks), len(msg.result.Albums), len(msg.result.Playlists)))
		}
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
		}
		if len(msg.Tracks) > 0 {
			m.queueTracks = msg.Tracks
		} else if msg.Track != nil {
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

	case SessionExpiredMsg:
		m.errMsg = "Session expired — opening browser to re-authenticate…"
		m.errExpiry = time.Now().Add(365 * 24 * time.Hour) // persists until restored

	case SessionRestoredMsg:
		m.errMsg = "✓ Re-authenticated with Apple Music"
		m.errExpiry = time.Now().Add(5 * time.Second)

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
	{"discover", "discover <n>|auto", "Queue n discovered songs now, or auto-discover indefinitely"},
	{"vol", "vol <0-100|+n|-n>", "Set, raise, or lower volume (e.g. vol 80, vol +10, vol -5)"},
	{"mute", "mute", "Toggle mute"},
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
			if m.cmdSuggIdx >= len(suggs) {
				m.cmdSuggIdx = len(suggs) - 1
			}
			m.cmdBuf = suggs[m.cmdSuggIdx].usage
			if idx := strings.Index(m.cmdBuf, " <"); idx >= 0 {
				m.cmdBuf = m.cmdBuf[:idx+1]
			}
		}
	case "up", "ctrl+p":
		suggs := m.commandSuggestions()
		if len(suggs) > 0 {
			m.cmdSuggIdx = max(0, min(len(suggs)-1, m.cmdSuggIdx)-1)
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
	case strings.HasPrefix(cmd, "discover"):
		arg := strings.TrimSpace(strings.TrimPrefix(cmd, "discover"))
		if arg == "" || arg == "auto" {
			return m.startDiscovery(true, 1)
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n <= 0 {
			m.errMsg = ":discover requires a positive number or 'auto'"
			m.errExpiry = time.Now().Add(3 * time.Second)
			return nil
		}
		return m.startDiscovery(false, n)
	case cmd == "mute":
		if m.preMuteVol >= 0 {
			// Currently muted — restore previous volume.
			vol := m.preMuteVol
			m.preMuteVol = -1
			m.appendLog(fmt.Sprintf("[vol] unmuted → %.0f%%", vol*100))
			return m.playerCmd(func() error { return m.player.SetVolume(vol) })
		}
		// Mute: save current volume and set to 0.
		m.preMuteVol = m.playerState.Volume
		m.appendLog("[vol] muted")
		return m.playerCmd(func() error { return m.player.SetVolume(0) })

	case strings.HasPrefix(cmd, "vol"):
		arg := strings.TrimSpace(strings.TrimPrefix(cmd, "vol"))
		if arg == "" {
			m.errMsg = fmt.Sprintf("volume: %d%%", int(m.playerState.Volume*100))
			m.errExpiry = time.Now().Add(3 * time.Second)
			return nil
		}
		var newVol float64
		switch {
		case strings.HasPrefix(arg, "+"):
			delta, err := strconv.Atoi(arg[1:])
			if err != nil || delta < 0 {
				m.errMsg = ":vol +n requires a positive number"
				m.errExpiry = time.Now().Add(3 * time.Second)
				return nil
			}
			newVol = clamp(m.playerState.Volume+float64(delta)/100, 0, 1)
		case strings.HasPrefix(arg, "-"):
			delta, err := strconv.Atoi(arg[1:])
			if err != nil || delta < 0 {
				m.errMsg = ":vol -n requires a positive number"
				m.errExpiry = time.Now().Add(3 * time.Second)
				return nil
			}
			newVol = clamp(m.playerState.Volume-float64(delta)/100, 0, 1)
		default:
			n, err := strconv.Atoi(arg)
			if err != nil || n < 0 || n > 100 {
				m.errMsg = ":vol requires 0-100, +n, or -n"
				m.errExpiry = time.Now().Add(3 * time.Second)
				return nil
			}
			newVol = float64(n) / 100
		}
		m.preMuteVol = -1 // clear mute state on explicit vol change
		m.appendLog(fmt.Sprintf("[vol] → %.0f%%", newVol*100))
		return m.playerCmd(func() error { return m.player.SetVolume(newVol) })

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

// fetchLyricsCmd fetches lyrics for t from LRCLIB asynchronously.
func (m *Model) fetchLyricsCmd(t *provider.Track) tea.Cmd {
	trackID := views.PlaybackID(*t)
	artist, title, album, dur := t.Artist, t.Title, t.Album, t.Duration
	client := m.lyricsClient
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		res, err := client.Fetch(ctx, artist, title, album, dur)
		return lyricsResultMsg{trackID: trackID, result: res, err: err}
	}
}

// fetchFeedCmd fetches personalised recommendations from the provider asynchronously.
func (m *Model) fetchFeedCmd() tea.Cmd {
	prov := m.provider
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		groups, err := prov.GetRecommendations(ctx)
		return feedResultMsg{groups: groups, err: err}
	}
}

// fetchFeedItemTracksCmd loads the tracks for a recommendation item then either
// plays them (play=true) or appends them to the queue.
func (m *Model) fetchFeedItemTracksCmd(item *provider.RecommendationItem, play bool) tea.Cmd {
	snap := *item
	prov := m.provider
	m.appendLog(fmt.Sprintf("[feed] loading %q (%s)…", snap.Title, snap.Kind))
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		var tracks []provider.Track
		var err error
		switch snap.Kind {
		case "album":
			tracks, err = prov.GetAlbumTracks(ctx, snap.ID)
		case "playlist":
			tracks, err = prov.GetCatalogPlaylistTracks(ctx, snap.ID)
		default:
			err = fmt.Errorf("unknown kind %q", snap.Kind)
		}
		return feedTracksMsg{item: snap, tracks: tracks, play: play, err: err}
	}
}

// startDiscovery activates discovery mode with the configured similarity metric.
// autoMode=false is a one-shot: n songs are queued immediately, then discovery stops.
func (m *Model) startDiscovery(autoMode bool, n int) tea.Cmd {
	if m.playerState.Track == nil {
		m.errMsg = "nothing is playing"
		m.errExpiry = time.Now().Add(3 * time.Second)
		return nil
	}
	sim := m.discovery.similarity
	if sim == 0 {
		sim = 0.7 // default if no metric was selected yet
	}
	m.discovery.enabled = true
	m.discovery.autoMode = autoMode
	m.discovery.refillCap = n
	m.discovery.seed = m.playerState.Track
	m.discovery.similarity = sim
	m.discovery.refilling = true // guard against double-trigger while search is in flight
	m.discovery.triggeredForID = ""
	m.syncDiscoveryView()
	mode := "auto"
	if !autoMode {
		mode = fmt.Sprintf("%d songs", n)
	}
	m.appendLog(fmt.Sprintf("[discovery] started from %q (similarity %.0f%%, mode=%s)",
		m.playerState.Track.Title, sim*100, mode))
	return m.runDiscoverySearch()
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

	// When vibe panel has text-input focus or picker is open, route all keys to it.
	if m.vibe.IsFocused() || m.vibe.PickerActive() {
		return m.vibe.Update(msg)
	}

	// Panel nav keys toggle their panel (always checked first).
	for i, p := range m.panels {
		if k == p.NavKey() {
			if m.activePanel == i {
				m.activePanel = -1 // toggle off
			} else {
				m.activePanel = i
				// Trigger a feed load when opening the feed panel for the first time.
				if p == m.feedP && m.feedP.m.NeedsLoad() {
					m.feedP.m.SetLoading()
					m.lastKey = ""
					return m.fetchFeedCmd()
				}
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
		} else if m.playerState.Track != nil && !m.vibe.PickerActive() {
			// Open the metric picker; pre-select the closest option to the
			// current similarity (defaults to 0.7 on first use).
			sim := m.discovery.similarity
			if sim == 0 {
				sim = 0.7
			}
			m.vibe.ShowPicker(sim)
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
		// Feed panel special keys.
		if m.panels[m.activePanel] == m.feedP {
			switch k {
			case "r":
				m.feedP.m.SetLoading()
				return m.fetchFeedCmd()
			case "enter":
				if item := m.feedP.m.SelectedItem(); item != nil {
					return m.fetchFeedItemTracksCmd(item, true)
				}
			case "tab":
				if item := m.feedP.m.SelectedItem(); item != nil {
					return m.fetchFeedItemTracksCmd(item, false)
				}
			default:
				return m.feedP.m.Update(msg)
			}
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
		if len(m.queueTracks) > 0 {
			m.queueMiniOffset++
		}

	case "k", "up":
		m.lastKey = ""
		if m.queueMiniOffset > 0 {
			m.queueMiniOffset--
		}

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
	m.searchGen++
	gen := m.searchGen
	m.search.SetState(nil, true, nil)
	return func() tea.Msg {
		time.Sleep(400 * time.Millisecond)
		return searchDebounceMsg{query: query, gen: gen}
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
		info.AutoMode = m.discovery.autoMode
		info.Count = m.discovery.refillCap
	}
	m.vibe.SetDiscovery(info)
}

// purgeSkippedFromQueue removes every queued track whose PlaybackID appears in
// the discovery skip blacklist. It also tells the JS player to remove those
// slots so both sides stay in sync. Iterates from the end so index removal
// doesn't shift the positions of entries not yet processed.
func (m *Model) purgeSkippedFromQueue() {
	if len(m.discovery.skipped) == 0 {
		return
	}
	changed := false
	for i := len(m.queueIDs) - 1; i >= 0; i-- {
		if !m.discovery.skipped[m.queueIDs[i]] {
			continue
		}
		m.appendLog(fmt.Sprintf("[skip] removing from queue: %s — %s",
			m.queueTracks[i].Artist, m.queueTracks[i].Title))
		if m.player != nil {
			idx := i // capture for closure
			_ = m.player.RemoveFromQueue(idx)
		}
		m.queueTracks = slices.Delete(m.queueTracks, i, i+1)
		m.queueIDs = slices.Delete(m.queueIDs, i, i+1)
		changed = true
	}
	if changed {
		m.queue.SetTracks(m.queueTracks)
	}
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
	refillCap := m.discovery.refillCap
	if refillCap <= 0 {
		refillCap = 1
	}
	queries := discoveryQueries(seed, similarity)
	m.appendLog(fmt.Sprintf("[discovery] queries (sim=%.0f%%): %v", similarity*100, queries))

	// Snapshot both the skip blacklist and the already-queued set so the
	// goroutine can filter without racing on the model's maps/slices.
	exclude := make(map[string]bool, len(m.discovery.skipped)+len(m.queueIDs))
	for k := range m.discovery.skipped {
		exclude[k] = true
	}
	for _, id := range m.queueIDs {
		exclude[id] = true
	}
	// Also exclude by normalised artist||title to catch the same song under
	// a different ID (e.g. library vs catalog copy).
	for _, t := range m.queueTracks {
		exclude[strings.ToLower(t.Artist+"||"+t.Title)] = true
	}

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
				id := views.PlaybackID(t)
				key := strings.ToLower(t.Artist + "||" + t.Title)
				if exclude[id] || exclude[key] {
					continue // already queued, skipped, or blacklisted
				}
				if !seen[key] {
					seen[key] = true
					merged = append(merged, t)
				}
			}
		}

		rand.Shuffle(len(merged), func(i, j int) { //nolint:gosec
			merged[i], merged[j] = merged[j], merged[i]
		})
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
//
// Apple Music's catalog search is artist/title indexed; bare genre strings
// ("indie", "r&b playlist") rarely match songs. Instead we always build
// queries from artist names — either the seed artist or curated artists that
// are representative of the seed's genre or adjacent genres.
func discoveryQueries(seed *provider.Track, similarity float64) []string {
	artist := seed.Artist
	album := seed.Album

	// Resolve the primary genre from the track metadata.  Apple Music often
	// appends the catch-all "Music" genre; skip it when a more specific tag
	// is available.
	genre := ""
	for _, g := range seed.Genres {
		if g != "Music" && g != "" {
			genre = g
			break
		}
	}

	// genrePool returns a randomised slice of artist names that are
	// representative of the given Apple Music genre string (case-insensitive
	// prefix match).  Falls back to a broad alternative/indie pool.
	pool := discoveryArtistPool(genre)

	// pick selects n distinct random elements from pool, avoiding `exclude`.
	pick := func(n int, exclude ...string) []string {
		excl := make(map[string]bool, len(exclude))
		for _, e := range exclude {
			excl[strings.ToLower(e)] = true
		}
		// Shuffle a copy so each call produces different results.
		shuffled := make([]string, len(pool))
		copy(shuffled, pool)
		rand.Shuffle(len(shuffled), func(i, j int) { //nolint:gosec
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		var out []string
		for _, a := range shuffled {
			if !excl[strings.ToLower(a)] {
				out = append(out, a)
				if len(out) == n {
					break
				}
			}
		}
		return out
	}

	switch {
	case similarity >= 0.85:
		// Same-artist focus: search the artist directly, plus a specific album
		// when available for breadth across their catalogue.
		qs := []string{artist}
		if album != "" {
			qs = append(qs, artist+" "+album)
		}
		// Add one more artist from the same genre pool for slight variety.
		qs = append(qs, pick(1, artist)...)
		return qs

	case similarity >= 0.65:
		// Seed artist + a couple of related artists from the same genre.
		qs := []string{artist}
		qs = append(qs, pick(2, artist)...)
		return qs

	case similarity >= 0.45:
		// Genre-focused: seed artist as anchor + 2–3 artists from same pool.
		qs := []string{artist}
		qs = append(qs, pick(3, artist)...)
		return qs

	case similarity >= 0.20:
		// Exploration: seed genre pool + one artist from an adjacent genre.
		adj := discoveryArtistPool(discoveryAdjacentGenre(genre))
		qs := pick(2, artist)
		// Pick one from adjacent pool.
		rand.Shuffle(len(adj), func(i, j int) { adj[i], adj[j] = adj[j], adj[i] }) //nolint:gosec
		if len(adj) > 0 {
			qs = append(qs, adj[0])
		}
		return qs

	default:
		// Pure discovery: three artists from two completely random genre pools.
		genres := []string{
			"Electronic", "Jazz", "Hip-Hop/Rap", "R&B/Soul",
			"Folk", "Classical", "Country", "Reggae",
		}
		rand.Shuffle(len(genres), func(i, j int) { genres[i], genres[j] = genres[j], genres[i] }) //nolint:gosec
		p1 := discoveryArtistPool(genres[0])
		p2 := discoveryArtistPool(genres[1])
		rand.Shuffle(len(p1), func(i, j int) { p1[i], p1[j] = p1[j], p1[i] }) //nolint:gosec
		rand.Shuffle(len(p2), func(i, j int) { p2[i], p2[j] = p2[j], p2[i] }) //nolint:gosec
		var qs []string
		if len(p1) > 0 {
			qs = append(qs, p1[0])
		}
		if len(p2) > 0 {
			qs = append(qs, p2[0])
		}
		if len(p1) > 1 {
			qs = append(qs, p1[1])
		}
		return qs
	}
}

// discoveryAdjacentGenre returns a genre that is stylistically adjacent to g.
func discoveryAdjacentGenre(g string) string {
	adjacency := map[string]string{
		"alternative": "Electronic",
		"indie":       "Folk",
		"electronic":  "Alternative",
		"pop":         "R&B/Soul",
		"hip-hop":     "R&B/Soul",
		"r&b":         "Hip-Hop/Rap",
		"folk":        "Alternative",
		"jazz":        "Soul",
		"soul":        "Jazz",
		"rock":        "Alternative",
		"metal":       "Rock",
		"classical":   "Jazz",
		"country":     "Folk",
	}
	low := strings.ToLower(g)
	for k, v := range adjacency {
		if strings.Contains(low, k) {
			return v
		}
	}
	return "Electronic" // safe fallback
}

// discoveryArtistPool returns a curated list of artist names that are
// representative of g (matched by case-insensitive substring).
// Apple Music search resolves artist names to tracks reliably.
func discoveryArtistPool(g string) []string {
	low := strings.ToLower(g)

	type genreEntry struct {
		key     string
		artists []string
	}
	entries := []genreEntry{
		{"alternative", []string{
			"Radiohead", "The National", "Pixies", "Sonic Youth", "Pavement",
			"Dinosaur Jr", "Built to Spill", "Guided by Voices", "Yo La Tengo",
			"My Bloody Valentine", "Slowdive", "Ride", "Mazzy Star", "Neutral Milk Hotel",
		}},
		{"indie", []string{
			"Bon Iver", "Fleet Foxes", "Arcade Fire", "Modest Mouse", "Death Cab for Cutie",
			"Sufjan Stevens", "Bright Eyes", "Phosphorescent", "Big Thief", "Phoebe Bridgers",
			"Waxahatchee", "Angel Olsen", "Sharon Van Etten", "Hand Habits", "Hazel English",
		}},
		{"electronic", []string{
			"Four Tet", "Burial", "Aphex Twin", "Caribou", "James Blake",
			"Boards of Canada", "Massive Attack", "Portishead", "Thom Yorke",
			"Floating Points", "Jon Hopkins", "Nils Frahm", "Nicolas Jaar", "Arca",
		}},
		{"pop", []string{
			"Lorde", "Lana Del Rey", "Grimes", "FKA twigs", "Charli XCX",
			"Caroline Polachek", "Carly Rae Jepsen", "Perfume Genius", "Weyes Blood", "Aldous Harding",
		}},
		{"hip-hop", []string{
			"Kendrick Lamar", "Frank Ocean", "Tyler the Creator", "Danny Brown", "Vince Staples",
			"JPEGMAFIA", "Denzel Curry", "Little Simz", "Injury Reserve", "billy woods",
		}},
		{"r&b", []string{
			"Frank Ocean", "SZA", "Blood Orange", "Solange", "Kelela",
			"Syd", "Moses Sumney", "Sampha", "NAO", "Tirzah",
		}},
		{"folk", []string{
			"Iron & Wine", "Gillian Welch", "Jason Isbell", "Phosphorescent", "Gregory Alan Isakov",
			"Josh Ritter", "Anaïs Mitchell", "The Tallest Man on Earth", "Nick Drake", "John Martyn",
		}},
		{"jazz", []string{
			"Brad Mehldau", "Nubya Garcia", "Kamasi Washington", "Snarky Puppy", "Thundercat",
			"Makaya McCraven", "Sons of Kemet", "Shabaka Hutchings", "Charles Mingus", "Bill Evans",
		}},
		{"soul", []string{
			"Leon Bridges", "Nathaniel Rateliff", "Anderson Paak", "Charles Bradley",
			"Sharon Jones", "Michael Kiwanuka", "Lianne La Havas", "Durand Jones",
		}},
		{"rock", []string{
			"Wilco", "The War on Drugs", "Kurt Vile", "Spoon", "Parquet Courts",
			"Drive-By Truckers", "Steve Gunn", "Jeff Tweedy", "Ty Segall", "Thee Oh Sees",
		}},
		{"metal", []string{
			"Mastodon", "Baroness", "Converge", "Neurosis", "Pallbearer",
			"Inter Arma", "Bell Witch", "Deafheaven", "Wolves in the Throne Room",
		}},
		{"classical", []string{
			"Nils Frahm", "Max Richter", "Johann Johannsson", "Ólafur Arnalds",
			"Lubomyr Melnyk", "Dustin O'Halloran", "Ryuichi Sakamoto",
		}},
		{"country", []string{
			"Sturgill Simpson", "Chris Stapleton", "Jason Isbell", "Colter Wall",
			"Tyler Childers", "Charley Crockett", "Nikki Lane", "Margo Price",
		}},
		{"reggae", []string{
			"Bob Marley", "Toots and the Maytals", "Peter Tosh", "Lee Scratch Perry",
			"Burning Spear", "Steel Pulse", "The Congos",
		}},
	}

	for _, e := range entries {
		if strings.Contains(low, e.key) {
			return e.artists
		}
	}

	// Broad fallback used when the genre is unknown.
	return []string{
		"Radiohead", "Bon Iver", "The National", "Fleet Foxes", "Arcade Fire",
		"Sufjan Stevens", "Bright Eyes", "James Blake", "Four Tet", "Caribou",
		"Big Thief", "Phoebe Bridgers", "Angel Olsen", "Weyes Blood", "Wilco",
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
	lyricsActive := m.activePanel >= 0 && m.panels[m.activePanel] == m.lyricsP
	feedActive := m.activePanel >= 0 && m.panels[m.activePanel] == m.feedP
	fullWidth := libraryActive || queueActive || lyricsActive || feedActive || m.mode == modeSearch || m.mode == modeCommand || m.debugView

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
	case lyricsActive:
		m.lyricsP.SetSize(inner-2, panelH)
		for _, line := range toLines(m.lyricsP.View(), panelH) {
			sb.WriteString("│ " + padRight(line, inner-2) + " │\n")
		}
	case feedActive:
		m.feedP.SetSize(inner-2, panelH)
		for _, line := range toLines(m.feedP.View(), panelH) {
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
	var volStr string
	if m.preMuteVol >= 0 {
		volStr = styles.Playing.Render("🔇 muted")
	} else {
		volStr = styles.QueueItemMuted.Render(fmt.Sprintf("♪ %d%%", vol))
	}

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
	total := len(m.queue.m.Tracks())

	// Header: "Queue  12 tracks"
	var headerLabel string
	if total > 0 {
		countStr := styles.QueueItemMuted.Render(fmt.Sprintf("  %d tracks", total))
		headerLabel = styles.Header.Render("Queue") + countStr
	} else {
		headerLabel = styles.Header.Render("Queue")
	}
	sep := styles.QueueItemMuted.Render(strings.Repeat("─", 5))

	currentTitle := ""
	if m.playerState.Track != nil {
		currentTitle = m.playerState.Track.Title
	}

	indexW := len(fmt.Sprintf("%d", total)) // digit width for index numbers
	tracks := m.queue.m.Tracks()
	var trackLines []string
	for i, t := range tracks {
		idx := fmt.Sprintf("%*d. ", indexW, i+1)
		label := t.Artist + " — " + t.Title
		if t.Title == currentTitle {
			numStr := styles.ControlActive.Render(idx)
			line := truncateStr(label, w-2-indexW-2)
			trackLines = append(trackLines, numStr+styles.ControlActive.Render("▶ "+line))
		} else {
			numStr := styles.QueueItemMuted.Render(idx)
			line := truncateStr(label, w-2-indexW-2)
			trackLines = append(trackLines, numStr+styles.QueueItem.Render(line))
		}
	}
	if len(trackLines) == 0 {
		trackLines = []string{styles.QueueItemMuted.Render("  Queue is empty")}
	}

	// header + sep occupy 2 lines; remaining rows hold track entries.
	visibleRows := max(0, h-2)
	// Clamp offset so we never scroll past the end.
	maxOffset := max(0, len(trackLines)-visibleRows)
	if m.queueMiniOffset > maxOffset {
		m.queueMiniOffset = maxOffset
	}
	visible := trackLines[m.queueMiniOffset:]
	if len(visible) > visibleRows {
		visible = visible[:visibleRows]
	}

	result := append([]string{headerLabel, sep}, visible...)
	for len(result) < h {
		result = append(result, "")
	}
	return result
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
		case m.activePanel >= 0 && m.panels[m.activePanel] == m.lyricsP:
			parts = []string{
				styles.ModeNormal.Render("LYRICS"),
				accent.Render("j/k") + muted.Render(" scroll"),
				accent.Render("g/G") + muted.Render(" top/bottom"),
				accent.Render("esc") + muted.Render(" close"),
			}
		case m.activePanel >= 0 && m.panels[m.activePanel] == m.feedP:
			parts = []string{
				styles.ModeNormal.Render("FEED"),
				accent.Render("Enter") + muted.Render(" play"),
				accent.Render("Tab") + muted.Render(" add to queue"),
				accent.Render("j/k") + muted.Render(" navigate"),
				accent.Render("r") + muted.Render(" refresh"),
				accent.Render("esc") + muted.Render(" close"),
			}
		default:
			parts = []string{
				styles.ModeNormal.Render("NORMAL"),
				accent.Render(":") + muted.Render(" command"),
				accent.Render("/") + muted.Render(" search"),
				accent.Render("l") + muted.Render(" library"),
				accent.Render("q") + muted.Render(" queue"),
				accent.Render("y") + muted.Render(" lyrics"),
				accent.Render("F") + muted.Render(" feed"),
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

	discoverHint := accent.Render("d") + muted.Render(" metric")
	if m.discovery.enabled {
		discoverHint = accent.Render("d") + styles.Playing.Render(" ● discovery")
	} else if m.vibe.PickerActive() {
		discoverHint = accent.Render("d") + styles.Playing.Render(" picking…")
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
