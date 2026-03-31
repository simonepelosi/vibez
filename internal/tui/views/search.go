package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	Track      *provider.Track // first track, for instant UI update (may be nil)
	PlaylistID string          // non-empty → use SetPlaylist
	StartIdx   int             // start position within the playlist
}

// PlaybackID returns the best ID to use for MusicKit queue descriptors.
// Catalog IDs (numeric, e.g. "1622205917") are preferred because MusicKit
// resolves them against the configured storefront and streams full tracks.
// Library IDs (i.XXXXX) are a fallback for songs without a catalog match
// (e.g. iCloud Music Library uploads that were never matched to the catalog).
func PlaybackID(t provider.Track) string {
	if t.CatalogID != "" {
		return t.CatalogID
	}
	return t.ID
}

type searchResultMsg struct {
	result *provider.SearchResult
	err    error
}

type SearchModel struct {
	provider provider.Provider
	cursor   int
	results  []provider.Track
	loading  bool
	err      error
	width    int
	height   int
}

func NewSearch(prov provider.Provider) *SearchModel {
	return &SearchModel{
		provider: prov,
	}
}

func (m *SearchModel) Init() tea.Cmd {
	return nil
}

func (m *SearchModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetState updates the search results state directly (called by the model).
func (m *SearchModel) SetState(results []provider.Track, loading bool, err error) {
	m.results = results
	m.loading = loading
	m.err = err
}

// Results returns the current search results.
func (m *SearchModel) Results() []provider.Track { return m.results }

// Loading returns whether a search is in progress.
func (m *SearchModel) Loading() bool { return m.loading }

// SetCursor sets the cursor position.
func (m *SearchModel) SetCursor(n int) { m.cursor = n }

// Cursor returns the current cursor position.
func (m *SearchModel) Cursor() int { return m.cursor }

// Focus is a no-op kept for backward compatibility.
func (m *SearchModel) Focus() {}

// Focused always returns false; input focus is now managed by the model.
func (m *SearchModel) Focused() bool { return false }

func (m *SearchModel) Update(msg tea.Msg) (*SearchModel, tea.Cmd) {
	if srm, ok := msg.(searchResultMsg); ok {
		m.loading = false
		if srm.err != nil {
			m.err = srm.err
		} else {
			m.err = nil
			if srm.result != nil {
				m.results = searchResultItems(srm.result)
				m.cursor = 0
			}
		}
	}
	return m, nil
}

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

func (m *SearchModel) View() string {
	var sb strings.Builder
	switch {
	case m.err != nil:
		sb.WriteString(styles.ErrorStyle.Render("⚠  " + m.err.Error()))
	case m.loading:
		sb.WriteString(styles.Spinner.Render("  searching…"))
	case len(m.results) == 0 && !m.loading:
		// empty — model handles the empty-state hint
	default:
		sb.WriteString(m.renderResults())
	}
	return sb.String()
}

func (m *SearchModel) renderResults() string {
	if len(m.results) == 0 {
		return ""
	}
	selected := styles.Playing.Bold(true)
	var sb strings.Builder
	for i, t := range m.results {
		if i == m.cursor {
			sb.WriteString(selected.Render("  ● "+t.Title) + "\n")
		} else {
			sb.WriteString(styles.QueueItem.Render("    "+t.Title) + "\n")
		}
		sb.WriteString(styles.QueueItemMuted.Render("    "+t.Artist+" — "+t.Album) + "\n\n")
	}
	return sb.String()
}

// --- searchTrackItem kept for test compatibility ---

type searchTrackItem struct{ t provider.Track }

func (i searchTrackItem) Title() string {
	return i.t.Title
}
func (i searchTrackItem) Description() string {
	return fmt.Sprintf("%s — %s", i.t.Artist, i.t.Album)
}
func (i searchTrackItem) FilterValue() string { return i.t.Title }

func searchResultItems(r *provider.SearchResult) []provider.Track {
	return r.Tracks
}
