package views

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// PlayTracksMsg is emitted when the user selects a track to play.
// Track carries the metadata for an immediate (optimistic) UI update so the
// Now Playing view feels instant — audio startup latency is hidden from the user.
// When PlaylistID is set, the player uses SetPlaylist instead of SetQueue so
// MusicKit resolves the playlist natively without per-song catalog ID lookups.
type PlayTracksMsg struct {
	IDs        []string
	Tracks     []provider.Track // all tracks in the queue, parallel to IDs
	Track      *provider.Track  // first track, for instant UI update (may be nil)
	PlaylistID string           // non-empty → use SetPlaylist
	StartIdx   int              // start position within the playlist
}

// QueueTracksMsg is emitted when the user appends library tracks without
// interrupting playback.
type QueueTracksMsg struct {
	IDs      []string
	Tracks   []provider.Track
	Label    string
	PlayNext bool
}

// PlaybackID returns the best ID to use for MusicKit queue descriptors.
// Library tracks (IDs prefixed with "i.") must use their library ID directly
// so MusicKit never encounters a CONTENT_RESTRICTED error — the catalog copy
// of a track may be region-locked even when the user already owns it in their
// library. Catalog IDs are used for tracks not present in the user's library.
func PlaybackID(t provider.Track) string {
	// Library tracks must use their library ID directly — never CONTENT_RESTRICTED.
	if strings.HasPrefix(t.ID, "i.") {
		return t.ID
	}
	if t.CatalogID != "" {
		return t.CatalogID
	}
	return t.ID
}

// searchResultMsg is used internally by scheduleSearch (views package only).
type searchResultMsg struct {
	result *provider.SearchResult
	err    error
}

// searchRow is one entry in the unified search result list.
// Section headers are non-selectable; item rows (track/album/artist/playlist)
// are selectable.
type searchRow struct {
	header   bool
	label    string // header text (only when header=true)
	track    *provider.Track
	album    *provider.Album
	artist   *provider.Artist
	playlist *provider.Playlist
}

// isItem reports whether this row is a selectable item (not a header).
func (r searchRow) isItem() bool {
	return r.track != nil || r.album != nil || r.artist != nil || r.playlist != nil
}

// rowLines returns the number of visual lines a row occupies.
func rowLines(r searchRow) int {
	if r.header {
		return 1
	}
	return 2
}

// artistView is an artist opened from the results: their albums replace the
// result list until Esc puts the results back where they were.
type artistView struct {
	artist  provider.Artist
	albums  []provider.Album
	loading bool
	err     error
	// cursor and scroll of the result list, restored on close.
	cursor, scroll int
}

// SearchModel holds search results rendered as a unified multi-section list
// (Tracks, Albums, Playlists) with keyboard navigation.
type SearchModel struct {
	provider provider.Provider
	results  *provider.SearchResult
	rows     []searchRow
	cursor   int // row index of the currently highlighted item
	scroll   int // index of the first rendered row
	width    int
	height   int
	loading  bool
	err      error
	artist   *artistView // non-nil while an artist's albums are shown
}

func NewSearch(prov provider.Provider) *SearchModel {
	return &SearchModel{provider: prov}
}

func (m *SearchModel) Init() tea.Cmd { return nil }

