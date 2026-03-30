package views

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/simone-vibes/vibez/internal/provider"
	"github.com/simone-vibes/vibez/internal/tui/styles"
)

// PlayTracksMsg is emitted by the search view when the user presses Enter on a
// result. The model handles it by calling player.SetQueue.
type PlayTracksMsg struct {
	IDs []string
}

type searchResultMsg struct {
	result *provider.SearchResult
	err    error
}

type SearchModel struct {
	provider      provider.Provider
	input         textinput.Model
	list          list.Model
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
	ti.PromptStyle = lipgloss.NewStyle().Foreground(styles.ColorPrimary)
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.ColorFg)

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(styles.ColorPrimary)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(styles.ColorSubtle)

	l := list.New(nil, delegate, 0, 0)
	l.SetShowTitle(false)
	l.SetFilteringEnabled(false)

	return &SearchModel{
		provider: prov,
		input:    ti,
		list:     l,
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
	m.input.Width = w - 4
	m.list.SetSize(w, h-3)
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
				m.list.SetItems(searchResultItems(msg.result))
			}
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.input.Blur()
			return m, nil
		case "enter":
			if !m.input.Focused() {
				// User pressed Enter on a search result — play it.
				if item, ok := m.list.SelectedItem().(searchTrackItem); ok {
					return m, func() tea.Msg {
						return PlayTracksMsg{IDs: []string{item.t.ID}}
					}
				}
				var cmd tea.Cmd
				m.list, cmd = m.list.Update(msg)
				return m, cmd
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

		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
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
	inputBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorPrimary).
		Padding(0, 1).
		Width(m.width - 2).
		Render(m.input.View())

	var body string
	switch {
	case m.err != nil:
		body = styles.ErrorStyle.Render("⚠  " + m.err.Error())
	case m.loading:
		body = styles.QueueItemMuted.Render("  searching…")
	default:
		body = m.list.View()
	}

	hint := styles.KeyHint.Render("esc · arrows · enter to play")
	return inputBox + "\n" + hint + "\n" + body
}

// --- list.Item adapters for search results ---

type searchTrackItem struct{ t provider.Track }

func (i searchTrackItem) Title() string {
	return i.t.Title
}
func (i searchTrackItem) Description() string {
	return fmt.Sprintf("%s — %s", i.t.Artist, i.t.Album)
}
func (i searchTrackItem) FilterValue() string { return i.t.Title }

func searchResultItems(r *provider.SearchResult) []list.Item {
	var items []list.Item
	for _, t := range r.Tracks {
		items = append(items, searchTrackItem{t})
	}
	return items
}
