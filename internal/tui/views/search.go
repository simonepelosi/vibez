package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
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

type searchResultMsg struct {
	result *provider.SearchResult
	err    error
}

// searchTrackItem implements list.Item.
type searchTrackItem struct{ t provider.Track }

func (i searchTrackItem) Title() string       { return i.t.Title }
func (i searchTrackItem) Description() string { return fmt.Sprintf("%s — %s", i.t.Artist, i.t.Album) }
func (i searchTrackItem) FilterValue() string { return i.t.Title }

// SearchModel holds search results rendered with a list.Model (same as library/queue).
type SearchModel struct {
	list     list.Model
	provider provider.Provider
	results  []provider.Track
	loading  bool
	err      error
}

func NewSearch(prov provider.Provider) *SearchModel {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(styles.ColorAccent).Bold(true)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(styles.ColorSubtle)
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.
		Foreground(styles.ColorFg)
	delegate.Styles.NormalDesc = delegate.Styles.NormalDesc.
		Foreground(styles.ColorMuted)

	l := list.New(nil, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)

	return &SearchModel{list: l, provider: prov}
}

func (m *SearchModel) Init() tea.Cmd {
	return nil
}

func (m *SearchModel) SetSize(w, h int) {
	m.list.SetSize(w, h)
}

// SetState replaces the current results/loading/error state.
func (m *SearchModel) SetState(results []provider.Track, loading bool, err error) {
	m.results = results
	m.loading = loading
	m.err = err
	items := make([]list.Item, len(results))
	for i, t := range results {
		items[i] = searchTrackItem{t}
	}
	m.list.SetItems(items)
	m.list.ResetSelected()
}

// Results returns the current track list.
func (m *SearchModel) Results() []provider.Track { return m.results }

// Loading returns whether a search is in progress.
func (m *SearchModel) Loading() bool { return m.loading }

// SelectedTrack returns the currently highlighted track, or nil if none.
func (m *SearchModel) SelectedTrack() *provider.Track {
	if item, ok := m.list.SelectedItem().(searchTrackItem); ok {
		t := item.t
		return &t
	}
	return nil
}

// SelectedIndex returns the current list cursor index.
func (m *SearchModel) SelectedIndex() int { return m.list.Index() }

// Update forwards key messages to the list (for ↑/↓ navigation).
func (m *SearchModel) Update(msg tea.KeyMsg) (*SearchModel, tea.Cmd) {
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the list (used inside the search popup).
func (m *SearchModel) View() string {
	if m.loading {
		return styles.QueueItemMuted.Render("  searching…")
	}
	if m.err != nil {
		return styles.ErrorStyle.Render("⚠  " + m.err.Error())
	}
	if len(m.results) == 0 {
		return ""
	}
	return m.list.View()
}

// Focus / Focused kept for backward compatibility.
func (m *SearchModel) Focus()        {}
func (m *SearchModel) Focused() bool { return false }

// SetCursor / Cursor kept for backward compatibility.
func (m *SearchModel) SetCursor(_ int) {}
func (m *SearchModel) Cursor() int     { return m.list.Index() }

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