func (m *SearchModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetResults updates the model with a full search result (tracks + albums + playlists).
func (m *SearchModel) SetResults(result *provider.SearchResult, loading bool, err error) {
	m.artist = nil // a new query leaves the artist behind
	m.loading = loading
	m.err = err
	m.results = result
	m.rebuildRows()
	m.cursor = m.advance(-1, 1)
	m.scroll = 0
}

// SetState is kept for backward compatibility with callers that only have tracks.
func (m *SearchModel) SetState(tracks []provider.Track, loading bool, err error) {
	var result *provider.SearchResult
	if tracks != nil {
		result = &provider.SearchResult{Tracks: tracks}
	}
	m.SetResults(result, loading, err)
}

func (m *SearchModel) rebuildRows() {
	m.rows = nil
	if m.artist != nil {
		m.rows = append(m.rows, searchRow{header: true, label: m.artist.artist.Name})
		for i := range m.artist.albums {
			m.rows = append(m.rows, searchRow{album: &m.artist.albums[i]})
		}
		return
	}
	if m.results == nil {
		return
	}
	if len(m.results.Tracks) > 0 {
		m.rows = append(m.rows, searchRow{header: true, label: "Tracks"})
		for i := range m.results.Tracks {
			t := &m.results.Tracks[i]
			m.rows = append(m.rows, searchRow{track: t})
		}
	}
	if len(m.results.Albums) > 0 {
		m.rows = append(m.rows, searchRow{header: true, label: "Albums"})
		for i := range m.results.Albums {
			a := &m.results.Albums[i]
			m.rows = append(m.rows, searchRow{album: a})
		}
	}
	if len(m.results.Artists) > 0 {
		m.rows = append(m.rows, searchRow{header: true, label: "Artists"})
		for i := range m.results.Artists {
			m.rows = append(m.rows, searchRow{artist: &m.results.Artists[i]})
		}
	}
	if len(m.results.Playlists) > 0 {
		m.rows = append(m.rows, searchRow{header: true, label: "Playlists"})
		for i := range m.results.Playlists {
			p := &m.results.Playlists[i]
			m.rows = append(m.rows, searchRow{playlist: p})
		}
	}
}

// advance returns the index of the next selectable row in direction dir (+1/-1)
// starting from `from`. Returns `from` if no selectable row is found (or 0
// when from==-1 and there are no items).
func (m *SearchModel) advance(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(m.rows); i += dir {
		if m.rows[i].isItem() {
			return i
		}
	}
	if from < 0 {
		return 0
	}
	return from
}

// ensureCursorVisible adjusts m.scroll so the cursor row is fully visible
// within m.height visual lines.
func (m *SearchModel) ensureCursorVisible() {
	h := max(1, m.height)
	// Cursor is above the scroll window: scroll up to it.
	if m.cursor < m.scroll {
		m.scroll = m.cursor
		// If the row directly above the cursor is a section header, pull it
		// into view too — otherwise the first item in a section appears with
		// its header clipped off.
		if m.scroll > 0 && m.rows[m.scroll-1].header {
			m.scroll--
		}
		return
	}
	// Count visual lines from the scroll start through the cursor row (inclusive).
	lines := 0
	for i := m.scroll; i <= m.cursor && i < len(m.rows); i++ {
		lines += rowLines(m.rows[i])
	}
	// Scroll forward until the cursor row fits inside the available height.
	for lines > h && m.scroll < m.cursor {
		lines -= rowLines(m.rows[m.scroll])
		m.scroll++
	}
	// After any scroll adjustment, if the section header for the topmost
	// visible item sits just above the viewport AND there is room for it,
	// pull it in.  This handles the case where cursor == scroll (cursor is
	// exactly at the top of the viewport but its section header got clipped).
	if m.scroll > 0 && m.rows[m.scroll-1].header && lines+1 <= h {
		m.scroll--
	}
}

// Results returns the current track list (backward compat).
func (m *SearchModel) Results() []provider.Track {
	if m.results == nil {
		return nil
	}
	return m.results.Tracks
}

// Loading returns whether a search is in progress.
func (m *SearchModel) Loading() bool { return m.loading }

// SelectedTrack returns the highlighted track, or nil when an album/playlist is selected.
func (m *SearchModel) SelectedTrack() *provider.Track {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].track
}

// SelectedAlbum returns the highlighted album, or nil.
func (m *SearchModel) SelectedAlbum() *provider.Album {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].album
}

// SelectedArtist returns the highlighted artist, or nil.
func (m *SearchModel) SelectedArtist() *provider.Artist {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].artist
}

// OpenArtist replaces the results with the artist's albums, shown as loading
// until SetArtistAlbums delivers them.
func (m *SearchModel) OpenArtist(a provider.Artist) {
	m.artist = &artistView{artist: a, loading: true, cursor: m.cursor, scroll: m.scroll}
	m.rebuildRows()
	m.cursor = m.advance(-1, 1)
	m.scroll = 0
}

