package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// playbackID returns the best ID to use for MusicKit queue descriptors.
// Catalog IDs (numeric, e.g. "1622205917") are preferred because MusicKit
// resolves them against the configured storefront and streams full tracks.
// Library IDs (i.XXXXX) are a fallback for songs without a catalog match
// (e.g. iCloud Music Library uploads that were never matched to the catalog).
func playbackID(t provider.Track) string {
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
	provider      provider.Provider
	input         textinput.Model
	cursor        int
	results       []provider.Track
	lastQuery     string
	debounceTimer *time.Timer
	loading       bool
	err           error
	width         int
	height        int
}

func NewSearch(prov provider.Provider) *SearchModel {
	ti := textinput.New()
	ti.Placeholder = "Search tracks, albums, playlists…"
	ti.CharLimit = 200
	ti.Prompt = "/ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(styles.ColorAccent)
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.ColorFg)

	return &SearchModel{
		provider: prov,
		input:    ti,
	}
}

func (m *SearchModel) Init() tea.Cmd {
	return nil
}

func (m *SearchModel) Focus() {
	m.input.Focus()
}

func (m *SearchModel) Focused() bool {
	return m.input.Focused()
}

func (m *SearchModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.input.Width = max(0, w-4)
}

func (m *SearchModel) Update(msg tea.Msg) (*SearchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case searchResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			if msg.result != nil {
				m.results = searchResultItems(msg.result)
				m.cursor = 0
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.input.Blur()
			return m, nil
		case "enter":
			if !m.input.Focused() && len(m.results) > 0 {
				track := m.results[m.cursor]
				t := track
				return m, func() tea.Msg {
					return PlayTracksMsg{IDs: []string{playbackID(track)}, Track: &t}
				}
			}
		case "up", "k":
			if !m.input.Focused() {
				m.cursor = max(0, m.cursor-1)
				return m, nil
			}
		case "down", "j":
			if !m.input.Focused() && len(m.results) > 0 {
				m.cursor = min(len(m.results)-1, m.cursor+1)
				return m, nil
			}
		}

		if m.input.Focused() {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			query := m.input.Value()
			if query != m.lastQuery {
				m.lastQuery = query
				m.err = nil
				m.loading = query != ""
				return m, tea.Batch(cmd, m.scheduleSearch(query))
			}
			return m, cmd
		}
	}

	return m, nil
}

func (m *SearchModel) scheduleSearch(query string) tea.Cmd {
	if m.debounceTimer != nil {
		m.debounceTimer.Stop()
	}
	if query == "" {
		return nil
	}
	prov := m.provider
	return func() tea.Msg {
		// 300ms debounce implemented by sleeping in the cmd.
		time.Sleep(300 * time.Millisecond)
		result, err := prov.Search(context.Background(), query)
		return searchResultMsg{result: result, err: err}
	}
}

func (m *SearchModel) View() string {
	var sb strings.Builder

	// Input line with underline separator — no box, just a thin rule.
	sb.WriteString("  ")
	sb.WriteString(m.input.View())
	sb.WriteString("\n")
	sb.WriteString(styles.Separator.Render(strings.Repeat("─", max(0, m.width))))
	sb.WriteString("\n\n")

	switch {
	case m.err != nil:
		sb.WriteString(styles.ErrorStyle.Render("⚠  " + m.err.Error()))
	case m.loading:
		sb.WriteString(styles.QueueItemMuted.Render("  searching…"))
	default:
		sb.WriteString(m.renderResults())
	}

	return sb.String()
}

func (m *SearchModel) renderResults() string {
	if len(m.results) == 0 {
		return ""
	}
	selected := lipgloss.NewStyle().Foreground(styles.ColorGlow5).Bold(true)
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