// SetArtistAlbums delivers the albums for the open artist. A reply for an
// artist that is no longer open is dropped.
func (m *SearchModel) SetArtistAlbums(artistID string, albums []provider.Album, err error) {
	if m.artist == nil || m.artist.artist.ID != artistID {
		return
	}
	m.artist.albums = albums
	m.artist.loading = false
	m.artist.err = err
	m.rebuildRows()
	m.cursor = m.advance(-1, 1)
	m.scroll = 0
}

// InArtist reports whether an artist's albums are shown.
func (m *SearchModel) InArtist() bool { return m.artist != nil }

// CloseArtist goes back to the search results, where they were left.
func (m *SearchModel) CloseArtist() {
	if m.artist == nil {
		return
	}
	cursor, scroll := m.artist.cursor, m.artist.scroll
	m.artist = nil
	m.rebuildRows()
	m.cursor, m.scroll = cursor, scroll
}

// SelectedPlaylist returns the highlighted playlist, or nil.
func (m *SearchModel) SelectedPlaylist() *provider.Playlist {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].playlist
}

// SelectedIndex returns the 0-based item index across all sections (headers excluded).
func (m *SearchModel) SelectedIndex() int {
	count := 0
	for i := 0; i < m.cursor && i < len(m.rows); i++ {
		if m.rows[i].isItem() {
			count++
		}
	}
	return count
}

// Update handles key navigation (↑ ↓ PgUp PgDn).
func (m *SearchModel) Update(msg tea.KeyPressMsg) (*SearchModel, tea.Cmd) {
	switch msg.String() {
	case "up":
		if prev := m.advance(m.cursor, -1); prev != m.cursor {
			m.cursor = prev
			m.ensureCursorVisible()
		}
	case "down":
		if next := m.advance(m.cursor, 1); next != m.cursor {
			m.cursor = next
			m.ensureCursorVisible()
		}
	case "pgup":
		for range 5 {
			prev := m.advance(m.cursor, -1)
			if prev == m.cursor {
				break
			}
			m.cursor = prev
		}
		m.ensureCursorVisible()
	case "pgdown":
		for range 5 {
			next := m.advance(m.cursor, 1)
			if next == m.cursor {
				break
			}
			m.cursor = next
		}
		m.ensureCursorVisible()
	}
	return m, nil
}

// sectionColor returns the accent colour for a given section label.
// Each section uses a distinct warm/cool hue so the three groups are
// immediately distinguishable at a glance.
func sectionColor(label string) color.Color {
	switch label {
	case "Albums":
		return styles.ColorPrimary // violet  #C678DD
	case "Artists":
		return styles.ColorSubtle // blue  #61AFEF
	case "Playlists":
		return styles.ColorSecondary // green  #98C379
	default: // "Tracks"
		return styles.ColorAccentWarm // warm amber
	}
}

// View renders the multi-section result list within the allocated height.
func (m *SearchModel) View() string {
	if m.loading {
		return styles.QueueItemMuted.Render("  searching…")
	}
	if m.err != nil {
		return styles.ErrorStyle.Render("⚠  " + m.err.Error())
	}
	if m.artist != nil && len(m.artist.albums) == 0 {
		hs := lipgloss.NewStyle().Foreground(sectionColor("Albums")).Bold(true).Italic(true)
		var status string
		switch {
		case m.artist.loading:
			status = styles.QueueItemMuted.Render("loading albums…")
		case m.artist.err != nil:
			status = styles.ErrorStyle.Render("⚠  " + m.artist.err.Error())
		default:
			status = styles.QueueItemMuted.Render("no albums")
		}
		return "  " + hs.Render(m.artist.artist.Name) + "\n  " + status
	}
	if len(m.rows) == 0 {
		return ""
	}

	itemTitle := lipgloss.NewStyle().Foreground(styles.ColorFg)
	itemDesc := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	tagStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)

	var sb strings.Builder
	linesLeft := m.height
	start := max(0, m.scroll)

	// Seed currentAccent from the nearest header that sits above the current
	// scroll window.  Without this, items whose section header has already
	// scrolled out of view would render with the wrong colour (the default
	// "Tracks" amber) until the header scrolls back into the viewport.
	currentAccent := sectionColor("Tracks")
	if m.artist != nil {
		currentAccent = sectionColor("Albums") // the one header is the artist's name
	}
	for i := start - 1; i >= 0 && m.artist == nil; i-- {
		if m.rows[i].header {
			currentAccent = sectionColor(m.rows[i].label)
			break
		}
	}

	for i := start; i < len(m.rows) && linesLeft > 0; i++ {
		row := m.rows[i]

		if row.header {
			if m.artist == nil {
				currentAccent = sectionColor(row.label)
			}
			hs := lipgloss.NewStyle().
				Foreground(currentAccent).
				Bold(true).
				Italic(true)
			sb.WriteString("  " + hs.Render(row.label) + "\n")
			linesLeft--
			continue
		}

		// Item rows require 2 lines; skip if there is not enough room.
		if linesLeft < 2 {
			break
		}

		sel := i == m.cursor
		cur := "  "
		if sel {
			cur = lipgloss.NewStyle().Foreground(currentAccent).Render("▶ ")
		}
		tStyle := itemTitle
		dStyle := itemDesc
		if sel {
			tStyle = lipgloss.NewStyle().Foreground(currentAccent).Bold(true)
			dStyle = lipgloss.NewStyle().Foreground(currentAccent).Faint(true)
		}

		switch {
		case row.track != nil:
			t := row.track
			sb.WriteString(cur + tStyle.Render(t.Title) + "\n")
			sb.WriteString("    " + dStyle.Render(fmt.Sprintf("%s — %s", t.Artist, t.Album)) + "\n")

		case row.album != nil:
			a := row.album
			parts := []string{a.Artist}
			if m.artist != nil {
				// Every album here is the artist's; the year orders them.
				parts = nil
				if len(a.ReleaseDate) >= 4 {
					parts = append(parts, a.ReleaseDate[:4])
				}
			}
			if a.TrackCount > 0 {
				parts = append(parts, fmt.Sprintf("%d tracks", a.TrackCount))
			}
			desc := strings.Join(parts, "  ·  ")
			sb.WriteString(cur + tStyle.Render(a.Title) + tagStyle.Render(" [album]") + "\n")
			sb.WriteString("    " + dStyle.Render(desc) + "\n")

		case row.artist != nil:
			sb.WriteString(cur + tStyle.Render(row.artist.Name) + tagStyle.Render(" [artist]") + "\n")
			sb.WriteString("    " + dStyle.Render("albums, singles and EPs") + "\n")

		case row.playlist != nil:
			p := row.playlist
			desc := ""
			if p.TrackCount > 0 {
				desc = fmt.Sprintf("%d tracks", p.TrackCount)
			}
			sb.WriteString(cur + tStyle.Render(p.Name) + tagStyle.Render(" [playlist]") + "\n")
			sb.WriteString("    " + dStyle.Render(desc) + "\n")
		}
		linesLeft -= 2
	}

	return sb.String()
}

// Focus / Focused — kept for backward compatibility (input is managed by the model).
func (m *SearchModel) Focus()        {}
func (m *SearchModel) Focused() bool { return false }

// SetCursor / Cursor — kept for backward compatibility.
func (m *SearchModel) SetCursor(_ int) {}
func (m *SearchModel) Cursor() int     { return m.SelectedIndex() }

func (m *SearchModel) scheduleSearch(query string) tea.Cmd {
	if query == "" {
		return nil
	}
	prov := m.provider
	return func() tea.Msg {
		time.Sleep(300 * time.Millisecond)
		result, err := prov.Search(context.Background(), query)
		return searchResultMsg{result: result, err: err}
	}
}

func searchResultItems(r *provider.SearchResult) []provider.Track {
	return r.Tracks
}
